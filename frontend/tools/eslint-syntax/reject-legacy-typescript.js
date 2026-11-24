import { appendFileSync, existsSync, readFileSync } from 'node:fs';
import { registerHooks } from 'node:module';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
const compilerPackages = new Set(['typescript', '@typescript/typescript6', '@typescript/native', 'typescript-native-bridge']);
const checked = new Set();
const packageCache = new Map();
let rejected = false;
export function supportsNativeFloor(version) {
    const parts = /^(\d+)\.(\d+)\.(\d+)(?:-([^+]+))?(?:\+.+)?$/.exec(version);
    if (!parts)
        return false;
    const major = Number(parts[1]);
    const minor = Number(parts[2]);
    const patch = Number(parts[3]);
    return major > 7 || (major === 7 && (minor > 0 || patch > 2 || (patch === 2 && parts[4] === undefined)));
}
function packageFor(path) {
    const visited = [];
    let directory = dirname(path);
    let result = null;
    for (;;) {
        if (packageCache.has(directory)) {
            result = packageCache.get(directory) ?? null;
            break;
        }
        visited.push(directory);
        const manifestPath = join(directory, 'package.json');
        if (existsSync(manifestPath)) {
            const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'));
            if (manifest && typeof manifest === 'object' && 'name' in manifest && typeof manifest.name === 'string') {
                if (compilerPackages.has(manifest.name)) {
                    result = { name: manifest.name, version: 'version' in manifest && typeof manifest.version === 'string' ? manifest.version : '' };
                }
                break;
            }
        }
        const parent = dirname(directory);
        if (parent === directory)
            break;
        directory = parent;
    }
    for (const item of visited)
        packageCache.set(item, result);
    return result;
}
registerHooks({
    resolve(specifier, context, nextResolve) {
        const result = nextResolve(specifier, context);
        if (!result.url.startsWith('file:') || checked.has(result.url))
            return result;
        const path = fileURLToPath(result.url);
        const compiler = packageFor(path);
        if (!compiler) {
            checked.add(result.url);
            return result;
        }
        const allowed = supportsNativeFloor(compiler.version);
        const log = process.env.NATIVE_LINT_TYPESCRIPT_LOAD_LOG ?? process.env.QUALIFICATION_TYPESCRIPT_LOAD_LOG;
        if (log)
            appendFileSync(log, JSON.stringify({ pid: process.pid, package: compiler.name, version: compiler.version, path, parentURL: context.parentURL, allowed }) + '\n');
        if (!allowed) {
            rejected = true;
            throw new Error(`Legacy TypeScript initialization blocked: ${compiler.name}@${compiler.version}`);
        }
        checked.add(result.url);
        return result;
    },
});
process.on('exit', () => {
    if (rejected)
        process.exitCode = 1;
});
