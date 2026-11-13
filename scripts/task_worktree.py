#!/usr/bin/env python3
"""Manage development worktrees; reclaim only tasks proven to be published."""
import argparse
from contextlib import contextmanager
from datetime import datetime, timezone
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

DEFAULT_APP = Path('/Applications/Sasa.app')
DEFAULT_DATA = Path.home() / 'Library/Application Support/Fuyu'


def git(root, *args, check=True):
    return subprocess.run(
        ['git', '-C', str(root), *args], check=check,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE,
    )


def text(root, *args):
    return git(root, *args).stdout.decode().strip()


def now():
    return datetime.now(timezone.utc).isoformat()


class Tasks:
    def __init__(self, root):
        self.root = Path(text(root, 'rev-parse', '--show-toplevel')).resolve()
        common = Path(text(root, 'rev-parse', '--git-common-dir'))
        self.common = (self.root / common).resolve()
        # Git lists the primary worktree first, even when invoked in a linked one.
        self.main = Path(self.worktrees()[0]['worktree']).resolve()
        self.directory = self.common / 'fuyu-tasks'
        self.registry = self.directory / 'tasks.json'
        self.parent = self.main.parent / (self.main.name + '.worktrees')

    def worktrees(self):
        records = []
        for block in git(self.root, 'worktree', 'list', '--porcelain', '-z').stdout.split(b'\0\0'):
            record = {}
            for field in block.split(b'\0'):
                if field:
                    key, _, value = os.fsdecode(field).partition(' ')
                    record[key] = value
            if record:
                records.append(record)
        return records

    def require_main(self):
        if self.root != self.main:
            raise RuntimeError(f'请在集成目录执行此操作：{self.main}')

    @contextmanager
    def locked(self):
        self.directory.mkdir(parents=True, exist_ok=True)
        with (self.directory / 'tasks.lock').open('a') as handle:
            fcntl.flock(handle, fcntl.LOCK_EX)
            records = json.loads(self.registry.read_text()) if self.registry.exists() else {}
            yield records
            fd, name = tempfile.mkstemp(dir=self.directory, prefix='.tasks-')
            try:
                with os.fdopen(fd, 'w') as output:
                    json.dump(records, output, ensure_ascii=False, indent=2)
                    output.write('\n')
                os.replace(name, self.registry)
            finally:
                Path(name).unlink(missing_ok=True)

    def create(self, name):
        self.require_main()
        if not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9_-]{0,79}', name):
            raise RuntimeError('任务名使用 1–80 位字母、数字、短横线或下划线，以字母或数字开头。')
        # Retry previously missed reclamation before creating another worktree.
        self.cleanup()
        with self.locked() as records:
            if name in records:
                raise RuntimeError(f'任务名已登记，请使用新名称：{name}')
            path = self.parent / name
            branch = 'task/' + name
            base = text(self.main, 'rev-parse', 'HEAD')
            self.parent.mkdir(parents=True, exist_ok=True)
            git(self.main, 'worktree', 'add', '-b', branch, str(path), base)
            records[name] = {
                'path': str(path), 'branch': branch, 'base': base,
                'state': 'developing', 'createdAt': now(),
            }
        print(f'任务：{name}\n目录：{path}\n分支：{branch}\n起点：{base}')
        if git(self.main, 'status', '--porcelain').stdout:
            print('集成目录存在未提交修改；新 worktree 从上述提交创建，未复制这些修改。')

    def integrated(self, name):
        self.require_main()
        with self.locked() as records:
            task = records[name]
            path = Path(task['path'])
            if task['state'] == 'reclaimed':
                raise RuntimeError('该任务已回收。')
            if git(path, 'status', '--porcelain', '--untracked-files=all').stdout:
                raise RuntimeError('任务目录仍有未提交内容；获得提交授权并完成提交后再登记集成。')
            head = text(path, 'rev-parse', 'HEAD')
            if head == task['base']:
                raise RuntimeError('任务尚无独立提交。')
            commit = text(self.main, 'rev-parse', 'HEAD')
            if git(self.main, 'merge-base', '--is-ancestor', head, commit, check=False).returncode:
                raise RuntimeError('任务提交尚未合入集成目录 HEAD；请保留任务历史进行合入。')
            task.update(state='integrated', taskHead=head, integratedCommit=commit, integratedAt=now())
        print(f'已登记集成：{name}；发布前继续保留 worktree。')

    def installed(self):
        app = DEFAULT_APP
        releases = app.parent / '.Fuyu-releases'
        latest = json.loads((releases / 'latest.json').read_text())
        transaction = latest['transaction']
        if Path(transaction).name != transaction:
            raise RuntimeError('发布事务路径无效。')
        receipt = json.loads((releases / transaction / 'receipt.json').read_text())
        if (receipt['state'] != 'installed' or receipt['app'] != str(app)
                or receipt['data'] != str(DEFAULT_DATA)):
            raise RuntimeError('最新回执不是默认正式版的成功安装记录。')
        identity = json.loads((app / 'Contents/Resources/release.json').read_text())
        if identity != receipt['identity'] or identity.get('channel') != 'stable':
            raise RuntimeError('当前正式版身份与最新发布回执不一致。')
        # Use the same inventory implementation as promotion, without changing it.
        import release
        if release.inventory(app) != receipt['installedFiles']:
            raise RuntimeError('当前正式版文件与发布回执不一致。')
        return identity

    def commit_digest(self, commit):
        digest = hashlib.sha256()
        entries = git(self.main, 'ls-tree', '-rz', '--full-tree', commit).stdout.split(b'\0')
        for entry in sorted((e for e in entries if e), key=lambda e: e.split(b'\t', 1)[1]):
            metadata, name = entry.split(b'\t', 1)
            mode, kind, oid = metadata.split()
            if kind != b'blob':
                raise RuntimeError('含子模块的源码暂不支持自动回收。')
            blob = git(self.main, 'cat-file', 'blob', oid.decode()).stdout
            digest.update(name + b'\0')
            if mode == b'120000':
                digest.update(b'link:' + blob)
            else:
                permissions = 0o755 if mode == b'100755' else 0o644
                digest.update(str(permissions).encode() + b':' + hashlib.sha256(blob).hexdigest().encode())
        return digest.hexdigest()

    def reclaim(self, records, identity):
        commit = identity['sourceVersion']
        # Dirty candidates cannot prove which committed task content was shipped.
        if self.commit_digest(commit) != identity['sourceDigest']:
            print('保留任务：发布候选包含未提交内容或文件权限差异，无法自动核验完整提交。')
            return
        registered = {str(Path(w['worktree']).resolve()): w for w in self.worktrees()}
        for name, task in records.items():
            if task['state'] != 'integrated':
                continue
            path = Path(task['path'])
            reason = None
            entry = registered.get(str(path.resolve()))
            if path != self.parent / name or not entry:
                reason = '目录位置或 Git worktree 登记不匹配'
            elif entry.get('branch') != 'refs/heads/' + task['branch']:
                reason = '任务分支已切换'
            elif 'locked' in entry:
                reason = 'worktree 已锁定'
            elif text(path, 'rev-parse', 'HEAD') != task['taskHead']:
                reason = '登记集成后又产生了提交'
            elif git(path, 'status', '--porcelain', '--untracked-files=all').stdout:
                reason = '仍有未提交或未跟踪内容'
            elif git(self.main, 'merge-base', '--is-ancestor', task['integratedCommit'], commit, check=False).returncode:
                reason = '本次发布尚未包含登记的集成提交'
            if reason:
                print(f'保留 {name}：{reason}')
                continue
            result = git(self.main, 'worktree', 'remove', str(path), check=False)
            if result.returncode:
                # MyGo's build cache vendors read-only files that Git's
                # removal cannot unlink; make everything writable first.
                subprocess.run(['chmod', '-R', 'u+w', str(path)], check=False)
                result = git(self.main, 'worktree', 'remove', str(path), check=False)
                if result.returncode:
                    print(f'保留 {name}：{result.stderr.decode().strip()}')
                    continue
            task.update(state='reclaimed', publishedBuild=identity['buildID'], reclaimedAt=now())
            print(f'已回收 {name}：{path}；分支 {task["branch"]} 保留。')

    def cleanup(self):
        self.require_main()
        with self.locked() as records:
            if not any(task['state'] == 'integrated' for task in records.values()):
                return
            try:
                identity = self.installed()
                self.reclaim(records, identity)
            except (OSError, ValueError, KeyError, RuntimeError, subprocess.CalledProcessError) as error:
                print(f'任务回收暂缓：{error}', file=sys.stderr)

    def listing(self):
        with self.locked() as records:
            for name, task in records.items():
                print(f'{name}\t{task["state"]}\t{task["path"]}')
            if not records:
                print('尚无登记任务。')


def main():
    parser = argparse.ArgumentParser(description='Sasa 开发 worktree 登记与发布后回收')
    commands = parser.add_subparsers(dest='command', required=True)
    for command in ('create', 'integrated'):
        commands.add_parser(command).add_argument('name')
    for command in ('list', 'cleanup', 'require-main'):
        commands.add_parser(command)
    # Match promotion's parser, including its supported flag abbreviations.
    promotion = commands.add_parser('after-promote')
    promotion.add_argument('build_id')
    promotion.add_argument('--check', action='store_true')
    promotion.add_argument('--app')
    promotion.add_argument('--data-dir')
    args = parser.parse_args()
    root = Path(__file__).resolve().parent.parent
    tasks = Tasks(root)
    if args.command == 'create':
        tasks.create(args.name)
    elif args.command == 'integrated':
        tasks.integrated(args.name)
    elif args.command == 'list':
        tasks.listing()
    elif args.command == 'require-main':
        tasks.require_main()
    elif args.command == 'after-promote':
        # Isolated promotion and preflight never reclaim development directories.
        if not args.check and args.app is None and args.data_dir is None:
            tasks.cleanup()
    else:
        tasks.cleanup()


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.CalledProcessError) as error:
        print(f'worktree 操作失败：{error}', file=sys.stderr)
        # Installation already succeeded; a reclamation error is retried later.
        sys.exit(0 if len(sys.argv) > 1 and sys.argv[1] == 'after-promote' else 1)
