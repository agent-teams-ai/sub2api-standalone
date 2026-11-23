import assert from 'node:assert/strict';
import oxc from 'eslint-parser-oxc';
import vueParser from 'vue-eslint-parser';
import vue from 'eslint-plugin-vue';
import globals from 'globals';
import bundle from './generated/rules.cjs';
export const plugin = bundle.default;
function isRecord(value) {
    return typeof value === 'object' && value !== null && !Array.isArray(value);
}
export function readRules(value) {
    assert(isRecord(value), 'Expected a rule record');
    const rules = {};
    for (const [name, entry] of Object.entries(value)) {
        assert(Array.isArray(entry), `Expected a rule tuple: ${name}`);
        const values = entry;
        const severity = values[0];
        assert(severity === 0 || severity === 1 || severity === 2, `Invalid rule severity: ${name}`);
        rules[name] = [severity, ...values.slice(1)];
    }
    return rules;
}
export function rulesFor(value) {
    const rules = readRules(value);
    // Preserve the established defaults across the supported tool versions.
    assert.deepEqual(rules['@typescript-eslint/no-unused-vars'], [1, { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }]);
    assert.deepEqual(rules['no-inner-declarations'], [2]);
    assert.deepEqual(rules['no-shadow-restricted-names'], [2]);
    rules['@typescript-eslint/no-unused-vars'] = [1, { argsIgnorePattern: '^_', varsIgnorePattern: '^_', caughtErrors: 'none' }];
    rules['no-inner-declarations'] = [2, 'functions', { blockScopedFunctions: 'disallow' }];
    rules['no-shadow-restricted-names'] = [2, { reportGlobalThis: false }];
    return rules;
}
export function scriptParser(lang) {
    return {
        meta: { name: `@agent-teams/eslint-native-syntax/${lang}`, version: '0.1.0' },
        parse(code, options) {
            return oxc.parse(code, { ...options, lang });
        },
        parseForESLint(code, options) {
            // Vue supplies the original .vue path. Use Oxc's public language option
            // so a TypeScript script is not silently interpreted as TSX.
            return oxc.parseForESLint(code, { ...options, lang });
        },
    };
}
export function createConfig(policy) {
    assert(isRecord(policy) && isRecord(policy.rules), 'Expected a versioned baseline policy');
    const profiles = policy.rules;
    return [
        {
            ignores: [
                '**/node_modules/**', '**/dist/**', '**/.cache/**', '**/.vite/**',
                'vite.config.js', 'vite.config.d.ts',
                // Immutable third-party sources are hash checked by build.ts. The
                // maintained adapter, config, build and tests remain in normal lint.
                'tools/eslint-syntax/vendor/**',
                'tools/eslint-syntax/generated/**',
                'tools/eslint-syntax/index.js', 'tools/eslint-syntax/index.d.ts',
                'tools/eslint-syntax/reject-legacy-typescript.js', 'tools/eslint-syntax/reject-legacy-typescript.d.ts',
            ],
        },
        {
            files: ['**/*.{js,jsx,mjs,cjs,ts,tsx,mts,cts,vue}'],
            plugins: { '@typescript-eslint': plugin, vue },
            linterOptions: { reportUnusedDisableDirectives: 'off' },
            languageOptions: {
                parser: oxc,
                ecmaVersion: 'latest',
                sourceType: 'module',
                globals: { ...globals.browser, ...globals.es2021, ...globals.node },
            },
            rules: rulesFor(profiles.cjs),
        },
        { files: ['**/*.{ts,tsx,mts,cts}'], rules: rulesFor(profiles.ts) },
        {
            files: ['**/*.vue'],
            languageOptions: {
                parser: vueParser,
                parserOptions: {
                    parser: { ts: scriptParser('ts'), tsx: scriptParser('tsx'), js: scriptParser('js'), jsx: scriptParser('jsx') },
                    extraFileExtensions: ['.vue'],
                },
            },
            processor: vue.processors.vue,
            rules: rulesFor(profiles.vue),
        },
    ];
}
