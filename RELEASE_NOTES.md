# StudioForge v0.5.0-beta.4

_v0.5.0-beta.4 is a visual-polish and fix pass on the web UI: the app no longer shows a stale cached
UI after an update, several Settings-screen oddities are gone, and the whole interface got a
typography and motion cleanup. beta.1–beta.3's theme, shutdown-safety, navigation, and honest-state
fixes are unchanged and summarized below. / v0.5.0-beta.4 — это проход по визуальной полировке и
исправлениям веб-интерфейса: приложение больше не показывает устаревший закэшированный интерфейс после
обновления, несколько странностей на экране Настроек устранены, а весь интерфейс получил
переработанную типографику и анимации. Исправления темы, безопасного завершения, навигации и честных
состояний из beta.1–beta.3 не изменились и вкратце описаны ниже._

## English

This beta fixes a real update-visibility bug — the app could keep showing yesterday's UI after being
upgraded — cleans up several rough edges in Settings, and gives the whole interface a lighter, more
consistent look with subtle motion.

### What changed

- **No more stale UI after an update.** The embedded web UI was served without any cache instructions,
  so a browser could keep rendering the old interface after StudioForge itself had been updated. The
  app's shell now always revalidates, while its versioned assets are cached long-term and instantly
  replaced when they change — you should now always see the current UI after an update.
- **Settings status chips look right.** The small status pill next to each integration (Git, Rojo,
  OpenRouter, etc.) used to stretch into a stray, clipped circle around the label. It's now a clean,
  compact pill.
- **No more English/raw-code leftovers in Settings.** The database diagnostic used to show the literal
  word "ok"; integration cards showed raw ids like "git" or "openrouter" instead of names; and several
  guidance sentences ("Rojo CLI not found...", "Add your OpenRouter API key...") stayed in English even
  in the Russian UI. All of that is now properly localized.
- **A calmer sidebar footer.** The connection status, demo-mode badge, and version number used to
  crowd into overlapping, wrapping text in the narrow sidebar. They're now a clean two-line layout,
  with tooltips for anything that gets truncated.
- **First-run setup polish.** The initial checklist showed raw internal names instead of readable
  labels, and — while chasing a cramped-looking hint — we found and fixed a styling bug that made
  enabled primary buttons look disabled in the setup wizard, the New Project dialog, and Settings.
- **Chat header no longer overflows.** Long Russian status badges could overflow the chat header; they
  now wrap and truncate cleanly with tooltips, and the composer's controls line up on one baseline.
- **Cleaner typography and spacing throughout.** Roughly 25 slightly-different font sizes were
  consolidated into one consistent scale, border radii and padding were unified, and muted text is no
  longer unintentionally extra-faded.
- **Subtle, tasteful motion.** Views fade in smoothly when you switch between them, the slash-command
  menu eases in, and status indicators transition instead of snapping — all of it turned off
  automatically if your system has reduced-motion enabled.

beta.1–beta.3 already reorganized navigation, made previously-dead buttons work, surfaced failed-run
reasons, fixed shutdown safety, and more — see [CHANGELOG.md](CHANGELOG.md) for the full detail.

### Before installing

- This is a beta release, following several alpha pre-releases. Back up important Roblox projects.
- NVIDIA and OpenRouter require their own API keys. Model availability and limits belong to the
  selected provider.
- Screenshot understanding works only with a model marked as vision-capable.
- Windows and macOS packages are currently unsigned.
- The bootstrap token used to authenticate a freshly opened tab is, as before, only ever accepted from
  the URL hash.

## Русский

Эта бета устраняет реальную проблему с видимостью обновлений — приложение могло продолжать показывать
вчерашний интерфейс после обновления, — приводит в порядок несколько шероховатостей в Настройках и
делает весь интерфейс более лёгким и последовательным на вид, с деликатной анимацией.

### Что изменилось

- **Больше нет устаревшего интерфейса после обновления.** Встроенный веб-интерфейс отдавался вообще без
  инструкций кэширования, поэтому браузер мог продолжать показывать старый интерфейс после того, как сам
  StudioForge уже обновился. Теперь оболочка приложения всегда перепроверяется заново, а версионные
  ресурсы кэшируются надолго и мгновенно заменяются при изменении — после обновления вы всегда должны
  видеть актуальный интерфейс.
- **Статус-чипы в Настройках выглядят правильно.** Маленькая пилюля статуса рядом с каждой интеграцией
  (Git, Rojo, OpenRouter и т. д.) раньше растягивалась в странный обрезанный круг вокруг подписи. Теперь
  это аккуратная компактная пилюля.
- **Больше нет английских/сырых обрывков в Настройках.** Диагностика базы данных показывала буквальное
  слово "ok"; карточки интеграций показывали сырые идентификаторы вроде "git" или "openrouter" вместо
  названий; а несколько поясняющих фраз ("Rojo CLI not found...", "Add your OpenRouter API key...")
  оставались на английском даже в русском интерфейсе. Всё это теперь корректно локализовано.
- **Более спокойный футер сайдбара.** Статус соединения, значок демо-режима и номер версии раньше
  теснились в перекрывающихся, переносящихся строках в узком сайдбаре. Теперь это аккуратная
  двухстрочная раскладка с подсказками для всего, что обрезается.
- **Полировка первичной настройки.** Начальный чек-лист показывал сырые внутренние имена вместо понятных
  подписей, а при исправлении тесноватой подсказки под ним была найдена и исправлена ошибка стилей,
  из-за которой включённые основные кнопки выглядели отключёнными в мастере настройки, диалоге нового
  проекта и футерах Настроек.
- **Заголовок чата больше не переполняется.** Длинные русские статус-бейджи могли переполнять заголовок
  чата; теперь они аккуратно переносятся и обрезаются с подсказками, а элементы управления в поле ввода
  выровнены по одной базовой линии.
- **Более чистая типографика и отступы везде.** Около 25 слегка различающихся размеров шрифта сведены к
  единой последовательной шкале, радиусы скругления и отступы унифицированы, а приглушённый текст больше
  не затемняется случайно вдвойне.
- **Деликатная, уместная анимация.** Экраны плавно проявляются при переключении между ними, меню
  slash-команд плавно появляется, а статусные индикаторы переходят между состояниями плавно, а не рывком
  — всё это автоматически отключается, если в системе включён режим уменьшенной анимации.

beta.1–beta.3 уже переработали навигацию, заставили ранее нерабочие кнопки работать, показали причины
неудачных запусков, исправили безопасность завершения работы и многое другое — подробности в
[CHANGELOG.md](CHANGELOG.md).

### Перед установкой

- Это beta-релиз, следующий за несколькими alpha-версиями. Делайте резервные копии важных
  Roblox-проектов.
- Для NVIDIA и OpenRouter нужны отдельные API-ключи. Доступность моделей и лимиты задаёт провайдер.
- Скриншоты понимают только модели с поддержкой vision.
- Сборки Windows и macOS пока не подписаны.
- Bootstrap-токен для авторизации свежеоткрытой вкладки, как и раньше, принимается только из URL hash.
