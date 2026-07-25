# StudioForge v0.5.0-rc.3

_v0.5.0-rc.3 is a fix-only release candidate on top of rc.2, and it is all about one thing: getting
Roblox Studio recognised reliably. A project you edit through Team Create — a place opened from
roblox.com rather than from a local build — could not be reached by a run at all, and several
narrower failures around the same handshake made Studio look absent when it was open in front of
you. No new features, nothing removed. / v0.5.0-rc.3 — релиз-кандидат поверх rc.2, целиком про
исправления, и почти все они об одном: надёжно распознавать Roblox Studio. Проект, который вы
редактируете через Team Create (плейс, открытый с roblox.com, а не собранный локально), запуски не
могли увидеть в принципе, а несколько более узких сбоев вокруг того же рукопожатия заставляли
StudioForge считать Studio закрытой, когда она была открыта прямо перед вами. Новых функций нет,
ничего не удалено._

## English

### Fixes you will notice

- **A project edited on roblox.com can now be reached by a run.** Studio identifies a window by the
  single name the launcher reports for it, and that name is not always a file name: a place opened
  from a local build reports the build's file name, while a place opened from roblox.com — Team
  Create included — reports the place's display name and no file name at all. Matching only on the
  built file name meant such a project was refused permanently, with the right place open the whole
  time and nothing you could do about it: reopening Studio, restarting StudioForge and rebuilding
  the place all left the comparison unchanged.

  A project is now recognised by either name. Open **Studio sessions**, pick your project next to
  the open instance, and press **Recognise the project by this name** — the name is taken straight
  from what the launcher reports, so it cannot be mistyped. Clearing it returns the project to being
  matched on its built file alone. Nothing needs restarting afterwards; the next run picks it up.

- **Auto-open no longer opens the wrong window for a project hosted on Roblox.** StudioForge opens
  places by launching Studio on a local build, and a Team Create place is reachable only by being
  opened from Roblox. Left alone, a project set up as a roblox.com place would have had its local
  build launched — a second, unrelated window in front of you while your collaborators sit in the
  hosted place. That is now refused with a notice saying what to open instead, and the mismatch
  message for such a project no longer advises letting StudioForge open it automatically, which was
  advice you could not act on.

- **A run started moments after Studio opened is no longer told another MCP client owns Studio.**
  The Studio plugin attaches to the launcher in two steps, and only the first one reports an error;
  the second returns an empty list that used to be taken as final. A run in that window lost every
  Studio tool, and you were sent to close a competing client that did not exist. Both steps are now
  waited out. When the wait genuinely runs out, the message no longer asserts a cause it cannot
  observe — it names both a competing MCP client and a disabled Studio MCP plugin.

- **A turn no longer gets refused over a place mismatch that never happened.** Studio lists an
  instance's ID before filling in the place it holds, so a listing caught in that gap carries a
  nameless instance and came back as "does not hold this project's place … found (unnamed)" — with
  the right place open all along. A nameless listing now counts as a registration still in progress.
  A place that genuinely differs is reported at once, as before, and still names what is open.

- **A refresh caught mid-registration no longer blanks a Studio session's name** in the Studio
  Sessions view until a later refresh happened to catch a complete listing.

- **The Studio badge, the Open Studio button and the sessions refresh stay responsive** while a
  Studio sits there registering nothing — a plugin left disabled used to make each of them wait out
  the full run-gate window. They now answer on a short budget and let the next poll correct a stale
  answer; a run, which keeps its access decision for its whole length, still waits the full window.

### Known limits of Studio matching

Worth knowing before you rely on it, and spelled out in `docs/KNOWN_LIMITATIONS.md`: the launcher
reports only a name, an ID and an active flag per instance — there is no place or universe ID to
match on. So two open places sharing a display name are ambiguous and refused rather than guessed
at, and renaming the place on Roblox breaks the match until you press the button again. Roblox also
hands its MCP connection to one client at a time, so a run can still occasionally report that
another MCP client holds Studio; retrying is the fix, and that behaviour is unchanged by this
release.

### Before installing

- This is a release candidate for a public beta. Back up anything you care about; StudioForge takes
  its own daily database backup, but your project files are yours to protect.
- Upgrading from rc.2 needs no migration and no configuration change — replace the binary and start
  it again.
- An agent with `workspace-write` or above can change files in the project folder you point it at.
  Where that project is a Team Create place, its edits land in the live session your collaborators
  are already in.
- Roblox Studio integration needs a single Studio window open, with Studio MCP enabled.
- Claude runs need Claude Code installed and signed in; OpenRouter and NVIDIA runs need an API key
  for the selected provider.
- Screenshot understanding works only with a model marked as vision-capable.
- Windows and macOS packages are currently unsigned.
- The bootstrap token used to authenticate a freshly opened tab is, as before, only ever accepted
  from the URL hash.

### Downloads and verification

- `StudioForge-v0.5.0-rc.3-windows-amd64.zip` — Windows (amd64).
- `StudioForge-v0.5.0-rc.3-macos-arm64.zip` — macOS (arm64).
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

- **Проект, который редактируется на roblox.com, теперь доступен запускам.** Studio опознаётся по
  единственному имени, которое отдаёт лаунчер, и это имя не всегда является именем файла: плейс,
  открытый из локальной сборки, сообщает имя её файла, а плейс, открытый с roblox.com (в том числе
  через Team Create), — отображаемое имя и никакого имени файла вовсе. Сопоставление только по имени
  собранного файла означало, что такой проект отклонялся навсегда: нужный плейс был открыт всё это
  время, и сделать было нечего — переоткрытие Studio, перезапуск StudioForge и пересборка плейса
  ничего не меняли в сравнении.

  Теперь проект узнаётся по любому из двух имён. Откройте **Сессии Studio**, выберите свой проект
  рядом с открытым экземпляром и нажмите **Узнавать проект по этому имени** — имя берётся прямо из
  того, что сообщает лаунчер, так что опечатка исключена. Если очистить его, проект снова
  сопоставляется только по собранному файлу. Перезапускать после этого ничего не нужно: следующий
  запуск подхватит настройку сам.

- **Автооткрытие больше не открывает не то окно для проекта, размещённого на Roblox.** StudioForge
  открывает плейсы, запуская Studio на локальной сборке, а плейс Team Create доступен только при
  открытии с Roblox. Раньше проект, настроенный как облачный, получил бы запуск своей локальной
  сборки — второе, постороннее окно перед вами, пока соавторы сидят в размещённом плейсе. Теперь это
  отклоняется с сообщением о том, что открыть вместо этого, а сообщение о несовпадении для такого
  проекта больше не советует дать StudioForge открыть плейс автоматически — совет, которому нельзя
  было последовать.

- **Запуск сразу после открытия Studio больше не сообщает, что Studio занята другим MCP-клиентом.**
  Плагин Studio подключается к лаунчеру в два шага, и об ошибке сообщает только первый; второй
  возвращал пустой список, который считался окончательным. Запуск в этот момент терял все
  инструменты Studio, а вас отправляли закрывать конкурирующий клиент, которого не существовало.
  Теперь ожидаются оба шага. Когда ожидание действительно истекает, сообщение больше не утверждает
  причину, которую не может наблюдать, — оно называет и конкурирующий MCP-клиент, и отключённый
  плагин Studio MCP.

- **Ход больше не отклоняется из-за несовпадения плейса, которого не было.** Studio публикует ID
  экземпляра раньше, чем плейс, который он держит, поэтому листинг, попавший в этот промежуток, нёс
  безымянный экземпляр и возвращался как «does not hold this project's place … found (unnamed)» —
  при том что нужный плейс был открыт. Теперь безымянный листинг считается незавершённой
  регистрацией. Плейс, который действительно отличается, по-прежнему сообщается сразу и с указанием
  того, что открыто.

- **Обновление, попавшее в середину регистрации, больше не обнуляет имя сессии Studio** в списке
  сессий до следующего удачного обновления.

- **Значок Studio, кнопка «Открыть Studio» и обновление списка сессий остаются отзывчивыми**, пока
  Studio висит, ничего не регистрируя: отключённый плагин заставлял каждый из них ждать полное окно
  запуска. Теперь они отвечают по короткому бюджету, а устаревший ответ поправит следующий опрос;
  запуск, который сохраняет решение о доступе на всю свою длину, по-прежнему ждёт полное окно.

### Известные ограничения сопоставления

Стоит знать, прежде чем на это полагаться (подробнее — в `docs/KNOWN_LIMITATIONS.md`): лаунчер
сообщает по экземпляру только имя, ID и признак активности — ни place ID, ни universe ID для
сопоставления нет. Поэтому два открытых плейса с одинаковым отображаемым именем считаются
неоднозначными и отклоняются, а не угадываются, а переименование плейса на Roblox ломает
сопоставление, пока вы снова не нажмёте кнопку. Кроме того, Roblox отдаёт своё MCP-подключение
одному клиенту за раз, так что запуск всё ещё может изредка сообщить, что Studio держит другой
MCP-клиент; лечится повтором, и этот релиз здесь ничего не меняет.

### Перед установкой

- Это релиз-кандидат публичной беты. Сделайте резервную копию важного: StudioForge ежедневно
  бэкапит свою базу, но файлы ваших проектов защищаете вы сами.
- Обновление с rc.2 не требует миграций и изменения настроек — замените бинарник и запустите снова.
- Агент с уровнем `workspace-write` и выше может менять файлы в папке проекта, на которую вы его
  направили. Если этот проект — плейс Team Create, его правки попадают в живую сессию, где уже
  находятся ваши соавторы.
- Интеграция с Roblox Studio требует одного открытого окна Studio с включённым Studio MCP.
- Для запусков Claude нужен установленный Claude Code с выполненным входом; для OpenRouter и NVIDIA
  — API-ключ выбранного провайдера.
- Понимание скриншотов работает только с моделями, отмеченными как поддерживающие изображения.
- Пакеты для Windows и macOS сейчас без цифровой подписи.
- Bootstrap-токен, которым авторизуется свежеоткрытая вкладка, как и раньше принимается только из
  хеша URL.

### Загрузка и проверка

- `StudioForge-v0.5.0-rc.3-windows-amd64.zip` — Windows (amd64).
- `StudioForge-v0.5.0-rc.3-macos-arm64.zip` — macOS (arm64).
- `SHA256SUMS.txt` — контрольные суммы обоих архивов; проверьте их через
  `sha256sum -c SHA256SUMS.txt` (или `Get-FileHash` в Windows), прежде чем запускать скачанный
  неподписанный бинарник.
- Оба бинарника — неподписанные сборки: Windows SmartScreen и macOS Gatekeeper предупредят при
  первом запуске, для этого релиза это ожидаемо.

### Сообщить о проблеме

Сообщайте о проблемах на странице GitHub Issues проекта: укажите вашу ОС, точную строку версии из
`--version` и, по возможности, диагностический архив `studioforge doctor --bundle` (секреты
удаляются перед экспортом).
