import argparse
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import textwrap
import unittest
from unittest.mock import patch

import yaml


ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('upgrade_comparator', Path(__file__).with_name('upgrade_comparator.py'))
comparator = importlib.util.module_from_spec(spec)
spec.loader.exec_module(comparator)


class UpgradeComparatorTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='native-upgrade-comparator-test-')
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.base, self.head = self.directory / 'base', self.directory / 'head'
        for name, root in [('base', self.base), ('head', self.head)]:
            root.mkdir()
            self.git(root, 'init', '-q', '--initial-branch=fixture')
            for path in ['Dockerfile', 'backend/Makefile', 'deploy/docker-entrypoint.sh']:
                target = root / path
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes((ROOT / path).read_bytes())
            (root / 'backend/resources').mkdir()
            (root / 'backend/resources/data.json').write_text('{"fixed":true}\n')
            (root / 'backend/go.mod').write_text('module example.com/fixture\n\ngo 1.27.4\n')
            (root / '.gitignore').write_text('ignored.go\nbackend/bin/\n')
            (root / '.dockerignore').write_text('.git\nbackend/bin\n')
            (root / 'fixture-owner.txt').write_text(name)
            self.commit(root)
        self.base_sha = self.git(self.base, 'rev-parse', 'HEAD')
        self.head_sha = self.git(self.head, 'rev-parse', 'HEAD')

    def git(self, root, *args):
        return subprocess.check_output(['git', '-C', str(root), '-c', 'user.name=Comparator Fixture',
                                        '-c', 'user.email=comparator-fixture@example.invalid',
                                        '-c', 'commit.gpgsign=false', *args], text=True).strip()

    def commit(self, root):
        self.git(root, 'add', '.')
        self.git(root, 'commit', '-qm', 'immutable comparator fixture')
        if root == self.head:
            self.head_sha = self.git(root, 'rev-parse', 'HEAD')

    def verify(self):
        return comparator.verify_runtime(self.base, self.head, self.base_sha, self.head_sha)

    def test_matching_runtime_keeps_all_copied_inputs_and_global_arguments(self):
        comparator.clean_source(self.base, self.base_sha)
        result = self.verify()
        self.assertEqual(set(result['copiedInputs']), {'backend/resources', 'deploy/docker-entrypoint.sh', '.dockerignore'})
        self.assertEqual(set(result['globalRuntimeArgs']), {'ALPINE_IMAGE', 'POSTGRES_IMAGE', 'GOLANG_IMAGE', 'GOPROXY', 'GOSUMDB'})
        comparator.verify_make_target(self.base)

    def test_frontend_only_build_changes_are_not_an_old_frontend_dependency(self):
        path = self.head / 'Dockerfile'
        path.write_text(path.read_text().replace('node:26.11.1-alpine', 'node:24.21.0-alpine')
                        .replace('RUN pnpm run build', 'RUN pnpm run newer-owning-build'))
        self.commit(self.head)
        self.verify()

    def test_runtime_and_resource_builder_changes_are_rejected(self):
        path = self.head / 'Dockerfile'
        original = path.read_text()
        for old, new, error in [('EXPOSE 8080', 'EXPOSE 8081', 'runtime Docker stages'),
                                ('COPY backend/ ./', 'COPY backend/ ./\nRUN rm -rf resources', 'resource preparation')]:
            with self.subTest(old=old):
                path.write_text(original.replace(old, new))
                with self.assertRaisesRegex(ValueError, error):
                    self.verify()

    def test_runtime_global_and_transitive_defaults_are_rejected(self):
        path = self.head / 'Dockerfile'
        original = path.read_text()
        for old, new in [('alpine:3.21', 'alpine:3.22'), ('postgres:18-alpine', 'postgres:19-alpine'),
                         ('golang:1.27.4-alpine', 'golang:1.27.5-alpine')]:
            with self.subTest(old=old):
                path.write_text(original.replace(old, new))
                with self.assertRaisesRegex(ValueError, 'global runtime ARG differs'):
                    self.verify()
        for root, version in [(self.base, '3.21'), (self.head, '3.22')]:
            (root / 'Dockerfile').write_text(original.replace('ARG ALPINE_IMAGE=alpine:3.21',
                f'ARG RUNTIME_VERSION={version}\nARG ALPINE_IMAGE=alpine:${{RUNTIME_VERSION}}'))
        with self.assertRaisesRegex(ValueError, 'RUNTIME_VERSION'):
            self.verify()

    def test_changed_entrypoint_resources_and_context_filter_are_rejected(self):
        for path in ['deploy/docker-entrypoint.sh', 'backend/resources/data.json', '.dockerignore']:
            with self.subTest(path=path):
                target = self.head / path
                original = target.read_bytes()
                target.write_bytes(original + b'changed\n')
                self.commit(self.head)
                with self.assertRaisesRegex(ValueError, 'differs'):
                    self.verify()
                target.write_bytes(original)
                self.commit(self.head)

    def test_unknown_inherited_copy_stage_is_not_silently_ignored(self):
        for root in [self.base, self.head]:
            path = root / 'Dockerfile'
            path.write_text(path.read_text().replace('EXPOSE 8080',
                'COPY --from=unreviewed-stage /generated /app/generated\nEXPOSE 8080'))
        with self.assertRaisesRegex(ValueError, 'inherited COPY stage'):
            self.verify()

    def test_dirty_wrong_revision_and_ignored_build_inputs_are_rejected(self):
        with self.assertRaisesRegex(ValueError, 'immutable commit'):
            comparator.clean_source(self.base, 'a' * 40)
        tracked = self.base / 'fixture-owner.txt'
        tracked.write_text('dirty')
        with self.assertRaisesRegex(ValueError, 'tracked source differs'):
            comparator.clean_source(self.base, self.base_sha)
        tracked.write_text('base')
        for name in ['extra.go', 'ignored.go']:
            path = self.base / name
            path.write_text('package fixture\n')
            with self.assertRaisesRegex(ValueError, 'untracked or ignored'):
                comparator.clean_source(self.base, self.base_sha)
            path.unlink()

    def test_only_known_generated_backend_artifact_is_allowed_after_build(self):
        binary = self.base / 'backend/bin/server'
        binary.parent.mkdir()
        binary.write_bytes(b'inert build-output fixture')
        comparator.clean_source(self.base, self.base_sha, generated={'backend/bin/server'})
        binary.with_name('other.go').write_text('package fixture\n')
        with self.assertRaisesRegex(ValueError, 'other.go'):
            comparator.clean_source(self.base, self.base_sha, generated={'backend/bin/server'})

    def test_make_frontend_dependency_or_embed_flag_cannot_be_selected(self):
        path = self.base / 'backend/Makefile'
        original = path.read_text()
        for text in [original.replace('build:\n', 'build: frontend\n'),
                     original.replace('go build ', 'go build -tags embed ')]:
            path.write_text(text)
            with self.assertRaisesRegex(ValueError, 'nonembedded backend target'):
                comparator.verify_make_target(self.base)

    def test_runtime_mismatch_fails_before_any_build_or_container_execution(self):
        entrypoint = self.head / 'deploy/docker-entrypoint.sh'
        entrypoint.write_text(entrypoint.read_text() + '\n# incompatible runtime\n')
        self.commit(self.head)
        args = argparse.Namespace(base=str(self.base), head=str(self.head), base_sha=self.base_sha,
                                  head_sha=self.head_sha, image='sub2api:head', output=str(self.directory / 'output'))
        original = subprocess.check_output
        with patch.object(comparator.subprocess, 'check_output', wraps=original) as commands:
            with self.assertRaisesRegex(ValueError, 'copied runtime input differs'):
                comparator.prepare(args)
        self.assertTrue(commands.call_args_list)
        self.assertTrue(all(call.args[0][0] == 'git' for call in commands.call_args_list))
        self.assertFalse((self.directory / 'output').exists())

    def test_binary_build_info_requires_real_base_revision_pin_and_clean_native_build(self):
        info = ('/tmp/baseline: go1.27.4\n\tbuild\tCGO_ENABLED=0\n\tbuild\tGOOS=linux\n'
                '\tbuild\tGOARCH=amd64\n\tbuild\tvcs=git\n'
                f'\tbuild\tvcs.revision={self.base_sha}\n\tbuild\tvcs.modified=false\n')
        comparator.verify_build_info(info, self.base_sha, 'go1.27.4', 'amd64')
        invalid = [info.replace('false', 'true'), info.replace(self.base_sha, self.head_sha),
                   info.replace('go1.27.4', 'go1.27.3'), info.replace('CGO_ENABLED=0', 'CGO_ENABLED=1'),
                   info.replace('GOARCH=amd64', 'GOARCH=arm64'), info + '\tbuild\t-tags=embed\n']
        for value in invalid:
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    comparator.verify_build_info(value, self.base_sha, 'go1.27.4', 'amd64')
        comparator.verify_version(f'Sub2API 1.2.3 (commit: {self.base_sha}, built: 2026-01-01)', self.base_sha)
        with self.assertRaisesRegex(ValueError, 'expected commit'):
            comparator.verify_version(f'Sub2API 1.2.3 (commit: {self.head_sha}, built: 2026-01-01)', self.base_sha)

    def test_receipt_stays_incomplete_when_binary_changes_after_preparation(self):
        output = self.directory / 'receipt'
        output.mkdir()
        binary = output / 'baseline-backend'
        binary.write_bytes(b'inert artifact fixture')
        receipt = {'contract': comparator.CONTRACT, 'phase': 'backend-prepared',
                   'binaryPath': str(binary), 'binarySHA256': comparator.sha256(binary),
                   'oldFrontendQualified': False, 'upgradeSmokeCompleted': False}
        path = output / 'receipt.json'
        path.write_text(json.dumps(receipt))
        binary.write_bytes(b'changed')
        with self.assertRaisesRegex(ValueError, 'changed during'):
            comparator.complete(output)
        self.assertFalse(json.loads(path.read_text())['upgradeSmokeCompleted'])
        binary.write_bytes(b'inert artifact fixture')
        comparator.complete(output)
        completed = json.loads(path.read_text())
        self.assertTrue(completed['upgradeSmokeCompleted'])
        self.assertFalse(completed['oldFrontendQualified'])

    def test_existing_run_app_keeps_environment_and_limits_binary_mount_to_base(self):
        workflow = yaml.safe_load((ROOT / '.github/workflows/backend-ci.yml').read_text())
        step = next(x for x in workflow['jobs']['container']['steps'] if x.get('name') == 'Fresh install, upgrade and login smoke')
        script = step['run']
        function = script[script.index('run_app()'):script.index('wait_ready()')]
        commands = self.directory / 'docker-commands.jsonl'
        fake_bin = self.directory / 'bin'
        fake_bin.mkdir()
        docker = fake_bin / 'docker'
        docker.write_text('#!/usr/bin/env python3\nimport json,os,sys\nwith open(os.environ["DOCKER_COMMANDS"],"a") as f: f.write(json.dumps(sys.argv[1:])+"\\n")\n')
        docker.chmod(0o755)
        binary = self.directory / 'binary with space'
        env = {**os.environ, 'PATH': str(fake_bin) + os.pathsep + os.environ['PATH'],
               'DOCKER_COMMANDS': str(commands), 'HISTORICAL_BINARY': str(binary)}
        subprocess.run(['bash', '-euc', textwrap.dedent(function) + '\nrun_app base volume database image 1 "$HISTORICAL_BINARY"\nrun_app upgraded volume database image 1\n'], env=env, check=True)
        base, head = [json.loads(line) for line in commands.read_text().splitlines()]
        mount_index = base.index('--mount')
        self.assertEqual(base[mount_index + 1], f'type=bind,source={binary},target=/app/sub2api,readonly')
        del base[mount_index:mount_index + 2]
        base[base.index('base')] = 'upgraded'
        self.assertEqual(base, head)
        self.assertIn('AUTO_SETUP=true', head)
        self.assertIn('volume:/app/data', head)
        self.assertLess(script.index('check_app upgraded'), script.index('clean_log upgraded'))
        self.assertLess(script.index('clean_log upgraded'), script.index('upgrade_comparator.py complete'))
        self.assertNotIn('-t sub2api:base', script)


if __name__ == '__main__':
    unittest.main()
