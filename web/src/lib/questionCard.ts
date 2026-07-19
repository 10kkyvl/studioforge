// A coding agent that wants the user to pick between a small number of
// discrete options ends its turn with a fenced ```studioforge-question block
// (question + options JSON). The backend detects that same fence in a
// fully-buffered assistant message, republishes it as a "question" run event,
// and parks the run in waiting_decision. This module is the frontend half of
// that contract: it validates the live event payload and, for history, pulls
// the same fence back out of a persisted chat message's raw text so a
// question asked earlier in a thread still renders as a card after a reload.

export type QuestionOption = {
  label: string;
  description: string;
};

export type QuestionCard = {
  question: string;
  options: QuestionOption[];
};

const FENCE_RE = /```studioforge-question\r?\n([\s\S]*?)\r?\n```/;

/**
 * Validates and normalizes a parsed question payload — used both for a live
 * "question" event's payload (already JSON) and for JSON pulled out of a
 * fenced block in persisted text. Anything that does not match the
 * question/options contract returns null so a malformed payload is treated
 * as ordinary noise rather than crashing or half-rendering a card.
 */
export function normalizeQuestionPayload(raw: unknown): QuestionCard | null {
  if (!raw || typeof raw !== 'object') return null;
  const value = raw as Record<string, unknown>;
  if (typeof value.question !== 'string' || value.question.trim() === '') return null;
  if (!Array.isArray(value.options) || value.options.length === 0) return null;
  const options: QuestionOption[] = [];
  for (const entry of value.options) {
    if (!entry || typeof entry !== 'object') return null;
    const option = entry as Record<string, unknown>;
    if (typeof option.label !== 'string' || option.label.trim() === '') return null;
    const description = typeof option.description === 'string' ? option.description : '';
    options.push({ label: option.label, description });
  }
  return { question: value.question, options };
}

/**
 * Pulls a ```studioforge-question fenced block out of a persisted message's
 * raw text, mirroring the backend's own detection. Malformed JSON inside the
 * fence, or JSON that does not match the contract, is ignored (returns null)
 * rather than thrown — the message then just renders as plain text, same as
 * the backend falling back to ordinary message text on the same input.
 */
export function shouldAnswerQuestion(
  key: string,
  sending: boolean,
  answeredKeys: ReadonlySet<string>,
): boolean {
  return !sending && !answeredKeys.has(key);
}

export function extractQuestionFence(
  text: string,
): { card: QuestionCard; remainder: string } | null {
  const match = FENCE_RE.exec(text);
  if (!match) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(match[1]);
  } catch {
    return null;
  }
  const card = normalizeQuestionPayload(parsed);
  if (!card) return null;
  const remainder = (text.slice(0, match.index) + text.slice(match.index + match[0].length)).trim();
  return { card, remainder };
}
