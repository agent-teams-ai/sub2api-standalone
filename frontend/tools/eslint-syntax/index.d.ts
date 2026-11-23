import type { ESLint, Linter } from 'eslint';
import oxc, { type Lang } from 'eslint-parser-oxc';
export declare const plugin: ESLint.Plugin;
export declare function readRules(value: unknown): Linter.RulesRecord;
export declare function rulesFor(value: unknown): Linter.RulesRecord;
export declare function scriptParser(lang: Lang): typeof oxc;
export declare function createConfig(policy: unknown): Linter.Config[];
