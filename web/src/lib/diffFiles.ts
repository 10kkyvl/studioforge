export type DiffFile = { path: string; deleted: boolean; text: string };

// Git quotes non-ASCII path bytes as C-style octal escapes, not JSON escapes.
function decodeGitPath(value: string): string {
  if (!value.startsWith('"')) return value.replace(/^[ab]\//, '');
  if (!value.endsWith('"')) return '';
  const bytes: number[] = [];
  const encoder = new TextEncoder();
  const escapes: Record<string, string> = {
    a: '\x07',
    b: '\b',
    t: '\t',
    n: '\n',
    v: '\v',
    f: '\f',
    r: '\r',
    '\\': '\\',
    '"': '"',
  };
  for (let i = 1; i < value.length - 1; i++) {
    if (value[i] === '\\') {
      const octal = value.slice(i + 1).match(/^[0-7]{1,3}/)?.[0];
      if (octal) {
        bytes.push(parseInt(octal, 8));
        i += octal.length;
      } else {
        const escaped = escapes[value[++i]];
        if (escaped === undefined) return '';
        bytes.push(...encoder.encode(escaped));
      }
    } else {
      const char = String.fromCodePoint(value.codePointAt(i)!);
      bytes.push(...encoder.encode(char));
      i += char.length - 1;
    }
  }
  return new TextDecoder().decode(new Uint8Array(bytes)).replace(/^[ab]\//, '');
}

export function diffFiles(diff: string): DiffFile[] {
  const blocks = diff.split(/(?=^diff --git )/m).filter(Boolean);
  return blocks.map((text) => {
    const lines = text.split('\n');
    // Only inspect metadata: an added content line can itself start with +++.
    const hunk = lines.findIndex((line) => line.startsWith('@@'));
    const metadata = lines.slice(0, hunk < 0 ? lines.length : hunk);
    const newPath = metadata.find((line) => line.startsWith('+++ '))?.slice(4);
    const deleted =
      newPath === '/dev/null' || metadata.some((line) => line.startsWith('deleted file mode '));
    let raw = deleted ? metadata.find((line) => line.startsWith('--- '))?.slice(4) : newPath;
    if (!raw && lines[0]?.startsWith('diff --git ')) {
      const header = lines[0].slice(11);
      if (header.startsWith('"')) {
        raw = header.match(/^"(?:[^"\\]|\\.)*" (.+)$/)?.[1];
      } else {
        const separator = header.lastIndexOf(' b/');
        if (separator >= 0) raw = header.slice(separator + 1);
      }
    }
    return { path: raw ? decodeGitPath(raw) : '', deleted, text };
  });
}
