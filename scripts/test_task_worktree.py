import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import release
import task_worktree
from task_worktree import Tasks, git, text


class TaskWorktreeTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve() / 'repo'
        self.root.mkdir()
        app_patch = patch.object(task_worktree, 'DEFAULT_APP', self.root.parent / 'Sasa.app')
        app_patch.start()
        self.addCleanup(app_patch.stop)
        data_patch = patch.object(task_worktree, 'DEFAULT_DATA', self.root.parent / 'data')
        data_patch.start()
        self.addCleanup(data_patch.stop)
        git(self.root, 'init', '-b', 'main')
        git(self.root, 'config', 'user.email', 'fixture@example.invalid')
        git(self.root, 'config', 'user.name', 'Fixture')
        (self.root / '.gitignore').write_text('.cache/\nbin/\n')
        (self.root / 'file.txt').write_text('base\n')
        self.commit(self.root, 'base')
        self.tasks = Tasks(self.root)
        self.tasks.create('example')
        self.task = self.tasks.parent / 'example'

    def commit(self, root, message):
        # All fixture indexes and repositories are isolated temporary files.
        git(root, 'add', '.')
        git(root, 'commit', '-m', message)
        return text(root, 'rev-parse', 'HEAD')

    def integrate(self):
        (self.task / 'file.txt').write_text('task\n')
        head = self.commit(self.task, 'task')
        git(self.root, 'merge', '--ff-only', head)
        self.tasks.integrated('example')
        return head

    def records(self):
        return json.loads(self.tasks.registry.read_text())

    def identity(self):
        head = text(self.root, 'rev-parse', 'HEAD')
        return {
            'sourceVersion': head, 'sourceDigest': self.tasks.commit_digest(head),
            'buildID': 'fixture-build', 'channel': 'stable',
        }

    def cleanup_published(self, identity=None):
        with patch.object(self.tasks, 'installed', return_value=identity or self.identity()):
            self.tasks.cleanup()

    def test_create_shares_git_and_starts_at_committed_head(self):
        self.assertEqual(text(self.task, 'rev-parse', 'HEAD'), text(self.root, 'rev-parse', 'HEAD'))
        self.assertEqual(Tasks(self.task).common, self.tasks.common)
        self.assertEqual(Tasks(self.task).main, self.root)
        self.assertEqual(self.records()['example']['state'], 'developing')
        self.assertFalse((self.task / '.cache').exists())

    def test_creation_does_not_copy_dirty_main_or_change_index(self):
        (self.root / 'file.txt').write_text('pending\n')
        before = git(self.root, 'diff', '--cached').stdout
        self.tasks.create('second')
        self.assertEqual((self.tasks.parent / 'second/file.txt').read_text(), 'base\n')
        self.assertEqual(git(self.root, 'diff', '--cached').stdout, before)
        self.assertEqual((self.root / 'file.txt').read_text(), 'pending\n')

    def test_invalid_or_repeated_name_is_rejected(self):
        for name in ('../escape', 'example'):
            with self.assertRaises(RuntimeError):
                self.tasks.create(name)

    def test_linked_worktree_cannot_manage_public_preview(self):
        with self.assertRaises(RuntimeError):
            Tasks(self.task).require_main()

    def test_integration_requires_clean_committed_task(self):
        with self.assertRaises(RuntimeError):
            self.tasks.integrated('example')
        (self.task / 'new.txt').write_text('untracked')
        with self.assertRaises(RuntimeError):
            self.tasks.integrated('example')

    def test_integration_requires_ancestry(self):
        (self.task / 'file.txt').write_text('task')
        self.commit(self.task, 'task')
        with self.assertRaises(RuntimeError):
            self.tasks.integrated('example')

    def test_integration_rejects_merge_commits(self):
        (self.task / 'file.txt').write_text('task')
        self.commit(self.task, 'task')
        git(self.root, 'merge', '--no-ff', 'task/example', '-m', 'merge task')
        with self.assertRaisesRegex(RuntimeError, 'merge 提交'):
            self.tasks.integrated('example')
        self.assertEqual(self.records()['example']['state'], 'developing')

    def test_integrated_task_is_retained_until_publication(self):
        self.integrate()
        with patch.object(self.tasks, 'installed', side_effect=FileNotFoundError('no receipt')):
            self.tasks.cleanup()
        self.assertTrue(self.task.exists())
        self.assertEqual(self.records()['example']['state'], 'integrated')

    def test_published_task_and_ignored_cache_are_removed_branch_remains(self):
        self.integrate()
        (self.task / '.cache').mkdir()
        (self.task / '.cache/data').write_text('cache')
        self.cleanup_published()
        self.assertFalse(self.task.exists())
        self.assertEqual(self.records()['example']['state'], 'reclaimed')
        self.assertTrue(text(self.root, 'rev-parse', 'task/example'))
        self.assertEqual(len(self.tasks.worktrees()), 1)

    def test_unpublished_and_developing_tasks_are_retained(self):
        old = self.identity()
        self.integrate()
        self.tasks.create('developing')
        self.cleanup_published(old)
        self.assertTrue(self.task.exists())
        self.assertTrue((self.tasks.parent / 'developing').exists())

    def test_dirty_candidate_does_not_prove_delivery(self):
        self.integrate()
        identity = self.identity()
        identity['sourceDigest'] = hashlib.sha256(b'dirty').hexdigest()
        self.cleanup_published(identity)
        self.assertTrue(self.task.exists())

    def test_dirty_staged_and_untracked_task_changes_are_preserved(self):
        self.integrate()
        for kind in ('dirty', 'staged', 'untracked'):
            if kind == 'untracked':
                (self.task / 'extra').write_text('extra')
            else:
                (self.task / 'file.txt').write_text(kind)
                if kind == 'staged':
                    git(self.task, 'add', 'file.txt')
            index = git(self.task, 'diff', '--cached').stdout
            self.cleanup_published()
            self.assertTrue(self.task.exists())
            self.assertEqual(git(self.task, 'diff', '--cached').stdout, index)

    def test_new_task_commit_requires_new_integration(self):
        self.integrate()
        (self.task / 'file.txt').write_text('followup')
        self.commit(self.task, 'followup')
        self.cleanup_published()
        self.assertTrue(self.task.exists())

    def test_locked_and_switched_worktrees_are_preserved(self):
        self.integrate()
        git(self.root, 'worktree', 'lock', str(self.task))
        self.cleanup_published()
        self.assertTrue(self.task.exists())
        git(self.root, 'worktree', 'unlock', str(self.task))
        git(self.task, 'switch', '-c', 'other')
        self.cleanup_published()
        self.assertTrue(self.task.exists())

    def test_cleanup_is_idempotent_and_retried_on_creation(self):
        self.integrate()
        with patch.object(self.tasks, 'installed', return_value=self.identity()):
            self.tasks.create('next')
            self.tasks.cleanup()
        self.assertFalse(self.task.exists())
        self.assertTrue((self.tasks.parent / 'next').exists())

    def test_commit_digest_matches_release_source_identity_with_links_and_mode(self):
        (self.root / 'script').write_text('#!/bin/sh\n')
        (self.root / 'script').chmod(0o755)
        (self.root / 'link').symlink_to('file.txt')
        head = self.commit(self.root, 'modes')
        with patch.object(release, 'ROOT', self.root):
            revision, digest = release.source_identity()
        self.assertEqual(revision, head)
        self.assertEqual(self.tasks.commit_digest(head), digest)

    def test_installed_receipt_and_actual_files_are_required(self):
        app = self.root / 'Sasa.app'
        resources = app / 'Contents/Resources'
        resources.mkdir(parents=True)
        identity = self.identity()
        (resources / 'release.json').write_text(json.dumps(identity))
        transaction = app.parent / '.Sasa-releases/transaction'
        transaction.mkdir(parents=True)
        (transaction.parent / 'latest.json').write_text(json.dumps({'transaction': 'transaction'}))
        receipt = {
            'state': 'installed', 'app': str(app), 'data': str(task_worktree.DEFAULT_DATA),
            'identity': identity, 'installedFiles': release.inventory(app),
        }
        (transaction / 'receipt.json').write_text(json.dumps(receipt))
        with patch.object(task_worktree, 'DEFAULT_APP', app):
            self.assertEqual(self.tasks.installed(), identity)
            (resources / 'tampered').write_text('changed')
            with self.assertRaises(RuntimeError):
                self.tasks.installed()

    def test_preflight_and_isolated_promotion_do_not_cleanup(self):
        for forwarded in (['build', '--check'], ['build', '--ch'],
                          ['build', '--app=/tmp/Sasa.app'],
                          ['build', '--data-dir', '/tmp/data'],
                          ['build', '--data-d', '/tmp/data']):
            with patch('sys.argv', ['task_worktree.py', 'after-promote', *forwarded]), \
                    patch.object(task_worktree, 'Tasks', return_value=self.tasks), \
                    patch.object(self.tasks, 'cleanup') as cleanup:
                task_worktree.main()
                cleanup.assert_not_called()
        with patch('sys.argv', ['task_worktree.py', 'after-promote', 'build']), \
                patch.object(task_worktree, 'Tasks', return_value=self.tasks), \
                patch.object(self.tasks, 'cleanup') as cleanup:
            task_worktree.main()
            cleanup.assert_called_once()


if __name__ == '__main__':
    unittest.main()
