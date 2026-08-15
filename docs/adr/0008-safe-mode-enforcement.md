# ADR 0008: Enforce safe mode in the scheduler, not in HTTP handlers

- Status: Accepted
- Date: 2026-08-15

## Context

`--safe-mode` is documented as disabling AI, MCP, and Rojo workers. Before
this change, that promise was kept by two `if s.safeMode` checks living in
`internal/api/api.go` — one in `createRun` (`POST /api/v1/runs`), one in
`runAction` for the `resume`/`restart` actions — plus
`schedulerManager.SetLimits(1, 1, 1, 1)` in `internal/app/app.go`, which reads
as a kill switch but is actually a concurrency ceiling: it does not stop a
run from being submitted, it only stops more than one from running at once,
and a `SetLimits` call made later (from a settings change, or from a future
code path that reads `concurrency` again) silently overwrites it with no
error and no safe-mode awareness at all.

Auditing every path that can start a worker found three it did not cover:

1. **`resolveDecision`** (`POST /api/v1/decisions/{id}/resolve`) approving a
   pending correction submits the stored `scheduler.Job` straight to
   `scheduler.Manager.Submit` with no safe-mode check anywhere in the call
   path — approving a decision started a worker in safe mode.
2. **`startSync`** (`POST /api/v1/projects/{id}/sync`) started `rojo serve`
   unconditionally. The flag's own documented meaning ("disable AI, MCP, and
   Rojo workers") explicitly promises Rojo is covered; it was not.
3. **`openStudio`** (`POST /api/v1/projects/{id}/open-studio`) launched the
   Roblox Studio process and the Studio MCP provisioner unconditionally.

None of this was a case of an endpoint author forgetting a check they knew
about — it is what happens when "enforce policy X" is stated as "check X at
every call site" instead of as "call site N reaches the one function that
enforces X." Every new endpoint that can start a worker starts from zero
safe-mode awareness by construction, and there is no test that fails when one
is missed, because there is no single thing to test against.

## Decision

Move enforcement to `scheduler.Manager.Submit`
(`internal/scheduler/scheduler.go`) — the one function that creates a run row
and admits it to the run queue. `Submit` now checks `m.SafeMode()` as its
first line and returns the sentinel `scheduler.ErrSafeMode` before doing
anything else. `Manager.SetSafeMode(bool)`/`SafeMode() bool` guard a field
under the same mutex the manager already uses for its other state.

This closes all three gaps above by construction rather than by finding and
patching each one: `createRun`, `runAction`'s resume/restart, and
`resolveDecision`'s approve all end at `Submit`, so all three are refused the
same way without three separate reviewed changes, and any future endpoint
that reaches `Submit` inherits the same refusal automatically. Auditing for
other direct callers of the lower-level `createRun`/`admit` pair (the two
functions `Submit` itself composes) found exactly one:
`scheduler.scheduleCorrection`, the automatic-correction path a failed
playtest validation schedules. It bypasses `Submit` and calls `createRun`/
`admit` directly, so it now carries its own `SafeMode()` check too — not
because it is reachable today (safe mode is decided once at daemon startup,
so `Submit` already refuses the very first run that would ever reach
validation and schedule a correction), but because "the one enforcement
point" is only true if every creator of a run honors it, and a second,
unguarded path defeats the point of having one.

The existing HTTP-handler checks in `createRun` and `runAction` are kept, not
removed: they are a UX optimization (a 409 before any of the request's other
work — task-readiness checks, provider diagnostics, memory search,
checkpointing — runs), not the enforcement itself. `internal/api/api.go` maps
`errors.Is(err, scheduler.ErrSafeMode)` to the same 409 `safe_mode` response
in `createRun`, `runAction`, and `resolveDecision`, so an operator sees the
identical error whether the fast handler check caught it or `Submit` did.

Rojo is not started through the scheduler at all, so `Submit` cannot cover
it. `internal/rojo.Manager` gets its own `SetSafeMode`/`SafeMode`, checked
first in `Start`, `Build`, and `InstallPlugin` — the three functions that
`exec` anything — with its own sentinel `rojo.ErrSafeMode`. `internal/app`
sets both flags from the one `opts.SafeMode` at startup. `startSync` and
`openStudio` additionally get a direct `if s.safeMode` check at the top of
the handler, since — unlike a run — there is no lower-level Rojo/Studio
choke point that every current and future caller of these two specific
actions is guaranteed to share.

The Studio MCP provisioner closures in `internal/app/app.go` (Claude's
`Provision`, and OpenRouter's/NVIDIA's `ProvisionLive`) also short-circuit on
`opts.SafeMode`, returning an empty grant with a `Notice` instead of calling
into the provisioner. This is unreachable in production today, for the same
reason `scheduleCorrection`'s check is: nothing reaches these closures unless
a run already exists, and `Submit` already refused it. It is kept anyway as
the concrete shape safe mode's "no process starts" promise takes at this
layer — the alternative was leaving the provisioner itself trusted to only
ever be called for a safe-mode-cleared run, which is exactly the kind of
implicit invariant this whole change exists to stop relying on.

## Alternatives considered

1. **Keep the checks in HTTP handlers, and just add the three missing
   ones.** Rejected as treating the symptom, not the defect: it fixes the
   three gaps found in this audit and leaves the next one to be found the
   same way, by a security review reading the code by hand rather than by a
   test failing.
2. **Do not run the scheduler loop at all while safe mode is on**, instead of
   refusing at `Submit`. Rejected: `Manager.Pause`/`Manager.Cancel` operate on
   runs already in `m.active`, and `Manager.Cancel` also has a path for a run
   that is `queued`, `waiting_decision`, or `paused` with no live goroutine.
   A run left over from before the daemon was restarted with `--safe-mode`
   must still be pausable and cancellable — safe mode blocks new work, it is
   not a suspend of the daemon's own run-management machinery — so the loop,
   and the ability to act on runs already known to it, has to keep working.

## Consequences

- `scheduler.Manager.SetLimits(1, 1, 1, 1)` is no longer called for safe
  mode. Nothing else in the codebase called it with those specific values, so
  removing it changes no other behavior; the manager's real defaults
  (`globalLimit: 6, perProjectLimit: 2, perProviderLimit: 4, perModelLimit:
  3`) are unaffected by safe mode either way now, since `Submit` refuses
  before any of them are ever consulted.
- A future endpoint that starts a worker by calling `scheduler.Manager.Submit`
  is safe-mode-correct with no extra code. One that starts a worker any other
  way (a second `rojo.Manager`-style external process, a third MCP
  provisioner) is not covered automatically and needs the same two things
  Rojo got here: its own `SetSafeMode`, and a decision about whether a fast
  handler-level check is also worth adding for UX.

# ADR 0008: Централизовать safe mode в планировщике, а не в HTTP-хендлерах (Русский)

- Статус: Принято
- Дата: 2026-08-15

## Контекст

`--safe-mode` документирован как флаг, отключающий AI-, MCP- и
Rojo-воркеры. До этого изменения это обещание держалось на двух проверках
`if s.safeMode` в `internal/api/api.go` — одна в `createRun` (`POST
/api/v1/runs`), другая в `runAction` для действий `resume`/`restart` — плюс
на вызове `schedulerManager.SetLimits(1, 1, 1, 1)` в
`internal/app/app.go`, который читается как выключатель, но на самом деле
является потолком параллелизма: он не мешает запуску отправиться на
исполнение, а лишь ограничивает число одновременно работающих запусков —
и более поздний вызов `SetLimits` (из смены настроек, или из будущего кода,
заново читающего `concurrency`) молча перезаписывает его без ошибки и без
всякого учёта safe mode.

Аудит всех путей, способных запустить воркер, нашёл три, которые не были
закрыты:

1. **`resolveDecision`** (`POST /api/v1/decisions/{id}/resolve`) при
   одобрении отложенного correction-решения отправляет сохранённый
   `scheduler.Job` напрямую в `scheduler.Manager.Submit` без единой проверки
   safe mode на всём пути — одобрение решения запускало воркер в safe mode.
2. **`startSync`** (`POST /api/v1/projects/{id}/sync`) запускал `rojo serve`
   безусловно. Собственное описание флага ("disable AI, MCP, and Rojo
   workers") прямо обещает, что Rojo тоже отключается; на деле это было не
   так.
3. **`openStudio`** (`POST /api/v1/projects/{id}/open-studio`) безусловно
   запускал процесс Roblox Studio и Studio MCP provisioner.

Ни один из этих случаев не был следствием того, что автор эндпоинта забыл
про уже известную проверку — это ровно то, что происходит, когда "соблюдай
политику X" сформулировано как "проверяй X в каждой точке входа", а не как
"точка входа N обязана дойти до единственной функции, которая X
обеспечивает". Каждый новый эндпоинт, способный запустить воркер, по
умолчанию ничего не знает про safe mode, и нет теста, который упал бы при
пропущенной проверке — потому что нет единой вещи, против которой его можно
было бы написать.

## Решение

Перенести enforcement в `scheduler.Manager.Submit`
(`internal/scheduler/scheduler.go`) — единственную функцию, которая создаёт
строку запуска и допускает его в очередь. `Submit` теперь первой же строкой
проверяет `m.SafeMode()` и возвращает sentinel `scheduler.ErrSafeMode` раньше
любой другой работы. `Manager.SetSafeMode(bool)`/`SafeMode() bool` защищают
поле тем же мьютексом, которым менеджер уже защищает остальное состояние.

Это закрывает все три найденные дыры не тремя отдельными точечными
патчами, а одним изменением по построению: `createRun`, ветка
resume/restart в `runAction` и одобрение в `resolveDecision` — все трое
заканчиваются в `Submit`, поэтому все трое отказывают одинаково без трёх
отдельных проверенных изменений, а любой будущий эндпоинт, доходящий до
`Submit`, наследует тот же отказ автоматически. Проверка прочих прямых
вызовов пары `createRun`/`admit` более низкого уровня (тех самых двух
функций, из которых состоит сам `Submit`) нашла ровно одного такого
вызывающего: `scheduler.scheduleCorrection` — путь автоматического
correction-запуска, который планирует провалившаяся playtest-валидация. Он
обходит `Submit` и вызывает `createRun`/`admit` напрямую, поэтому теперь тоже
несёт собственную проверку `SafeMode()` — не потому, что это достижимо
сегодня (safe mode решается один раз при старте демона, поэтому `Submit`
уже отказывает самому первому запуску, который вообще мог бы дойти до
валидации и запланировать correction), а потому что утверждение "единственная
точка enforcement" верно только тогда, когда его соблюдает каждый создатель
запуска, а второй, никем не охраняемый путь сводит на нет весь смысл иметь
одну точку.

Существующие проверки в HTTP-хендлерах `createRun` и `runAction` не
удаляются: это UX-оптимизация (409 до того, как выполнится вся остальная
работа запроса — проверка готовности задачи, диагностика провайдера, поиск
по памяти, чекпоинт), а не сам enforcement. `internal/api/api.go` мапит
`errors.Is(err, scheduler.ErrSafeMode)` в тот же ответ 409 с кодом
`safe_mode` в `createRun`, `runAction` и `resolveDecision`, так что оператор
видит одинаковую ошибку независимо от того, поймал её быстрый хендлер или
`Submit`.

Rojo вообще не запускается через планировщик, так что `Submit` не может его
покрыть. `internal/rojo.Manager` получает собственные `SetSafeMode`/
`SafeMode`, проверяемые первыми в `Start`, `Build` и `InstallPlugin` — трёх
функциях, которые что-либо исполняют — и собственный sentinel
`rojo.ErrSafeMode`. `internal/app` выставляет оба флага из одного и того же
`opts.SafeMode` при старте. `startSync` и `openStudio` дополнительно получают
прямую проверку `if s.safeMode` в начале хендлера, поскольку — в отличие от
запуска — для этих двух конкретных действий нет единой точки более низкого
уровня для Rojo/Studio, которую гарантированно разделяли бы все нынешние и
будущие вызывающие.

Замыкания Studio MCP provisioner в `internal/app/app.go` (`Provision` для
Claude, `ProvisionLive` для OpenRouter/NVIDIA) тоже коротко замыкаются на
`opts.SafeMode`, возвращая пустой grant с `Notice` вместо обращения к
provisioner. Сегодня это недостижимо в продакшене, по той же причине, что и
проверка в `scheduleCorrection`: до этих замыканий ничего не доходит, пока не
существует запуск, а `Submit` его уже отказал. Это оставлено намеренно как
конкретное воплощение обещания safe mode "ни один процесс не запускается" на
этом слое — альтернативой было бы доверять provisioner неявный инвариант
"меня вызывают только для запуска, прошедшего проверку safe mode", а именно
от таких неявных инвариантов и призвано избавить это изменение целиком.

## Рассмотренные альтернативы

1. **Оставить проверки в HTTP-хендлерах и просто добавить три
   недостающие.** Отвергнуто как лечение симптома, а не причины: это закрыло
   бы три дыры, найденные в этом аудите, и оставило бы следующую — до
   которой снова дойдут вручную, при очередном security-review, а не
   благодаря упавшему тесту.
2. **Вовсе не запускать цикл планировщика, пока включён safe mode**, вместо
   отказа в `Submit`. Отвергнуто: `Manager.Pause`/`Manager.Cancel` работают
   с запусками, уже находящимися в `m.active`, а у `Manager.Cancel` есть
   отдельная ветка для запуска в состоянии `queued`, `waiting_decision` или
   `paused` без живой горутины. Запуск, оставшийся с прошлого старта демона
   до появления `--safe-mode`, обязан оставаться доступным для паузы и
   отмены — safe mode блокирует новую работу, а не приостанавливает
   собственный механизм управления запусками демона — поэтому цикл, и
   возможность действовать над уже известными ему запусками, должны
   продолжать работать.

## Последствия

- `scheduler.Manager.SetLimits(1, 1, 1, 1)` больше не вызывается для safe
  mode. Больше нигде в кодовой базе он не вызывался с этими конкретными
  значениями, поэтому удаление ничего другого не меняет; настоящие значения
  по умолчанию у менеджера (`globalLimit: 6, perProjectLimit: 2,
  perProviderLimit: 4, perModelLimit: 3`) теперь никак не зависят от safe
  mode в любом случае, поскольку `Submit` отказывает раньше, чем к ним вообще
  обращаются.
- Будущий эндпоинт, запускающий воркер через `scheduler.Manager.Submit`,
  корректен по отношению к safe mode без единой лишней строчки кода. Тот,
  что запускает воркер любым другим способом (второй внешний процесс по
  образцу `rojo.Manager`, третий MCP provisioner), автоматически не
  покрывается и нуждается в том же, что получил здесь Rojo: собственном
  `SetSafeMode` и осознанном решении, стоит ли ради UX добавлять ещё и
  быстрый хендлер-level check.
