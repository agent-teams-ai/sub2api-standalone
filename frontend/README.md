# Frontend checks

Use the repository's Node.js and pnpm pins. Install dependencies with
`pnpm install --frozen-lockfile`.

- `pnpm run typecheck` checks the application, the Vite configuration, and the
  provider connection test files. It uses the native TypeScript compiler.
- `pnpm run build` runs the locale completeness check and that same typecheck
  before producing the Vite production bundle.
- `pnpm run dev` starts Vite and the native compiler in watch mode. Type errors
  appear in the terminal and Vite's browser overlay, which clears after a
  successful recheck. Closing Vite also closes the compiler process tree.
- `pnpm run lint:check` performs the separately configured syntax lint checks.
- `pnpm run test:run` runs the Vitest suite.

## Vue and TypeScript

The frontend pins `typescript@7.1.0-beta` and
`@vue/content-mapper@4.0.0-alpha.3`. These are prerelease versions. TypeScript
7.0.2 does not support the content mapper protocol needed to typecheck Vue
single-file components with its native compiler. The rest of the Vue and Vite
toolchain keeps its existing versions.

The mapper is configured in `tsconfig.json`. TypeScript receives
`--runExternalCode` explicitly so that it may run this pinned mapper. Both the
TypeScript files and the generated Vue template code are checked by TypeScript
7.1; there is no fallback to an earlier semantic compiler. The mapper also uses
the project's TypeScript 7.1 AST API, so the `typescript` package in this directory
must remain the native compiler package.

Vue's content mapper always checks `v-model` assignments and unknown component
props. The former `strictTemplates` and `strictVModel` options have no equivalent
in this mapper. Consumers of the shared Select component therefore validate its
actual event value before assigning it to a narrower field. The Select event can
still carry strings, numbers, booleans, or null, including clearable and creatable
selections and custom value keys.

The previous `vue-tsc` and `vite-plugin-checker` paths were removed because they
did not run this native Vue checking path. The Vite development plugin invokes
the same compiler directly with watch mode and the content mapper permission.
The Vite commands select `vite.config.ts` explicitly so that the generated
`vite.config.js` from the referenced Node project cannot shadow the current
configuration.

## Lint qualification

`pnpm lint:check` runs the maintained [native syntax lint package](tools/eslint-syntax/README.md)
with ESLint, Vue and Oxc. It retains the existing effective rule policy, file
coverage and inline directives through pinned upstream syntax-rule sources.
The owning command checks the strict native TypeScript tooling projects,
rebuilds and compares compiled JavaScript and declarations, verifies source
provenance and installed package bytes, and runs the actual ESLint CLI.

Lint remains a required gate alongside typechecking, tests and builds. Its
qualification rejects older compiler API initialization, including a repeated
import after a caught rejection; an older compiler or embedded parser is not
an accepted fallback.

Upstream references:

- [TypeScript 7.1 beta and content mappers](https://devblogs.microsoft.com/typescript/announcing-typescript-7-1-beta/)
- [Vue content mapper package and documentation](https://www.npmjs.com/package/@vue/content-mapper/v/4.0.0-alpha.3)
- [TypeScript 7.0 compatibility notes](https://devblogs.microsoft.com/typescript/announcing-typescript-7-0/)
