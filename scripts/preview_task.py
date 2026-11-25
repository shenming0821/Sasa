import argparse
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

import release
from task_worktree import Tasks, git, text


def task_directory(tasks, name):
    records = json.loads(tasks.registry.read_text()) if tasks.registry.exists() else {}
    record = records.get(name)
    if not record or record['state'] == 'reclaimed':
        raise RuntimeError(f'找不到有效任务：{name}')
    path = tasks.parent / name
    if not any(Path(entry['worktree']).resolve() == path and entry.get('branch') == 'refs/heads/task/' + name
               for entry in tasks.worktrees()) or Path(record['path']) != path:
        raise RuntimeError(f'任务目录或分支与登记不符：{name}')
    return path


def task_patch(task, base):
    if git(task, 'ls-files', '--unmerged').stdout:
        raise RuntimeError('请先处理任务目录中的未解决冲突。')
    options = ['diff', '--binary', '--full-index', '--no-ext-diff', '--no-textconv', '--no-renames',
               '--src-prefix=a/', '--dst-prefix=b/']
    parts = [git(task, *options, base, '--').stdout]
    for name in git(task, 'ls-files', '--others', '--exclude-standard', '-z').stdout.split(b'\0'):
        if name:
            path = os.fsdecode(name)
            result = git(task, *options, '--no-index', '--', '/dev/null', path, check=False)
            if result.returncode != 1:
                raise RuntimeError(f'无法读取新增文件：{path}')
            parts.append(result.stdout)
    return b''.join(parts)


def prepare_snapshot(main, task, snapshot):
    head = text(main, 'rev-parse', 'HEAD')
    base = text(task, 'merge-base', head, 'HEAD')
    patch = task_patch(task, base)
    git(main, 'clone', '--quiet', '--shared', '--no-checkout', '--', str(main), str(snapshot))
    git(snapshot, 'checkout', '--quiet', '--detach', head)
    if patch:
        subprocess.run(['git', '-C', snapshot, 'apply', '--3way', '--whitespace=nowarn', '-'], input=patch, check=True)
    if head != text(main, 'rev-parse', 'HEAD') or patch != task_patch(task, base):
        raise RuntimeError('准备验收期间源码发生变化，请重新运行。')
    print(f'验收任务：{task.name}；主目录基线：{head}', flush=True)
    subprocess.run(['git', '-C', snapshot, 'diff', '--cached', '--stat', head], check=True)


def preview(tasks, name):
    tasks.require_main()
    task = task_directory(tasks, name)
    public_bin = tasks.main / 'bin'
    with release.locked(public_bin / 'build.lock'), tempfile.TemporaryDirectory(prefix='sasa-preview-task-') as temporary:
        snapshot = Path(temporary) / 'source'
        prepare_snapshot(tasks.main, task, snapshot)
        native_cache = tasks.main / '.cache/ghostty-native'
        native_cache.mkdir(parents=True, exist_ok=True)
        (snapshot / '.cache').mkdir(exist_ok=True)
        (snapshot / '.cache/ghostty-native').symlink_to(native_cache, target_is_directory=True)
        spec = importlib.util.spec_from_file_location('preview_release', snapshot / 'scripts/release.py')
        builder = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(builder)
        builder.BIN = public_bin
        builder.CANDIDATES = public_bin / 'candidates'
        builder.PREVIEW_APP = public_bin / 'Sasa Preview.app'
        build_id = builder.build()
        builder.run_preview(build_id, timeout=30)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='在公共 Preview 验收一个已登记的开发任务')
    parser.add_argument('name', metavar='任务名')
    args = parser.parse_args()
    try:
        preview(Tasks(Path.cwd()), args.name)
    except (KeyError, OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        sys.exit(f'任务验收失败：{error}')
