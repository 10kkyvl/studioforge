# ADR 0005: Confine `run_command` per platform

- Status: Accepted
- Date: 2026-08-14

## Context

`run_command` is the one place in StudioForge where a model chooses what
executable and arguments actually run — every other subprocess StudioForge
starts (`rojo serve`/`build`/`plugin install`, the Claude Code CLI, the
Studio MCP shim, StudioForge's own `git` checkpoints and operations,
diagnostic probes, the browser launcher, the Roblox Studio GUI launch) has
its argv chosen by StudioForge itself, or — for the Claude Code CLI — *is*
the agent and carries its own permission model. Before this change,
`workspace-write`'s only defense against a `run_command` process was the
allowlist checked in `agenttools` (command identity, no inline eval flags,
no `go run`) — a barrier on which command runs, not a boundary on what a
permitted one can then do to the machine.

## Decision

Give `run_command` real OS-level confinement, scoped to exactly the process
it starts (and that process's own descendants), implemented per platform in
`internal/processes`:

- **Windows** — a Job Object per confined command. The child is created
  `CREATE_SUSPENDED`, assigned to the job, and only then resumed, so there
  is no window in which the child — or a grandchild it spawns immediately —
  can escape the job. Limits: 128 active processes, 8 GiB job memory,
  kill-on-job-close, die-on-unhandled-exception.
  `BREAKAWAY_OK`/`SILENT_BREAKAWAY_OK` are never set on the job, so
  `CREATE_BREAKAWAY_FROM_JOB` requested from inside fails. Killing a run
  terminates the job (`TerminateJobObject`), not `taskkill /T`, which walks
  parent PIDs and therefore loses an orphaned grandchild —
  `internal/processes/confine_windows_test.go` proves an orphaned
  grandchild is reaped through the job, and the same test fails without it.
  The filesystem is **not** confined by this: a job object bounds processes
  and memory, not file writes, and Windows has no filesystem sandbox
  without a driver, which StudioForge does not ship.
- **macOS** — `sandbox-exec` with a profile StudioForge generates per run.
  Writes are denied by default and allowed only under the project root, the
  temp directory, and the build caches ordinary tooling needs
  (`~/Library/Caches`, `~/Library/Developer`, `~/.cache`, `~/.npm`,
  `~/.cargo`, `~/go/pkg/mod`, `~/go/pkg/sumdb`) plus the usual `/dev`
  entries. Reads are unrestricted, and network egress is deliberately
  untouched (issue #29). Paths reach the profile as `-D` parameters to
  `sandbox-exec`, never interpolated into the profile text, so a path
  containing profile-language metacharacters cannot corrupt the policy.
- **Linux** — not implemented; see [Linux specification](#linux-specification-not-implemented)
  below.

Confinement is chosen per permission profile, not per platform:
`workspace-write` requests the full policy above and **fails closed** — if
confinement cannot be established (job creation fails, `sandbox-exec` is
missing, the platform has no implementation), the command is refused rather
than run unconfined. `danger-full-access` requests reaping only — nothing
outlives a cancelled run — with no filesystem or resource policy, and does
not hard-fail, because that profile claims no boundary to fail closed on.
`read-only` never reaches this at all, since `run_command` is not
registered on that profile. `studioforge doctor` runs `ProbeConfinement`
(create-and-configure a job object with no process attached, on Windows;
compile-and-run a trivial profile with `sandbox-exec`, on macOS) so an
operator learns whether confinement actually works on their machine before
a run needs it, not from a mid-run failure.

**Verification status.** The Windows implementation is exercised on every
CI run (`go`, `race` jobs, `windows-latest`), and the `go` job additionally
runs `go test -run TestConfined ./internal/processes/...`
(`internal/processes/toolchain_confined_test.go`) with
`STUDIOFORGE_ALLOW_UNCONFINED` explicitly cleared, so a real `npm install`,
`go test`, and `go mod download` execute inside a Windows Job Object rather
than only through the synthetic `TestHelperProcess` binary the rest of the
suite uses — this has been run and confirmed passing on a real Windows
machine, not just in CI. The macOS implementation gets the same test file
run against it by the `macos` CI job (`go test
./internal/processes/...` on `macos-latest`), which now also installs
Node via `actions/setup-node@v4` so the `npm install` case has something
to exercise; unlike the Windows case, nobody has run these toolchain tests
on real macOS hardware from this repository as of this ADR — the `macos`
CI job on the pull request that introduces this test file is the first
real evidence of whether `sandbox-exec`'s allowed-write paths
(`~/Library/Caches`, `~/.npm`, `~/go/pkg/mod`, …) are actually sufficient
for `npm install` and `go test` to succeed, not an analysis performed here.
Rojo is not installed on any CI runner (Windows or macOS), so
`TestConfinedRojoBuildSucceeds` legitimately skips everywhere in CI today;
`rojo build` under confinement is exercised only on a machine that happens
to have Rojo on `PATH`, and has not been verified under `sandbox-exec` at
all. Network egress under confinement remains untouched by design
(issue #29) and this status says nothing about it.

## Alternatives considered

1. **Assign to the job after `cmd.Start()`.** The simplest-looking option:
   start the process normally, then call `AssignProcessToJobObject` on its
   PID. Rejected — this leaves an unbounded race window between the
   process starting and the assignment call running, during which the
   child can spawn a grandchild that is never pulled into the job, because
   job membership is not retroactive. A fast-starting build tool that
   forks immediately defeats this in practice, not just in theory.
2. **Call `windows.CreateProcess` directly**, bypassing `os/exec`. This
   would let StudioForge assign the job before resuming the process in one
   uninterrupted sequence, closing the race above without needing
   `CREATE_SUSPENDED` plumbing through `os/exec`. Rejected — it throws away
   everything `os/exec.Cmd` already provides that the rest of
   `internal/processes` depends on: `StdoutPipe`/`StderrPipe`, `Wait`,
   `WaitDelay`, and `exec.CommandContext` for cancellation. Reimplementing
   that plumbing on top of a raw `CreateProcess` call is a second process
   supervisor, not a fix to the race.
3. **A sponsor process using `SysProcAttr.ParentProcess`.** Job membership
   is inherited at process creation, so a small sponsor process could be
   created already inside the job, then used as the `ParentProcess` for the
   real command, giving job membership from the first instruction with no
   suspend/resume dance at all. This is correct, and is kept on record as
   the fallback if `resumeMainThread`'s thread-enumeration approach (below)
   ever proves unreliable in practice. It was not the initial choice
   because it costs an extra binary mode (StudioForge would need to run
   itself as the sponsor) and a process per confined command, and it breaks
   `taskkill /T`-based tooling and diagnostics that expect a direct
   parent/child relationship, which the job-based kill path (`Kill()` via
   `TerminateJobObject`) was already written to avoid depending on.

## The `NtResumeProcess` fast path

Resuming a process started `CREATE_SUSPENDED` has one documented route:
`CreateToolhelp32Snapshot(TH32CS_SNAPTHREAD)` over every thread on the
machine, walked with `Thread32First`/`Thread32Next` to find the one thread
belonging to this process, then `ResumeThread` on it
(`resumeMainThread`). Measured at up to ~40 ms per command and scaling with
total system thread count rather than with the child being resumed — a
cost the confinement work should not force onto every `run_command` call.

`ntdll!NtResumeProcess` resumes every thread of a process by handle in one
call, with no enumeration. It is undocumented, but stable since Windows XP
and used in practice by tools such as Process Explorer. Given the
documented path's real cost (measured ~40 ms → ~0.05 ms for the job
assignment step with the fast path in place — see
[docs/SECURITY.md](../SECURITY.md#process-confinement-for-run_command) for
the full benchmark), and that `resumeMainThread` is retained as an
automatic fallback on any `NtResumeProcess` failure rather than removed,
using the undocumented call was judged an acceptable trade: correctness
does not depend on it staying available, only performance does.

## Consequences

- `run_command` on `workspace-write` costs roughly 0.3–0.6 ms more per
  command than running unconfined (measured, one Windows machine,
  `-benchtime 20x`) — a cost worth paying for a real process/resource
  boundary.
- Linux contributors and CI must set `STUDIOFORGE_ALLOW_UNCONFINED=1` to
  run the test suite, since `workspace-write` fails closed there with no
  implementation to fall back to.
- `studioforge doctor`'s `confinement` check becomes part of the standard
  pre-run diagnosis, and its wording must keep stating the platform
  boundary honestly (see [docs/KNOWN_LIMITATIONS.md](../KNOWN_LIMITATIONS.md)
  and [docs/SECURITY.md](../SECURITY.md)) rather than implying a uniform
  sandbox.
- If `resumeMainThread`'s Toolhelp approach is ever found unreliable
  (observed in the wild, not merely theoretical), the sponsor-process
  design in alternative 3 above is the recorded fallback, not a redesign
  from scratch.

## Linux specification (not implemented)

StudioForge does not ship on Linux; `workspace-write` refuses to start a
`run_command` process there rather than run it unconfined, and
`STUDIOFORGE_ALLOW_UNCONFINED=1` is the named escape hatch for contributors
and CI (`internal/processes/confine_other.go`). This section is a plan, not
a shipped feature — it exists so a future implementation starts from a
reviewed design instead of an ad hoc one.

Two approaches were identified as viable, in order of preference:

1. **A user namespace plus a bind-mounted view of the project.** Re-exec
   into a new user namespace (`CLONE_NEWUSER`) alongside a mount namespace
   (`CLONE_NEWNS`), bind-mount the project root read-write and everything
   else read-only (or not at all, mirroring the macOS profile's allowlist:
   the project root, a temp directory, and language/package-manager
   caches), and `pivot_root`/`chroot` into that view before `execve`. This
   is the closest Linux analogue to what `sandbox-exec` gives on macOS —
   filesystem writes are actually denied outside the allowed paths, not
   merely discouraged — and it composes with a process/resource boundary
   via a cgroup (below) for parity with the Windows job object's process
   and memory caps.
2. **A seccomp-bpf filter** restricting the syscalls a confined command may
   issue (in particular filtering `openat`/`open`/`rename`/`unlink`-family
   calls by path prefix is not directly expressible in seccomp-bpf, which
   only sees syscall arguments, not resolved paths — so a filter alone
   cannot reproduce the path-scoped write policy the other two platforms
   have; it would need to be paired with the bind-mount approach above
   rather than replace it). Seccomp is more valuable here as a second layer
   restricting dangerous syscall classes outright (e.g. `ptrace`,
   `mount`, module loading) than as the primary confinement mechanism.

A cgroup (`cgroup v2`, `pids.max` and `memory.max`) would provide the
process-count and memory ceiling equivalent to the Windows job object's
`JOB_OBJECT_LIMIT_ACTIVE_PROCESS`/`JOB_OBJECT_LIMIT_JOB_MEMORY`, and
`cgroup.kill` gives the same guaranteed-reap-on-close semantics
`TerminateJobObject` gives on Windows.

Recommendation: implement (1) plus the cgroup layer as the `ConfineAgent`
policy for Linux, matching the fail-closed contract `workspace-write`
already has on the other two platforms; treat (2) as a follow-up hardening
layer, not a substitute. Neither is scheduled; this section exists so the
decision, once made, does not have to be re-derived.

---

# ADR 0005: Изоляция `run_command` по платформам (Русский)

- Статус: Принято
- Дата: 2026-08-14

## Контекст

`run_command` — единственное место в StudioForge, где модель сама выбирает,
какой исполняемый файл и с какими аргументами запустить: у любого другого
подпроцесса, который запускает StudioForge (`rojo serve`/`build`/`plugin
install`, CLI Claude Code, Studio MCP shim, собственные операции и
checkpoint'ы git StudioForge, диагностические пробы, запуск браузера,
запуск GUI Roblox Studio), argv выбирает сам StudioForge, а CLI Claude
Code — это и есть сам агент со своей моделью разрешений. До этого
изменения единственной защитой `workspace-write` от процесса
`run_command` был allowlist, проверяемый в `agenttools` (идентичность
команды, запрет inline eval-флагов, запрет `go run`) — барьер на то, какая
команда запускается, а не граница на то, что разрешённая команда может
после этого сделать с машиной.

## Решение

Дать `run_command` настоящую OS-изоляцию, ограниченную ровно тем
процессом, который он запускает (и потомками этого процесса), реализованную
по платформам в `internal/processes`:

- **Windows** — Job Object на каждую изолированную команду. Дочерний
  процесс создаётся `CREATE_SUSPENDED`, назначается в job и только затем
  возобновляется, поэтому нет окна, в котором сам процесс — или
  порождённый им сразу внук — мог бы выйти из job. Лимиты: 128 активных
  процессов, 8 ГиБ памяти job, kill-on-job-close, die-on-unhandled-exception.
  `BREAKAWAY_OK`/`SILENT_BREAKAWAY_OK` на job никогда не устанавливаются,
  поэтому `CREATE_BREAKAWAY_FROM_JOB` изнутри завершается ошибкой.
  Завершение запуска останавливает job (`TerminateJobObject`), а не
  `taskkill /T`, который обходит по родительским PID и поэтому теряет
  осиротевшего внука — `internal/processes/confine_windows_test.go`
  доказывает, что осиротевший внук уничтожается через job, и тот же тест
  падает без него. Файловая система этим не изолируется: job object
  ограничивает процессы и память, а не запись файлов, а у Windows нет
  песочницы файловой системы без драйвера, который StudioForge не
  поставляет.
- **macOS** — `sandbox-exec` с профилем, который StudioForge генерирует на
  каждый запуск. Запись по умолчанию запрещена и разрешена только внутри
  корня проекта, временного каталога и кэшей сборки, нужных обычным
  инструментам (`~/Library/Caches`, `~/Library/Developer`, `~/.cache`,
  `~/.npm`, `~/.cargo`, `~/go/pkg/mod`, `~/go/pkg/sumdb`), плюс обычные
  записи `/dev`. Чтение не ограничено, а сетевой доступ намеренно не
  тронут (issue #29). Пути попадают в профиль как параметры `-D` для
  `sandbox-exec`, а не подставляются в текст профиля, поэтому путь с
  метасимволами языка профилей не может испортить политику.
- **Linux** — не реализовано; см. [спецификацию для Linux](#спецификация-для-linux-не-реализовано)
  ниже.

Изоляция выбирается по permission-профилю, а не по платформе:
`workspace-write` запрашивает полную политику выше и **отказывает закрыто
(fails closed)** — если изоляцию невозможно установить (не создаётся job,
отсутствует `sandbox-exec`, на платформе нет реализации), команда
отклоняется, а не запускается неизолированной. `danger-full-access`
запрашивает только reaping — ничто не переживает отменённый запуск — без
файловой или ресурсной политики, и не проваливается жёстко, поскольку этот
профиль не заявляет границу, на которой можно отказать закрыто.
`read-only` вообще сюда не доходит, поскольку `run_command` на этом
профиле не регистрируется. `studioforge doctor` запускает
`ProbeConfinement` (создать-и-настроить job object без привязанного
процесса на Windows; скомпилировать-и-выполнить тривиальный профиль через
`sandbox-exec` на macOS), чтобы оператор узнал, действительно ли изоляция
работает на его машине, до того, как она понадобится запуску, а не из
ошибки посреди запуска.

**Статус проверки.** Реализация для Windows выполняется на каждом прогоне
CI (джобы `go`, `race`, `windows-latest`), и джоб `go` дополнительно
запускает `go test -run TestConfined ./internal/processes/...`
(`internal/processes/toolchain_confined_test.go`) с явно очищенной
`STUDIOFORGE_ALLOW_UNCONFINED`, так что настоящие `npm install`, `go test`
и `go mod download` выполняются внутри реального Windows Job Object, а не
только через синтетический бинарник `TestHelperProcess`, которым
пользуется остальной набор тестов, — это прогонялось и подтверждено
проходящим на реальной машине с Windows, а не только в CI. Тот же файл
тестов запускается и в CI-джобе `macos` (`go test
./internal/processes/...` на `macos-latest`), который теперь также
устанавливает Node через `actions/setup-node@v4`, чтобы сценарию `npm
install` было что проверять; в отличие от Windows, никто из этого
репозитория ещё не запускал эти тесты тулчейна на реальном железе macOS —
джоб `macos` в pull request, вводящем этот файл тестов, станет первым
реальным подтверждением того, что разрешённых `sandbox-exec` путей на
запись (`~/Library/Caches`, `~/.npm`, `~/go/pkg/mod`, …) действительно
хватает для успешного `npm install` и `go test`, а не анализом,
проведённым здесь. Rojo не установлен ни на одном CI-раннере (ни на
Windows, ни на macOS), поэтому `TestConfinedRojoBuildSucceeds` законно
скипается в CI повсеместно; `rojo build` под изоляцией проверяется только
на машине, где Rojo случайно оказался в `PATH`, и под `sandbox-exec` вообще
не проверялся. Сетевой доступ под изоляцией по-прежнему намеренно не
тронут (issue #29), и этот статус ничего о нём не утверждает.

## Рассмотренные альтернативы

1. **Назначать в job после `cmd.Start()`.** Самый простой на вид вариант:
   запустить процесс обычным образом, затем вызвать
   `AssignProcessToJobObject` по его PID. Отклонено — это оставляет
   неограниченное окно гонки между стартом процесса и вызовом назначения,
   в течение которого дочерний процесс может породить внука, который
   никогда не попадёт в job, поскольку членство в job не ретроактивно.
   Быстро стартующий сборочный инструмент, который форкается немедленно,
   обходит это на практике, а не только в теории.
2. **Вызывать `windows.CreateProcess` напрямую**, в обход `os/exec`. Это
   позволило бы назначить job до возобновления процесса одной непрерывной
   последовательностью, закрывая гонку выше без необходимости прокидывать
   `CREATE_SUSPENDED` через `os/exec`. Отклонено — это отбрасывает всё, что
   `os/exec.Cmd` уже даёт и от чего зависит остальной `internal/processes`:
   `StdoutPipe`/`StderrPipe`, `Wait`, `WaitDelay` и `exec.CommandContext`
   для отмены. Реализовать этот функционал заново поверх сырого вызова
   `CreateProcess` — это второй супервизор процессов, а не исправление
   гонки.
3. **Sponsor-процесс с `SysProcAttr.ParentProcess`.** Членство в job
   наследуется при создании процесса, поэтому небольшой sponsor-процесс
   можно было бы создать уже внутри job, а затем использовать его как
   `ParentProcess` для реальной команды, давая членство в job с первой же
   инструкции без пляски с suspend/resume вообще. Это корректный подход, и
   он зафиксирован как запасной вариант на случай, если подход
   `resumeMainThread` с перечислением потоков (ниже) когда-либо окажется
   ненадёжным на практике. Он не был выбран изначально, поскольку стоит
   дополнительного режима бинарника (StudioForge пришлось бы запускать
   себя же как sponsor) и процесса на каждую изолированную команду, а
   также ломает основанные на `taskkill /T` инструменты и диагностику,
   ожидающие прямой связи родитель/потомок, — от зависимости от которой
   путь завершения через job (`Kill()` через `TerminateJobObject`) как раз
   и был написан, чтобы не зависеть.

## Быстрый путь `NtResumeProcess`

У возобновления процесса, запущенного с `CREATE_SUSPENDED`, один
документированный маршрут: `CreateToolhelp32Snapshot(TH32CS_SNAPTHREAD)`
по всем потокам машины, обход через `Thread32First`/`Thread32Next` в
поисках единственного потока этого процесса, затем `ResumeThread` на нём
(`resumeMainThread`). Измерено до ~40 мс на команду, и стоимость растёт с
общим числом потоков в системе, а не с самим возобновляемым дочерним
процессом — стоимость, которую изоляция не должна навязывать каждому
вызову `run_command`.

`ntdll!NtResumeProcess` возобновляет все потоки процесса по хендлу одним
вызовом, без перечисления. Он недокументирован, но стабилен начиная с
Windows XP и используется на практике такими инструментами, как Process
Explorer. Учитывая реальную стоимость документированного пути (измерено
~40 мс → ~0.05 мс для шага назначения в job с быстрым путём — полный
бенчмарк см. в [docs/SECURITY.md](../SECURITY.md#process-confinement-for-run_command)),
и то, что `resumeMainThread` сохранён как автоматический fallback при
любой ошибке `NtResumeProcess`, а не удалён, использование
недокументированного вызова было признано приемлемым компромиссом:
корректность от него не зависит, только производительность.

## Последствия

- `run_command` на `workspace-write` стоит примерно на 0.3–0.6 мс дороже на
  команду, чем неизолированный запуск (измерено, одна машина с Windows,
  `-benchtime 20x`) — цена, оправданная реальной границей по процессам и
  ресурсам.
- Контрибьюторам и CI на Linux нужно выставлять
  `STUDIOFORGE_ALLOW_UNCONFINED=1`, чтобы запускать тестовый набор,
  поскольку `workspace-write` там отказывает закрыто без реализации,
  на которую можно опереться.
- Проверка `confinement` в `studioforge doctor` становится частью
  стандартной диагностики перед запуском, и её формулировки должны
  честно описывать границу по платформе (см.
  [docs/KNOWN_LIMITATIONS.md](../KNOWN_LIMITATIONS.md) и
  [docs/SECURITY.md](../SECURITY.md)), а не подразумевать единую песочницу.
- Если подход `resumeMainThread` с Toolhelp когда-либо окажется
  ненадёжным на практике (не только в теории), зафиксированный запасной
  вариант — sponsor-процесс из альтернативы 3 выше, а не редизайн с нуля.

## Спецификация для Linux (не реализовано)

StudioForge не поставляется под Linux; `workspace-write` там отказывается
запускать процесс `run_command`, вместо того чтобы запустить его
неизолированным, а `STUDIOFORGE_ALLOW_UNCONFINED=1` — именованный escape
hatch для контрибьюторов и CI (`internal/processes/confine_other.go`).
Этот раздел — план, а не реализованная функция: он существует, чтобы
будущая реализация отталкивалась от рассмотренного дизайна, а не от
придуманного на ходу.

Выявлены два жизнеспособных подхода, в порядке предпочтения:

1. **User namespace плюс bind-mount представление проекта.** Re-exec в
   новый user namespace (`CLONE_NEWUSER`) вместе с mount namespace
   (`CLONE_NEWNS`), bind-mount корня проекта на чтение-запись, а всего
   остального — только на чтение (или вообще без доступа, зеркалируя
   allowlist профиля macOS: корень проекта, временный каталог и кэши
   языков/пакетных менеджеров), и `pivot_root`/`chroot` в это
   представление перед `execve`. Это ближайший на Linux аналог того, что
   даёт `sandbox-exec` на macOS, — запись файлов вне разрешённых путей
   реально запрещена, а не просто не поощряется, — и это сочетается с
   границей по процессам/ресурсам через cgroup (ниже) для паритета с
   лимитами процессов и памяти Windows job object.
2. **Фильтр seccomp-bpf**, ограничивающий системные вызовы, которые может
   делать изолированная команда (в частности, фильтрация семейства
   `openat`/`open`/`rename`/`unlink` по префиксу пути напрямую не
   выражается в seccomp-bpf, который видит только аргументы системного
   вызова, а не разрешённые пути, — поэтому один только фильтр не может
   воспроизвести политику записи, ограниченную путями, которая есть на
   двух других платформах; его нужно сочетать с подходом bind-mount выше,
   а не заменять им). Seccomp здесь ценнее как второй слой,
   ограничивающий опасные классы системных вызовов напрямую (например,
   `ptrace`, `mount`, загрузку модулей), чем как основной механизм
   изоляции.

Cgroup (`cgroup v2`, `pids.max` и `memory.max`) дал бы эквивалент лимита по
числу процессов и памяти, аналогичный
`JOB_OBJECT_LIMIT_ACTIVE_PROCESS`/`JOB_OBJECT_LIMIT_JOB_MEMORY` у Windows
job object, а `cgroup.kill` даёт ту же гарантию уничтожения при закрытии,
что `TerminateJobObject` даёт на Windows.

Рекомендация: реализовать (1) плюс слой cgroup как политику `ConfineAgent`
для Linux, соответствующую контракту fail-closed, который у
`workspace-write` уже есть на двух других платформах; относиться к (2) как
к дополнительному слою усиления, а не замене. Ни один из подходов не
запланирован; этот раздел существует, чтобы решение, когда оно будет
принято, не пришлось выводить заново.
