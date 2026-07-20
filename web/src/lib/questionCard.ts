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
const FENCE_RE_GLOBAL = /```studioforge-question\r?\n([\s\S]*?)\r?\n```/g;

const MAX_QUESTION_LENGTH = 2000;
const MIN_OPTIONS = 2;
const MAX_OPTIONS = 4;
const MAX_LABEL_LENGTH = 120;
const MAX_DESCRIPTION_LENGTH = 600;
const MAX_FENCE_BODY_BYTES = 8192;

// The scheduler stamps a stuck-escalation's own message event with this
// RawType (see scheduler.go's emit calls under "scheduler.stuck"), and it
// survives into a persisted message's own rawType field the same way it
// does on the live SSE event — this is the one marker that tells a
// stuck-escalation question card apart from the agent's own natural
// question, live or after a reload, without adding anything to the fence's
// own {question, options} JSON contract.
export const STUCK_ESCALATION_RAW_TYPE = 'scheduler.stuck';

export function isStuckEscalation(rawType: string | undefined | null): boolean {
  return rawType === STUCK_ESCALATION_RAW_TYPE;
}

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
  if (typeof value.question !== 'string') return null;
  const trimmedQuestion = value.question.trim();
  if (trimmedQuestion === '' || trimmedQuestion.length > MAX_QUESTION_LENGTH) return null;
  if (
    !Array.isArray(value.options) ||
    value.options.length < MIN_OPTIONS ||
    value.options.length > MAX_OPTIONS
  ) {
    return null;
  }
  const options: QuestionOption[] = [];
  const seenLabels = new Set<string>();
  for (const entry of value.options) {
    if (!entry || typeof entry !== 'object') return null;
    const option = entry as Record<string, unknown>;
    if (typeof option.label !== 'string') return null;
    const trimmedLabel = option.label.trim();
    if (trimmedLabel === '' || trimmedLabel.length > MAX_LABEL_LENGTH) return null;
    if (seenLabels.has(trimmedLabel)) return null;
    seenLabels.add(trimmedLabel);
    let description = '';
    if (option.description !== undefined) {
      if (typeof option.description !== 'string') return null;
      description = option.description;
    }
    if (description.length > MAX_DESCRIPTION_LENGTH) return null;
    options.push({ label: option.label, description });
  }
  return { question: value.question, options };
}

export function shouldAnswerQuestion(
  key: string,
  sending: boolean,
  answeredKeys: ReadonlySet<string>,
): boolean {
  return !sending && !answeredKeys.has(key);
}

/**
 * Pulls a ```studioforge-question fenced block out of a persisted message's
 * raw text, mirroring the backend's own detection. Malformed JSON inside the
 * fence, or JSON that does not match the contract, is ignored (returns null)
 * rather than thrown — the message then just renders as plain text, same as
 * the backend falling back to ordinary message text on the same input. A
 * message with zero or more than one such fence, or a fence whose raw JSON
 * body exceeds the backend's size limit, is likewise rejected rather than
 * guessing which fence was intended.
 */
export function extractQuestionFence(
  text: string,
): { card: QuestionCard; remainder: string } | null {
  const fenceMatches = text.match(FENCE_RE_GLOBAL);
  if (!fenceMatches || fenceMatches.length !== 1) return null;

  const match = FENCE_RE.exec(text);
  if (!match) return null;
  const bodyByteLength = new TextEncoder().encode(match[1]).length;
  if (bodyByteLength > MAX_FENCE_BODY_BYTES) return null;

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
