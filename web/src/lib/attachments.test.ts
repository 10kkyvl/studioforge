import { describe, expect, it } from 'vitest';
import { parseAttachments } from './attachments';

describe('parseAttachments', () => {
  it('returns the text unchanged when there is no attachments block', () => {
    expect(parseAttachments('Build me a neon lobby')).toEqual({
      text: 'Build me a neon lobby',
      images: [],
    });
  });
  it('splits a trailing attachments block off the prose, mirroring appendAttachmentsBlock', () => {
    const text =
      'What is wrong with this spawn?\n\n## Attached images\n' +
      '- .studioforge/attachments/2026-07-19-a1b2c3d4e5f6.png\n' +
      '- .studioforge/attachments/2026-07-19-deadbeef0000.jpg';
    expect(parseAttachments(text)).toEqual({
      text: 'What is wrong with this spawn?',
      images: [
        '.studioforge/attachments/2026-07-19-a1b2c3d4e5f6.png',
        '.studioforge/attachments/2026-07-19-deadbeef0000.jpg',
      ],
    });
  });
  it('handles a task-prefixed prompt (buildTaskPrompt) ahead of the attachments block', () => {
    const text =
      'Task: Fix the lobby lighting\nDescription here\n\nLook at this\n\n## Attached images\n' +
      '- .studioforge/attachments/2026-07-19-a1b2c3d4e5f6.png';
    const parsed = parseAttachments(text);
    expect(parsed.images).toEqual(['.studioforge/attachments/2026-07-19-a1b2c3d4e5f6.png']);
    expect(parsed.text).toBe('Task: Fix the lobby lighting\nDescription here\n\nLook at this');
  });
  it('degenerates to an empty prose string when the block is the entire message', () => {
    expect(
      parseAttachments('## Attached images\n- .studioforge/attachments/2026-07-19-abc123.png'),
    ).toEqual({ text: '', images: ['.studioforge/attachments/2026-07-19-abc123.png'] });
  });
  it('ignores a heading that only coincidentally appears mid-sentence', () => {
    // No leading newline before it and not attachments-shaped afterward — the
    // prose merely mentions the phrase, so nothing should be split out.
    expect(parseAttachments('Please add a ## Attached images section to the doc')).toEqual({
      text: 'Please add a ## Attached images section to the doc',
      images: [],
    });
  });
});
