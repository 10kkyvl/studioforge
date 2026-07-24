# Screenshots

All app screenshots in this folder are real UI captures — no mockups, no drawn-up or edited
recreations of the product. They are taken from the built-in `--mock` demo (three seeded projects:
Skyline Obby, Harbor Tycoon, Neon Arena), which exercises the real domain/API and UI without needing
a live Claude account, Roblox Studio, or Rojo.

## Capturing app screenshots

From `web/`:

```bash
npm run build
npm run screenshots
```

`npm run screenshots` uses Playwright to boot the real Go daemon with `--mock` against a temporary,
isolated data directory, then drives the actual embedded UI to capture each image. A Go toolchain is
required on the machine running this (the script builds/starts the daemon binary), in addition to
Node.js for Playwright itself.

## Naming convention

| File                    | Content                                                                      |
| ----------------------- | ---------------------------------------------------------------------------- |
| `dashboard.png`         | Projects view, default width, three `--mock` demo projects.                  |
| `dashboard-900x600.png` | Same view, cropped/sized for a specific embed (e.g. a narrower README slot). |
| `first-run.png`         | First-run setup wizard.                                                      |
| `chat-run.png`          | Project chat with agent/team selection and a run in progress.                |
| `run-diff.png`          | Completed run's diff panel with the rollback confirmation UI.                |
| `studio-sessions.png`   | Studio Sessions view with its (mock) session chips.                          |
| `social-preview.png`    | GitHub social preview card — not an app screenshot, see below.               |

New screenshots should follow the same `kebab-case.png` pattern, optionally suffixed with
`-WIDTHxHEIGHT` when a specific crop/size is needed for a particular embed.

## Social preview image

`social-preview.png` is not a captured app screenshot — it's a static, self-contained brand card.
It's rendered from [`social-preview.html`](social-preview.html) at exactly 1280x640 via:

```bash
npm run screenshots:social
```

GitHub's social preview slot is not set from a file in the repo automatically; a maintainer has to
upload `social-preview.png` manually at **GitHub → repository Settings → General → Social preview**.

## Optimization

PNG optimization (e.g. `oxipng` or `pngquant`) is optional and manual — run it locally before
committing a new or updated screenshot if you want a smaller file, but it is not part of the capture
scripts and not required.
