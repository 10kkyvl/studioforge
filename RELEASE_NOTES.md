# StudioForge v0.5.0-rc.2

_v0.5.0-rc.2 is a fix-only release candidate on top of rc.1. It repairs the Restart button, makes
Git checkpoints and stuck-run detection work on every provider instead of only Claude, stops the
Studio launcher from leaving processes behind on Windows, and clears up a handful of smaller
annoyances. No new features, nothing removed. / v0.5.0-rc.2 — релиз-кандидат поверх rc.1, целиком
про исправления. Починена кнопка Restart, точки отката Git и защита от зависших запусков теперь
работают со всеми провайдерами, а не только с Claude, лаунчер Studio перестал оставлять после себя
процессы в Windows, плюс исправлена россыпь мелочей. Новых функций нет, ничего не удалено._

## English

### Fixes you will notice

- **Restart works properly again.** Restarting a run used to lose the chat it belonged to — the
  restarted run simply never showed up in the conversation. It also silently ran without the
  stuck-run safety net, without the playtest validation you had switched on for that agent, and, on
  OpenRouter and NVIDIA, with no memory of the conversation so far. A restart now behaves like any
  other message in that chat, and takes its own Git checkpoint first.

- **Every provider gets a Git checkpoint before it starts.** Only Claude runs were snapshotted
  before, so "roll back this run" had nothing to roll back to after an OpenRouter or NVIDIA run
  changed your files. Now any run that can change files gets a rollback point first. Plan-mode runs,
  which change nothing, still skip it.

- **Stuck-run detection works on OpenRouter and NVIDIA too.** The check that notices an agent going
  in circles only understood Claude's output, so on other providers the setting was switched on but
  the only thing that could actually fire was the "no output at all for ten minutes" timeout. Now a
  repeating loop is caught on every provider, and editing a file counts as progress and resets the
  counter, exactly as it does for Claude.

- **A retried answer no longer appears twice.** When a provider hiccups mid-answer, StudioForge
  retries automatically and the model starts its answer over. The chat used to glue the new answer
  onto the half-finished one. Now the half-finished text is cleared first.

- **`apply_patch` really is all-or-nothing now.** If a multi-file edit failed partway through, the
  files already written stayed changed while the agent was told nothing had applied. Those files are
  now restored.

- **Studio no longer leaves stray processes behind on Windows.** Every check of "is Studio open?"
  started a launcher, and closing it killed only the outer process, leaving `StudioMCP.exe` running.
  Since the Studio badge checks regularly, these piled up over a long session. The whole process
  tree is now shut down properly.

- **Sending the same request twice at once no longer errors out.** Two identical submissions racing
  each other used to make one of them fail with a database error instead of quietly returning the
  run the other one had just started.

- **Settings save cleanly or not at all.** An invalid value used to be caught only while applying,
  by which point some of the other settings in the same save had already been written. Everything is
  now validated before anything is saved.

- **A project is no longer added when its folder could not be prepared.** You used to get an error
  message and the project appeared in the list anyway.

- **Editing an agent no longer fails for leaving fields alone.** Saving an agent without touching
  concurrency or budget used to be rejected.

- **The live view survives loading a long history.** Opening a project with a lot of past activity
  could drop the live connection mid-load, which showed up as the view briefly freezing and
  reconnecting.

- Clipped tool output no longer ends in a broken character, and the scheduler stops accumulating
  bookkeeping entries for every project and model it has ever run.

### Security note worth reading

The `workspace-write` permission level now refuses the most direct ways of running arbitrary code
through an allowed command: `node -e`, `python -c`, `go run`, and `npx` (which downloads and runs any
package you name) all require `danger-full-access` from this release on.

To be clear about what that does and does not mean — and `docs/SECURITY.md` now says this plainly
instead of implying otherwise — this allowlist is a barrier, not a sandbox. An agent that can both
write files and run a build tool can always arrange to execute code, for instance through a test
file, an npm script, or a Makefile. Give `danger-full-access` deliberately, and prefer
`workspace-write` or `read-only` until you have a reason not to.

### Before installing

- This is a release candidate for a public beta. Back up anything you care about; StudioForge takes
  its own daily database backup, but your project files are yours to protect.
- Upgrading from rc.1 needs no migration and no configuration change — replace the binary and start
  it again.
- An agent with `workspace-write` or above can change files in the project folder you point it at.
- Roblox Studio integration needs a single Studio window open, with Studio MCP enabled.
- Claude runs need Claude Code installed and signed in; OpenRouter and NVIDIA runs need an API key
  for the selected provider.
- Screenshot understanding works only with a model marked as vision-capable.
- Windows and macOS packages are currently unsigned.
- The bootstrap token used to authenticate a freshly opened tab is, as before, only ever accepted
  from the URL hash.

### Downloads and verification

- `StudioForge-v0.5.0-rc.2-windows-amd64.zip` — Windows (amd64).
- `StudioForge-v0.5.0-rc.2-macos-arm64.zip` — macOS (arm64).
- `SHA256SUMS.txt` — checksums for both archives; verify with `sha256sum -c SHA256SUMS.txt` (or
  `Get-FileHash` on Windows) before running an unsigned binary you downloaded.
- Both binaries are unsigned development builds — Windows SmartScreen and macOS Gatekeeper will warn
  on first launch; that is expected for this release.

### Reporting issues

Please report problems on the project's GitHub Issues page, including your OS, the exact version
string from `--version`, and, where possible, a `studioforge doctor --bundle` diagnostic bundle
(secrets are redacted before export).

## Русский

### Что вы заметите

- **Кнопка Restart снова работает как надо.** Раньше перезапуск терял чат, к которому относился, —
  перезапущенный run просто не появлялся в переписке. Ещё он молча работал без защиты от зависания,
  без проверки плейтестом, которую вы включили для этого агента, а на OpenRouter и NVIDIA — вообще
  без памяти о предыдущем разговоре. Теперь перезапуск ведёт себя как обычное сообщение в том же
  чате и сначала делает точку отката в Git.

- **Точка отката Git создаётся для всех провайдеров.** Раньше снимок делался только перед запусками
  Claude, поэтому после того, как OpenRouter или NVIDIA меняли ваши файлы, кнопке «откатить запуск»
  было не к чему возвращаться. Теперь точку отката получает любой запуск, который может менять
  файлы. Режим планирования, который ничего не меняет, её по-прежнему не требует.

- **Защита от зависших запусков работает на OpenRouter и NVIDIA.** Проверка, которая замечает, что
  агент ходит по кругу, понимала только формат Claude — на других провайдерах настройка была
  включена, но сработать мог лишь таймаут «десять минут вообще без вывода». Теперь зацикливание
  ловится у всех провайдеров, а правка файла считается прогрессом и сбрасывает счётчик, ровно как у
  Claude.

- **Повторённый ответ больше не дублируется.** Если провайдер сбоит на середине ответа, StudioForge
  автоматически повторяет запрос, и модель начинает ответ заново. Раньше чат приклеивал новый ответ
  к недописанному. Теперь недописанный текст сначала убирается.

- **`apply_patch` действительно «всё или ничего».** Если правка нескольких файлов падала на
  середине, уже записанные файлы оставались изменёнными, хотя агенту сообщалось, что ничего не
  применилось. Теперь такие файлы восстанавливаются.

- **Studio больше не оставляет лишние процессы в Windows.** Каждая проверка «открыта ли Studio»
  запускала лаунчер, а при закрытии убивался только внешний процесс — `StudioMCP.exe` продолжал
  висеть. Значок Studio проверяется регулярно, поэтому за долгую сессию таких процессов набиралось
  много. Теперь дерево процессов закрывается целиком.

- **Одинаковый запрос, отправленный дважды одновременно, больше не даёт ошибку.** Раньше в такой
  гонке один из двух падал с ошибкой базы данных вместо того, чтобы просто вернуть уже запущенный
  run.

- **Настройки сохраняются целиком или не сохраняются вовсе.** Раньше некорректное значение
  отлавливалось только на этапе применения, когда часть остальных настроек из того же сохранения уже
  была записана. Теперь всё проверяется до записи.

- **Проект не добавляется, если его папку не удалось подготовить.** Раньше вы получали ошибку, а
  проект всё равно появлялся в списке.

- **Редактирование агента не падает из-за незаполненных полей.** Сохранение агента без изменения
  «параллельности» и бюджета раньше отклонялось.

- **Живой просмотр переживает загрузку большой истории.** Открытие проекта с большим числом прошлых
  событий могло оборвать живое соединение прямо во время загрузки — со стороны это выглядело как
  короткое зависание и переподключение.

- Обрезанный вывод инструмента больше не заканчивается «битым» символом, а планировщик перестал
  накапливать служебные записи по каждому проекту и модели, которые когда-либо запускались.

### Важное замечание про безопасность

Уровень доступа `workspace-write` теперь запрещает самые прямые способы выполнить произвольный код
через разрешённую команду: `node -e`, `python -c`, `go run` и `npx` (который скачивает и запускает
любой указанный пакет) с этого релиза требуют `danger-full-access`.

Чтобы не создавать ложного впечатления — и `docs/SECURITY.md` теперь говорит об этом прямо, — этот
список разрешённых команд является барьером, а не песочницей. Агент, который может и записывать
файлы, и запускать сборку, при желании всегда сможет выполнить код: через тестовый файл, npm-скрипт
или Makefile. Выдавайте `danger-full-access` осознанно и по умолчанию оставайтесь на
`workspace-write` или `read-only`.

### Перед установкой

- Это релиз-кандидат публичной беты. Сделайте резервную копию важного: StudioForge ежедневно
  бэкапит свою базу, но файлы ваших проектов защищаете вы сами.
- Обновление с rc.1 не требует миграций и изменения настроек — замените бинарник и запустите снова.
- Агент с уровнем `workspace-write` и выше может менять файлы в папке проекта, на которую вы его
  направили.
- Интеграция с Roblox Studio требует одного открытого окна Studio с включённым Studio MCP.
- Для запусков Claude нужен установленный Claude Code с выполненным входом; для OpenRouter и NVIDIA
  — API-ключ выбранного провайдера.
- Понимание скриншотов работает только с моделями, отмеченными как поддерживающие изображения.
- Пакеты для Windows и macOS сейчас без цифровой подписи.
- Bootstrap-токен, которым авторизуется свежеоткрытая вкладка, как и раньше принимается только из
  хеша URL.

### Загрузка и проверка

- `StudioForge-v0.5.0-rc.2-windows-amd64.zip` — Windows (amd64).
- `StudioForge-v0.5.0-rc.2-macos-arm64.zip` — macOS (arm64).
- `SHA256SUMS.txt` — контрольные суммы обоих архивов; проверьте их через
  `sha256sum -c SHA256SUMS.txt` (или `Get-FileHash` в Windows), прежде чем запускать скачанный
  неподписанный бинарник.
- Оба бинарника — неподписанные сборки: Windows SmartScreen и macOS Gatekeeper предупредят при
  первом запуске, для этого релиза это ожидаемо.

### Сообщить о проблеме

Сообщайте о проблемах на странице GitHub Issues проекта: укажите вашу ОС, точную строку версии из
`--version` и, по возможности, диагностический архив `studioforge doctor --bundle` (секреты
удаляются перед экспортом).
