import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdtemp, readdir, readFile, mkdir, rm, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { tmpdir } from 'node:os';
import { dirname, join, relative, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { build, version as esbuildVersion } from 'esbuild';
import { ruleNames, upstreamCommit, upstreamVersion } from './contract.ts';
import upstreamManifest from './upstream-manifest.json' with { type: 'json' };
import { supportsNativeFloor } from './reject-legacy-typescript.ts';

const root = fileURLToPath(new URL('.', import.meta.url));
const vendor = join(root, 'vendor');
const hash = (value: string | Uint8Array): string => createHash('sha256').update(value).digest('hex');
const slash = (value: string): string => value.split(sep).join('/');

interface SourcePatch {
  path: string;
  before: string;
  after: string;
  reason: string;
}

function between(source: string, startMarker: string, endMarker: string): string {
  const start = source.indexOf(startMarker);
  const end = source.indexOf(endMarker, start);
  assert(start >= 0 && end > start, 'Pinned upstream source boundary changed');
  return source.slice(start, end);
}

function adaptSource(path: string, source: string): { contents: string; reason: string } | undefined {
  switch (path) {
    case 'packages/eslint-plugin/src/util/index.ts':
      return {
        contents: [
          "export { createRule } from './createRule';",
          "export { collectVariables } from './collectUnusedVariables';",
          "export { getNameLocationInGlobalDirectiveComment, isFunction } from './astUtils';",
          "export { getStaticStringValue } from './getStaticStringValue';",
          "export { isDefinitionFile } from './misc';",
          "export { nullThrows, NullThrowsReasons } from '@typescript-eslint/utils/eslint-utils';",
          '',
        ].join('\n'),
        reason: 'Export the unchanged syntax helpers used by the selected rules; omit unrelated typed rules.',
      };
    case 'packages/utils/src/index.ts':
      return {
        contents: [
          "export * as ASTUtils from './ast-utils';",
          "export * as ESLintUtils from './eslint-utils';",
          "export * as TSESLint from './ts-eslint';",
          "export { AST_NODE_TYPES, AST_TOKEN_TYPES } from '@typescript-eslint/types';",
          '',
        ].join('\n'),
        reason: 'Export only the official namespaces and AST constants used by the syntax slice.',
      };
    case 'packages/utils/src/eslint-utils/index.ts':
      return {
        contents: "export { RuleCreator } from './RuleCreator';\nexport { nullThrows, NullThrowsReasons } from './nullThrows';\n",
        reason: 'Retain the official rule factory and assertions without typed parser services.',
      };
    case 'packages/utils/src/ts-eslint/index.ts':
      return {
        contents: "export { Scope } from './Scope';\n",
        reason: 'Retain the original Scope export without unused ESLint wrapper classes.',
      };
    case 'packages/eslint-plugin/src/util/astUtils.ts':
      return {
        contents: [
          "import type { TSESLint, TSESTree } from '@typescript-eslint/utils';",
          "import { escapeRegExp } from './escapeRegExp';",
          "export * from '@typescript-eslint/utils/ast-utils';",
          between(source, 'export function getNameLocationInGlobalDirectiveComment(', '\n// Copied from typescript '),
          '',
        ].join('\n'),
        reason: 'Keep the exact global-comment helper and ESTree predicates; omit compiler-node utilities.',
      };
    case 'packages/eslint-plugin/src/util/misc.ts':
      return {
        contents: "const DEFINITION_EXTENSIONS = ['.d.ts', '.d.cts', '.d.mts'] as const;\n" +
          between(source, 'export function isDefinitionFile(', '\n/**\n * Upper cases the first character or the string\n */') + '\n',
        reason: 'Use the same literal filename extensions with the original definition-file predicate.',
      };
    case 'packages/eslint-plugin/src/rules/no-unnecessary-type-constraint.ts': {
      const changes = [
        ["import * as ts from 'typescript';", ''],
        [' as ts.Extension', ''],
        ['ts.Extension.Cts', "'.cts'"],
        ['ts.Extension.Mts', "'.mts'"],
        ['ts.Extension.Tsx', "'.tsx'"],
      ];
      let contents = source;
      for (const [before, after] of changes) {
        assert(before !== undefined && after !== undefined);
        assert.equal(contents.split(before).length, 2, `Pinned upstream expression changed: ${before}`);
        contents = contents.replace(before, after);
      }
      return { contents, reason: 'Use ordinary filename strings instead of unavailable classic compiler enum properties.' };
    }
    default:
      return undefined;
  }
}

export async function buildRules(check: boolean): Promise<void> {
  assert.equal(upstreamManifest.commit, upstreamCommit);
  assert.equal(upstreamManifest.tag, `v${upstreamVersion}`);
  assert.equal(upstreamManifest.license, 'MIT');
  const knownInputs = new Map(upstreamManifest.files.map(file => [file.path, file]));
  assert.equal(knownInputs.size, upstreamManifest.files.length, 'Duplicate upstream inputs');
  const vendorFiles: string[] = [];
  async function inspectDirectory(directory: string): Promise<void> {
    for (const item of await readdir(directory, { withFileTypes: true })) {
      const path = join(directory, item.name);
      assert(!item.isSymbolicLink(), `Symlink in immutable upstream source: ${path}`);
      if (item.isDirectory()) await inspectDirectory(path);
      else {
        assert(item.isFile(), `Unexpected upstream file type: ${path}`);
        vendorFiles.push(slash(relative(vendor, path)));
      }
    }
  }
  await inspectDirectory(vendor);
  assert.deepEqual(vendorFiles.sort(), [...knownInputs.keys()].sort(), 'Unexpected or missing vendored files');
  for (const file of upstreamManifest.files) {
    assert.equal(file.version, upstreamVersion);
    assert(file.path === 'LICENSE' || file.package === '@typescript-eslint/eslint-plugin' || file.package === '@typescript-eslint/utils');
    const bytes = await readFile(join(vendor, file.path));
    assert.equal(hash(bytes), file.sha256, `Upstream source changed: ${file.path}`);
    const gitBlob = createHash('sha1').update(`blob ${bytes.byteLength}\0`).update(bytes).digest('hex');
    assert.equal(gitBlob, file.gitBlob, `Upstream Git blob changed: ${file.path}`);
  }

  const patches: SourcePatch[] = [];
  const entry = ruleNames.map((name, index) => `import rule${index} from './vendor/packages/eslint-plugin/src/rules/${name}.ts';`).join('\n') +
    '\nexport const rules = {\n' + ruleNames.map((name, index) => `  '${name}': rule${index},`).join('\n') +
    "\n};\nexport default { meta: { name: '@agent-teams/eslint-native-syntax', version: '0.1.0' }, rules };\n";
  const external = new Set(['@typescript-eslint/types', '@typescript-eslint/scope-manager', '@eslint-community/eslint-utils', 'eslint/use-at-your-own-risk', 'node:path']);
  const result = await build({
    absWorkingDir: root,
    stdin: { contents: entry, resolveDir: root, sourcefile: 'generated-rule-entry.ts', loader: 'ts' },
    outfile: 'generated/rules.cjs',
    write: false,
    bundle: true,
    platform: 'node',
    format: 'cjs',
    target: 'node24',
    treeShaking: true,
    metafile: true,
    sourcemap: false,
    banner: { js: '// Generated from pinned MIT-licensed typescript-eslint sources; see ../vendor/LICENSE and provenance.json.' },
    external: [...external],
    tsconfigRaw: { compilerOptions: { target: 'ES2022', useDefineForClassFields: true } },
    plugins: [{
      name: 'pinned-upstream-syntax-source',
      setup(api) {
        api.onResolve({ filter: /^(?:typescript(?:\/|$)|ts-api-utils(?:\/|$)|@typescript\/|@typescript-eslint\/(?:parser|typescript-estree|type-utils)(?:\/|$))/ }, ({ path }) => {
          throw new Error(`Compiler and type-checker implementations cannot enter the syntax bundle: ${path}`);
        });
        api.onResolve({ filter: /^@typescript-eslint\/utils(?:\/.*)?$/ }, ({ path }) => {
          const subpath = path.slice('@typescript-eslint/utils'.length).replace(/^\//, '');
          return { path: join(vendor, 'packages/utils/src', subpath, 'index.ts') };
        });
        api.onLoad({ filter: /\.ts$/ }, async ({ path }) => {
          const sourcePath = slash(relative(vendor, path));
          const source = await readFile(path, 'utf8');
          const expected = knownInputs.get(sourcePath);
          assert(expected, `Unregistered upstream source: ${sourcePath}`);
          assert.equal(hash(source), expected.sha256);
          const patch = adaptSource(sourcePath, source);
          if (patch) {
            patches.push({ path: sourcePath, before: hash(source), after: hash(patch.contents), reason: patch.reason });
          }
          return { contents: patch?.contents ?? source, loader: 'ts', resolveDir: dirname(path) };
        });
      },
    }],
  });
  assert(result.metafile && result.outputFiles?.length === 1, 'Expected one auditable bundle');
  const output = result.outputFiles[0];
  assert(output);
  const inputRecords = [];
  for (const path of Object.keys(result.metafile.inputs).sort()) {
    if (path === 'generated-rule-entry.ts') {
      inputRecords.push({ path, package: '@agent-teams/eslint-native-syntax', version: '0.1.0', sha256: hash(entry) });
      continue;
    }
    const sourcePath = slash(relative(vendor, resolve(root, path)));
    const expected = knownInputs.get(sourcePath);
    assert(expected, `Unregistered or embedded dependency input: ${path}`);
    inputRecords.push({ path, package: expected.package, version: expected.version, sha256: expected.sha256 });
  }
  assert.equal(inputRecords.length, upstreamManifest.files.length, 'Missing or unexpected source inputs');
  for (const outputInfo of Object.values(result.metafile.outputs)) {
    for (const dependency of outputInfo.imports) {
      assert(dependency.external && external.has(dependency.path), `Unexpected runtime dependency: ${dependency.path}`);
    }
  }
  assert.equal(patches.length, 7, 'All seven documented source boundaries must be checked');
  patches.sort((left, right) => left.path.localeCompare(right.path, 'en'));
  const require = createRequire(import.meta.url);
  const compiler = join(dirname(require.resolve('typescript/package.json')), 'lib/tsc.js');
  const guard = join(root, 'reject-legacy-typescript.ts');
  const version = spawnSync(process.execPath, ['--import', guard, compiler, '--version'], { encoding: 'utf8', env: process.env });
  assert.ifError(version.error);
  assert.equal(version.status, 0, version.stderr);
  const compilerVersion = version.stdout.trim().replace(/^Version /, '');
  assert(supportsNativeFloor(compilerVersion), `Unsupported semantic compiler: ${compilerVersion}`);
  const temporary = await mkdtemp(join(tmpdir(), 'native-syntax-runtime-'));
  const runtimeFiles = new Map<string, string>();
  try {
    const compiled = spawnSync(process.execPath, ['--import', guard, compiler, '--project', join(root, 'tsconfig.runtime.json'), '--outDir', temporary], { encoding: 'utf8', env: process.env });
    assert.ifError(compiled.error);
    assert.equal(compiled.status, 0, compiled.stdout + compiled.stderr);
    const names = ['index.js', 'index.d.ts', 'reject-legacy-typescript.js', 'reject-legacy-typescript.d.ts'];
    assert.deepEqual((await readdir(temporary)).sort(), [...names].sort(), 'Unexpected runtime compiler outputs');
    for (const name of names) runtimeFiles.set(name, await readFile(join(temporary, name), 'utf8'));
  } finally {
    await rm(temporary, { recursive: true, force: true });
  }
  const provenance = {
    contract: 'native-syntax-rule-bundle/v1',
    upstreamCommit,
    upstreamVersion,
    esbuildVersion,
    availableRules: ruleNames,
    sourcePatches: patches,
    inputs: inputRecords,
    embeddedCompilerImplementations: 0,
    bundleSHA256: hash(output.contents),
    runtimeCompiler: {
      version: compilerVersion,
      configSHA256: hash(await readFile(join(root, 'tsconfig.runtime.json'))),
      source: await Promise.all(['index.ts', 'reject-legacy-typescript.ts'].map(async name => ({ path: name, sha256: hash(await readFile(join(root, name))) }))),
      outputs: [...runtimeFiles].map(([path, contents]) => ({ path, sha256: hash(contents) })),
    },
    metafile: result.metafile,
  };
  const files = new Map([
    ['generated/rules.cjs', output.text],
    ['generated/provenance.json', JSON.stringify(provenance, null, 2) + '\n'],
    ...runtimeFiles,
  ]);
  await mkdir(join(root, 'generated'), { recursive: true });
  for (const [name, contents] of files) {
    const path = join(root, name);
    if (check) assert.equal(await readFile(path, 'utf8'), contents, `Generated artifact differs: ${name}`);
    else await writeFile(path, contents);
  }
  process.stdout.write(`Verified ${inputRecords.length} pinned inputs and ${ruleNames.length} official rules; embedded compiler implementations: 0\n`);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  assert(process.argv.length <= 3 && (process.argv[2] === undefined || process.argv[2] === '--check'), 'Usage: build.ts [--check]');
  await buildRules(process.argv[2] === '--check');
}
