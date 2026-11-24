# Native syntax lint

This package supplies the syntax lint used by the Native frontend. The semantic
compiler remains the native TypeScript 7.1 compiler selected by the owning
frontend package. The lint parser uses Oxc; it does not use a classic TypeScript
compiler or parser API.

## Preserved policy

`frontend/eslint-policy.json` records the existing effective rule selection from
ESLint 8.57.1, typescript-eslint 7.18.0 and eslint-plugin-vue 9.33.0, together with
the repository overrides. It was composed from the published static configs;
capturing it did not execute a TypeScript compiler API. The same nine extensions
remain selected. There are 138 enabled rules for TypeScript extensions and 150
for Vue and JavaScript extensions, including 14 enabled TypeScript syntax rules.

The current supported runtime is ESLint 10.15.0, eslint-plugin-vue 10.12.1,
vue-eslint-parser 10.4.2, eslint-parser-oxc 0.1.1 and its native Oxc 0.148 parser.
Published typescript-eslint 8.73.0 supplies its scope manager, visitor keys and
AST constants. Vue still owns SFC parsing, script-setup scopes, template rules,
style `v-bind()` references and comment directives. Four small parser delegates
set Oxc's public `lang` option while keeping the original file path; they do not
modify its AST or implement a scope manager.

Three explicit options retain old defaults: `caughtErrors: "none"` for
`@typescript-eslint/no-unused-vars`, `blockScopedFunctions: "disallow"` for
`no-inner-declarations`, and `reportGlobalThis: false` for
`no-shadow-restricted-names`. Unused disable comments keep their former policy.
`effective-policy.json` fixes every resolved rule option for the maintained
profile. The globally disabled `no-explicit-any` implementation remains present
because existing files reference it in comments; enabling it inline still
reports real findings. Other existing disabled rules remain disabled.

## Upstream source and adaptations

The normal typescript-eslint plugin imports classic compiler APIs. Its public
lazy rules entry avoids loading unrelated rules, but the selected rules still
import shared utility barrels that initialize classic APIs. The package here is
an explicitly maintained source adaptation, not an upstream-supported plugin
entry point or an alternative implementation of the TypeScript API.

The 48 original TypeScript inputs in `vendor/` come from the official
[typescript-eslint v8.73.0 source](https://github.com/typescript-eslint/typescript-eslint/tree/ca9c0eb71075c197e7c27c7eee2e65c6a3d24fc9).
`upstream-manifest.json` pins the commit, package versions, SHA-256 and Git blob
identity of each input and of the original MIT license. Source bytes stay
unchanged. The build applies seven small, hash-checked adaptations:

- Four export barrels expose only the syntax helpers, AST constants and original
  scope namespace used by the selected rules.
- The global-comment location helper is retained verbatim without unrelated
  compiler-node helpers in its source file.
- The definition-file helper is retained verbatim with the same three filename
  strings instead of compiler enum values.
- The unnecessary-type-constraint rule uses the same filename strings for its
  `.cts`, `.mts` and `.tsx` suggestion behavior.

All 15 rule implementations otherwise remain upstream code, including their
diagnostics, scope analysis, suggestions and fixers. This is a syntax-only
profile. It does not supply type-aware lint rules, and it does not replace the
frontend's independent native TypeScript semantic checks.

## Owning verification

`pnpm run lint:check` runs the strict TypeScript 7 tooling project, verifies the
generated bundle, runs the maintained negative and positive fixtures, and then
executes the real ESLint CLI over the frontend. `pnpm run lint` performs the same
qualification before the existing fix operation. These commands are used by the
existing required frontend CI lanes on Node 24 and Node 26.

The qualification verifies all nine extension profiles, all rule IDs and
options, availability of each real rule implementation, invalid TS/JS and SFC
parse errors with their locations, template and script directives, script-setup
and style references, definition-file behavior, extension-sensitive suggestions
and a real fixer. It also checks that every application source remains selected.

The runtime guard rejects compiler packages below 7.0.2 before initialization,
including repeated imports after a caught rejection. Its negative test uses an
inert sentinel with an intentionally forbidden package manifest; it never loads
a legacy compiler. Set `NATIVE_LINT_TYPESCRIPT_LOAD_LOG` to a writable JSONL path
to retain load observations.

The load guard is only one boundary. `build.ts --check` verifies the complete
vendored file set, original hashes and Git blobs, every esbuild input and every
external dependency. Compiler, parser, type-utils and type-checker imports are
rejected at bundling. Unknown inputs fail the build. The generated provenance
includes exact inputs, transformations, esbuild metadata and the output hash;
regeneration must match both checked-in artifacts byte for byte. Therefore a
prebuilt bundle cannot silently embed an unchecked compiler implementation.

To deliberately regenerate after reviewing an upstream or adapter change, run
`node --import ./tools/eslint-syntax/reject-legacy-typescript.ts tools/eslint-syntax/build.ts`
from `frontend/`, then run the owning qualification. Dependency updates require
reviewing the original baseline defaults and the full effective-policy diff.
The only production dependency change removes the unused direct
`@lobehub/icons` dependency and its now unreachable peer dependency tree. The
application renders the existing local SVG paths; both components and their
upstream attribution comments are unchanged. All other production dependency
versions, the Vue/Vite runtime, app code and backend remain unchanged.

The consumer imports the installed `file:tools/eslint-syntax` package. The
qualification compares every installed package file with these source bytes,
including the bundle and provenance. After editing this package, recreate its
dependency installation from the frozen lockfile. A stale installed copy fails
qualification before the owning lint can run; it is not silently used instead
of the reviewed source.

The installed package exports compiled JavaScript and compiler-emitted
declarations. Its TypeScript sources are retained for review and reproducible
builds; consumers do not depend on Node stripping TypeScript in `node_modules`.
The same native TypeScript compiler emits the runtime adapter and guard in
`build.ts`, and their source/output hashes are included in the provenance.

## Licensing

The maintained adapter, configuration, build and qualification code follow the
repository's LGPL-3.0-or-later license, copied to `LICENSE`. The upstream inputs
retain their MIT license in `vendor/LICENSE`. Their copyright and license
notices remain available alongside the generated bundle.
