# StudioForge v0.5.0-rc.1

_v0.5.0-rc.1 is a release candidate for the public beta: a reworked first-run setup experience, task
dependencies that are now actually enforced instead of just tracked, two privacy improvements (log
redaction and automatic history cleanup), and a hardened release pipeline. / v0.5.0-rc.1 — это
релиз-кандидат публичной беты: переработанный первый запуск, зависимости задач, которые теперь
реально соблюдаются, а не просто отслеживаются, два улучшения приватности (редактирование логов и
автоматическая очистка истории) и укреплённый конвейер релизов._

## English

### Setup & onboarding

- **The first-run setup wizard is reworked.** Checks now show a clear ok / warning / error / missing
  state instead of a flat list, and they're grouped into required prerequisites, AI providers, and
  optional feature integrations, so it's obvious what's actually blocking you versus what's just
  optional. Safe mode and demo (`--mock`) mode are explained in plain terms right where you'd pick
  them. Each fixable problem links straight to the right Settings card instead of leaving you to
  guess. Setup only refuses to finish on a real database or data-directory error — everything else
  offers a clearly labeled "continue anyway" limited mode. New users with no projects yet land
  straight in the New Project flow instead of an empty dashboard.
- **Real screenshots in the README**, in both English and Russian: an actual dashboard image and a
  short "see it in action" section, plus a refreshed social preview image for link previews. All of
  them are captured automatically from the app's own demo mode, so they show the real product instead
  of stale mockups.
- **A new NVIDIA connection check.** Doctor and the setup wizard now report the real state of an
  NVIDIA API key — configured, invalid, missing — the same way they already did for OpenRouter.

### Tasks & runs

- **Task dependencies are now enforced, not just tracked.** If you mark a task as depending on another
  one, StudioForge now actually refuses to start a run on it until that dependency (and anything it in
  turn depends on) is marked complete — previously the dependency link was recorded but nothing
  stopped you from starting anyway. Blocked tasks are visibly marked in the chat's task picker and on
  the task board, the "start" action is disabled on them, and the app tells you exactly which
  dependencies are still unfinished. Resuming a run or letting the app retry a failed step
  automatically still works normally, since that's continuing work already in progress rather than
  starting something new.

### Privacy & data

- **StudioForge's own application logs are now scrubbed for secrets**, the same way diagnostic
  bundles and run history already were. API keys, auth tokens, cookies, and session tokens are
  automatically blanked out before anything is written to a log line, so a log file is safer to share
  when asking for help.
- **Old run history now cleans itself up automatically.** By default, StudioForge keeps the detailed,
  step-by-step trace of a finished run for 90 days, then quietly prunes it in the background — your
  chat messages, run summaries, costs, and checkpoints are never touched, only the verbose internal
  trace. The retention period is configurable via the `event_retention_days` setting (0 turns the
  cleanup off), and `studioforge maintenance --prune-events` triggers a cleanup by hand.

### Release quality

- **The docs were brought back in line with what the app actually does.** Provider descriptions match
  reality (Claude Code, OpenRouter, NVIDIA NIM; Codex is documented as a removed legacy provider whose
  run history stays readable), features that used to be described as "not reachable yet" are now
  described as working (per-run diff, rollback, Rojo live-sync, Studio session discovery, project
  memory, task dependencies), and the known-limitations list reflects what's genuinely still missing
  today (there's no memory-management UI yet) rather than gaps that have since been closed.
- **A documentation consistency check** now runs in CI and fails the build if stale wording or a
  placeholder link sneaks back into the docs.
- **A release preflight check** verifies the release version agrees across the git tag,
  `web/package.json`, these release notes, and the changelog before a release is cut.
- **Release artifact verification** checks ZIP contents, validates checksums, and confirms the macOS
  app kept its executable bit.
- **Native smoke tests** run the packaged Windows and macOS binaries with `--mock` before a release is
  considered good, catching a broken build before it ships.
- **More reliable packaging**, including a packaging step that no longer leaves stale files behind
  from a previous run.

### Before installing

- This is a release candidate for the public beta. Back up important Roblox projects.
- NVIDIA and OpenRouter require their own API keys. Model availability and limits belong to the
  selected provider.
- Screenshot understanding works only with a model marked as vision-capable.
- Windows and macOS packages are currently unsigned.
- The bootstrap token used to authenticate a freshly opened tab is, as before, only ever accepted from
  the URL hash.

### Downloads and verification

- `StudioForge-v0.5.0-rc.1-windows-amd64.zip` — Windows (amd64).
- `StudioForge-v0.5.0-rc.1-macos-arm64.zip` — macOS (arm64).
- `SHA256SUMS.txt` — checksums for both archives; verify with `sha256sum -c SHA256SUMS.txt` (or
  `Get-FileHash` on Windows) before running an unsigned binary you downloaded.
- Both binaries are unsigned development builds — Windows SmartScreen and macOS Gatekeeper will warn on
  first launch; that is expected for this release.

### Reporting issues

Please report problems on the project's GitHub Issues page, including your OS, the exact version
string from `--version`, and, where possible, a `studioforge doctor --bundle` diagnostic bundle
(secrets are redacted before export).

## Русский

### Установка и первый запуск

- **Мастер первого запуска переработан.** Проверки теперь показывают чёткий статус ok / warning /
  error / missing вместо плоского списка и сгруппированы на обязательные предпосылки, AI-провайдеров
  и опциональные интеграции — сразу понятно, что реально блокирует запуск, а что необязательно. Safe
  mode и демо-режим (`--mock`) объяснены простыми словами прямо там, где вы их выбираете. Каждая
  решаемая проблема ведёт прямо на нужную карточку Settings, а не оставляет гадать. Завершение
  настройки отказывает только при настоящей ошибке базы данных или каталога данных — во всех
  остальных случаях предлагается явно подписанный ограниченный режим "продолжить всё равно". Новые
  пользователи без единого проекта сразу попадают в сценарий создания проекта, а не на пустой
  dashboard.
- **Настоящие скриншоты в README** на английском и русском: реальное изображение dashboard и
  короткий раздел "как это выглядит", плюс обновлённая картинка для превью ссылок в соцсетях. Все они
  получены автоматически из встроенного демо-режима приложения, поэтому показывают настоящий продукт,
  а не устаревшие макеты.
- **Новая проверка подключения NVIDIA.** Doctor и мастер настройки теперь показывают реальное
  состояние ключа NVIDIA API — настроен, недействителен, отсутствует — так же, как уже делали для
  OpenRouter.

### Задачи и запуски

- **Зависимости задач теперь реально соблюдаются, а не только отслеживаются.** Если вы отметили, что
  задача зависит от другой, StudioForge теперь действительно откажется запускать run по ней, пока эта
  зависимость (и всё, от чего зависит она сама) не будет отмечена завершённой — раньше связь
  сохранялась, но ничто не мешало запустить run всё равно. Заблокированные задачи явно помечены в
  выборе задачи в чате и на доске задач, кнопка запуска у них отключена, а приложение показывает,
  какие именно зависимости ещё не завершены. Продолжение (Resume) уже идущего run'а или
  автоматический повтор упавшего шага по-прежнему работают как обычно, поскольку это продолжение уже
  начатой работы, а не старт новой.

### Приватность и данные

- **Собственные логи приложения StudioForge теперь тоже редактируются**, так же, как уже
  редактировались диагностические архивы и история запусков. API-ключи, токены авторизации, cookies и
  токены сессии автоматически заменяются заглушкой ещё до записи в лог, так что файл лога безопаснее
  прикладывать к обращению за помощью.
- **Старая история запусков теперь очищается сама.** По умолчанию StudioForge хранит подробный,
  пошаговый след завершённого run'а 90 дней, а затем незаметно удаляет его в фоне — сообщения чата,
  сводки запусков, стоимость и checkpoints при этом никогда не трогаются, устаревает только подробный
  внутренний след. Срок хранения задаётся настройкой `event_retention_days` (0 отключает очистку), а
  команда `studioforge maintenance --prune-events` запускает очистку вручную.

### Качество релиза

- **Документация снова соответствует тому, что реально делает приложение.** Описания провайдеров
  отражают реальность (Claude Code, OpenRouter, NVIDIA NIM; Codex описан как удалённый устаревший
  провайдер, история запусков которого остаётся читаемой), функции, ранее названные "пока
  недостижимыми", теперь описаны как рабочие (diff по запуску, откат, Rojo live-sync, обнаружение
  сессий Studio, память проекта, зависимости задач), а список известных ограничений отражает то, чего
  реально ещё не хватает сегодня (пока нет интерфейса управления памятью), а не закрытые с тех пор
  пробелы.
- **Проверка согласованности документации** теперь выполняется в CI и останавливает сборку, если в
  документацию вернётся устаревшая формулировка или заглушка вместо ссылки.
- **Предрелизная проверка** сверяет версию релиза в git-теге, `web/package.json`, этих release notes
  и changelog перед сборкой релиза.
- **Проверка артефактов релиза** проверяет содержимое ZIP, контрольные суммы и то, что приложение
  macOS сохранило бит исполняемости.
- **Нативные дымовые тесты** запускают собранные бинарники Windows и macOS с `--mock` перед тем, как
  релиз считается годным, — это ловит сломанную сборку до того, как она попадёт к пользователям.
- **Более надёжная сборка пакетов**, включая шаг упаковки, который больше не оставляет позади
  устаревшие файлы от предыдущего запуска.

### Перед установкой

- Это релиз-кандидат публичной беты. Делайте резервные копии важных Roblox-проектов.
- Для NVIDIA и OpenRouter нужны отдельные API-ключи. Доступность моделей и лимиты задаёт провайдер.
- Скриншоты понимают только модели с поддержкой vision.
- Сборки Windows и macOS пока не подписаны.
- Bootstrap-токен для авторизации свежеоткрытой вкладки, как и раньше, принимается только из URL hash.

### Загрузка и проверка

- `StudioForge-v0.5.0-rc.1-windows-amd64.zip` — Windows (amd64).
- `StudioForge-v0.5.0-rc.1-macos-arm64.zip` — macOS (arm64).
- `SHA256SUMS.txt` — контрольные суммы обоих архивов; проверяйте через `sha256sum -c SHA256SUMS.txt`
  (или `Get-FileHash` в Windows) перед запуском скачанного неподписанного бинарника.
- Оба бинарника — неподписанные сборки для разработки: Windows SmartScreen и macOS Gatekeeper выдадут
  предупреждение при первом запуске — это ожидаемо для этого релиза.

### Сообщить о проблеме

Пожалуйста, сообщайте о проблемах на странице GitHub Issues проекта, указав вашу ОС, точную строку
версии из `--version` и, по возможности, диагностический пакет `studioforge doctor --bundle` (секреты
редактируются перед экспортом).
