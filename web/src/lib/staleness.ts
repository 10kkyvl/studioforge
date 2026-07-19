// Rapidly switching project (or any other selection an async load is scoped
// to) can land a slow/reordered response after something newer has already
// superseded it. The fix is a generation counter bumped once per switch: a
// load captures it before the await and, after resolving, only applies its
// result if the counter is still the one it captured — otherwise the
// response is stale and must be discarded silently.
export function isStaleGeneration(captured: number, current: number): boolean {
  return captured !== current;
}
