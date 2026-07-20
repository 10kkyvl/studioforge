import { describe, expect, it } from 'vitest';
import {
  extractQuestionFence,
  normalizeQuestionPayload,
  shouldAnswerQuestion,
} from './questionCard';

describe('normalizeQuestionPayload', () => {
  it('normalizes a well-formed payload, defaulting a missing description to empty', () => {
    expect(
      normalizeQuestionPayload({
        question: 'Which mesh format should I use?',
        options: [{ label: 'FBX', description: 'Standard interchange format' }, { label: 'OBJ' }],
      }),
    ).toEqual({
      question: 'Which mesh format should I use?',
      options: [
        { label: 'FBX', description: 'Standard interchange format' },
        { label: 'OBJ', description: '' },
      ],
    });
  });
  it('rejects a non-object payload', () => {
    for (const raw of [null, undefined, 'question?', 42, []]) {
      expect(normalizeQuestionPayload(raw)).toBeNull();
    }
  });
  it('rejects a missing or blank question', () => {
    expect(normalizeQuestionPayload({ options: [{ label: 'OBJ' }] })).toBeNull();
    expect(normalizeQuestionPayload({ question: '  ', options: [{ label: 'OBJ' }] })).toBeNull();
  });
  it('rejects a missing, non-array, or empty options list', () => {
    expect(normalizeQuestionPayload({ question: 'Pick one' })).toBeNull();
    expect(normalizeQuestionPayload({ question: 'Pick one', options: 'FBX' })).toBeNull();
    expect(normalizeQuestionPayload({ question: 'Pick one', options: [] })).toBeNull();
  });
  it('rejects an option with a missing or blank label', () => {
    expect(
      normalizeQuestionPayload({ question: 'Pick one', options: [{ description: 'x' }] }),
    ).toBeNull();
    expect(
      normalizeQuestionPayload({ question: 'Pick one', options: [{ label: '  ' }] }),
    ).toBeNull();
  });
  it('rejects a malformed entry inside an otherwise valid options array', () => {
    expect(
      normalizeQuestionPayload({ question: 'Pick one', options: [{ label: 'FBX' }, 'OBJ'] }),
    ).toBeNull();
  });
});

describe('extractQuestionFence', () => {
  it('extracts the card and strips the fence out of the remaining text', () => {
    const text =
      'Here is the tradeoff.\n\n' +
      '```studioforge-question\n' +
      '{"question": "Which mesh format?", "options": [{"label": "FBX", "description": "Standard"}, {"label": "OBJ", "description": ""}]}\n' +
      '```';
    expect(extractQuestionFence(text)).toEqual({
      card: {
        question: 'Which mesh format?',
        options: [
          { label: 'FBX', description: 'Standard' },
          { label: 'OBJ', description: '' },
        ],
      },
      remainder: 'Here is the tradeoff.',
    });
  });
  it('returns null when there is no fence at all', () => {
    expect(extractQuestionFence('Just plain prose, no fence here.')).toBeNull();
  });
  it('ignores malformed JSON inside the fence instead of throwing', () => {
    const text = '```studioforge-question\n{not valid json\n```';
    expect(extractQuestionFence(text)).toBeNull();
  });
  it('ignores a fence whose JSON does not match the question/options contract', () => {
    const text = '```studioforge-question\n{"question": "Pick one"}\n```';
    expect(extractQuestionFence(text)).toBeNull();
  });
  it('joins text on both sides of the fence into the remainder', () => {
    const text =
      'Before.\n```studioforge-question\n' +
      '{"question": "Q?", "options": [{"label": "A", "description": ""}]}\n' +
      '```\nAfter.';
    const result = extractQuestionFence(text);
    expect(result?.remainder).toBe('Before.\n\nAfter.');
  });
  it('rejects a fence with no newline before the closing backticks, matching the backend contract', () => {
    const text = '```studioforge-question\n{"question": "Q?", "options": [{"label": "A"}]}```';
    expect(extractQuestionFence(text)).toBeNull();
  });
  it('finds the question fence even when an unrelated code fence precedes it', () => {
    const text =
      'Here is a snippet:\n```lua\nprint("hi")\n```\n' +
      '```studioforge-question\n' +
      '{"question": "Which mesh format?", "options": [{"label": "FBX", "description": ""}]}\n' +
      '```';
    const result = extractQuestionFence(text);
    expect(result?.card.question).toBe('Which mesh format?');
  });
});

describe('shouldAnswerQuestion', () => {
  it('allows answering a fresh question while idle', () => {
    expect(shouldAnswerQuestion('q1', false, new Set())).toBe(true);
  });
  it('blocks a second click on the same question while a run is in flight', () => {
    expect(shouldAnswerQuestion('q1', true, new Set())).toBe(false);
  });
  it('blocks re-answering a question already recorded as answered', () => {
    expect(shouldAnswerQuestion('q1', false, new Set(['q1']))).toBe(false);
  });
  it('does not block a different question card that is still unanswered', () => {
    expect(shouldAnswerQuestion('q2', false, new Set(['q1']))).toBe(true);
  });
});
