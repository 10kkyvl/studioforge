# StudioForge v0.5.0-beta.1

## English

This beta is a visual and reliability pass: the light theme is fully themed and accessible, the
interface no longer flashes the wrong theme on load, and two backend edge cases around task status and
event streaming are fixed.

### What changed

- **Light theme fixes.** Border and status colors (success/warning/danger) are now correctly defined in
  the light theme, fixing invisible borders and off-palette colors in the Runs view, chat, and the
  OpenRouter model picker. Primary button text now meets contrast guidelines.
- **No more theme flash.** The saved theme is applied before the page renders, and the browser's own
  theme color follows whichever theme is active.
- **System theme by default.** New installs now follow the OS light/dark preference instead of always
  starting in Dark. Switching themes now transitions smoothly.
- **Localized chat commands.** Slash-command confirmations (`/task`, `/plan`, `/do`, `/open`) now reply
  in the interface's selected language instead of always in English.
- **Cleaner run lists.** Long run and thread titles are now truncated instead of overflowing.
- **More reliable event streaming.** A transient replay failure on the live events endpoint no longer
  surfaces as a raw error mid-stream; the client reconnects automatically.
- **Correct task status.** A task linked to a run no longer shows as "running" when the run itself
  failed to start.

### Before installing

- This is a beta release, following several alpha pre-releases. Back up important Roblox projects.
- NVIDIA and OpenRouter require their own API keys. Model availability and limits belong to the
  selected provider.
- Screenshot understanding works only with a model marked as vision-capable.
- Windows and macOS packages are currently unsigned.

## Русский

Эта бета — проход по внешнему виду и надёжности: светлая тема полностью оформлена и доступна, интерфейс
больше не мигает не той темой при загрузке, а два пограничных случая в бэкенде — со статусом задачи и
стримингом событий — исправлены.

### Что изменилось

- **Исправления светлой темы.** Цвета границ и статусов (success/warning/danger) теперь корректно
  определены в светлой теме — это чинит невидимые границы и цвета не в палитре в разделе Runs, в чате и в
  выборе модели OpenRouter. Текст на основной кнопке теперь соответствует требованиям контрастности.
- **Без мигания темой.** Сохранённая тема применяется до рендера страницы, а цвет темы браузера
  соответствует активной теме.
- **Тема System по умолчанию.** Новые установки теперь следуют настройке ОС (светлая/тёмная), а не
  всегда открываются в тёмной теме. Переключение темы теперь происходит плавно.
- **Локализованные команды чата.** Подтверждения slash-команд (`/task`, `/plan`, `/do`, `/open`) теперь
  отвечают на выбранном языке интерфейса, а не всегда на английском.
- **Чище список запусков.** Длинные названия запусков и чатов теперь обрезаются, а не переполняют
  строку.
- **Надёжнее стриминг событий.** Временный сбой воспроизведения событий больше не показывается как
  сырая ошибка посреди потока; клиент переподключается автоматически.
- **Верный статус задачи.** Задача, привязанная к запуску, больше не показывается как "running", если
  сам запуск не удалось создать.

### Перед установкой

- Это beta-релиз, следующий за несколькими alpha-версиями. Делайте резервные копии важных
  Roblox-проектов.
- Для NVIDIA и OpenRouter нужны отдельные API-ключи. Доступность моделей и лимиты задаёт провайдер.
- Скриншоты понимают только модели с поддержкой vision.
- Сборки Windows и macOS пока не подписаны.
