import type { ESLint, Rule } from 'eslint';

declare const bundle: {
  default: ESLint.Plugin;
  rules: Readonly<Record<string, Rule.RuleModule>>;
};

export = bundle;
