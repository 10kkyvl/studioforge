import DOMPurify from 'dompurify';
import { marked } from 'marked';

const allowedTags = [
  'p',
  'br',
  'strong',
  'em',
  'del',
  'code',
  'pre',
  'blockquote',
  'ul',
  'ol',
  'li',
  'h1',
  'h2',
  'h3',
  'h4',
  'a',
  'hr',
  'table',
  'thead',
  'tbody',
  'tr',
  'th',
  'td',
  'input',
];

// Agent output is untrusted even when it came from a configured provider.
// Marked handles the Markdown grammar; DOMPurify is the security boundary
// before the result reaches Svelte's {@html} block. Remote images and raw
// layout tags are intentionally omitted: chat attachments have their own
// authenticated renderer and model prose must not reshape the application.
export function renderMarkdown(source: string): string {
  if (!source || typeof window === 'undefined') return '';
  const html = marked.parse(source, { async: false, breaks: true, gfm: true });
  return DOMPurify.sanitize(html, {
    ALLOWED_TAGS: allowedTags,
    ALLOWED_ATTR: ['href', 'title', 'class', 'type', 'checked', 'disabled'],
    ALLOW_DATA_ATTR: false,
  });
}
