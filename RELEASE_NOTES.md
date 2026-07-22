# StudioForge v0.5.0-alpha.1

## English

This alpha makes long Roblox Studio sessions safer and adds more model choices.

### What changed

- **NVIDIA NIM provider.** Add an NVIDIA API key in Settings and run supported coding models directly.
- **Vision for Studio.** Vision-capable OpenRouter and NVIDIA models can inspect pasted images and
  screenshots returned by Roblox Studio.
- **Reliable API runs.** Temporary network errors, timeouts, interrupted streams, rate limits, and
  temporary upstream failures are retried automatically.
- **Queued follow-ups.** Send another message while an agent is working. It waits in the same chat,
  keeps the conversation context, and can be removed without stopping the active run.
- **Saved place protection.** Opening Studio no longer rebuilds an existing `.rbxl`. Rojo creates the
  place only when it is missing, so saved Studio work is not silently replaced.
- **Cleaner chat.** Streaming uses one live response bubble, Markdown is rendered safely, and model
  or connection failures are easier to understand.

### Before installing

- This is an alpha release. Back up important Roblox projects.
- NVIDIA and OpenRouter require their own API keys. Model availability and limits belong to the
  selected provider.
- Screenshot understanding works only with a model marked as vision-capable.
- Windows and macOS packages are currently unsigned.

## Русский

Эта альфа делает долгую работу с Roblox Studio безопаснее и добавляет больше моделей.

### Что изменилось

- **Провайдер NVIDIA NIM.** Добавьте API-ключ NVIDIA в настройках и запускайте поддерживаемые модели
  напрямую.
- **Зрение для Studio.** Vision-модели OpenRouter и NVIDIA видят вставленные картинки и скриншоты,
  полученные из Roblox Studio.
- **Надёжные API-запуски.** Временные ошибки сети, таймауты, оборванные ответы, rate limit и временные
  сбои провайдера повторяются автоматически.
- **Очередь сообщений.** Новое сообщение можно отправить, пока агент занят. Оно продолжит тот же чат
  и может быть удалено из очереди без остановки активного запуска.
- **Защита сохранённого place.** При открытии Studio существующий `.rbxl` больше не пересобирается.
  Rojo создаёт place только при отсутствии файла, поэтому сохранённая работа не пропадает.
- **Чище чат.** Стриминг обновляет один live-ответ, Markdown отображается безопасно, а ошибки модели
  и соединения стали понятнее.

### Перед установкой

- Это alpha-релиз. Делайте резервные копии важных Roblox-проектов.
- Для NVIDIA и OpenRouter нужны отдельные API-ключи. Доступность моделей и лимиты задаёт провайдер.
- Скриншоты понимают только модели с поддержкой vision.
- Сборки Windows и macOS пока не подписаны.
