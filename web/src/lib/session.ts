// Where the operator was last time. StudioForge is a tool people leave open for
// days and come back to, so reopening it on the projects list — rather than the
// chat they were mid-conversation in — costs them two clicks every session.
//
// This is deliberately localStorage rather than a server setting: it is
// per-browser placement, not an account preference, and it must be readable
// synchronously during mount so the first paint is already the right view.

const VIEW_KEY = 'studioforge-view';
const PROJECT_KEY = 'studioforge-project';

// Storage throws rather than returning null in a few real cases — Safari's
// private mode, and any embedding that blocks third-party storage. A remembered
// view is a convenience, so every failure here degrades to "start fresh".
function read(key: string): string {
  try {
    return localStorage.getItem(key) ?? '';
  } catch {
    return '';
  }
}

function write(key: string, value: string) {
  try {
    if (value) localStorage.setItem(key, value);
    else localStorage.removeItem(key);
  } catch {
    // Nothing to do: the session simply won't be restored next time.
  }
}

// loadView returns the stored view only when it is still one the app offers, so
// a renamed or removed view in a newer build cannot strand someone on a blank
// screen they have no way to navigate away from.
export function loadView(allowed: readonly string[]): string {
  const stored = read(VIEW_KEY);
  return allowed.includes(stored) ? stored : '';
}

export function saveView(view: string) {
  write(VIEW_KEY, view);
}

// loadProject is validated against the projects that actually exist rather than
// trusted, because a project can be deleted from another window, or the whole
// data directory swapped, between sessions.
export function loadProject(known: readonly { id: string }[]): string {
  const stored = read(PROJECT_KEY);
  return known.some((project) => project.id === stored) ? stored : '';
}

export function saveProject(projectId: string) {
  write(PROJECT_KEY, projectId);
}
