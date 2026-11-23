import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readdir, readFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { dirname, extname, join, relative } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { ESLint } from 'eslint';
import ts from 'typescript';
import oxc from 'eslint-parser-oxc';
import config from '../../eslint.config.mjs';
import baseline from '../../eslint-policy.json' with { type: 'json' };
import effectivePolicy from './effective-policy.json' with { type: 'json' };
import ownedPackage from './package.json' with { type: 'json' };
import { extensions, ruleNames } from './contract.ts';
import { plugin, readRules, scriptParser } from '@agent-teams/eslint-native-syntax';
import { ruleFixtures } from './fixtures.ts';
import { supportsNativeFloor } from './reject-legacy-typescript.ts';

const cwd = fileURLToPath(new URL('../..', import.meta.url));
const engine = new ESLint({ cwd, overrideConfigFile: true, overrideConfig: config });
const baselineHash = '8855b796f924ba7a5e249404f5b3db33165e76bc067d9b335b64d60ede6e2f08';

test('actual compiler, original baseline and all official rule implementations', async () => {
  assert(supportsNativeFloor(ts.version), `Unexpected TypeScript API: ${ts.version}`);
  for (const version of ['5.9.3', '6.0.3', '7.0.0', '7.0.1', '7.0.2-rc.1', 'unknown']) assert.equal(supportsNativeFloor(version), false);
  for (const version of ['7.0.2', '7.0.2+build', '7.1.0-beta', '8.0.0']) assert.equal(supportsNativeFloor(version), true);
  assert.equal(createHash('sha256').update(await readFile(new URL('../../eslint-policy.json', import.meta.url))).digest('hex'), baselineHash);
  assert.equal(effectivePolicy.baselineSHA256, baselineHash);
  assert.equal(ESLint.version, effectivePolicy.eslint);
  assert.deepEqual(Object.keys(plugin.rules ?? {}).sort(), [...ruleNames].sort());
  for (const name of ruleNames) {
    const rule = plugin.rules?.[name];
    assert(rule && typeof rule.create === 'function', `Missing real upstream implementation: ${name}`);
    assert.equal(rule.meta?.docs?.url, `https://typescript-eslint.io/rules/${name}`);
  }
});

test('installed runtime versions and the local file package match the qualified source bytes', async t => {
  const require = createRequire(import.meta.url);
  for (const [name, version] of Object.entries(ownedPackage.dependencies)) {
    const manifest: unknown = JSON.parse(await readFile(require.resolve(`${name}/package.json`), 'utf8'));
    assert(manifest && typeof manifest === 'object' && 'name' in manifest && manifest.name === name && 'version' in manifest && manifest.version === version, `Unexpected runtime package: ${name}`);
  }
  const localRoot = fileURLToPath(new URL('.', import.meta.url));
  const installedRoot = dirname(require.resolve('@agent-teams/eslint-native-syntax'));
  async function inventory(root: string): Promise<Map<string, string>> {
    const files = new Map<string, string>();
    async function visit(directory: string): Promise<void> {
      for (const entry of await readdir(directory, { withFileTypes: true })) {
        if (entry.name === 'node_modules') continue;
        const path = join(directory, entry.name);
        assert(!entry.isSymbolicLink(), `Unexpected package source symlink: ${path}`);
        if (entry.isDirectory()) await visit(path);
        else {
          assert(entry.isFile());
          files.set(relative(root, path), createHash('sha256').update(await readFile(path)).digest('hex'));
        }
      }
    }
    await visit(root);
    return new Map([...files.entries()].sort(([left], [right]) => left.localeCompare(right, 'en')));
  }
  const source = await inventory(localRoot);
  const installed = await inventory(installedRoot);
  assert.deepEqual(installed, source, 'The installed file package is stale. Recreate the dependency installation from the frozen lockfile before linting.');
  t.diagnostic(`Verified ${source.size} source/package files against the actually imported installed package.`);
});

test('all nine existing file extensions retain the full rule policy and options', async t => {
  for (const extension of extensions) {
    await t.test(extension, async () => {
      const actual = await engine.calculateConfigForFile(`policy.${extension}`);
      const profile = ['ts', 'tsx', 'cts', 'mts'].includes(extension) ? 'ts' : 'vue';
      assert.deepEqual(readRules(actual.rules), effectivePolicy.rules[profile]);
      const before = readRules(baseline.rules[profile]);
      const activeBefore = Object.entries(before).filter(([, value]) => Array.isArray(value) && value[0] !== 0).map(([name]) => name).sort();
      const activeAfter = Object.entries(readRules(actual.rules)).filter(([, value]) => Array.isArray(value) && value[0] !== 0).map(([name]) => name).sort();
      assert.deepEqual(activeAfter, activeBefore);
      assert.equal(activeAfter.length, profile === 'ts' ? 138 : 150);
      assert.equal(activeAfter.filter(name => name.startsWith('@typescript-eslint/')).length, 14);
      assert.equal(actual.linterOptions.reportUnusedDisableDirectives, 0);
      assert.equal(actual.languageOptions.sourceType, 'module');
    });
  }
});

test('real TypeScript, Vue, core and inline-directive findings', async t => {
  for (const [name, source, expected, extension = 'ts'] of ruleFixtures) {
    await t.test(name, async () => {
      const [result] = await engine.lintText(source, { filePath: `qualified-${name}.${extension}` });
      assert(result);
      assert.equal(result.fatalErrorCount, 0, JSON.stringify(result.messages));
      assert(!result.messages.some(message => message.ruleId === null), JSON.stringify(result.messages));
      if (expected === null) assert.deepEqual(result.messages, []);
      else assert(result.messages.some(message => message.ruleId === expected), JSON.stringify(result.messages));
    });
  }
});

test('script and template re-enable directives resume real checking', async () => {
  const [script] = await engine.lintText('/* eslint-disable @typescript-eslint/no-unused-vars */\nconst first = 1;\n/* eslint-enable @typescript-eslint/no-unused-vars */\nconst second = 2;\n/* trailing comment */', { filePath: 'directive.ts' });
  assert(script);
  assert.equal(script.fatalErrorCount, 0);
  assert.deepEqual(script.messages.map(message => [message.ruleId, message.line]), [['@typescript-eslint/no-unused-vars', 4]]);
  const [template] = await engine.lintText('<template>\n<!-- eslint-disable vue/valid-v-if -->\n<div v-if />\n<!-- eslint-enable vue/valid-v-if -->\n<div v-if />\n</template>', { filePath: 'Directive.vue' });
  assert(template);
  assert.equal(template.fatalErrorCount, 0);
  assert.deepEqual(template.messages.map(message => [message.ruleId, message.line]), [['vue/valid-v-if', 5]]);
});

test('unknown inline rules fail rather than becoming no-op rules', async () => {
  const [result] = await engine.lintText('// eslint-disable-next-line @typescript-eslint/unknown-rule\nexport const value = 1;', { filePath: 'unknown-rule.ts' });
  assert(result);
  assert(result.errorCount > 0);
  assert(result.messages.some(message => message.message.includes('Definition for rule')));
});

test('Oxc language delegation retains original paths and native scope implementation', () => {
  const result = scriptParser('ts').parseForESLint('const value = <number>1;', { filePath: 'Example.vue', sourceType: 'module' });
  assert.equal(result.ast.type, 'Program');
  assert.equal(typeof result.scopeManager.addGlobals, 'function');
  assert.deepEqual(result.services, {});
  assert.throws(() => scriptParser('tsx').parseForESLint('const value = <number>1;', { filePath: 'Example.vue' }));
  assert.throws(() => scriptParser('js').parseForESLint('const value: number = 1;', { filePath: 'Example.vue' }));
  assert.equal(oxc.meta.version, '0.1.1');
});

test('filename-only source adaptations preserve diagnostics and suggestion output', async t => {
  for (const extension of ['ts', 'cts', 'mts', 'tsx']) {
    await t.test(extension, async () => {
      const source = 'export const identity = <T extends unknown>(value: T): T => value;';
      const [result] = await engine.lintText(source, { filePath: `suggestion.${extension}` });
      assert(result);
      assert.equal(result.fatalErrorCount, 0);
      const diagnostic = result.messages.find(message => message.ruleId === '@typescript-eslint/no-unnecessary-type-constraint');
      const suggestion = diagnostic?.suggestions?.[0];
      assert(suggestion);
      const { range, text } = suggestion.fix;
      assert.equal(source.slice(0, range[0]) + text + source.slice(range[1]), `export const identity = <T${extension === 'ts' ? '' : ','}>(value: T): T => value;`);
    });
  }
});

test('unchanged official fixer remains operational', async () => {
  const fixing = new ESLint({ cwd, overrideConfigFile: true, overrideConfig: config, fix: true });
  const [result] = await fixing.lintText('export const value = new Array(1, 2);', { filePath: 'fixer.ts' });
  assert(result);
  assert.equal(result.output, 'export const value = [1, 2];');
  assert.equal(result.errorCount, 0);
});

test('every application source file remains in the owning lint file set', async t => {
  const files: string[] = [];
  async function visit(directory: string): Promise<void> {
    for (const entry of await readdir(directory, { withFileTypes: true })) {
      const path = join(directory, entry.name);
      if (entry.isDirectory()) await visit(path);
      else if (entry.isFile() && extensions.some(extension => extname(path) === `.${extension}`)) files.push(path);
    }
  }
  await visit(join(cwd, 'src'));
  assert(files.length > 0);
  for (const file of files) assert.equal(await engine.isPathIgnored(file), false, `Application file dropped from lint: ${file}`);
  t.diagnostic(`All ${files.length} application source files remain selected by the owning ESLint configuration.`);
});

test('invalid scripts and Vue template/style expressions retain real parse errors and source locations', async t => {
  const cases = [
    { name: 'js', source: '\nconst value = ;\n', file: 'Invalid.js', line: 2 },
    { name: 'ts', source: '\nconst value: = 1;\n', file: 'Invalid.ts', line: 2 },
    { name: 'vue-script', source: '<script setup lang="ts">\nconst value: = 1;\n</script><template><div /></template>', file: 'InvalidScript.vue', line: 2 },
    { name: 'vue-template', source: '<template>\n<div>{{ value + }}</div>\n</template>', file: 'InvalidTemplate.vue', line: 2 },
    { name: 'vue-style', source: '<template><div /></template>\n<style scoped>\ndiv { color: v-bind(value +); }\n</style>', file: 'InvalidStyle.vue', line: 3 },
  ];
  for (const item of cases) {
    await t.test(item.name, async () => {
      const [result] = await engine.lintText(item.source, { filePath: item.file });
      assert(result);
      const messages = result.messages.filter(message => message.fatal || message.ruleId === 'vue/no-parsing-error');
      assert(messages.length > 0, JSON.stringify(result.messages));
      assert(messages.some(message => message.line === item.line && typeof message.column === 'number' && message.column > 0), JSON.stringify(messages));
    });
  }
});
