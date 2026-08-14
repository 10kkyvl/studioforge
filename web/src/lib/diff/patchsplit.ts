function stripAPrefix(path: string): string {
  return path.startsWith('a/') ? path.slice(2) : path;
}

function stripBPrefix(path: string): string {
  return path.startsWith('b/') ? path.slice(2) : path;
}

function unquote(path: string): string {
  if (path.length >= 2 && path.startsWith('"') && path.endsWith('"')) {
    return path.slice(1, -1).replace(/\\(.)/g, '$1');
  }
  return path;
}

function parseDiffGitLine(headerLine: string): { aPath: string; bPath: string } {
  const prefix = 'diff --git ';
  if (!headerLine.startsWith(prefix)) return { aPath: '', bPath: '' };
  const rest = headerLine.slice(prefix.length);

  if (rest.startsWith('"')) {
    const quotedMatch = /^"((?:[^"\\]|\\.)*)"\s+"((?:[^"\\]|\\.)*)"$/.exec(rest);
    if (quotedMatch) {
      return {
        aPath: stripAPrefix(unquote(`"${quotedMatch[1]}"`)),
        bPath: stripBPrefix(unquote(`"${quotedMatch[2]}"`)),
      };
    }
  }

  const splitIdx = rest.indexOf(' b/');
  if (splitIdx !== -1) {
    const aPart = rest.slice(0, splitIdx);
    const bPart = rest.slice(splitIdx + 1);
    return {
      aPath: stripAPrefix(unquote(aPart)),
      bPath: stripBPrefix(unquote(bPart)),
    };
  }

  return { aPath: rest, bPath: rest };
}

function firstLine(segment: string): string {
  const idx = segment.indexOf('\n');
  return idx === -1 ? segment : segment.slice(0, idx);
}

export function splitPatchByFile(raw: string): { path: string; patch: string }[] {
  if (raw.length === 0) return [];

  const starts: number[] = [];
  if (raw.startsWith('diff --git ')) starts.push(0);
  let searchFrom = 0;
  while (true) {
    const found = raw.indexOf('\ndiff --git ', searchFrom);
    if (found === -1) break;
    starts.push(found + 1);
    searchFrom = found + 1;
  }
  if (starts.length === 0) return [];

  const segments: { path: string; patch: string }[] = [];
  for (let i = 0; i < starts.length; i += 1) {
    const start = starts[i];
    const end = i + 1 < starts.length ? starts[i + 1] : raw.length;
    const patch = raw.slice(start, end);
    const header = firstLine(patch).replace(/\r$/, '');
    const { bPath } = parseDiffGitLine(header);
    segments.push({ path: bPath, patch });
  }
  return segments;
}
