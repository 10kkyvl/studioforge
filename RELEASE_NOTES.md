# StudioForge v0.5.0-beta.3

_v0.5.0-beta.3 is a UX overhaul of the web UI — navigation, run visibility, and honest status
everywhere; beta.1/beta.2's theme and shutdown-safety fixes are unchanged and summarized below. /
v0.5.0-beta.3 — это доработка UX веб-интерфейса: навигация, видимость запусков и честный статус везде;
исправления темы и безопасного завершения из beta.1/beta.2 не изменились и вкратце описаны ниже._

## English

This beta is a UX pass across the whole web UI: navigation is reorganized, buttons that used to do
nothing now work, failed runs finally explain why, and the interface stops showing placeholder data
and dev-facing jargon.

### What changed

- **"Start agent run" and "Run this agent" actually do something now.** These buttons on project
  cards, Overview, and Team previously did nothing when clicked. They now open Chat with the right
  project selected, and Team's version also makes that agent the conversation's lead.
- **Failed runs explain why.** A run's failure reason is now shown everywhere it matters: a banner on
  the Runs detail panel, an error strip in Chat, and a tooltip on failed status chips in Activity —
  previously the reason was recorded but never displayed anywhere.
- **Clearer sidebar status.** The footer used to show the word "Interrupted" whenever the live
  connection dropped, which read like a run failure. It now says "Online" or "Connection lost —
  reconnecting…", and the version number no longer shows a doubled "v".
- **A real session-error screen.** An expired or invalid session used to show a vague, English-only
  message. It's now localized (English/Russian) with clear next steps, and other load errors show a
  plain-language reason (timed out, network, session expired, not found, server error) instead of raw
  text like "HTTP 500".
- **No more stuck slash-command confirmations** when switching chats or projects.
- **Reorganized navigation.** The left sidebar's nine items are now grouped under Work, Project, and
  Monitoring headings, and the project selector no longer appears on screens where it did nothing
  (Activity, Runs, Studio sessions, Settings).
- **Discoverable slash commands.** Typing `/` in the chat box now opens a menu of available commands
  (`/task`, `/build`, `/playtest`, `/plan`, `/do`, `/open`) with a description of what each does.
- **Honest empty states.** A brand-new install now sees "Create your first project" instead of a
  confusing "no projects match this filter" message, and the same care was applied to Tasks, Runs,
  Chat, and Activity.
- **No more fake status cards.** The Overview page's "Project health: Verified" and "Git checkpointing:
  Active" were always shown, even for a project that had never run anything. They now reflect the
  project's real last-run status, or say "No data yet".
- **Plain-language labels everywhere**, in both English and Russian: effort levels, permission profiles
  ("Read only" / "Write in project" / "Full access (unsafe)"), the "Demo (no AI)" provider name, and
  status words that used to show raw internal codes.
- **Smaller polish:** the Runs view's own message box (which created runs disconnected from any chat)
  is gone in favor of a hint pointing to Chat; Activity's table gained a time column; the project count
  reads correctly in Russian; an "All projects" label appears on Team/Tasks when no project is
  selected; and the first-run setup screen now reassures you that missing tools can be configured later.

Beta.1 and beta.2 already fixed the light theme, theme-flash-on-load, task status, and shutdown-safety
issues — see [CHANGELOG.md](CHANGELOG.md) for the full detail.

### Before installing

- This is a beta release, following several alpha pre-releases. Back up important Roblox projects.
- NVIDIA and OpenRouter require their own API keys. Model availability and limits belong to the
  selected provider.
- Screenshot understanding works only with a model marked as vision-capable.
- Windows and macOS packages are currently unsigned.
- The bootstrap token used to authenticate a freshly opened tab is, as before, only ever accepted from
  the URL hash.

## Русский

Эта бета — проход по UX всего веб-интерфейса: навигация переработана, кнопки, которые раньше ничего не
делали, теперь работают, неудачные запуски наконец объясняют причину, а интерфейс больше не показывает
фиктивные данные и жаргон разработчика.

### Что изменилось

- **«Запустить агента» теперь действительно что-то делает.** Кнопки «Start agent run» на карточках
  проектов и в Overview, а также «Run this agent» в Team, раньше ничего не делали при нажатии. Теперь
  они открывают Чат с нужным выбранным проектом, а вариант из Team ещё и делает этого агента ведущим
  агентом треда.
- **Неудачные запуски объясняют причину.** Причина сбоя запуска теперь показывается везде, где это
  важно: баннер в панели деталей Runs, строка ошибки в Чате и подсказка при наведении на статус-чип
  неудачного запуска в Activity — раньше причина сохранялась, но нигде не отображалась.
- **Понятнее статус в сайдбаре.** Футер раньше показывал слово "Interrupted"/«Прерван», когда обрывалось
  живое соединение, что читалось как сбой самого запуска. Теперь там «Online»/«В сети» или «Connection
  lost — reconnecting…»/«Нет соединения — переподключение…», а номер версии больше не задваивает букву
  "v".
- **Настоящий экран ошибки сессии.** Просроченная или недействительная сессия раньше показывала
  расплывчатое сообщение только на английском. Теперь оно локализовано (английский/русский) с понятными
  следующими шагами, а другие ошибки загрузки показывают причину простыми словами (таймаут, сеть,
  сессия истекла, не найдено, ошибка сервера) вместо сырого текста вроде "HTTP 500".
- **Больше не зависают подтверждения slash-команд** при переключении чатов или проектов.
- **Перестроенная навигация.** Девять пунктов левого сайдбара теперь сгруппированы под заголовками
  «Работа», «Проект» и «Мониторинг», а селектор проекта больше не показывается на экранах, где он ни на
  что не влиял (Activity, Runs, Studio sessions, Settings).
- **Обнаруживаемые slash-команды.** Ввод `/` в поле чата теперь открывает меню доступных команд
  (`/task`, `/build`, `/playtest`, `/plan`, `/do`, `/open`) с описанием, что делает каждая.
- **Честные пустые состояния.** На свежей установке теперь показывается «Создайте первый проект», а не
  запутывающее «нет проектов, подходящих под фильтр» — то же самое сделано для Tasks, Runs, Chat и
  Activity.
- **Больше нет фиктивных карточек статуса.** Карточки Overview "Project health: Verified" и "Git
  checkpointing: Active" раньше показывались всегда, даже для проекта, в котором ещё ни разу не
  запускали агента. Теперь они отражают реальный статус последнего запуска проекта или говорят «Данных
  пока нет».
- **Понятные подписи везде**, на английском и русском: уровни усилия, профили разрешений («Только
  чтение» / «Запись в проекте» / «Полный доступ (опасно)»), название провайдера «Демо (без ИИ)» и
  статусные слова, которые раньше показывали сырые внутренние коды.
- **Более мелкие улучшения:** собственное поле ввода в Runs (создававшее запуски в отрыве от чата)
  убрано в пользу подсказки о том, что запуски начинаются в Чате; в таблице Activity появилась колонка
  времени; счётчик проектов теперь грамматически верен на русском; на Team/Tasks появляется метка «Все
  проекты», когда проект не выбран; а экран первичной настройки теперь уверяет, что недостающие
  инструменты можно настроить позже.

Beta.1 и beta.2 уже исправили светлую тему, мигание темы при загрузке, статус задачи и безопасность
завершения работы — подробности в [CHANGELOG.md](CHANGELOG.md).

### Перед установкой

- Это beta-релиз, следующий за несколькими alpha-версиями. Делайте резервные копии важных
  Roblox-проектов.
- Для NVIDIA и OpenRouter нужны отдельные API-ключи. Доступность моделей и лимиты задаёт провайдер.
- Скриншоты понимают только модели с поддержкой vision.
- Сборки Windows и macOS пока не подписаны.
- Bootstrap-токен для авторизации свежеоткрытой вкладки, как и раньше, принимается только из URL hash.
