import { execFileSync, spawn, type ChildProcessWithoutNullStreams } from 'node:child_process';
import { createServer } from 'node:net';
import { mkdtempSync, mkdirSync, rmSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { tmpdir } from 'node:os';

export interface DaemonHandle {
  daemon: ChildProcessWithoutNullStreams;
  baseURL: string;
  bootstrap: string;
  dataDir: string;
  binary: string;
}

export function freePort(): Promise<number> {
  return new Promise((resolvePort, reject) => {
    const server = createServer();
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => {
      const address = server.address();
      if (!address || typeof address === 'string') return reject(new Error('No TCP address'));
      server.close(() => resolvePort(address.port));
    });
  });
}

export interface StartDaemonOptions {
  env?: NodeJS.ProcessEnv;
}

export async function startDaemon(options: StartDaemonOptions = {}): Promise<DaemonHandle> {
  const root = resolve(process.cwd(), '..');
  const buildDir = mkdtempSync(join(tmpdir(), 'studioforge-e2e-build-'));
  const dataDir = mkdtempSync(join(tmpdir(), 'studioforge-e2e-data-'));
  mkdirSync(buildDir, { recursive: true });
  const binary = join(buildDir, process.platform === 'win32' ? 'studioforge.exe' : 'studioforge');
  execFileSync('go', ['build', '-o', binary, './cmd/studioforge'], { cwd: root, stdio: 'inherit' });
  const port = await freePort();
  const daemon = spawn(
    binary,
    ['--mock', '--no-open', '--port', String(port), '--data-dir', dataDir],
    {
      cwd: root,
      env: options.env ? { ...process.env, ...options.env } : process.env,
    },
  );
  let baseURL = '';
  let bootstrap = '';
  await new Promise<void>((resolveReady, reject) => {
    let output = '';
    const timeout = setTimeout(
      () => reject(new Error(`Daemon startup timed out: ${output}`)),
      20_000,
    );
    daemon.stdout.on('data', (chunk) => {
      output += chunk.toString();
      baseURL = output.match(/STUDIOFORGE_URL=(.+)/)?.[1]?.trim() ?? baseURL;
      bootstrap = output.match(/STUDIOFORGE_BOOTSTRAP=(.+)/)?.[1]?.trim() ?? bootstrap;
      if (baseURL && bootstrap) {
        clearTimeout(timeout);
        resolveReady();
      }
    });
    daemon.once('exit', (code) => {
      clearTimeout(timeout);
      reject(new Error(`Daemon exited early with ${code}: ${output}`));
    });
  });
  return { daemon, baseURL, bootstrap, dataDir, binary };
}

export async function stopDaemon(handle: DaemonHandle): Promise<void> {
  const { daemon, dataDir, binary } = handle;
  if (daemon && !daemon.killed) {
    daemon.kill();
    await new Promise((resolveExit) => {
      daemon.once('exit', resolveExit);
      setTimeout(resolveExit, 3_000);
    });
  }
  if (dataDir) rmSync(dataDir, { recursive: true, force: true });
  if (binary) rmSync(resolve(binary, '..'), { recursive: true, force: true });
}
