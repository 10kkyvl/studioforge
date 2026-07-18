import { readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const output = resolve(process.cwd(), '../internal/webui/dist');
const indexPath = resolve(output, 'index.html');
const index = await readFile(indexPath, 'utf8');
const match = index.match(/<script>([\s\S]*?)<\/script>/);
if (!match) throw new Error('SvelteKit bootstrap script was not found in index.html');
await writeFile(resolve(output, 'bootstrap.js'), match[1].trimStart(), 'utf8');
await writeFile(
  indexPath,
  index.replace(match[0], '<script src="/bootstrap.js"></script>').replace(/^[\t ]+$/gm, ''),
  'utf8',
);
