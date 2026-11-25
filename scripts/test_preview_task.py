import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import preview_task
from task_worktree import Tasks, git, text


class PreviewTaskTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.main = Path(temporary.name).resolve() / 'repo'
        self.main.mkdir()
        git(self.main, 'init', '-b', 'main')
        git(self.main, 'config', 'user.name', 'Fixture')
        git(self.main, 'config', 'user.email', 'fixture@example.invalid')
        (self.main / '.gitignore').write_text('/bin/\n/.cache/\n')
        (self.main / 'shared.txt').write_text('base\n')
        (self.main / 'delete.txt').write_text('remove\n')
        (self.main / 'scripts').mkdir()
        (self.main / 'scripts/release.py').write_text("""
from pathlib import Path
import json
ROOT = Path(__file__).resolve().parent.parent
def build():
    assert not (ROOT / 'delete.txt').exists()
    assert not (ROOT / 'private.txt').exists()
    assert (ROOT / 'main.txt').read_text() == 'main commit'
    assert (ROOT / 'feature.txt').read_text() == 'working'
    assert (ROOT / 'shared.txt').read_text() == 'task commit'
    assert (ROOT / 'alias').readlink() == Path('new binary')
    assert (ROOT / 'new binary').read_bytes() == bytes(range(256))
    return 'fixture-build'
def run_preview(build_id, timeout):
    (BIN / 'launch.json').write_text(json.dumps([str(ROOT), str(PREVIEW_APP), build_id]))
""")
        self.commit(self.main, 'base')
        self.tasks = Tasks(self.main)
        with patch.object(Tasks, 'cleanup'):
            self.tasks.create('sample')
        self.task = self.tasks.parent / 'sample'

    def commit(self, root, message):
        git(root, 'add', '-A')
        git(root, 'commit', '-qm', message)

    def state(self, root):
        return (text(root, 'rev-parse', 'HEAD'), git(root, 'diff', '--cached', '--binary').stdout,
                git(root, 'diff', '--binary').stdout, git(root, 'ls-files', '--others', '--exclude-standard', '-z').stdout)

    def test_command_builds_combined_source_without_changing_either_worktree(self):
        (self.task / 'shared.txt').write_text('task commit')
        self.commit(self.task, 'task change')
        (self.task / 'feature.txt').write_text('staged')
        git(self.task, 'add', 'feature.txt')
        (self.task / 'feature.txt').write_text('working')
        (self.task / 'delete.txt').unlink()
        (self.task / 'new binary').write_bytes(bytes(range(256)))
        (self.task / 'alias').symlink_to('new binary')
        (self.main / 'main.txt').write_text('main commit')
        self.commit(self.main, 'other change')
        (self.main / 'main.txt').write_text('main staged')
        git(self.main, 'add', 'main.txt')
        (self.main / 'main.txt').write_text('main working')
        (self.main / 'private.txt').touch()
        before = [self.state(root) for root in (self.main, self.task)]
        subprocess.run([Path(__file__).with_name('preview-task').resolve(), 'sample'], cwd=self.main, check=True)
        snapshot, app, build_id = json.loads((self.main / 'bin/launch.json').read_text())
        self.assertFalse(Path(snapshot).exists())
        self.assertEqual((Path(app), build_id), (self.main / 'bin/Sasa Preview.app', 'fixture-build'))
        self.assertEqual(before, [self.state(root) for root in (self.main, self.task)])

    def test_conflict_preserves_public_preview_and_worktrees(self):
        (self.main / 'shared.txt').write_text('new base')
        self.commit(self.main, 'conflict')
        (self.task / 'shared.txt').write_text('different change')
        before = [self.state(root) for root in (self.main, self.task)]
        with self.assertRaises(subprocess.CalledProcessError):
            preview_task.preview(self.tasks, 'sample')
        self.assertFalse((self.main / 'bin/launch.json').exists())
        self.assertEqual(before, [self.state(root) for root in (self.main, self.task)])

    def test_requires_main_and_registered_task_branch(self):
        with self.assertRaisesRegex(RuntimeError, '集成目录'):
            preview_task.preview(Tasks(self.task), 'sample')
        git(self.task, 'checkout', '--detach', 'HEAD')
        with self.assertRaisesRegex(RuntimeError, '登记不符'):
            preview_task.preview(self.tasks, 'sample')
        with self.assertRaisesRegex(RuntimeError, '找不到有效任务'):
            preview_task.preview(self.tasks, '../missing')

    def test_build_and_launch_failures_clean_the_snapshot(self):
        for stage in ('build', 'run_preview'):
            with self.subTest(stage=stage):
                (self.task / 'scripts/release.py').write_text(f"""
from pathlib import Path
def {stage}(*args, **kwargs):
    (BIN / 'failed-source').write_text(str(Path(__file__).resolve().parents[1]))
    raise RuntimeError('fixture failure')
""" + ('\ndef build(): return "fixture-build"\n' if stage == 'run_preview' else ''))
                before = [self.state(root) for root in (self.main, self.task)]
                with self.assertRaisesRegex(RuntimeError, 'fixture failure'):
                    preview_task.preview(self.tasks, 'sample')
                self.assertFalse(Path((self.main / 'bin/failed-source').read_text()).exists())
                self.assertEqual(before, [self.state(root) for root in (self.main, self.task)])
