import { describe, expect, it } from 'vitest';
import { renderMarkdown } from './markdown';

describe('renderMarkdown', () => {
  it('renders common AI formatting', () => {
    const html = renderMarkdown('**Bold**\n\n1. First\n2. Second\n\n```lua\nprint("hi")\n```');
    expect(html).toContain('<strong>Bold</strong>');
    expect(html).toContain('<ol>');
    expect(html).toContain('<code class="language-lua">');
  });

  it('removes executable HTML and unsafe links', () => {
    const html = renderMarkdown(
      '<script>alert(1)</script><img src=x onerror=alert(2)> [bad](javascript:alert(3))',
    );
    expect(html).not.toContain('<script');
    expect(html).not.toContain('<img');
    expect(html).not.toContain('href="javascript:');
    expect(html).not.toContain('onerror');
  });
});
