import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { createRequire } from 'node:module';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { extensions } from './contract.ts';

const args = process.argv.slice(2);
assert(args.length === 0 || (args.length === 1 && args[0] === '--fix'), 'Usage: run-eslint.ts [--fix]');
const require = createRequire(import.meta.url);
const cli = join(dirname(require.resolve('eslint/package.json')), 'bin/eslint.js');
const guard = fileURLToPath(new URL('./reject-legacy-typescript.ts', import.meta.url));
const result = spawnSync(process.execPath, ['--import', guard, cli, '.', '--ext', extensions.map(extension => `.${extension}`).join(','), ...args], {
  cwd: fileURLToPath(new URL('../..', import.meta.url)),
  env: process.env,
  stdio: 'inherit',
});
if (result.error) throw result.error;
if (result.signal) throw new Error(`ESLint terminated by ${result.signal}`);
process.exitCode = result.status ?? 1;
