# ADR 0007: Review-before-apply gate is apply-then-revert, not pre-action

- Status: Accepted
- Date: 2026-08-15

## Context

Issue #20 asked for a way to make an agent pause before its file changes
are considered final, so an operator can look at a diff and decide before
the run is done. StudioForge already had a related but narrower mechanism
— `decisions` (`POST /api/v1/decisions/{id}/resolve`), scoped to exactly
one producer, an exhausted playtest-correction budget — and it explicitly
does not pause a run before a file edit, a destructive command, or a
publish (see [docs/SECURITY.md](../SECURITY.md#honest-gaps-for-the-beta-release)
and [docs/KNOWN_LIMITATIONS.md](../KNOWN_LIMITATIONS.md)). Issue #20 is
that general case: an operator-configurable gate that reviews an agent's
own file changes, not a narrow safety valve for one failure mode.

The name "review-before-apply" invites a specific expectation — that the
agent's edits are held somewhere unapplied until the operator approves
them, the way a pull request holds a branch's commits out of `main` until
it is merged. That expectation does not match what a coding agent's own
tool calls actually are: `Edit`/`Write`/`MultiEdit` (Claude) and
`create_file`/`replace_exact_text`/`apply_patch` (the in-process
`agenttools` loop) write to the real project files on the real filesystem,
synchronously, as part of executing a turn. Nothing in the scheduler's
architecture intercepts a tool call before it runs — the scheduler observes
provider events *after* the provider (a subprocess for Claude, an
in-process HTTP loop for OpenRouter/NVIDIA) has already acted on them. By
the time StudioForge's own code learns an edit happened, the edit has
already happened.

## Decision

Give each agent a `reviewBeforeApply` setting, off by default. When it is
on and a turn edited at least one file, the run pauses — but the pause
happens at the **boundary of the turn**, in the exact same place a run
already parks in `waiting_decision` for the pre-existing "agent asked a
question" mechanism (after the provider's process/loop for that turn has
already exited and returned control to the scheduler), not inside the turn
before any individual tool call. The gate reuses the same state
(`waiting_decision`) and the same status field the question mechanism
already established, rather than inventing a second run-status vocabulary
for what is, from the scheduler's point of view, a second reason to pause
at the same kind of boundary.

The operator resolves an open review with `POST /api/v1/runs/{id}/review`:

- **`apply`** keeps every change and completes the run — a
  `waiting_decision` → `completed` transition, nothing reverted.
- **`reject`** reverts every file in the run's own diff, computed fresh
  from the checkpoint recorded at review time — a genuine in-place revert,
  never the checkpoint's non-destructive `SafeRollback` sibling, which
  switches the whole worktree onto a side branch instead of undoing
  anything where it stands.
- **`apply-selected`** takes a client-supplied list of files/hunks to
  **keep**, re-parses the diff fresh against the same checkpoint, inverts
  the keep-list into the complementary revert-list, and reverts that. A
  keep-list that no longer matches the actual diff — a path or hunk index
  the fresh parse does not have — fails closed with `invalid_selection`
  rather than silently reverting (or keeping) the wrong thing.

All three read the diff and the revert target from the filesystem as it
actually stands, not from any buffered or staged representation of the
agent's intent, because there is no such buffer to read from — this is the
central fact the rest of this ADR explains and justifies.

### Apply-then-revert, and why "pre-action gate" is the wrong name for it

State the consequence plainly, because it is easy to read the feature name
and expect the opposite: **by the time a review's diff is shown to the
operator, the edits it describes are already on disk, and the turn that
made them has already finished.** A build step the same turn ran afterward
could already have compiled the new code. A later tool call in the same
turn could already have read a file the agent had just written. This is
not a bug in the implementation — it follows from what "the turn already
ended" means, and the alternative designs that would avoid it were
considered and rejected (below) because each one broke something the
agent's own tool calls already depend on. `reject` and `apply-selected`
are therefore **revert** operations, sharing exactly the mechanism a
manual selective rollback already uses (`SelectiveRollback`, the same
function `POST /api/v1/runs/{id}/rollback` calls with a `files`/`hunks`
body) — the review gate did not get its own copy of that logic, it is a
second caller of the one that already existed, sharing the same error
table (`ErrNotGitRepo`, `ErrDirtyWorktree`, `ErrLaterChanges`,
`ErrPatchCheckFailed`, `ErrSelectionUnknown`) and the same HTTP-error
mapping (`writeRollbackError`).

### The project lease is held through the review, by transfer, not by release-and-reacquire

A pending review holds the project's write lease under the owner string
`review:<runID>` for as long as it is open, so a second run cannot start
against the same project — and touch the same files a still-open review is
about to revert — while the review is unresolved. The lease is not
released by the run and separately re-acquired by the review; it is moved
directly from the run's ownership to the review's with `Handle.Transfer`,
a single critical section that swaps the owner string on every key the
handle holds without the key ever leaving the lease table in between. A
release-then-acquire pair would open exactly the window this feature exists
to close: between the release and the next acquire, a second run could see
the project as free and start, and a later `reject` would then be
reverting files a run it knows nothing about is concurrently editing. If
the transfer itself cannot succeed — the lease is somehow not held the way
the run expects — the run fails outright and the review gate is never
opened at all, rather than opening a review that cannot actually claim the
exclusivity it promises.

### Expiry releases the lease; it does not resolve the review

`review_gate_expiry_hours` (default 24, valid range 1–168) bounds how long
a review may sit unresolved. On expiry, exactly one thing happens: the
project's write lease is released and the review's own status flips to
`expired` in the database — a plain status update, touching no run row and
no file on disk. The run stays in `waiting_decision`. The edits stay on
disk exactly as the agent left them. There is deliberately **no** automatic
`apply` and **no** automatic `reject` on expiry. Auto-apply would mean an
operator's silence — possibly because they never saw the review, not
because they approved it — gets treated as approval, for changes that may
be exactly the ones a review gate exists to catch. Auto-revert is worse:
the changes are already on disk and may already have been built on top of,
so an unattended revert on a timer is a destructive write happening because
nobody was watching, which is the opposite of what a safety mechanism
should do unsupervised. Releasing the lease is the one part of expiry that
*is* fail-closed, deliberately: an expired review must not permanently
starve every other run on the project of its write lease, so the lease
comes back into the pool, but the actual decision — apply what's there, or
revert it — is left for the operator to make explicitly with an ordinary
`POST /runs/{id}/rollback`, the same tool that handles a rollback the
operator wants for any other reason.

### Studio MCP is out of scope, exactly the way it already is for the diff panel

A run's edits are tracked two ways that never merge: a file-edit flag (set
when a provider event names a tool from the filesystem-edit list —
`Edit`/`Write`/`MultiEdit` for Claude, `create_file`/`replace_exact_text`/
`apply_patch` for `agenttools`) drives the review gate, and a separate
Studio-mutation flag (set when a tool call changes the currently open
Roblox place over Studio MCP — `multi_edit`, `execute_luau`, and similar)
drives the existing `studio_direct_edits` diff-panel notice. `reject` and
`apply-selected` revert only what `git` tracks, because `SelectiveRollback`
only ever calls `git checkout`/`git apply -R`. A live edit made to the open
place through Studio MCP never touched the filesystem in the first place,
so there is nothing in the diff for a review to show or a revert to undo —
the same limitation the diff panel already states plainly rather than
implying the review gate saw everything a run did.

## Alternatives considered

1. **Run the turn against a separate `git worktree` and only merge it into
   the real project on approval.** This would give a genuine
   before-the-operator-sees-it staging area, closing the apply-then-revert
   gap entirely. Rejected because the agent already built its understanding
   of "the project" around one fixed absolute path for the entire run — the
   working directory a subprocess was launched in, the paths a
   `agenttools.Workspace` root resolves against, any build or test command
   the agent already ran earlier in the same turn against that path. A
   worktree lives at a different path by construction; redirecting every
   one of those to a worktree path for the duration of review, then back
   for a resumed run, is not a smaller change than the gate itself, and it
   would make a resumed session behave differently depending on whether a
   review happened to be pending, which is worse than the limitation this
   ADR accepts.
2. **Accumulate the turn's edits as in-memory patches and apply them to
   disk only on approval.** This keeps the real project untouched until
   the operator decides, which sounds like the cleanest fix. Rejected
   because a coding agent's own tool loop is not write-only: it routinely
   reads back a file it just wrote — to verify an edit landed correctly, to
   compute a subsequent diff, to feed the next step of a multi-file
   change — and a multi-step task depends on those reads seeing its own
   prior writes within the same turn. Diverting every write into an
   in-memory buffer the agent's own subsequent reads do not see would
   silently break exactly the pattern most coding tasks rely on, trading a
   review-time guarantee for a correctness bug during the run itself.
3. **A copy-on-write filesystem overlay**, giving the agent a writable view
   backed by the real files without touching them until a commit step.
   This is the mechanism that would come closest to "real staging with no
   behavioral change for the agent." Rejected because it does not exist as
   a general-purpose primitive on Windows, which is where StudioForge
   mostly runs — there is no OS-level copy-on-write filesystem view
   available without a kernel driver, the exact same constraint that
   already rules out a Windows filesystem sandbox for `run_command`
   confinement ([ADR 0005](0005-run-command-confinement.md)). Building this
   for macOS alone (APFS clones) and refusing the feature on Windows
   entirely was rejected as inconsistent with every other gate in this
   codebase, which states platform limits honestly rather than shipping a
   feature that quietly does not exist on the platform most operators use.

## Consequences

- The review gate is real leverage against an unattended, unwanted change
  reaching a final `completed` run — but it is leverage exercised by
  reverting, not by holding anything back, and every surface describing
  this feature (UI copy, API docs, this ADR) must say so plainly rather
  than let the name imply a staging area that does not exist.
- `reject`/`apply-selected` inherit every failure mode `SelectiveRollback`
  already has — a dirty worktree, a later run's conflicting change, a hunk
  that no longer applies — because they are the same code path, not a
  parallel one that could drift from it.
- An operator who leaves a review open past `review_gate_expiry_hours`
  gets the project's write lease back, not a resolved review; the run sits
  in `waiting_decision` indefinitely until someone acts, the same as an
  unanswered question would.
- Nothing here reaches Studio MCP edits — an agent with `reviewBeforeApply`
  on and a Studio grant can still change the open place in a way this gate
  cannot see or undo, exactly as the diff panel's own `studio_direct_edits`
  notice already discloses for the non-review case.

---

# ADR 0007: Гейт review-before-apply — это apply-then-revert, а не pre-action (Русский)

- Статус: Принято
- Дата: 2026-08-15

## Контекст

Issue #20 просил способ заставить агента приостановиться до того, как его
правки файлов будут считаться финальными, чтобы оператор мог посмотреть на
diff и принять решение до завершения запуска. В StudioForge уже был
смежный, но более узкий механизм — `decisions`
(`POST /api/v1/decisions/{id}/resolve`), привязанный ровно к одному
producer'у, исчерпанному бюджету коррекций playtest, — и он прямо не
приостанавливает запуск перед правкой файла, деструктивной командой или
публикацией (см.
[docs/SECURITY.md](../SECURITY.md#honest-gaps-for-the-beta-release) и
[docs/KNOWN_LIMITATIONS.md](../KNOWN_LIMITATIONS.md)). Issue #20 — это
именно общий случай: настраиваемый оператором gate, который ревьюит
собственные правки файлов агента, а не узкий предохранитель для одного
конкретного сценария отказа.

Название «review-before-apply» подразумевает конкретное ожидание — что
правки агента где-то удерживаются неприменёнными, пока оператор их не
одобрит, — так же, как pull request держит коммиты ветки вне `main` до
merge. Это ожидание не соответствует тому, чем реально являются
собственные вызовы инструментов кодового агента: `Edit`/`Write`/
`MultiEdit` (Claude) и `create_file`/`replace_exact_text`/`apply_patch`
(внутренний loop `agenttools`) пишут в реальные файлы проекта на реальной
файловой системе, синхронно, как часть выполнения хода. Ничто в
архитектуре scheduler не перехватывает вызов инструмента до его
выполнения — scheduler наблюдает события провайдера *после* того, как
провайдер (подпроцесс для Claude, внутренний HTTP-loop для
OpenRouter/NVIDIA) уже подействовал на их основе. К моменту, когда
собственный код StudioForge узнаёт, что правка произошла, правка уже
произошла.

## Решение

Дать каждому агенту настройку `reviewBeforeApply`, по умолчанию
выключенную. Когда она включена и ход отредактировал хотя бы один файл,
запуск приостанавливается — но пауза происходит на **границе хода**, ровно
в том же месте, где запуск уже паркуется в `waiting_decision` для уже
существующего механизма «агент задал вопрос» (после того как
процесс/loop провайдера для этого хода уже завершился и вернул управление
scheduler'у), а не внутри хода перед каким-либо отдельным вызовом
инструмента. Гейт переиспользует то же состояние (`waiting_decision`) и то
же поле статуса, что уже установил механизм вопросов, вместо того чтобы
изобретать вторую лексику статусов запуска для того, что с точки зрения
scheduler является второй причиной приостановиться на той же границе.

Оператор разрешает открытое ревью через `POST /api/v1/runs/{id}/review`:

- **`apply`** сохраняет все изменения и завершает запуск — переход
  `waiting_decision` → `completed`, ничего не откатывается.
- **`reject`** откатывает каждый файл из собственного diff запуска,
  разобранного заново от чекпоинта, записанного в момент ревью, —
  настоящий откат на месте, никогда не non-destructive `SafeRollback`
  чекпоинта, который переключает всё рабочее дерево на отдельную ветку,
  вместо того чтобы отменить что-либо там, где оно стоит.
- **`apply-selected`** принимает от клиента список файлов/hunk'ов, которые
  нужно **сохранить**, разбирает diff заново от того же чекпоинта,
  инвертирует список сохранения в дополняющий список отката и откатывает
  его. Список сохранения, переставший соответствовать реальному diff, —
  путь или индекс hunk'а, которого в свежем разборе нет, — отказывает
  закрыто с `invalid_selection`, а не тихо откатывает (или сохраняет) не
  то.

Все три действия читают diff и цель отката с файловой системы в том виде,
в каком она реально находится, а не из какого-либо буферизованного или
staged представления намерения агента, — потому что такого буфера, из
которого можно было бы читать, не существует. Это центральный факт,
который объясняет и обосновывает остальную часть этого ADR.

### Apply-then-revert, и почему «pre-action gate» — неверное название

Сформулируем следствие прямо, потому что по названию функции легко ожидать
обратного: **к моменту, когда diff ревью показывается оператору, описанные
в нём правки уже на диске, а ход, который их сделал, уже завершился.** Шаг
сборки, который тот же ход выполнил после правок, уже мог скомпилировать
новый код. Последующий вызов инструмента в том же ходе уже мог прочитать
файл, который агент только что записал. Это не баг реализации — это
следует из того, что означает «ход уже завершился», и альтернативные
дизайны, которые избежали бы этого, были рассмотрены и отклонены (ниже),
потому что каждый из них ломал что-то, от чего уже зависят собственные
вызовы инструментов агента. Поэтому `reject` и `apply-selected` — это
операции **отката**, использующие ровно тот же механизм, что уже
использует ручной селективный откат (`SelectiveRollback`, та же функция,
которую вызывает `POST /api/v1/runs/{id}/rollback` с телом
`files`/`hunks`), — гейт ревью не получил собственную копию этой логики,
он второй вызывающий уже существовавшей, разделяющий ту же таблицу ошибок
(`ErrNotGitRepo`, `ErrDirtyWorktree`, `ErrLaterChanges`,
`ErrPatchCheckFailed`, `ErrSelectionUnknown`) и тот же маппинг в HTTP-ошибки
(`writeRollbackError`).

### Лиза проекта удерживается на время ревью передачей владения, а не release-и-acquire

Открытое ревью удерживает write-лизу проекта под именем владельца
`review:<runID>` на всё время, пока оно открыто, чтобы второй запуск не мог
начаться на том же проекте — и не тронул те же файлы, которые ещё открытое
ревью вот-вот откатит, — пока ревью не разрешено. Лиза не освобождается
запуском и отдельно не захватывается заново ревью; она напрямую
переносится от владения запуска к владению ревью через `Handle.Transfer` —
одну критическую секцию, которая меняет строку владельца на каждом ключе,
которым владеет handle, при этом ключ ни на миг не покидает таблицу лиз.
Пара release-затем-acquire открыла бы ровно то окно, для закрытия которого
существует эта функция: между release и следующим acquire второй запуск
мог бы увидеть проект как свободный и начаться, а последующий `reject`
тогда откатывал бы файлы, которые параллельно редактирует запуск, о
котором он ничего не знает. Если сама передача не может состояться —
лиза почему-то не удерживается так, как ожидает запуск, — запуск
проваливается сразу, и гейт ревью вообще не открывается, вместо того
чтобы открыть ревью, которое не может реально претендовать на
эксклюзивность, которую оно обещает.

### Истечение освобождает лизу; оно не разрешает ревью

`review_gate_expiry_hours` (по умолчанию 24, допустимый диапазон 1–168)
ограничивает, сколько ревью может простоять неразрешённым. По истечении
происходит ровно одно: write-лиза проекта освобождается, а собственный
статус ревью переключается в `expired` в базе данных — простое обновление
статуса, не затрагивающее ни строку запуска, ни файл на диске. Запуск
остаётся в `waiting_decision`. Правки остаются на диске в точности такими,
какими их оставил агент. По истечении намеренно **нет** ни автоматического
`apply`, ни автоматического `reject`. Auto-apply означал бы, что молчание
оператора — возможно, потому что он вообще не увидел ревью, а не потому
что одобрил его, — трактуется как одобрение именно тех изменений, для
поимки которых и существует гейт ревью. Auto-revert ещё хуже: изменения уже
на диске и, возможно, на них уже что-то построено, так что неотслеживаемый
откат по таймеру — это деструктивная запись, происходящая потому, что
никто не следил, а это противоположность тому, что должен делать
предохранительный механизм без присмотра. Освобождение лизы — единственная
часть истечения, которая *действительно* fail-closed, намеренно:
истёкшее ревью не должно навсегда лишить любой другой запуск на проекте
его write-лизы, поэтому лиза возвращается в пул, но само решение —
применить то, что есть, или откатить это — остаётся за оператором,
который принимает его явно обычным `POST /runs/{id}/rollback`, тем же
инструментом, что обрабатывает откат, нужный оператору по любой другой
причине.

### Studio MCP вне области действия — ровно так же, как уже для панели diff

Правки запуска отслеживаются двумя путями, которые никогда не сливаются:
флаг правки файла (выставляется, когда событие провайдера называет
инструмент из списка правки файловой системы — `Edit`/`Write`/
`MultiEdit` для Claude, `create_file`/`replace_exact_text`/`apply_patch`
для `agenttools`) управляет гейтом ревью, а отдельный флаг мутации Studio
(выставляется, когда вызов инструмента меняет открытый place Roblox через
Studio MCP — `multi_edit`, `execute_luau` и подобные) управляет уже
существующим уведомлением `studio_direct_edits` в панели diff. `reject` и
`apply-selected` откатывают только то, что отслеживает `git`, потому что
`SelectiveRollback` вызывает только `git checkout`/`git apply -R`. Живая
правка, сделанная в открытом place через Studio MCP, изначально вообще не
трогала файловую систему, так что показывать ревью в diff или отменять
откатом нечего — то же ограничение, которое панель diff уже честно
признаёт, вместо того чтобы подразумевать, будто гейт ревью видел всё, что
сделал запуск.

## Рассмотренные альтернативы

1. **Выполнять ход в отдельном `git worktree` и вливать его в реальный
   проект только после одобрения.** Это дало бы настоящую staging-область
   до того, как оператор что-либо увидит, полностью закрыв разрыв
   apply-then-revert. Отклонено, потому что агент уже строит своё
   понимание «проекта» вокруг одного фиксированного абсолютного пути на
   весь запуск — рабочей директории, в которой запущен подпроцесс, путей,
   относительно которых резолвит корень `agenttools.Workspace`, любой
   команды сборки или теста, которую агент уже выполнил раньше в том же
   ходе против этого пути. Worktree по своей природе находится по другому
   пути; перенаправлять каждый из них на путь worktree на время ревью, а
   затем обратно для продолженного запуска, — не меньшее изменение, чем
   сам гейт, и оно заставило бы продолженную сессию вести себя иначе в
   зависимости от того, было ли на тот момент открыто ревью, что хуже
   ограничения, принятого этим ADR.
2. **Накапливать правки хода как патчи в памяти и применять их к диску
   только по одобрению.** Это оставило бы реальный проект нетронутым, пока
   оператор не решит, что звучит как самое чистое решение. Отклонено,
   потому что собственный цикл инструментов кодового агента не является
   write-only: он регулярно перечитывает файл, который сам только что
   записал, — чтобы проверить, что правка легла корректно, вычислить
   последующий diff, подать результат в следующий шаг многофайлового
   изменения, — и многошаговая задача зависит от того, что эти чтения
   видят собственные более ранние записи в пределах того же хода.
   Отправка каждой записи в буфер в памяти, который собственные
   последующие чтения агента не видят, тихо сломала бы именно тот
   паттерн, на который опирается большинство кодовых задач, обменивая
   гарантию на момент ревью на баг корректности прямо во время выполнения
   запуска.
3. **Наложение файловой системы copy-on-write**, дающее агенту writable
   представление поверх реальных файлов без их изменения до шага commit.
   Это механизм, ближе всего подходящий к «настоящий staging без изменения
   поведения для агента». Отклонено, потому что он не существует как
   универсальный примитив на Windows, где StudioForge в основном и
   работает, — на уровне ОС нет доступного представления
   copy-on-write без драйвера ядра, ровно то же ограничение, что уже
   исключает песочницу файловой системы Windows для изоляции
   `run_command` ([ADR 0005](0005-run-command-confinement.md)). Построить
   это только для macOS (клоны APFS) и вовсе отказаться от функции на
   Windows было отклонено как несогласующееся с любым другим гейтом в этой
   кодовой базе, который честно формулирует платформенные ограничения,
   вместо того чтобы поставлять функцию, которой тихо не существует на
   платформе большинства операторов.

## Последствия

- Гейт ревью — реальный рычаг против неотслеживаемого, нежелательного
  изменения, доходящего до финального `completed` запуска, — но это рычаг,
  реализуемый откатом, а не удержанием чего-либо, и любая поверхность,
  описывающая эту функцию (текст UI, документация API, этот ADR), должна
  говорить об этом прямо, а не позволять названию подразумевать
  staging-область, которой не существует.
- `reject`/`apply-selected` наследуют все режимы отказа, которые уже есть
  у `SelectiveRollback`, — грязное рабочее дерево, конфликтующее изменение
  более позднего запуска, hunk, переставший применяться, — потому что это
  тот же код, а не параллельный, способный от него отклониться.
- Оператор, оставивший ревью открытым дольше `review_gate_expiry_hours`,
  получает обратно write-лизу проекта, а не разрешённое ревью; запуск
  бессрочно сидит в `waiting_decision`, пока кто-то не вмешается, — так же,
  как это было бы с неотвеченным вопросом.
- Ничто здесь не дотягивается до правок через Studio MCP — агент с
  включённым `reviewBeforeApply` и доступом к Studio всё равно может
  изменить открытый place так, что этот гейт этого не увидит и не сможет
  отменить, — ровно как уведомление `studio_direct_edits` панели diff уже
  раскрывает для случая без ревью.
