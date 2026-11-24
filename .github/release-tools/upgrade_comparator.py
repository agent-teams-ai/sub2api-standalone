"""Prepare an immutable historical backend for the existing DB-upgrade smoke.

This is a backend DB producer, not a qualification of an old frontend/image.
The head release image is still built normally. No legacy frontend is built.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shlex
import shutil
import subprocess


RUNTIME_START = 'FROM ${POSTGRES_IMAGE} AS pg-client\n'
BACKEND_START = 'FROM --platform=${BUILDPLATFORM} ${GOLANG_IMAGE} AS backend-builder\n'
VARIABLE = re.compile(r'\$(?:\{([A-Za-z_][A-Za-z_0-9]*)[^}]*\}|([A-Za-z_][A-Za-z_0-9]*))')
COMMIT = re.compile(r'[0-9a-f]{40}')
IMAGE_ID = re.compile(r'sha256:[0-9a-f]{64}')
CONTRACT = 'native-backend-upgrade-comparator/v1'


def command(args, *, cwd=None, env=None):
    return subprocess.check_output(args, cwd=cwd, env=env, text=True, stderr=subprocess.STDOUT).strip()


def git(root, *args):
    return command(['git', '-C', str(root), *args])


def sha256(path):
    with path.open('rb') as handle:
        return hashlib.file_digest(handle, 'sha256').hexdigest()


def clean_source(root, revision, generated=()):
    if not COMMIT.fullmatch(revision) or git(root, 'rev-parse', 'HEAD') != revision:
        raise ValueError('checkout does not match the immutable commit')
    if git(root, 'diff', '--name-only', 'HEAD', '--'):
        raise ValueError('tracked source differs from its immutable commit')
    extra = set()
    for flags in [[], ['--ignored']]:
        names = git(root, 'ls-files', '--others', *flags, '--exclude-standard', '-z')
        extra.update(name for name in names.split('\0') if name)
    if extra - set(generated):
        raise ValueError('untracked or ignored files could change the historical build: ' + ', '.join(sorted(extra - set(generated))))


def instructions(text):
    return [line.strip() for line in re.sub(r'\\\r?\n', ' ', text).splitlines()
            if line.strip() and not line.lstrip().startswith('#')]


def variables(text):
    return {left or right for left, right in VARIABLE.findall(text)}


def runtime_source(root):
    text = (root / 'Dockerfile').read_text()
    if text.count(RUNTIME_START) != 1:
        raise ValueError('unsupported runtime stage boundary')
    prefix, runtime = text.split(RUNTIME_START)
    runtime = RUNTIME_START + runtime
    if prefix.count(BACKEND_START) != 1:
        raise ValueError('unsupported backend resource stage boundary')
    backend_stage = BACKEND_START + prefix.split(BACKEND_START)[1]
    global_args = {}
    for line in instructions(prefix):
        if line.upper().startswith('FROM '):
            break
        if line.upper().startswith('ARG '):
            name, separator, value = line[4:].partition('=')
            if not separator or name in global_args:
                raise ValueError('runtime comparison requires explicit unique global ARG defaults')
            global_args[name] = value
    stages = [line for line in instructions(runtime) if line.upper().startswith('FROM ')]
    if stages != [RUNTIME_START.strip(), 'FROM ${ALPINE_IMAGE}']:
        raise ValueError('unsupported inherited runtime stages')
    return runtime, backend_stage, global_args


def tracked_input(root, revision, path):
    pure = PurePosixPath(path.rstrip('/'))
    if pure.is_absolute() or '..' in pure.parts or not pure.parts or any(c in path for c in '*?[$'):
        raise ValueError('runtime COPY requires a literal tracked input: ' + path)
    entry = git(root, 'ls-tree', revision, '--', str(pure))
    if not entry or '\n' in entry:
        raise ValueError('runtime input is not a single tracked file or tree: ' + path)
    mode, kind, digest = entry.split('\t', 1)[0].split()
    if mode not in {'100644', '100755', '040000'}:
        raise ValueError('unsupported runtime input mode: ' + path)
    descendants = git(root, 'ls-tree', '-r', revision, '--', str(pure))
    if any(line.startswith(('120000 ', '160000 ')) for line in descendants.splitlines()):
        raise ValueError('runtime input contains a symlink or submodule: ' + path)
    return {'mode': mode, 'kind': kind, 'gitObject': digest}


def verify_runtime(base, head, base_sha, head_sha):
    old_runtime, old_backend_stage, old_args = runtime_source(base)
    new_runtime, new_backend_stage, new_args = runtime_source(head)
    if old_runtime != new_runtime:
        raise ValueError('inherited runtime Docker stages differ')
    if old_backend_stage != new_backend_stage:
        raise ValueError('inherited backend resource preparation differs')
    pending = variables(old_runtime + old_backend_stage) & (old_args.keys() | new_args.keys())
    args = {}
    while pending:
        name = pending.pop()
        if name in args:
            continue
        if name not in old_args or name not in new_args or old_args[name] != new_args[name]:
            raise ValueError('inherited global runtime ARG differs: ' + name)
        args[name] = old_args[name]
        references = variables(args[name])
        if references - old_args.keys() or references - new_args.keys():
            raise ValueError('runtime ARG references an undeclared default')
        pending.update(references - args.keys())
    inputs = {}
    binary_copies = 0
    for line in instructions(old_runtime):
        words = shlex.split(line)
        if words[0].upper() == 'ADD':
            raise ValueError('unsupported runtime ADD input')
        if words[0].upper() != 'COPY':
            continue
        words.pop(0)
        source_stage = None
        while words and words[0].startswith('--'):
            option = words.pop(0)
            if option.startswith('--from='):
                source_stage = option.split('=', 1)[1]
            elif not option.startswith(('--chown=', '--chmod=')):
                raise ValueError('unsupported runtime COPY option')
        if len(words) < 2:
            raise ValueError('unsupported runtime COPY syntax')
        sources, destination = words[:-1], words[-1]
        if source_stage == 'pg-client':
            continue  # This entire stage and the reused image ID are identical.
        if source_stage not in {None, 'backend-builder'}:
            raise ValueError('unsupported inherited COPY stage')
        for source in sources:
            if source_stage == 'backend-builder' and source == '/app/sub2api' and destination == '/app/sub2api':
                binary_copies += 1
                continue
            if source_stage == 'backend-builder':
                if not source.startswith('/app/backend/'):
                    raise ValueError('unsupported backend-builder runtime input')
                source = 'backend/' + source.removeprefix('/app/backend/')
            old = tracked_input(base, base_sha, source)
            if old != tracked_input(head, head_sha, source):
                raise ValueError('copied runtime input differs: ' + source)
            inputs[source] = old
    if binary_copies != 1 or 'deploy/docker-entrypoint.sh' not in inputs or 'backend/resources' not in inputs:
        raise ValueError('expected binary, entrypoint and resources COPY contract is absent')
    for path in ['.dockerignore', 'Dockerfile.dockerignore']:
        old = git(base, 'ls-tree', base_sha, '--', path)
        new = git(head, 'ls-tree', head_sha, '--', path)
        if old != new:
            raise ValueError('Docker context filtering differs: ' + path)
        if old:
            inputs[path] = tracked_input(base, base_sha, path)
    return {'runtimeSHA256': hashlib.sha256(old_runtime.encode()).hexdigest(),
            'backendResourceStageSHA256': hashlib.sha256(old_backend_stage.encode()).hexdigest(),
            'globalRuntimeArgs': args, 'copiedInputs': inputs}


def verify_make_target(base):
    makefile = (base / 'backend/Makefile').read_text()
    match = re.search(r'^build:\n((?:\t[^\n]*\n)+)', makefile, re.MULTILINE)
    expected = '\tCGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -trimpath -o bin/server ./cmd/server\n'
    if not match or match.group(1) != expected:
        raise ValueError('historical owning Make build is not the supported nonembedded backend target')


def verify_version(output, revision):
    if f'(commit: {revision}, built: ' not in output:
        raise ValueError('executed binary version is not bound to the expected commit')


def verify_build_info(output, revision, go_version, architecture):
    settings = {}
    for line in output.splitlines():
        if line.startswith('\tbuild\t') and '=' in line:
            key, value = line.removeprefix('\tbuild\t').split('=', 1)
            settings[key] = value
    required = {'CGO_ENABLED': '0', 'GOOS': 'linux', 'GOARCH': architecture,
                'vcs': 'git', 'vcs.revision': revision, 'vcs.modified': 'false'}
    if not output.splitlines()[0].endswith(': ' + go_version) or any(settings.get(k) != v for k, v in required.items()):
        raise ValueError('historical binary build info does not match clean source/toolchain/target')
    if settings.get('-tags'):
        raise ValueError('historical backend unexpectedly uses build tags')
    return settings


def verify_runtime_image(images):
    if len(images) != 1 or not IMAGE_ID.fullmatch(images[0]['Id']):
        raise ValueError('head runtime image is not bound to an immutable image ID')
    image = images[0]
    if image['Os'] != 'linux' or image['Architecture'] not in {'amd64', 'arm64'}:
        raise ValueError('unsupported head runtime platform')
    if image['Config']['Entrypoint'] != ['/app/docker-entrypoint.sh'] or image['Config']['Cmd'] != ['/app/sub2api']:
        raise ValueError('head runtime entrypoint does not match the preserved application contract')
    # Image config may omit its empty default. Explicit malformed/nonempty
    # values still differ from the reviewed root-entrypoint contract.
    if image['Config']['WorkingDir'] != '/app' or image['Config'].get('User', '') != '':
        raise ValueError('head runtime working directory/user differs from the preserved entrypoint contract')
    return image


def prepare(args):
    base, head, output = (Path(value).resolve() for value in (args.base, args.head, args.output))
    clean_source(base, args.base_sha)
    clean_source(head, args.head_sha)
    if args.base_sha == args.head_sha:
        raise ValueError('historical comparator requires a distinct base commit')
    equivalence = verify_runtime(base, head, args.base_sha, args.head_sha)
    verify_make_target(base)
    go_directive = re.search(r'^go ([0-9]+\.[0-9]+\.[0-9]+)$', (base / 'backend/go.mod').read_text(), re.MULTILINE)
    if not go_directive:
        raise ValueError('historical backend does not declare an exact owning Go pin')
    required_go = 'go' + go_directive.group(1)
    env = {**os.environ, 'GOTOOLCHAIN': 'local', 'GOFLAGS': '-mod=readonly -buildvcs=true',
           'GOPROXY': 'https://proxy.golang.org,direct', 'GOSUMDB': 'sum.golang.org'}
    if command(['go', 'env', 'GOVERSION'], env=env) != required_go:
        raise ValueError('installed Go does not match the historical owning pin')
    image = verify_runtime_image(json.loads(command(['docker', 'image', 'inspect', args.image])))
    head_version = command(['docker', 'run', '--rm', '--entrypoint', '/app/sub2api', image['Id'], '--version'])
    verify_version(head_version, args.head_sha)
    if output.is_relative_to(base) or output.is_relative_to(head) or any(c in str(output) for c in ',\n\r'):
        raise ValueError('comparator output must be outside source and safe for a Docker bind mount')
    output.mkdir(parents=True, exist_ok=False)
    version = command(['sh', './scripts/resolve-version.sh'], cwd=base / 'backend')
    if not re.fullmatch(r'[0-9A-Za-z.+_-]+', version):
        raise ValueError('unsupported historical version string')
    date = git(base, 'show', '-s', '--format=%cI', args.base_sha)
    flags = f'-s -w -X main.Version={version} -X main.Commit={args.base_sha} -X main.Date={date} -X main.BuildType=release'
    env.update({'GOOS': 'linux', 'GOARCH': image['Architecture']})
    subprocess.run(['make', '-C', str(base / 'backend'), 'build', f'VERSION={version}', f'LDFLAGS={flags}'], env=env, check=True)
    clean_source(base, args.base_sha, generated={'backend/bin/server'})
    clean_source(head, args.head_sha)
    binary = output / 'baseline-backend'
    shutil.copyfile(base / 'backend/bin/server', binary)
    binary.chmod(0o555)
    info = command(['go', 'version', '-m', str(binary)], env=env)
    settings = verify_build_info(info, args.base_sha, required_go, image['Architecture'])
    baseline_version = command(['docker', 'run', '--rm', '--mount', f'type=bind,source={binary},target=/app/sub2api,readonly', image['Id'], '--version'])
    verify_version(baseline_version, args.base_sha)
    receipt = {'contract': CONTRACT, 'phase': 'backend-prepared', 'baseCommit': args.base_sha,
               'headCommit': args.head_sha, 'runtimeImageId': image['Id'], 'binaryPath': str(binary),
               'binarySHA256': sha256(binary), 'goVersion': required_go, 'buildSettings': settings,
               'runtimeEquivalence': equivalence, 'baselineRole': 'nonembedded-backend-db-producer',
               'oldFrontendQualified': False, 'upgradeSmokeCompleted': False}
    (output / 'receipt.json').write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')
    (output / 'baseline-build-info.txt').write_text(info + '\n')
    (output / 'baseline-version.log').write_text(baseline_version + '\n')
    (output / 'head-version.log').write_text(head_version + '\n')
    print(json.dumps(receipt, sort_keys=True))


def complete(output):
    receipt_path = Path(output) / 'receipt.json'
    receipt = json.loads(receipt_path.read_text())
    if receipt.get('contract') != CONTRACT or receipt.get('phase') != 'backend-prepared':
        raise ValueError('historical backend preparation was not verified')
    if sha256(Path(receipt['binaryPath'])) != receipt['binarySHA256']:
        raise ValueError('historical backend binary changed during the upgrade smoke')
    receipt.update({'phase': 'upgrade-smoke-completed', 'upgradeSmokeCompleted': True})
    receipt_path.write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest='command', required=True)
    build = subparsers.add_parser('prepare')
    for name in ['base', 'head', 'base-sha', 'head-sha', 'image', 'output']:
        build.add_argument('--' + name, required=True)
    finish = subparsers.add_parser('complete')
    finish.add_argument('--output', required=True)
    args = parser.parse_args()
    if args.command == 'prepare':
        prepare(args)
    else:
        complete(args.output)
