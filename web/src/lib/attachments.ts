// The "## Attached images" block appendAttachmentsBlock (internal/api/attachments.go)
// folds into a run's prompt before it's sent. The two sides must agree on
// this exact heading — it is how a chat message's raw text (round-tripped
// through runs.prompt_snapshot) is told apart from ordinary prose.
const ATTACHMENTS_HEADER = '## Attached images';

export type ParsedMessage = { text: string; images: string[] };

// Splits a chat message's stored text back into its prose and the image
// paths the backend appended to it, so the bubble can render thumbnails
// instead of a literal markdown list. The block is always the trailing
// section of the prompt (see appendAttachmentsBlock), so everything from the
// header onward is treated as the block rather than searched for line by
// line.
export function parseAttachments(text: string): ParsedMessage {
  const inline = text.startsWith(ATTACHMENTS_HEADER);
  const marker = inline ? 0 : text.indexOf(`\n${ATTACHMENTS_HEADER}`);
  if (!inline && marker === -1) return { text, images: [] };
  const headerAt = inline ? 0 : marker + 1;
  const before = text.slice(0, headerAt).replace(/\n+$/, '');
  const block = text.slice(headerAt + ATTACHMENTS_HEADER.length);
  const images = block
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.startsWith('- '))
    .map((line) => line.slice(2).trim())
    .filter(Boolean);
  return { text: before, images };
}
