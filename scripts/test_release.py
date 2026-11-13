import importlib.util
from contextlib import closing
from pathlib import Path
import plistlib
import sqlite3
import struct
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location('fuyu_release', Path(__file__).with_name('release.py'))
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)


class ExecutableUUIDTests(unittest.TestCase):
    def executable(self, directory, commands=None, payload=b'program'):
        if commands is None:
            commands = struct.pack('<II', 0x1b, 24) + bytes(range(16))
        path = Path(directory) / release.EXECUTABLE
        path.parent.mkdir(parents=True)
        path.write_bytes(struct.pack('<IIIIIIII', 0xfeedfacf, 0x100000c, 0, 2, 1, len(commands), 0, 0) + commands + payload)
        return path

    def test_channels_and_builds_have_distinct_repeatable_uuid(self):
        with tempfile.TemporaryDirectory() as directory:
            app = Path(directory)
            path = self.executable(app)
            (app / release.IDENTITY).parent.mkdir(parents=True)
            identity = {'buildID': 'one', 'buildNumber': '1', 'version': '0.1.0'}
            original = path.read_bytes()
            release.configure_channel(app, identity, 'preview')
            preview = path.read_bytes()
            release.configure_channel(app, identity, 'stable')
            stable = path.read_bytes()
            release.configure_channel(app, identity, 'preview')
            self.assertEqual(path.read_bytes(), preview)
            release.configure_channel(app, dict(identity, buildID='two'), 'preview')
            self.assertNotEqual(path.read_bytes()[40:56], preview[40:56])
            self.assertNotEqual(stable[40:56], preview[40:56])
            self.assertEqual(stable[:40] + stable[56:], original[:40] + original[56:])

    def test_payload_ignores_uuid_but_detects_program_changes(self):
        with tempfile.TemporaryDirectory() as directory, patch.object(release, 'run'):
            app = Path(directory)
            path = self.executable(app)
            expected = release.executable_payload(app)
            release.set_executable_uuid(path, b'x' * 16)
            self.assertEqual(release.executable_payload(app), expected)
            path.write_bytes(path.read_bytes() + b'changed')
            self.assertNotEqual(release.executable_payload(app), expected)

    def test_invalid_commands_fail_without_changing_executable(self):
        for commands in [b'', struct.pack('<II', 0x1b, 8), struct.pack('<II', 0x1b, 48)]:
            with self.subTest(commands=commands), tempfile.TemporaryDirectory() as directory:
                path = self.executable(directory, commands)
                original = path.read_bytes()
                with self.assertRaises(RuntimeError):
                    release.set_executable_uuid(path, bytes(16))
                self.assertEqual(path.read_bytes(), original)


class FakePlatform:
    def __init__(self, failure=False):
        self.failure = failure
        self.launches = []
        self.stops = []

    def stop(self, app, timeout):
        self.stops.append(app)

    def reopen(self, app, data, work):
        self.reopened_data = data

    def launch(self, app, data, identity, timeout, work):
        self.launches.append(identity)
        if self.failure:
            with sqlite3.connect(data / 'fuyu.db') as database:
                database.execute('UPDATE sample SET value="new-data"')
                database.execute('PRAGMA user_version=5')
            (data / 'new-file').write_text('startup-side-effect')
            raise RuntimeError('controlled startup failure')


class MacOSLaunchTests(unittest.TestCase):
    def test_open_uses_launch_services_and_preserves_runtime_paths(self):
        with patch.object(release, 'run') as run, patch.dict(release.os.environ, {'PATH': '/runtime/bin', 'CODEX_HOME': '/runtime/codex', 'CLAUDE_CONFIG_DIR': '/runtime/claude'}, clear=True):
            release.MacOS().open_app(Path('/Applications/Fuyu.app'), ['-data-dir', '/data'], Path('/logs/startup.log'))
        self.assertEqual(run.call_args.args, (
            '/usr/bin/open', '-n', '-a', '/Applications/Fuyu.app', '--stdout', '/logs/startup.log', '--stderr', '/logs/startup.log',
            '--env', 'PATH=/runtime/bin', '--env', 'CODEX_HOME=/runtime/codex', '--env', 'CLAUDE_CONFIG_DIR=/runtime/claude', '--args', '-data-dir', '/data'))

    def check_launch(self, reported_pid, running):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            platform = release.MacOS()
            identity = {'buildID': 'verified-build', 'channel': 'preview'}

            def acknowledge(app, arguments, log):
                ready = Path(arguments[arguments.index('-ready-file') + 1])
                release.write_json(ready, {'pid': reported_pid, 'token': arguments[-1], 'dataDir': str(root), **identity})

            with patch.object(platform, 'open_app', side_effect=acknowledge), patch.object(platform, 'running', side_effect=running), patch.object(release.time, 'sleep'):
                platform.launch(root / 'Fuyu.app', root, identity, 2, root)

    def test_launch_accepts_pid_from_verified_window_acknowledgement(self):
        self.check_launch(321, [[321], [321]])

    def test_launch_rejects_pid_outside_target_application(self):
        with self.assertRaisesRegex(RuntimeError, '启动确认不匹配'):
            self.check_launch(999, [[321]])

    def test_launch_rejects_process_exiting_after_window_acknowledgement(self):
        with self.assertRaisesRegex(RuntimeError, '窗口初始化后进程退出'):
            self.check_launch(321, [[321], []])


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        uuid_patch = patch.object(release, 'set_executable_uuid')
        uuid_patch.start()
        self.addCleanup(uuid_patch.stop)
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.bin = self.root / 'bin'
        self.candidates = self.bin / 'candidates'
        self.app = self.root / 'Applications/Fuyu.app'
        self.data = self.root / 'stable-data'
        self.build_id = 'candidate-1'
        self.identity = {'channel': 'preview', 'buildID': self.build_id, 'sourceVersion': 'source', 'sourceDigest': 'digest', 'version': '0.1.0', 'schemaVersion': 6, 'buildNumber': '1'}
        self.source = self.candidates / self.build_id / 'Fuyu Preview.app'
        for app, binary, channel in [(self.app, b'old-program', 'stable'), (self.source, b'tested-program', 'preview')]:
            (app / 'Contents/MacOS').mkdir(parents=True)
            (app / 'Contents/Resources').mkdir()
            (app / release.EXECUTABLE).write_bytes(binary)
            (app / release.EXECUTABLE).chmod(0o755)
            release.configure_channel(app, self.identity, channel)
        release.write_json(self.source.parent / 'candidate.json', {'identity': self.identity, 'files': release.inventory(self.source)})
        self.data.mkdir()
        with sqlite3.connect(self.data / 'fuyu.db') as database:
            database.execute('PRAGMA journal_mode=WAL')
            database.execute('PRAGMA user_version=4')
            database.execute('CREATE TABLE sample(value TEXT)')
            database.execute('INSERT INTO sample VALUES("original")')
        (self.data / 'draft').write_text('unsaved draft')
        self.patches = [patch.object(release, 'BIN', self.bin), patch.object(release, 'CANDIDATES', self.candidates), patch.object(release, 'PREVIEW_APP', self.bin / 'Fuyu Preview.app'), patch.object(release, 'sign'), patch.object(release, 'register'), patch.object(release, 'run')]
        for mock in self.patches:
            mock.start()
            self.addCleanup(mock.stop)

    def test_promotes_exact_candidate_without_rebuilding_or_overwriting_preview(self):
        before = release.inventory(self.source)
        release.promote(self.build_id, self.app, self.data)
        self.assertEqual((self.app / release.EXECUTABLE).read_bytes(), b'tested-program')
        self.assertEqual(release.inventory(self.source), before)
        self.assertEqual(release.read_json(self.app / release.IDENTITY)['channel'], 'stable')
        self.assertEqual((self.data / 'draft').read_text(), 'unsaved draft')
        with (self.app / 'Contents/Info.plist').open('rb') as handle:
            self.assertEqual(plistlib.load(handle)['CFBundleIdentifier'], 'com.shenming.fuyu')

    def test_snapshot_checkpointed_wal_without_sidecars(self):
        data = self.root / 'closed-wal-data'
        data.mkdir()
        path = data / 'fuyu.db'
        with closing(sqlite3.connect(path)) as connection:
            connection.execute('PRAGMA journal_mode=WAL')
            connection.execute('CREATE TABLE sample(value TEXT)')
            connection.execute('INSERT INTO sample VALUES("checkpointed")')
            connection.commit()
            connection.execute('PRAGMA wal_checkpoint(TRUNCATE)')
        for suffix in ['-wal', '-shm']:
            sidecar = Path(str(path) + suffix)
            if sidecar.exists():
                sidecar.unlink()
        backup = self.root / 'closed-wal-backup'
        release.snapshot_data(data, backup)
        with closing(sqlite3.connect(backup / 'fuyu.db')) as connection:
            self.assertEqual(connection.execute('PRAGMA quick_check').fetchone()[0], 'ok')
            self.assertEqual(connection.execute('SELECT value FROM sample').fetchone()[0], 'checkpointed')

    def test_changed_candidate_is_rejected_before_replacing_stable(self):
        (self.source / release.EXECUTABLE).write_bytes(b'untested')
        with self.assertRaisesRegex(RuntimeError, '校验失败'):
            release.promote(self.build_id, self.app, self.data)
        self.assertEqual((self.app / release.EXECUTABLE).read_bytes(), b'old-program')

    def test_copy_is_verified_before_replacing_stable(self):
        copytree = release.shutil.copytree

        def changed_copy(source, destination, *args, **kwargs):
            result = copytree(source, destination, *args, **kwargs)
            if source == self.source:
                (destination / release.EXECUTABLE).write_bytes(b'changed-during-copy')
            return result

        with patch.object(release.shutil, 'copytree', side_effect=changed_copy):
            with self.assertRaisesRegex(RuntimeError, '复制期间'):
                release.promote(self.build_id, self.app, self.data)
        self.assertEqual((self.app / release.EXECUTABLE).read_bytes(), b'old-program')

    def test_promote_installs_while_data_is_locked(self):
        with release.locked(self.data / 'fuyu.lock'):
            release.promote(self.build_id, self.app, self.data)
        self.assertEqual((self.app / release.EXECUTABLE).read_bytes(), b'tested-program')
        transaction = release.read_json(self.app.parent / '.Fuyu-releases/latest.json')['transaction']
        receipt = release.read_json(self.app.parent / '.Fuyu-releases' / transaction / 'receipt.json')
        self.assertEqual(receipt['state'], 'installed')
        self.assertTrue((self.app.parent / '.Fuyu-releases' / transaction / 'previous.app').exists())
        self.assertTrue((self.app.parent / '.Fuyu-releases' / transaction / 'data-before/fuyu.db').exists())

    def test_manual_rollback_keeps_new_data_when_schema_compatible(self):
        release.promote(self.build_id, self.app, self.data)
        (self.data / 'draft').write_text('newer draft')
        release.rollback(self.app, self.data, 1, platform=FakePlatform())
        self.assertEqual((self.app / release.EXECUTABLE).read_bytes(), b'old-program')
        self.assertEqual((self.data / 'draft').read_text(), 'newer draft')

    def test_first_install_redirects_legacy_launcher_and_preserves_old_app(self):
        legacy = self.bin / 'Fuyu.app'
        release.shutil.copytree(self.app, legacy)
        with patch.object(release, 'DEFAULT_APP', self.app), patch.object(release, 'DEFAULT_DATA', self.data):
            release.promote(self.build_id, self.app, self.data)
        self.assertTrue(legacy.is_symlink())
        self.assertEqual(legacy.resolve(), self.app.resolve())
        self.assertEqual((legacy / release.EXECUTABLE).read_bytes(), b'tested-program')
        self.assertTrue(list((self.bin / '.stable-backups').glob('*/Fuyu.app/Contents/MacOS/Fuyu')))

    def test_schema_rollback_requires_explicit_restore_and_preserves_newer_backup(self):
        release.promote(self.build_id, self.app, self.data)
        with sqlite3.connect(self.data / 'fuyu.db') as database:
            database.execute('PRAGMA user_version=5')
        (self.data / 'draft').write_text('newer draft')
        with self.assertRaisesRegex(RuntimeError, '--restore-data'):
            release.rollback(self.app, self.data, 1, platform=FakePlatform())
        self.assertEqual((self.app / release.EXECUTABLE).read_bytes(), b'tested-program')
        release.rollback(self.app, self.data, 1, restore=True, platform=FakePlatform())
        self.assertEqual((self.data / 'draft').read_text(), 'unsaved draft')
        self.assertTrue(list((self.app.parent / '.Fuyu-releases').glob('*/data-before-rollback-*/draft')))

    def test_preview_or_candidate_cannot_be_install_target(self):
        for target in [release.PREVIEW_APP, self.source]:
            with self.assertRaises(RuntimeError):
                release.promote(self.build_id, target, self.data)
        with self.assertRaises(RuntimeError):
            release.promote(self.build_id, self.app, self.app.parent / '.Fuyu-releases')


if __name__ == '__main__':
    unittest.main()
