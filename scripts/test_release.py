import importlib.util
from contextlib import closing
from pathlib import Path
import plistlib
import sqlite3
import struct
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location('sasa_release', Path(__file__).with_name('release.py'))
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)


class ApplicationRegistrationTests(unittest.TestCase):
    def test_registration_excludes_old_icons_in_release_backups(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            app = root / 'Sasa.app'
            previous = root / '.Sasa-releases/old/previous.app'
            legacy = root / '.Sasa-releases/old/legacy-installed.app'
            preview = root / '.Sasa-releases/other/previous.app'
            for bundle, identifier, icon in [
                    (app, 'com.shenming.sasa', 'Sasa.icns'),
                    (previous, 'com.shenming.sasa', 'Sasa.icns'),
                    (legacy, 'com.shenming.sasa', 'Sasa.icns'),
                    (preview, 'com.shenming.sasa.preview', 'SasaPreview.icns')]:
                metadata = bundle / 'Contents/Info.plist'
                metadata.parent.mkdir(parents=True)
                with metadata.open('wb') as handle:
                    plistlib.dump({'CFBundleIdentifier': identifier, 'CFBundleIconFile': icon}, handle)
            with patch.object(release, 'run') as run, patch.object(release.subprocess, 'run') as unregister, patch.object(Path, 'exists', return_value=True):
                unregister.return_value.returncode = 1
                release.register(app)
            operations = [call.args[0][1:] for call in unregister.call_args_list]
            self.assertEqual(operations, [['-u', str(legacy)], ['-u', str(previous)]])
            self.assertEqual(run.call_args.args[1:], ('-f', app))
            self.assertTrue((previous / 'Contents/Info.plist').exists())


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
            with sqlite3.connect(data / 'sasa.db') as database:
                database.execute('UPDATE sample SET value="new-data"')
                database.execute('PRAGMA user_version=5')
            (data / 'new-file').write_text('startup-side-effect')
            raise RuntimeError('controlled startup failure')


class MacOSLaunchTests(unittest.TestCase):
    def test_shutdown_resolves_old_and_new_executable_names(self):
        with tempfile.TemporaryDirectory() as directory:
            app = Path(directory) / 'app.app'
            (app / 'Contents').mkdir(parents=True)
            for name in ['Sasa', 'Sasa']:
                with (app / 'Contents/Info.plist').open('wb') as handle:
                    plistlib.dump({'CFBundleExecutable': name}, handle)
                self.assertEqual(release.application_executable(app), app / 'Contents/MacOS' / name)

    def test_open_uses_launch_services_and_preserves_runtime_paths(self):
        with patch.object(release, 'run') as run, patch.dict(release.os.environ, {'PATH': '/runtime/bin', 'CODEX_HOME': '/runtime/codex', 'CLAUDE_CONFIG_DIR': '/runtime/claude'}, clear=True):
            release.MacOS().open_app(Path('/Applications/Sasa.app'), ['-data-dir', '/data'], Path('/logs/startup.log'))
        self.assertEqual(run.call_args.args, (
            '/usr/bin/open', '-n', '-a', '/Applications/Sasa.app', '--stdout', '/logs/startup.log', '--stderr', '/logs/startup.log',
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
                platform.launch(root / 'Sasa.app', root, identity, 2, root)

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
        self.app = self.root / 'Applications/Sasa.app'
        self.data = self.root / 'stable-data'
        self.build_id = 'candidate-1'
        self.identity = {'channel': 'preview', 'buildID': self.build_id, 'sourceVersion': 'source', 'sourceDigest': 'digest', 'version': '0.1.0', 'schemaVersion': 6, 'buildNumber': '1'}
        self.source = self.candidates / self.build_id / 'Sasa Preview.app'
        for app, binary, channel in [(self.app, b'old-program', 'stable'), (self.source, b'tested-program', 'preview')]:
            (app / 'Contents/MacOS').mkdir(parents=True)
            (app / 'Contents/Resources').mkdir()
            (app / release.EXECUTABLE).write_bytes(binary)
            (app / release.EXECUTABLE).chmod(0o755)
            release.configure_channel(app, self.identity, channel)
        release.write_json(self.source.parent / 'candidate.json', {'identity': self.identity, 'files': release.inventory(self.source)})
        self.data.mkdir()
        with sqlite3.connect(self.data / 'sasa.db') as database:
            database.execute('PRAGMA journal_mode=WAL')
            database.execute('PRAGMA user_version=4')
            database.execute('CREATE TABLE sample(value TEXT)')
            database.execute('INSERT INTO sample VALUES("original")')
        (self.data / 'draft').write_text('unsaved draft')
        self.patches = [patch.object(release, 'LEGACY_APP', self.root / 'Applications/Fuyu.app'), patch.object(release, 'BIN', self.bin), patch.object(release, 'CANDIDATES', self.candidates), patch.object(release, 'PREVIEW_APP', self.bin / 'Sasa Preview.app'), patch.object(release, 'sign'), patch.object(release, 'register'), patch.object(release, 'run')]
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
            self.assertEqual(plistlib.load(handle)['CFBundleIdentifier'], 'com.shenming.sasa')

    def test_new_brand_keeps_application_identity_and_data(self):
        with (self.app / 'Contents/Info.plist').open('rb') as handle:
            metadata = plistlib.load(handle)
        self.assertEqual(metadata['CFBundleName'], 'Sasa')
        self.assertEqual(metadata['CFBundleDisplayName'], 'Sasa')
        self.assertEqual(metadata['CFBundleIdentifier'], 'com.shenming.sasa')
        self.assertEqual(metadata['CFBundleIconFile'], 'Sasa.icns')
        self.assertEqual(release.DEFAULT_DATA.name, 'Sasa')

    def test_rename_preserves_previous_install_and_removes_old_entries(self):
        legacy = self.app.parent / 'Fuyu.app'
        release.shutil.copytree(self.app, legacy)
        release.shutil.rmtree(self.app)
        old_executable = Path('Contents/MacOS/Sasa')
        (legacy / release.EXECUTABLE).rename(legacy / old_executable)
        with (legacy / 'Contents/Info.plist').open('rb') as handle:
            metadata = plistlib.load(handle)
        metadata['CFBundleExecutable'] = 'Sasa'
        with (legacy / 'Contents/Info.plist').open('wb') as handle:
            plistlib.dump(metadata, handle)
        self.bin.mkdir(exist_ok=True)
        (self.bin / 'fuyu').symlink_to('Fuyu.app/Contents/MacOS/Fuyu')
        with patch.object(release, 'DEFAULT_APP', self.app), patch.object(release, 'DEFAULT_DATA', self.data):
            release.promote(self.build_id, self.app, self.data)
        self.assertFalse(legacy.exists())
        self.assertFalse(legacy.is_symlink())
        self.assertEqual((self.app / release.EXECUTABLE).read_bytes(), b'tested-program')
        self.assertEqual((self.data / 'draft').read_text(), 'unsaved draft')
        self.assertEqual((self.bin / 'Sasa.app').resolve(), self.app.resolve())
        self.assertEqual((self.bin / 'sasa').resolve(), (self.app / release.EXECUTABLE).resolve())
        self.assertTrue((self.bin / 'sasa').is_symlink())
        transaction = release.read_json(self.app.parent / '.Sasa-releases/latest.json')['transaction']
        self.assertEqual((self.app.parent / '.Sasa-releases' / transaction / 'previous.app' / old_executable).read_bytes(), b'old-program')
        with patch.object(release, 'DEFAULT_APP', self.app), patch.object(release, 'DEFAULT_DATA', self.data):
            release.rollback(self.app, self.data, 1, platform=FakePlatform())
        self.assertEqual((self.app / old_executable).read_bytes(), b'old-program')
        self.assertFalse(legacy.exists())
        self.assertTrue((self.bin / 'sasa').is_symlink())
        self.assertEqual((self.bin / 'sasa').resolve(), (self.app / old_executable).resolve())

    def test_release_removes_existing_legacy_symlinks_without_recreating_them(self):
        legacy = self.app.parent / 'Fuyu.app'
        legacy.symlink_to(self.app, target_is_directory=True)
        self.bin.mkdir(exist_ok=True)
        (self.bin / 'Sasa.app').symlink_to(self.app, target_is_directory=True)
        (self.bin / 'fuyu').symlink_to('Fuyu.app/Contents/MacOS/Fuyu')
        with patch.object(release, 'DEFAULT_APP', self.app), patch.object(release, 'DEFAULT_DATA', self.data):
            release.promote(self.build_id, self.app, self.data)
            release.point_installed_entries(self.app, 'repeat')
        for old in [legacy, self.bin / 'Fuyu.app', self.bin / 'fuyu']:
            self.assertFalse(old.exists())
            self.assertFalse(old.is_symlink())
        self.assertEqual((self.bin / 'sasa').resolve(), (self.app / release.EXECUTABLE).resolve())

    def test_preview_stops_both_names_before_reusing_preview_data(self):
        platform = FakePlatform()
        legacy = self.bin / 'Fuyu Preview.app'
        release.shutil.copytree(self.app, legacy)
        with patch.object(release, 'MacOS', return_value=platform), patch.object(release, 'SUPPORT', self.root / 'support'):
            release.run_preview(self.build_id, 1)
        self.assertEqual(platform.stops, [release.PREVIEW_APP, legacy])
        self.assertEqual(len(platform.launches), 1)
        self.assertEqual(release.inventory(release.PREVIEW_APP), release.inventory(self.source))

    def test_rename_precheck_preserves_an_unrelated_old_application(self):
        legacy = self.app.parent / 'Fuyu.app'
        release.shutil.copytree(self.app, legacy)
        with (legacy / 'Contents/Info.plist').open('wb') as handle:
            plistlib.dump({'CFBundleIdentifier': 'another.application'}, handle)
        before = release.inventory(legacy)
        with patch.object(release, 'DEFAULT_APP', self.app):
            with self.assertRaisesRegex(RuntimeError, '已有其他应用'):
                release.promote(self.build_id, self.app, self.data, check=True)
        self.assertEqual(release.inventory(legacy), before)
        self.assertEqual((self.app / release.EXECUTABLE).read_bytes(), b'old-program')

    def test_snapshot_checkpointed_wal_without_sidecars(self):
        data = self.root / 'closed-wal-data'
        data.mkdir()
        path = data / 'sasa.db'
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
        with closing(sqlite3.connect(backup / 'sasa.db')) as connection:
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
        with release.locked(self.data / 'sasa.lock'):
            release.promote(self.build_id, self.app, self.data)
        self.assertEqual((self.app / release.EXECUTABLE).read_bytes(), b'tested-program')
        transaction = release.read_json(self.app.parent / '.Sasa-releases/latest.json')['transaction']
        receipt = release.read_json(self.app.parent / '.Sasa-releases' / transaction / 'receipt.json')
        self.assertEqual(receipt['state'], 'installed')
        self.assertTrue((self.app.parent / '.Sasa-releases' / transaction / 'previous.app').exists())
        self.assertTrue((self.app.parent / '.Sasa-releases' / transaction / 'data-before/sasa.db').exists())

    def test_manual_rollback_keeps_new_data_when_schema_compatible(self):
        release.promote(self.build_id, self.app, self.data)
        (self.data / 'draft').write_text('newer draft')
        release.rollback(self.app, self.data, 1, platform=FakePlatform())
        self.assertEqual((self.app / release.EXECUTABLE).read_bytes(), b'old-program')
        self.assertEqual((self.data / 'draft').read_text(), 'newer draft')

    def test_first_install_removes_legacy_launcher_and_preserves_old_app(self):
        legacy = self.bin / 'Fuyu.app'
        release.shutil.copytree(self.app, legacy)
        with patch.object(release, 'DEFAULT_APP', self.app), patch.object(release, 'DEFAULT_DATA', self.data):
            release.promote(self.build_id, self.app, self.data)
        self.assertFalse(legacy.exists())
        self.assertFalse(legacy.is_symlink())
        self.assertEqual((self.bin / 'Sasa.app' / release.EXECUTABLE).read_bytes(), b'tested-program')
        self.assertTrue(list((self.bin / '.stable-backups').glob('*/Fuyu.app/Contents/MacOS/Sasa')))

    def test_schema_rollback_requires_explicit_restore_and_preserves_newer_backup(self):
        release.promote(self.build_id, self.app, self.data)
        with sqlite3.connect(self.data / 'sasa.db') as database:
            database.execute('PRAGMA user_version=5')
        (self.data / 'draft').write_text('newer draft')
        with self.assertRaisesRegex(RuntimeError, '--restore-data'):
            release.rollback(self.app, self.data, 1, platform=FakePlatform())
        self.assertEqual((self.app / release.EXECUTABLE).read_bytes(), b'tested-program')
        release.rollback(self.app, self.data, 1, restore=True, platform=FakePlatform())
        self.assertEqual((self.data / 'draft').read_text(), 'unsaved draft')
        self.assertTrue(list((self.app.parent / '.Sasa-releases').glob('*/data-before-rollback-*/draft')))

    def test_preview_or_candidate_cannot_be_install_target(self):
        for target in [release.PREVIEW_APP, self.source]:
            with self.assertRaises(RuntimeError):
                release.promote(self.build_id, target, self.data)
        with self.assertRaises(RuntimeError):
            release.promote(self.build_id, self.app, self.app.parent / '.Sasa-releases')


if __name__ == '__main__':
    unittest.main()
