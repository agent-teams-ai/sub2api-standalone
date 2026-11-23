export const upstreamCommit = 'ca9c0eb71075c197e7c27c7eee2e65c6a3d24fc9';
export const upstreamVersion = '8.73.0';

// The existing Native profile enables the first fourteen rules. The final
// rule remains available for existing inline configuration and disable comments.
export const ruleNames = [
  'no-array-constructor',
  'no-duplicate-enum-values',
  'no-extra-non-null-assertion',
  'no-loss-of-precision',
  'no-misused-new',
  'no-namespace',
  'no-non-null-asserted-optional-chain',
  'no-this-alias',
  'no-unnecessary-type-constraint',
  'no-unsafe-declaration-merging',
  'no-unused-vars',
  'no-var-requires',
  'prefer-as-const',
  'triple-slash-reference',
  'no-explicit-any',
] as const;

export const extensions = ['vue', 'js', 'jsx', 'cjs', 'mjs', 'ts', 'tsx', 'cts', 'mts'] as const;
