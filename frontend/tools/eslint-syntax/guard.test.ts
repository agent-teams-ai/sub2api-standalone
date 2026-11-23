import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath, pathToFileURL } from 'node:url';

test('repeated caught legacy imports remain blocked before any module initialization', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'native-lint-denied-import-'));
  try {
    // This is an inert initialization sentinel, not a compiler implementation or
    // substitute API. The manifest exercises the guard's package/version boundary.
    await writeFile(join(directory, 'package.json'), JSON.stringify({ name: 'typescript', version: '6.0.3', type: 'module' }));
    const entry = join(directory, 'sentinel.mjs');
    await writeFile(entry, 'globalThis.nativeLintSentinelInitialized = true; export default 1;\n');
    const log = join(directory, 'attempts.jsonl');
    const script = `
      const attempts = [];
      for (let index = 0; index < 2; index++) {
        try { await import(${JSON.stringify(pathToFileURL(entry).href)}); attempts.push('loaded'); }
        catch (error) { attempts.push(String(error)); }
      }
      process.stdout.write(JSON.stringify({ attempts, initialized: globalThis.nativeLintSentinelInitialized ?? false }));
    `;
    const child = spawnSync(process.execPath, ['--import', fileURLToPath(new URL('./reject-legacy-typescript.ts', import.meta.url)), '--input-type=module', '--eval', script], {
      encoding: 'utf8',
      env: { ...process.env, NATIVE_LINT_TYPESCRIPT_LOAD_LOG: log },
    });
    assert.ifError(child.error);
    assert.equal(child.status, 1, child.stderr);
    const outcome: unknown = JSON.parse(child.stdout);
    assert.deepEqual(outcome, {
      attempts: ['Error: Legacy TypeScript initialization blocked: typescript@6.0.3', 'Error: Legacy TypeScript initialization blocked: typescript@6.0.3'],
      initialized: false,
    });
    const attempts = (await readFile(log, 'utf8')).trim().split('\n');
    assert.equal(attempts.length, 2);
    for (const attempt of attempts) {
      const record: unknown = JSON.parse(attempt);
      assert(record && typeof record === 'object' && 'allowed' in record && record.allowed === false);
    }
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
