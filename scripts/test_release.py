import importlib.util
from pathlib import Path
import plistlib
import sqlite3
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location('fuyu_release', Path(__file__).with_name('release.py'))
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)


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


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.bin = self.root / 'bin'
        self.candidates = self.bin / 'candidates'
        self.app = self.root / 'Applications/Fuyu.app'
        self.data = self.root / 'stable-data'
        self.build_id = 'candidate-1'
        self.identity = {'channel': 'preview', 'buildID': self.build_id, 'sourceVersion': 'source', 'sourceDigest': 'digest', 'version': '0.1.0', 'schemaVersion': 4, 'buildNumber': '1'}
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
        platform = FakePlatform()
        release.promote(self.build_id, self.app, self.data, 1, platform=platform)
        self.assertEqual((self.app / release.EXECUTABLE).read_bytes(), b'tested-program')
        self.assertEqual(release.inventory(self.source), before)
        self.assertEqual(release.read_json(self.app / release.IDENTITY)['channel'], 'stable')
        self.assertEqual(platform.launches[0]['buildID'], self.build_id)
        self.assertEqual((self.data / 'draft').read_text(), 'unsaved draft')
        with (self.app / 'Contents/Info.plist').open('rb') as handle:
            self.assertEqual(plistlib.load(handle)['CFBundleIdentifier'], 'com.shenming.fuyu')

    def test_changed_candidate_is_rejected_before_stopping_stable(self):
        (self.source / release.EXECUTABLE).write_bytes(b'untested')
        platform = FakePlatform()
        with self.assertRaisesRegex(RuntimeError, '校验失败'):
            release.promote(self.build_id, self.app, self.data, 1, platform=platform)
        self.assertEqual(platform.stops, [])
        self.assertEqual((self.app / release.EXECUTABLE).read_bytes(), b'old-program')

    def test_copy_is_verified_before_stopping_stable(self):
        copytree = release.shutil.copytree

        def changed_copy(source, destination, *args, **kwargs):
            result = copytree(source, destination, *args, **kwargs)
            if source == self.source:
                (destination / release.EXECUTABLE).write_bytes(b'changed-during-copy')
            return result

        platform = FakePlatform()
        with patch.object(release.shutil, 'copytree', side_effect=changed_copy):
            with self.assertRaisesRegex(RuntimeError, '复制期间'):
                release.promote(self.build_id, self.app, self.data, 1, platform=platform)
        self.assertEqual(platform.stops, [])
        self.assertEqual((self.app / release.EXECUTABLE).read_bytes(), b'old-program')

    def test_startup_failure_restores_app_database_and_drafts(self):
        with self.assertRaisesRegex(RuntimeError, '已恢复升级前'):
            release.promote(self.build_id, self.app, self.data, 1, platform=FakePlatform(True))
        self.assertEqual((self.app / release.EXECUTABLE).read_bytes(), b'old-program')
        self.assertFalse((self.data / 'new-file').exists())
        self.assertEqual((self.data / 'draft').read_text(), 'unsaved draft')
        with sqlite3.connect(self.data / 'fuyu.db') as database:
            self.assertEqual(database.execute('PRAGMA user_version').fetchone()[0], 4)
            self.assertEqual(database.execute('SELECT value FROM sample').fetchone()[0], 'original')

    def test_cancelled_quit_keeps_original_application(self):
        platform = FakePlatform()
        platform.stop = lambda *args: (_ for _ in ()).throw(RuntimeError('quit cancelled'))
        with self.assertRaisesRegex(RuntimeError, 'quit cancelled'):
            release.promote(self.build_id, self.app, self.data, 1, platform=platform)
        self.assertEqual((self.app / release.EXECUTABLE).read_bytes(), b'old-program')

    def test_active_data_lock_prevents_install(self):
        with release.locked(self.data / 'fuyu.lock'):
            with self.assertRaisesRegex(RuntimeError, '正在使用'):
                release.promote(self.build_id, self.app, self.data, 1, platform=FakePlatform())
        self.assertEqual((self.app / release.EXECUTABLE).read_bytes(), b'old-program')

    def test_manual_rollback_keeps_new_data_when_schema_compatible(self):
        release.promote(self.build_id, self.app, self.data, 1, platform=FakePlatform())
        (self.data / 'draft').write_text('newer draft')
        release.rollback(self.app, self.data, 1, platform=FakePlatform())
        self.assertEqual((self.app / release.EXECUTABLE).read_bytes(), b'old-program')
        self.assertEqual((self.data / 'draft').read_text(), 'newer draft')

    def test_first_install_redirects_legacy_launcher_and_preserves_old_app(self):
        legacy = self.bin / 'Fuyu.app'
        release.shutil.copytree(self.app, legacy)
        with patch.object(release, 'DEFAULT_APP', self.app), patch.object(release, 'DEFAULT_DATA', self.data):
            release.promote(self.build_id, self.app, self.data, 1, platform=FakePlatform())
        self.assertTrue(legacy.is_symlink())
        self.assertEqual(legacy.resolve(), self.app.resolve())
        self.assertEqual((legacy / release.EXECUTABLE).read_bytes(), b'tested-program')
        self.assertTrue(list((self.bin / '.stable-backups').glob('*/Fuyu.app/Contents/MacOS/Fuyu')))

    def test_schema_rollback_requires_explicit_restore_and_preserves_newer_backup(self):
        release.promote(self.build_id, self.app, self.data, 1, platform=FakePlatform())
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
                release.promote(self.build_id, target, self.data, 1, platform=FakePlatform())
        with self.assertRaises(RuntimeError):
            release.promote(self.build_id, self.app, self.app.parent / '.Fuyu-releases', 1, platform=FakePlatform())


if __name__ == '__main__':
    unittest.main()
