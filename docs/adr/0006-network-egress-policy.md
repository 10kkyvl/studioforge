# ADR 0006: Per-agent network egress policy for `run_command`

- Status: Accepted
- Date: 2026-08-15

## Context

[ADR 0005](0005-run-command-confinement.md) gave `run_command` — the one
place a model chooses what executable and arguments actually run — real
OS-level process/filesystem confinement, but left network egress
deliberately untouched (tracked there, and in
[docs/SECURITY.md](../SECURITY.md), as issue #29). A confined command could
still reach anything the network reaches: exfiltrate project source, pull
and run an arbitrary script from anywhere, or talk to a host that has
nothing to do with the build. That gap is issue #29, filed against the
confinement work #27 produced.

An agent-run command legitimately needs *some* network access most of the
time — `npm install`, `go mod download`, `cargo build`, `pip install`, and
`git clone`/`fetch` all reach out to a package registry or a source host as
part of ordinary, requested work. A policy that simply cut all network
access from `run_command` would break the allowlisted build tools
[ADR 0005](0005-run-command-confinement.md) already permits, for no benefit
over `danger-full-access`'s honest "no boundary" stance. The real question
is not "network or no network" but "which hosts, and can that actually be
enforced, or only asked for."

## Decision

Give each agent a `networkPolicy` setting — `unrestricted` (default,
today's behavior), `registry-only`, or `none` — carried inside the same
`ConfinementPolicy` struct [ADR 0005](0005-run-command-confinement.md)
already threads through `internal/processes` for process/filesystem
confinement (`ConfinementPolicy.Network`), not as a second, parallel
mechanism with its own plumbing. Enforcement differs sharply by platform,
and the difference is stated plainly rather than smoothed over:

- **macOS enforces `registry-only` and `none` for real**, through
  `sandbox-exec`. `none` appends `(deny network*)` to the generated SBPL
  profile — no network at all, the same shape [ADR 0005](0005-run-command-confinement.md)
  already uses for `(deny file-write*)`. `registry-only` appends
  `(deny network*)` followed by `(allow network-outbound (remote ip (param
  "PROXY")))` — the *only* network the profile allows is the loopback
  address of a local CONNECT proxy StudioForge starts for that one command
  (`internal/processes/netproxy`). The proxy speaks nothing but HTTP CONNECT
  on port 443 or 80: it reads the target host off the `CONNECT host:443`
  request line, checks it against a fixed allowlist of package-registry and
  source hosts (`registry.npmjs.org`, `registry.yarnpkg.com`,
  `proxy.golang.org`, `sum.golang.org`, `index.crates.io`,
  `static.crates.io`, `crates.io`, `pypi.org`, `files.pythonhosted.org`,
  `github.com`, `codeload.github.com`, `objects.githubusercontent.com`,
  `raw.githubusercontent.com`), and only then dials the real target and
  splices raw bytes in both directions. **It never terminates or inspects
  TLS** — the only thing it ever reads in cleartext is the `CONNECT`
  request line itself, before the splice begins; everything after that is
  opaque ciphertext passing through unexamined. The proxy's address reaches
  the SBPL profile as a `-D PROXY=…` parameter to `sandbox-exec`, referenced
  in the profile text only via `(param "PROXY")`, following the exact
  discipline [ADR 0005](0005-run-command-confinement.md) already established
  for `ROOT`/`HOME`/`TMP` — never interpolated into the profile string
  itself, so a value containing SBPL metacharacters cannot corrupt the
  policy. If no proxy address is available for some reason, building the
  `registry-only` profile fails outright rather than silently falling back
  to no restriction.
- **Windows and Linux cannot enforce `registry-only` or `none` at all**, so
  they do not pretend to: a policy stricter than `unrestricted` on either
  platform makes the command **fail closed before it starts** — the process
  is never launched, and the error names the policy as the cause and states
  plainly that this is a policy refusal, not a network failure, so an
  operator does not spend time debugging a "connection" that was never
  attempted. `STUDIOFORGE_ALLOW_UNCONFINED=1` — the escape hatch that lets
  the general absence of platform confinement degrade to unconfined on a
  platform with no implementation — does **not** lift this refusal. That
  is a deliberate difference, not an oversight: the escape hatch exists so
  contributors and CI can run the test suite on a platform that never
  claimed a filesystem/process boundary in the first place; a network
  policy is different because the operator explicitly asked for a
  *boundary*, and quietly downgrading "I asked for no network" to
  "unrestricted network" the moment the platform can't deliver it would be
  the one place this feature is allowed to lie by omission. Fail-closed, not
  fail-open, is the whole point.
- **Environment scrubbing prevents the operator's own proxy configuration
  from working around `registry-only`.** Before a `registry-only` command
  starts, both cases of every proxy-related environment variable
  (`HTTP_PROXY`/`http_proxy`, `HTTPS_PROXY`/`https_proxy`,
  `ALL_PROXY`/`all_proxy`, `NO_PROXY`/`no_proxy`) are stripped from the
  command's environment and replaced with the local proxy's own address for
  `HTTP_PROXY`/`HTTPS_PROXY`/`ALL_PROXY` and an empty `NO_PROXY` — an
  operator's ambient `NO_PROXY=*`, if one exists, would otherwise route
  every request around the proxy StudioForge is relying on to enforce the
  allowlist, silently turning `registry-only` into `unrestricted`.
- **A tool that ignores the proxy environment variables gets no network at
  all under `registry-only`, not an unrestricted fallback.** The SBPL
  profile denies everything except the proxy's own loopback address; a tool
  that dials directly, ignoring `HTTP_PROXY`/`HTTPS_PROXY`, simply cannot
  connect. This is correct fail-closed behavior — nothing quietly leaks past
  the allowlist — but it is a real, sharp-edged consequence an operator
  needs to know about before it looks like a broken build rather than a
  policy working as designed.

## Why `registry-only` cannot be expressed directly in SBPL, and why a proxy

SBPL (Sandbox Profile Language) can allow or deny network access by
protocol, address, and port — `(remote ip "host:port")` — but it has no
concept of a DNS hostname to filter on, only the resolved IP address the
kernel-level check actually sees. The tempting shortcut is to resolve each
allowlisted registry's hostname once, up front, and bake the resulting IP
addresses into the profile as `(remote ip "<addr>:443")` rules. That
shortcut was considered and rejected — see
[Alternatives considered](#alternatives-considered) below — because it is
fragile to the point of not working at all against how these hosts are
actually served: CDN-fronted (the same registry answers from a different
edge IP depending on where the resolver sits), IP-rotating (an operational
change on the registry's side that requires no cooperation from
StudioForge to break the profile), frequently dual-stack (IPv4 and IPv6
answers differ, and both need to be current), and sometimes redirecting to
a different host entirely (a package tarball served from a separate
storage domain). A profile built from a resolved snapshot goes stale on a
timescale StudioForge does not control and cannot detect from inside a
single `run_command` invocation.

A local CONNECT proxy sidesteps the whole problem: the proxy makes the
decision *after* seeing the plaintext hostname the client asked to connect
to (visible in the `CONNECT host:443` line, since TLS has not started yet
at that point), so the allowlist check is against the name the tool
actually intended to reach, not against whatever IP that name happened to
resolve to at profile-generation time. SBPL's job shrinks to the one thing
it is actually good at — allow network only to one fixed loopback address —
and the hostname-aware decision moves into StudioForge's own Go code, where
maintaining and testing an allowlist is ordinary work rather than a losing
race against DNS.

## Observability, and its honest limits

Every `run_command` invocation — regardless of policy, regardless of
whether the command itself succeeds — publishes a `network` run event
carrying the policy that was requested, whether it was actually enforced,
the platform, and an egress value: `unknown`, `observed`, or `blocked`.
`blocked` records `none`; `observed` records `registry-only`, backed by the
proxy's own per-host attempt log, since the proxy is directly in the path
and genuinely knows what it allowed or refused. These events are excluded
from the `event_retention_days` auto-prune that otherwise ages out verbose
run events, specifically so this one audit trail does not quietly disappear
along with routine tool-call noise.

`unrestricted` is the interesting case, because nothing is in the path to
observe it directly — there is no proxy, and no SBPL rule keyed to a
specific policy. On Windows, StudioForge samples anyway: once a second, it
lists the process IDs currently inside the confined command's Job Object
and intersects that with the operating system's live TCP connection table,
producing a real, if imperfect, egress record for a policy that otherwise
enforces nothing. Read that limitation for exactly what it is and no more:
this is a periodic snapshot of a live table, not a trace of every
connection event. A short-lived connection that opens and closes entirely
between two one-second samples is invisible to it. UDP traffic is invisible
to it outright — the underlying table is TCP-only. A positive result is
solid evidence: the sampled process was, at that moment, connected to that
remote endpoint. The *absence* of a positive result is not solid evidence
of anything — it means only that nothing was caught in the samples taken,
never that nothing happened. This asymmetry has to be preserved in every
surface that reports it: "no egress observed" must never be presented, worded,
or logged as "no network access occurred". A polling-based observer that
gets read as a firewall is worse than no observer at all, because it
invites exactly the kind of confidence this feature does not earn on
Windows and Linux.

## What this policy does not cover

Stated explicitly, so it is not discovered by surprise:

- **StudioForge's own outbound traffic** — the daemon's HTTP calls to
  OpenRouter's API, or the Claude CLI's own calls to its vendor API — is
  the product's traffic, not an agent's, and is untouched by this policy
  regardless of what `networkPolicy` an agent is configured with.
- **Studio MCP** is local IPC with an already-open Roblox Studio process,
  not network egress in any sense this policy models.
- **git over SSH** is not specially handled. Under `registry-only`, `git`
  is one of the allowlisted `run_command` tools and its HTTPS remotes are
  forced through the proxy by the environment scrub above, but SSH-based
  remotes do not honor `HTTP_PROXY`/`HTTPS_PROXY` at all — they are simply
  refused by the SBPL `(deny network*)` rule that only carves out the
  proxy's own address, the same as any other tool that dials directly.
- **A tool without proxy-environment-variable support** gets no network
  under `registry-only`, as already stated above — this is listed again
  here because it is the single most likely thing to be mistaken for a bug
  report.
- **The Claude Code provider path is entirely unaffected.** `networkPolicy`
  is wired only into the in-process agent loop OpenRouter (and NVIDIA,
  which is built on the same loop through `openrouter.NewCompatible`)
  runs — specifically its `run_command` tool. A Claude run uses Claude
  Code's own `Bash` tool, not `agenttools.run_command`, and carries no
  network policy at all.

## Alternatives considered

1. **Resolve each allowlisted registry hostname to an IP address ahead of
   time and bake that into the SBPL profile.** The obvious-looking
   shortcut, and the one rejected above in the most detail: it degrades
   from fragile to simply wrong on any registry that is CDN-fronted,
   IP-rotating, dual-stack, or redirects to a separate storage host — which
   describes essentially every registry on the default allowlist. It also
   requires StudioForge to re-resolve and rebuild the profile on some
   schedule it has no principled way to choose, trading a hard security
   boundary for a maintenance burden that silently breaks builds when it
   falls behind.
2. **`JOBOBJECT_NET_RATE_CONTROL` on Windows.** Windows Job Objects can cap
   a job's network bandwidth. Rejected as the mechanism for this feature
   because it throttles, it does not block: a rate-limited connection to a
   disallowed host still completes, just slowly, which is not what `none`
   or `registry-only` promise and would create exactly the kind of illusion
   of a boundary this document elsewhere insists on not creating. It might
   be a reasonable *addition* for a future denial-of-service concern, but
   it is not a substitute for an allow/deny decision.
3. **A Windows Filtering Platform driver, or any other kernel-level
   firewall component.** This is the only mechanism that would let Windows
   genuinely enforce `registry-only`/`none` the way macOS does. Rejected for
   this ADR because StudioForge ships no driver of any kind today, and
   adding one is a materially larger commitment — driver signing, kernel
   compatibility across Windows versions, a much larger blast radius if it
   misbehaves — than this feature justifies on its own. If Windows
   enforcement is ever built, it likely starts here, but that is a
   separate decision with its own review, not a rider on this one.
4. **A MITM proxy that terminates and re-signs TLS**, so the proxy itself
   could inspect and filter by full URL rather than by CONNECT hostname
   alone. Rejected because it would require installing a StudioForge root
   certificate into the operator's own trust store to avoid every TLS
   connection failing verification — a materially bigger ask of trust and
   attack surface than a policy feature scoped to `run_command` should need,
   and it would mean StudioForge decrypting package-manager traffic that
   may itself carry credentials (private registry tokens, for instance).
   CONNECT-level hostname filtering gets the actual goal — is this command
   allowed to talk to this host — without needing to see inside the
   connection at all.

## Consequences

- `registry-only` is a real, tested boundary on macOS and a documented,
  fail-closed refusal everywhere else — an operator who wants it enforced
  today needs a Mac; an operator on Windows or Linux who sets it gets an
  honest refusal to run the command at all, not a false sense of
  protection.
- The default remains `unrestricted` — today's behavior — so nothing
  changes for an existing agent unless an operator opts in.
- An agent's build step can fail under `registry-only` for reasons that
  look like network trouble but are policy working as intended: a registry
  or CDN host not yet on the allowlist, or a tool that does not honor
  `HTTP(S)_PROXY`. Extending the allowlist is a code change, not a runtime
  setting, as of this ADR.
- Windows network observability (`GetExtendedTcpTable` sampled once a
  second) must keep being described, everywhere it is surfaced, as
  evidence of egress rather than proof of its absence — see
  [docs/KNOWN_LIMITATIONS.md](../KNOWN_LIMITATIONS.md) and
  [docs/SECURITY.md](../SECURITY.md).
- Network audit events staying outside `event_retention_days` pruning means
  a very long-lived project accumulates them indefinitely; that trade was
  made deliberately, on the same reasoning that keeps chat-history messages
  out of the same prune.

---

# ADR 0006: Сетевая политика `run_command` для каждого агента (Русский)

- Статус: Принято
- Дата: 2026-08-15

## Контекст

[ADR 0005](0005-run-command-confinement.md) дал `run_command` — единственному
месту, где модель сама выбирает, какой исполняемый файл и с какими
аргументами запустить, — настоящую OS-изоляцию по процессам и файловой
системе, но намеренно не тронул сетевой доступ (отслеживается там же и в
[docs/SECURITY.md](../SECURITY.md) как issue #29). Изолированная команда
всё равно могла дотянуться до чего угодно в сети: выгрузить исходники
проекта, скачать и выполнить произвольный скрипт откуда угодно, или
обратиться к хосту, не имеющему отношения к сборке. Это и есть issue #29,
заведённый поверх работы по изоляции #27.

Команде, запущенной агентом, в большинстве случаев легитимно нужен
какой-то сетевой доступ — `npm install`, `go mod download`, `cargo build`,
`pip install` и `git clone`/`fetch` все обращаются к реестру пакетов или
хосту с исходниками как часть обычной, запрошенной работы. Политика,
просто отрезающая весь сетевой доступ у `run_command`, сломала бы
разрешённые сборочные инструменты, которые уже позволяет
[ADR 0005](0005-run-command-confinement.md), не давая при этом ничего
поверх честной позиции «границы нет» у `danger-full-access`. Реальный
вопрос не «сеть или без сети», а «какие хосты, и можно ли это реально
обеспечить, а не просто попросить».

## Решение

Дать каждому агенту настройку `networkPolicy` — `unrestricted` (по
умолчанию, сегодняшнее поведение), `registry-only` или `none` — которая
едет внутри той же структуры `ConfinementPolicy`, что
[ADR 0005](0005-run-command-confinement.md) уже прокидывает через
`internal/processes` для изоляции процессов/файловой системы
(`ConfinementPolicy.Network`), а не отдельным, параллельным механизмом со
своей собственной проводкой. Enforcement резко различается по платформам,
и это различие сформулировано прямо, а не сглажено:

- **macOS реально обеспечивает `registry-only` и `none`** через
  `sandbox-exec`. `none` добавляет `(deny network*)` к сгенерированному
  SBPL-профилю — сети нет вообще, та же форма, что
  [ADR 0005](0005-run-command-confinement.md) уже использует для
  `(deny file-write*)`. `registry-only` добавляет `(deny network*)`, а
  затем `(allow network-outbound (remote ip (param "PROXY")))` — *единственная*
  сеть, которую разрешает профиль, — это loopback-адрес локального
  CONNECT-прокси, который StudioForge поднимает на время этой одной
  команды (`internal/processes/netproxy`). Прокси не говорит ни на чём,
  кроме HTTP CONNECT на порт 443 или 80: он читает целевой хост из строки
  запроса `CONNECT host:443`, сверяет его с фиксированным allowlist хостов
  пакетных реестров и репозиториев исходников (`registry.npmjs.org`,
  `registry.yarnpkg.com`, `proxy.golang.org`, `sum.golang.org`,
  `index.crates.io`, `static.crates.io`, `crates.io`, `pypi.org`,
  `files.pythonhosted.org`, `github.com`, `codeload.github.com`,
  `objects.githubusercontent.com`, `raw.githubusercontent.com`), и только
  затем набирает реальный адрес назначения и сращивает сырые байты в обе
  стороны. **Он никогда не вскрывает и не инспектирует TLS** — единственное,
  что он вообще читает в открытом виде, это сама строка запроса `CONNECT`,
  до начала сращивания; всё, что идёт после, — непрозрачный шифротекст,
  проходящий насквозь без анализа. Адрес прокси попадает в SBPL-профиль как
  параметр `-D PROXY=…` для `sandbox-exec`, на который в тексте профиля
  ссылаются только через `(param "PROXY")`, следуя ровно той дисциплине,
  что [ADR 0005](0005-run-command-confinement.md) уже установил для
  `ROOT`/`HOME`/`TMP`, — никогда не подставляется в текст профиля напрямую,
  поэтому значение с метасимволами языка SBPL не может испортить политику.
  Если по какой-то причине адрес прокси недоступен, построение профиля
  `registry-only` завершается ошибкой, а не тихим откатом к отсутствию
  ограничений.
- **Windows и Linux вообще не могут обеспечить `registry-only` или
  `none`**, поэтому и не притворяются, что могут: политика строже
  `unrestricted` на любой из этих платформ приводит к тому, что команда
  **отказывает закрыто до старта** — процесс никогда не запускается, а
  ошибка называет политику причиной и прямо говорит, что это отказ
  политики, а не сетевой сбой, чтобы оператор не тратил время на отладку
  «соединения», которое даже не пыталось установиться.
  `STUDIOFORGE_ALLOW_UNCONFINED=1` — escape hatch, позволяющий общему
  отсутствию платформенной изоляции деградировать до неизолированного
  запуска на платформе без реализации, — этот отказ **не** снимает. Это
  намеренное различие, а не недосмотр: escape hatch существует, чтобы
  контрибьюторы и CI могли запускать тестовый набор на платформе, которая
  изначально не заявляла границу по файловой системе/процессам; сетевая
  политика — другое дело, потому что оператор явно попросил *границу*, и
  тихо понижать «я просил без сети» до «сеть без ограничений» в момент,
  когда платформа не может это дать, было бы единственным местом, где эта
  функция имела бы право солгать умолчанием. Отказ закрыто, а не открыто, —
  и есть весь смысл.
- **Очистка окружения не даёт собственной настройке прокси оператора
  обойти `registry-only`.** Перед запуском команды под `registry-only` обе
  регистровые формы каждой переменной окружения, связанной с прокси
  (`HTTP_PROXY`/`http_proxy`, `HTTPS_PROXY`/`https_proxy`,
  `ALL_PROXY`/`all_proxy`, `NO_PROXY`/`no_proxy`), удаляются из окружения
  команды и заменяются адресом локального прокси для
  `HTTP_PROXY`/`HTTPS_PROXY`/`ALL_PROXY` и пустым `NO_PROXY` — операторский
  `NO_PROXY=*`, если он есть, иначе увёл бы весь трафик мимо прокси, на
  который опирается enforcement allowlist, тихо превратив `registry-only`
  в `unrestricted`.
- **Инструмент, игнорирующий переменные окружения прокси, под
  `registry-only` не получает сети вообще, а не неограниченную сеть.**
  SBPL-профиль запрещает всё, кроме loopback-адреса самого прокси;
  инструмент, соединяющийся напрямую и игнорирующий
  `HTTP_PROXY`/`HTTPS_PROXY`, просто не может подключиться. Это правильное
  fail-closed поведение — ничего не просачивается мимо allowlist тихо, —
  но это реальное, острое следствие, о котором оператору нужно знать
  заранее, чтобы оно не выглядело как сломанная сборка вместо политики,
  работающей как задумано.

## Почему `registry-only` невозможно выразить напрямую в SBPL, и почему прокси

SBPL (Sandbox Profile Language) умеет разрешать или запрещать сетевой
доступ по протоколу, адресу и порту — `(remote ip "host:port")`, — но у
него нет понятия DNS-имени, по которому можно фильтровать, — только
разрешённый IP-адрес, который реально видит проверка на уровне ядра.
Соблазнительное короткое решение — разрешить имена всех хостов из
allowlist в IP-адреса один раз заранее и зашить получившиеся адреса в
профиль как правила `(remote ip "<addr>:443")`. Это решение было
рассмотрено и отклонено — см.
[Рассмотренные альтернативы](#рассмотренные-альтернативы-1) ниже — потому
что оно хрупко до полной неработоспособности применительно к тому, как эти
хосты реально обслуживаются: за CDN (один и тот же реестр отвечает с
разных edge-адресов в зависимости от того, где сидит резолвер), с ротацией
IP (операционное изменение на стороне реестра, не требующее никакого
участия StudioForge, чтобы сломать профиль), часто dual-stack (ответы
IPv4 и IPv6 различаются, и оба должны быть актуальны), а иногда с
редиректом на совсем другой хост (архив пакета, отдаваемый с отдельного
storage-домена). Профиль, построенный из снимка резолва, устаревает на
шкале времени, которую StudioForge не контролирует и не может
обнаружить изнутри одного вызова `run_command`.

Локальный CONNECT-прокси обходит всю эту проблему: прокси принимает
решение *после* того, как увидел открытое имя хоста, к которому клиент
попросил подключиться (видно в строке `CONNECT host:443`, поскольку TLS
на этом этапе ещё не начался), — поэтому проверка allowlist идёт против
имени, к которому инструмент реально намеревался обратиться, а не против
того, во что это имя резолвилось в момент генерации профиля. Задача SBPL
сужается до единственной вещи, в которой он реально хорош, — разрешить
сеть только на один фиксированный loopback-адрес, — а решение,
учитывающее имя хоста, переезжает в собственный Go-код StudioForge, где
поддержка и тестирование allowlist — обычная работа, а не проигрышная
гонка с DNS.

## Наблюдаемость и её честные пределы

Каждый вызов `run_command` — независимо от политики, независимо от того,
успешна ли сама команда, — публикует событие запуска `network` с
запрошенной политикой, признаком того, была ли она реально обеспечена,
платформой и значением egress: `unknown`, `observed` или `blocked`.
`blocked` фиксируется для `none`; `observed` — для `registry-only`,
опираясь на собственный журнал попыток прокси по хостам, поскольку прокси
физически стоит на пути и реально знает, что он разрешил или отклонил. Эти
события исключены из автоочистки `event_retention_days`, которая иначе
удаляет устаревшие подробные события запуска, — именно для того, чтобы
этот аудиторский след не исчез тихо вместе с обычным шумом вызовов
инструментов.

`unrestricted` — интересный случай, потому что ничто не стоит на пути,
чтобы наблюдать его напрямую: прокси нет, и нет SBPL-правила, привязанного
к конкретной политике. На Windows StudioForge всё равно снимает показания:
раз в секунду он получает список PID процессов, находящихся сейчас внутри
Job Object изолированной команды, и пересекает его с текущей таблицей TCP-
соединений операционной системы, получая реальную, но неполную запись
egress для политики, которая иначе вообще ничего не обеспечивает. Это
ограничение нужно читать ровно так, как оно есть, и не более: это
периодический снимок живой таблицы, а не трассировка каждого события
соединения. Короткое соединение, которое открылось и закрылось целиком
между двумя секундными снимками, для него невидимо. UDP-трафик для него
невидим вообще — базовая таблица только TCP. Положительный результат —
надёжное свидетельство: сэмплированный процесс в этот момент был
подключён к этой удалённой точке. *Отсутствие* положительного результата
надёжным свидетельством чего-либо не является — оно означает лишь, что
ничего не попало в сделанные снимки, но никогда не то, что ничего не
произошло. Эту асимметрию нужно сохранять на каждой поверхности, где
результат показывается: «egress не замечен» никогда не должно подаваться,
формулироваться или логироваться как «сетевого доступа не было».
Наблюдатель на основе опроса, который прочитают как файрвол, хуже, чем
отсутствие наблюдателя вообще, потому что он приглашает именно ту
уверенность, которую эта функция не заслуживает на Windows и Linux.

## Что эта политика не покрывает

Сформулировано прямо, чтобы не обнаружилось внезапно:

- **Собственный исходящий трафик StudioForge** — HTTP-вызовы демона к API
  OpenRouter, или собственные вызовы CLI Claude к его vendor API, — это
  трафик продукта, а не агента, и эта политика его не трогает независимо
  от того, какой `networkPolicy` настроен у агента.
- **Studio MCP** — локальный IPC с уже открытым процессом Roblox Studio,
  а не сетевой egress в том смысле, который моделирует эта политика.
- **git по SSH** отдельно не обрабатывается. Под `registry-only` `git` —
  один из разрешённых инструментов `run_command`, и его HTTPS-remote'ы
  принудительно направляются через прокси очисткой окружения выше, но
  SSH-remote'ы вообще не соблюдают `HTTP_PROXY`/`HTTPS_PROXY` — они просто
  отклоняются правилом SBPL `(deny network*)`, вырезающим только адрес
  самого прокси, точно так же, как любой другой инструмент, соединяющийся
  напрямую.
- **Инструмент без поддержки переменных окружения прокси** под
  `registry-only` не получает сети — это уже сказано выше и повторено
  здесь отдельно, потому что это самое вероятное, что примут за баг-репорт.
- **Путь провайдера Claude Code целиком не затронут.** `networkPolicy`
  подключён только к внутреннему agent loop, которым пользуется OpenRouter
  (и NVIDIA, построенный на том же loop через `openrouter.NewCompatible`),
  — конкретно к его инструменту `run_command`. Запуск Claude использует
  собственный инструмент `Bash` Claude Code, а не `agenttools.run_command`,
  и не несёт никакой сетевой политики вообще.

## Рассмотренные альтернативы

1. **Заранее разрешить имена хостов всех реестров из allowlist в IP-адреса
   и зашить их в SBPL-профиль.** Самое очевидное на вид короткое решение и
   отклонённое здесь подробнее всего: оно деградирует от хрупкого до
   попросту неверного на любом реестре за CDN, с ротацией IP, dual-stack
   или с редиректом на отдельный storage-хост — а это по сути описывает
   почти каждый реестр из дефолтного allowlist. Оно также требует, чтобы
   StudioForge переразрешал имена и пересобирал профиль по какому-то
   расписанию, которое не на чем принципиально основать, — обменивая
   жёсткую границу безопасности на бремя обслуживания, тихо ломающее
   сборки, как только оно отстаёт.
2. **`JOBOBJECT_NET_RATE_CONTROL` на Windows.** Windows Job Object умеет
   ограничивать пропускную способность сети для job. Отклонено как
   механизм для этой функции, потому что это throttling, а не блокировка:
   соединение с запрещённым хостом с ограниченной скоростью всё равно
   завершается, просто медленно, — а это не то, что обещают `none` или
   `registry-only`, и создало бы ровно ту иллюзию границы, от которой этот
   документ в остальном намеренно отказывается. Это может быть разумным
   *дополнением* на случай будущей проблемы denial-of-service, но не
   заменой решению allow/deny.
3. **Драйвер Windows Filtering Platform или любой другой файрвол уровня
   ядра.** Это единственный механизм, который позволил бы Windows реально
   обеспечивать `registry-only`/`none` так же, как это делает macOS.
   Отклонено для этого ADR, потому что StudioForge сегодня не поставляет
   никакого драйвера, а добавление его — существенно более крупное
   обязательство (подпись драйвера, совместимость с ядром на разных
   версиях Windows, значительно больший blast radius в случае сбоя), чем
   оправдывает эта функция сама по себе. Если enforcement для Windows
   когда-нибудь будет построен, он, вероятно, начнётся отсюда, но это
   отдельное решение со своим собственным review, а не довесок к этому.
4. **MITM-прокси, вскрывающий и переподписывающий TLS**, чтобы сам прокси
   мог инспектировать и фильтровать по полному URL, а не только по имени
   хоста из CONNECT. Отклонено, потому что потребовало бы установки
   корневого сертификата StudioForge в системное хранилище доверия
   оператора, чтобы избежать провала проверки TLS на каждом соединении, —
   существенно больший запрос доверия и площади атаки, чем должна требовать
   функция политики, ограниченная `run_command`, и означало бы, что
   StudioForge расшифровывает трафик пакетных менеджеров, который сам
   может нести учётные данные (например, токены приватных реестров).
   Фильтрация по имени хоста на уровне CONNECT достигает реальной цели —
   разрешено ли этой команде говорить с этим хостом — вообще не заглядывая
   внутрь соединения.

## Последствия

- `registry-only` — реальная, протестированная граница на macOS и
  документированный, fail-closed отказ везде ещё — оператору, которому
  нужен реальный enforcement сегодня, нужен Mac; оператор на Windows или
  Linux, задавший эту политику, получает честный отказ запускать команду
  вообще, а не ложное чувство защищённости.
- По умолчанию остаётся `unrestricted` — сегодняшнее поведение, — поэтому
  для существующего агента ничего не меняется, пока оператор не включит
  политику сам.
- Шаг сборки агента может провалиться под `registry-only` по причинам,
  выглядящим как сетевые неполадки, но являющимся политикой, работающей
  как задумано: хост реестра или CDN, ещё не попавший в allowlist, или
  инструмент, не соблюдающий `HTTP(S)_PROXY`. Расширение allowlist на
  момент этого ADR — изменение кода, а не настройка времени выполнения.
- Наблюдаемость сети на Windows (`GetExtendedTcpTable`, снимаемая раз в
  секунду) должна везде, где она показывается, по-прежнему описываться как
  свидетельство egress, а не доказательство его отсутствия — см.
  [docs/KNOWN_LIMITATIONS.md](../KNOWN_LIMITATIONS.md) и
  [docs/SECURITY.md](../SECURITY.md).
- То, что сетевые аудиторские события остаются вне очистки
  `event_retention_days`, означает, что очень долгоживущий проект
  накапливает их бессрочно; это решение принято намеренно, по той же
  логике, что оставляет сообщения истории чата вне той же очистки.
