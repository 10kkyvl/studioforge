# StudioForge guide (English)

This page includes the complete Russian guide below. For the full English guide, installation instructions, operations, integration setup, troubleshooting, and accessibility notes, see [docs/en/README.md](../en/README.md). StudioForge is a local, secure, multi-project AI studio for Roblox with a native Go daemon and an embedded bilingual browser UI.

---

# Руководство StudioForge (русский)

> **Публичная бета-версия.** Это предрелизная сборка (v0.5.0-beta.3). Часть описанных ниже функций реализована в коде, но пока не доступна из работающего приложения — такие места отмечены явно. Полную verification-матрицу см. в [Known Limitations](../KNOWN_LIMITATIONS.md).

## Установка

### Windows 10/11 amd64

Скачайте `windows-amd64.zip`, сравните SHA-256 с `SHA256SUMS.txt`, распакуйте и запустите:

```powershell
./studioforge.exe --mock
```

Неподписанная development-сборка может вызвать SmartScreen. Сначала проверьте checksum и источник. Для source build нужны Go 1.25+, Node.js 22+, npm и Git; выполните `./scripts/dev.ps1`.

### macOS 12+ Apple Silicon

Проверьте `macos-arm64.zip` командой `shasum -a 256`, распакуйте и откройте `StudioForge.app`. Для неподписанной development-сборки один раз используйте Control-click → Open. Не отключайте Gatekeeper глобально. Runtime не требует Node.js.

## Первый запуск

Wizard проверяет каталог данных, SQLite, Git, Claude Code/auth, Rojo, официальный Studio MCP launcher и ключ OpenRouter (состояние ключа и доступность каталога моделей). Для повторной проверки откройте Settings. `--safe-mode` отключает AI workers, MCP и Rojo, оставляя диагностику/backup/export. `--mock` создаёт три независимых demo workspace и работает через настоящее domain core/API.

## NVIDIA NIM

Добавьте API-ключ NVIDIA в **Настройки → Агенты и интеграции**, затем выберите NVIDIA-модель для
агента. Временные ошибки сети, таймауты, rate limit и оборванный stream повторяются автоматически.
Vision-модели, например Kimi K2.6, получают вставленные изображения и последний снимок от Studio
`screen_capture`; текстовым моделям скрытый скриншот не отправляется.

Сообщения, отправленные во время работы агента, ждут в очереди того же чата и по порядку продолжают
его контекст. При открытии Studio существующий сохранённый `.rbxl` сохраняется: Rojo создаёт place
только при отсутствии файла и больше не перезаписывает сохранённую работу скрытой пересборкой.

## Claude Code

Установите и авторизуйте Claude Code по актуальной официальной документации Anthropic:

```powershell
claude --version
claude auth status
```

StudioForge читает `claude --help` и добавляет только реально доступные flags. Adapter использует print/stream JSON, ограниченные env, turns/budget и безопасный permission mode. Авторизацией владеет Claude Code; token не сохраняется в SQLite. Это локальный CLI-подпроцесс — рантайм принципиально отличается от OpenRouter (см. ниже), который работает через HTTP API и собственный встроенный agent-цикл StudioForge.

## OpenRouter

OpenRouter — это HTTP API, а не локальный CLI. В отличие от Claude Code, которого StudioForge запускает как подпроцесс, для OpenRouter StudioForge реализует собственный встроенный agent-цикл: ограниченный tool loop, который стримит ответ модели и по ходу выполняет локальные инструменты проекта (чтение файлов, поиск, grep, создание, правка, patch, mkdir, git, run_command — набор доступных операций зависит от permission profile агента: read-only, workspace-write или danger-full-access) и инструменты Roblox Studio MCP через отдельный живой per-run MCP-клиент с fail-closed allowlist по тому же profile.

Нужен API-ключ — задайте его в **Настройки → Агенты и интеграции** или через переменную окружения `OPENROUTER_API_KEY`; ключ нужен **даже для бесплатных моделей**. Ключ хранится в системном хранилище учётных данных ОС (Windows Credential Manager / macOS Keychain) с резервом на переменную окружения и хранение только на текущую сессию — StudioForge никогда не сохраняет его в SQLite, событиях запусков, логах или diagnostic bundle. Карточка в Settings показывает состояние ключа (не настроен / не проверен / настроен / недействителен), его источник (связка ключей ОС / только сессия / переменная окружения) и кнопку **Проверить соединение**.

Каталог моделей загружается вживую из публичного Models API OpenRouter и кешируется на 6 часов, с ручным обновлением из Settings. Если запрос к API не проходит, StudioForge использует последний успешный кеш, а если кеша ещё не было — встроенный датированный fallback-снимок. Picker показывает поддержку tools и vision, размер контекста, бесплатность и достоверность данных. Известная модель без tool calls отклоняется; неизвестный или устаревший ID, включая динамический `openrouter/free`, разрешается только после отдельного явного подтверждения, а backend повторно обновляет каталог перед запуском. Кураторская подборка группирует модели по категориям: Free automatic, Free recommended, Best coding, Balanced, Fast and cheap, Strong reasoning и Large context; сверено 2026-07-21.

Бесплатные модели менее стабильны: переменные качество и задержка, более низкие rate limits, доступность может меняться — они лучше подходят для небольших задач. `openrouter/free` сам выбирает подходящую бесплатную модель среди доступных. **Free-режим никогда молча не переключается на платную модель** — если подходящей бесплатной модели нет, run завершится ошибкой, а не тихим переключением на платную.

История разговора хранится по chat-треду в SQLite, а не только в памяти процесса, и потому переживает перезапуск daemon; partial-дельты не записываются, а каждый assistant turn во время стриминга обновляет один live bubble. При разрастании истории StudioForge детерминированно сжимает её по числу токенов, не обращаясь к модели за суммаризацией. Run сообщает usage и денежную стоимость (фактическую из OpenRouter либо оценочную из полного доступного прайса каталога) и перед каждым запросом проверяет оставшийся бюджет без дополнительного дорогого final request. На моделях со зрением к сообщению можно приложить изображение; на модели без поддержки зрения run завершится понятной ошибкой вместо того, чтобы молча отбросить вложение. После run'а доступен тот же цикл пост-ран playtest-валидации в Studio, что и для Claude Code.

**Provider routing** (необязательные Advanced-настройки) по умолчанию задаёт безопасные значения — StudioForge всегда требует провайдеров, поддерживающих параметры запроса (`require_parameters: true`), иначе tool-вызовы могут не сработать; отдельно можно настроить сбор данных (data collection), zero-data-retention и разрешение резервных провайдеров (fallbacks).

Провайдер **Codex CLI удалён**: он больше не запускается и не обнаруживается, у него больше нет настройки пути к executable, `CODEX_HOME` или диагностики, и он недоступен для выбора у новых агентов. Более ранние запуски с `provider="codex"` остаются доступны только в истории — с бейджем «Устаревший провайдер» — Restart и Resume для них возвращают понятную ошибку, потому что перезапустить их нельзя; сами эти запуски не мигрируются, не переписываются и не удаляются.

## Roblox Studio MCP и несколько Studio

Обновите Roblox Studio, откройте **Assistant → … → Manage MCP Servers** и включите **Studio as MCP server**. Официальные launchers:

- Windows: `cmd.exe /c %LOCALAPPDATA%\Roblox\mcp.bat`
- macOS: `/Applications/RobloxStudio.app/Contents/MacOS/StudioMCP`

StudioForge получает список инструментов через capability discovery. Доступ к Studio выдаётся run'у только когда открыт ровно один экземпляр Studio: Claude Code использует собственный MCP-клиент, поэтому StudioForge не может закрепить instance на чужом соединении, а официальный launcher не принимает аргумент выбора instance. При нескольких открытых Studio доступ не выдаётся, а run продолжается без Studio. Экран **Сессии Studio** и его bind-действие существуют в UI, но в этой альфа-версии реальные открытые instances Studio в него не попадают — его строки это demo-данные. Изменяющие/playtest jobs держат эксклюзивный resource `studio:<id>`.

**Не реализовано в этой бета-версии** (см. [Known Limitations](../KNOWN_LIMITATIONS.md)): по замыслу playtest contract — выбрать instance → проверить state → start → simulated input → console/screenshots → stop → structured result → bug tasks. StudioForge не автоматизирует playtest и не делает скриншоты сама уже сегодня.

## Rojo

Установите Rojo 7 CLI и Studio plugin. Проверьте `rojo --version`. Для выбранного `*.project.json` StudioForge собирает (build) place-файл и открывает его в Studio. **Не реализовано в этой бета-версии** (см. [Known Limitations](../KNOWN_LIMITATIONS.md)): отдельная live-sync сессия — supervised `rojo serve` на уникальном loopback port, с потоковым stdout/stderr, запретом duplicate session и stop/restart — реализована и покрыта тестами в `internal/rojo`, но ни один HTTP endpoint её не запускает, не останавливает и не опрашивает; доступна только сборка (build). VS Code extension не заменяет CLI.

## Проекты, команды и конкурентность

Project source остаётся в пользовательском каталоге; приложение хранит canonical path/fingerprint. Новый проект сразу получает default agent; при старте такой агент также добавляется старым проектам, у которых команды не было. В разделе **Команда** можно создавать, редактировать, включать/выключать и запускать агентов Claude Code, OpenRouter и mock. Опциональная `.agent/` может содержать `constitution.yaml` и `requirements.md` — StudioForge читает ровно эти два файла целиком и подставляет их в system prompt каждого run'а. Остальное содержимое `.agent/` (architecture, prompts, skills, memory) в этой альфа-версии не читается. Большие transcripts находятся в SQLite.

В **Настройки → Агенты и интеграции** задаются default provider/model/effort, общая параллельность, пути к Claude, Rojo, Git и Roblox Studio MCP, а также API-ключ OpenRouter. Пустое поле пути использует PATH/platform discovery. Изменения применяются сразу, а diagnostic cards показывают фактический путь (или, для OpenRouter, состояние ключа), версию, auth status и подсказку.

Scheduler справедливо обходит project queues. Разные проекты могут писать одновременно, а второй writer одного проекта ждёт `project:<id>:write`. Resources сортируются и захватываются атомарно. Проверяются global/project/provider/model limits и budget. Events сначала сохраняются в SQLite, затем отправляются через SSE.

Pause/resume выполняются между событиями; cancel завершает provider/process tree. После аварии активные runs становятся `interrupted` и доступны для restart. Все histories, tasks, agents и budgets изолированы по `project_id`. (Project-scoped memory store — SQLite FTS5 с Put/Search — теперь пишет одну запись на каждый завершённый run и подставляет до пяти релевантных прошлых записей в system prompt следующего run'а в этом проекте; это минимальное подключение, а не суммаризированная или курируемая память.)

## Безопасность и permissions

- Оставляйте loopback listener; `--unsafe-host` не превращает daemon в безопасный remote service.
- **В продукте нет гейта одобрения оператором перед опасным действием.** Тип записи `Decision`, endpoint resolve и экран review существовали в ранней альфа-версии, но были удалены — у них не было ни одного живого producer'а, и ничего не заняло их место как safety gate. Функция interactive-question (`studioforge-question`) закрывает случай, когда агент останавливается посреди run'а за вводом оператора, но не является гейтом одобрения перед деструктивным действием.
- **Задумано, но не реализовано в этой альфа-версии:** Git rollback на branch `studioforge/rollback-<timestamp>` на проверенном commit, без force-reset и без удаления untracked files. `internal/gitops` (`SafeRollback`, `Tag`) реализован и покрыт тестами, но не открыт ни одним API endpoint; `Status` и `DiffHead` уже подключены — экран чата показывает диф завершённого run'а относительно его checkpoint commit. (StudioForge отдельно делает auto-commit Git checkpoint перед каждым не-plan run Claude, так что откат возможен вручную.)
- Canonical/symlink checks не позволяют выйти за зарегистрированный root.

## Backup, export и import

Автоматический SQLite backup создаётся не чаще раза в 24 часа. В Settings есть ручная команда.

```powershell
studioforge export --project PROJECT_ID --output project.zip
studioforge import --file project.zip
studioforge import --file project.zip --apply --path C:\existing\project
```

Portable archive содержит metadata, agents и tasks, но не копирует project source. Зависимости задач (создайте задачу с полем `dependencies`, перечисляющим ID других задач проекта; цикл будет отклонён) входят в состав своих задач, хотя выполнение run'а пока не проверяет, завершены ли зависимости задачи. Import сначала показывает preview и конфликты.

## Решение проблем

- **Another instance:** используйте уже запущенное окно или штатно завершите процесс.
- **401:** bootstrap token одноразовый; используйте исходную browser session или перезапустите daemon.
- **Claude:** проверьте `claude --version` и `claude auth status`.
- **OpenRouter:** проверьте состояние ключа в Settings и нажмите **Проверить соединение**; ключ нужен даже для бесплатных моделей. `OPENROUTER_API_KEY` в окружении используется как резерв, если ключ не сохранён в Settings.
- **Codex (устаревший провайдер):** провайдер удалён — новые запуски с ним недоступны. Более старые запуски видны в истории с бейджем «Устаревший провайдер»; Restart и Resume для них возвращают понятную ошибку, потому что перезапустить их нельзя.
- **Studio ambiguous:** доступ к Studio выдаётся только при одном открытом instance — закройте лишние окна Studio, оставьте один и повторите попытку. (Bind-действие в Studio Sessions в этой бета-версии не влияет на реальные instances — оно работает только с demo-данными.)
- **Rojo:** проверьте CLI, `*.project.json` и свободный port.
- **Database:** выполните `studioforge doctor --bundle diagnostics.zip`; восстанавливайте backup только при остановленном daemon.

Доступность: видимый focus, keyboard navigation, Alt+1…Alt+9, responsive layout и ограниченное видимое окно event log. Точные ограничения и результаты проверки: [Known Limitations](../KNOWN_LIMITATIONS.md).
