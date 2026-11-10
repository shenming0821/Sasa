#!/usr/bin/env python3
import argparse
from contextlib import closing, contextmanager
import ctypes
from datetime import datetime, timezone
import fcntl
import hashlib
import json
import os
from pathlib import Path
import plistlib
import shutil
import socket
import sqlite3
import subprocess
import sys
import tempfile
import time
import uuid

ROOT = Path(__file__).resolve().parent.parent
BIN = ROOT / 'bin'
CANDIDATES = BIN / 'candidates'
SUPPORT = Path.home() / 'Library/Application Support'
DEFAULT_DATA = SUPPORT / 'Fuyu'
DEFAULT_APP = Path('/Applications/Fuyu.app')
PREVIEW_APP = BIN / 'Fuyu Preview.app'
IDENTITY = Path('Contents/Resources/release.json')
EXECUTABLE = Path('Contents/MacOS/Fuyu')


def run(*args, **kwargs):
    return subprocess.run([str(arg) for arg in args], check=True, **kwargs)


def read_json(path):
    return json.loads(path.read_text())


def write_json(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')
    path.chmod(0o600)


@contextmanager
def locked(path):
    path.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
    with open(path, 'a', opener=lambda name, flags: os.open(name, flags, 0o600)) as handle:
        try:
            fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise RuntimeError(f'正在使用，稍后重试：{path}') from None
        yield


def digest_file(path):
    digest = hashlib.sha256()
    with path.open('rb') as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b''):
            digest.update(chunk)
    return digest.hexdigest()


def executable_payload(app):
    with tempfile.TemporaryDirectory(prefix='fuyu-payload-') as temporary:
        executable = Path(temporary) / 'Fuyu'
        shutil.copy2(app / EXECUTABLE, executable)
        run('codesign', '--remove-signature', executable)
        return digest_file(executable)


def inventory(folder):
    records = {}
    for path in sorted(folder.rglob('*')):
        relative = path.relative_to(folder).as_posix()
        if path.is_symlink():
            raise RuntimeError(f'候选版本包含符号链接：{relative}')
        if path.is_file():
            records[relative] = {'sha256': digest_file(path), 'mode': path.stat().st_mode & 0o777}
    return records


def source_identity():
    revision = run('git', 'rev-parse', 'HEAD', cwd=ROOT, capture_output=True, text=True).stdout.strip()
    names = run('git', 'ls-files', '-z', '--cached', '--others', '--exclude-standard', cwd=ROOT, capture_output=True).stdout.split(b'\0')
    digest = hashlib.sha256()
    for name in sorted(set(names) - {b''}):
        path = ROOT / os.fsdecode(name)
        digest.update(name + b'\0')
        if path.is_symlink():
            digest.update(b'link:' + os.fsencode(os.readlink(path)))
        elif path.is_file():
            digest.update(str(path.stat().st_mode & 0o777).encode() + b':' + digest_file(path).encode())
        elif not path.exists():
            digest.update(b'deleted')
        else:
            raise RuntimeError(f'不支持的源码项：{path}')
    return revision, digest.hexdigest()


def configure_channel(app, identity, channel):
    identity = dict(identity, channel=channel)
    write_json(app / IDENTITY, identity)
    preview = channel == 'preview'
    name = 'Fuyu Preview' if preview else 'Fuyu'
    bundle_id = 'com.shenming.fuyu.preview' if preview else 'com.shenming.fuyu'
    metadata = {
        'CFBundleExecutable': 'Fuyu', 'CFBundleIdentifier': bundle_id,
        'CFBundleName': name, 'CFBundleDisplayName': name,
        'CFBundleIconFile': 'FuyuPreview.icns' if preview else 'Fuyu.icns',
        'CFBundlePackageType': 'APPL', 'CFBundleShortVersionString': identity['version'],
        'CFBundleVersion': identity['buildNumber'], 'NSHighResolutionCapable': True,
        'NSLocalNetworkUsageDescription': 'Fuyu 需要连接你配置的局域网模型服务，以发送任务并接收回复。',
    }
    with (app / 'Contents/Info.plist').open('wb') as handle:
        plistlib.dump(metadata, handle)


def sign(app):
    signing = os.environ.get('FUYU_CODESIGN_IDENTITY', '-')
    with (app / 'Contents/Info.plist').open('rb') as handle:
        identifier = plistlib.load(handle)['CFBundleIdentifier']
    run('codesign', '--force', '--sign', signing, '--timestamp=none', app / 'Contents/Frameworks/libghostty.dylib')
    run('codesign', '--force', '--sign', signing, '--timestamp=none', '--identifier', identifier, app)
    run('codesign', '--verify', '--deep', '--strict', app)


def register(app):
    tool = Path('/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister')
    if tool.exists():
        run(tool, '-f', app)


def make_icons(resources, temporary):
    base = ROOT / 'assets/app-icon.png'
    badge = temporary / 'preview.svg'
    svg = (ROOT / 'assets/app-icon.svg').read_text()
    badge.write_text(svg.replace('</svg>', '<rect x="716" y="716" width="288" height="288" rx="70" fill="#7544C4"/><path d="M806 934V786H865C934 786 934 870 865 870H806" fill="none" stroke="white" stroke-width="25" stroke-linejoin="round"/></svg>'))
    preview = temporary / 'preview.png'
    run('sips', '-s', 'format', 'png', badge, '--out', preview, stdout=subprocess.DEVNULL)
    for source, name in [(base, 'Fuyu'), (preview, 'FuyuPreview')]:
        iconset = temporary / f'{name}.iconset'
        iconset.mkdir()
        for size in [16, 32, 128, 256, 512]:
            for scale, suffix in [(1, ''), (2, '@2x')]:
                pixels = size * scale
                run('sips', '-z', pixels, pixels, source, '--out', iconset / f'icon_{size}x{size}{suffix}.png', stdout=subprocess.DEVNULL)
        run('iconutil', '-c', 'icns', iconset, '-o', resources / f'{name}.icns')


def build():
    revision, source_digest = source_identity()
    now = datetime.now(timezone.utc)
    build_id = now.strftime('%Y%m%dT%H%M%SZ') + '-' + source_digest[:10] + '-' + uuid.uuid4().hex[:6]
    identity = {
        'channel': 'preview', 'buildID': build_id, 'sourceVersion': revision,
        'sourceDigest': source_digest, 'version': '0.1.0', 'schemaVersion': 4,
        'buildNumber': f'{(now.date() - datetime(2026, 1, 1).date()).days + 1}.{now.hour}.{now.minute}', 'builtAt': now.isoformat(),
    }
    CANDIDATES.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.build-', dir=CANDIDATES) as temporary:
        temporary = Path(temporary)
        app = temporary / 'Fuyu Preview.app'
        contents = app / 'Contents'
        resources = contents / 'Resources'
        (contents / 'MacOS').mkdir(parents=True)
        (resources / 'licenses').mkdir(parents=True)
        shutil.copy2(ROOT / 'internal/ui/fonts/OFL.txt', resources / 'licenses/MPLUSCodeLatin-OFL.txt')
        run('python3', ROOT / 'scripts/prepare-ghostty.py', contents)
        package = 'github.com/shenming0821/Fuyu/internal/release'
        flags = f'-X {package}.BuildID={build_id} -X {package}.SourceVersion={revision} -X {package}.SourceDigest={source_digest}'
        run(ROOT / 'scripts/go', 'build', '-ldflags', flags, '-o', app / EXECUTABLE, './cmd/desktop', cwd=ROOT)
        make_icons(resources, temporary)
        configure_channel(app, identity, 'preview')
        sign(app)
        if source_identity() != (revision, source_digest):
            raise RuntimeError('构建期间源码发生变化，请重新构建候选版本。')
        write_json(temporary / 'candidate.json', {'identity': identity, 'files': inventory(app), 'payloadDigest': executable_payload(app)})
        for extra in temporary.iterdir():
            if extra.name not in {'Fuyu Preview.app', 'candidate.json'}:
                if extra.is_dir():
                    shutil.rmtree(extra)
                else:
                    extra.unlink()
        destination = CANDIDATES / build_id
        temporary.rename(destination)
    link = BIN / 'fuyu-preview'
    if link.exists() and not link.is_symlink():
        raise RuntimeError(f'保留既有文件，请手动处理：{link}')
    link.unlink(missing_ok=True)
    link.symlink_to('Fuyu Preview.app/Contents/MacOS/Fuyu')
    print(f'Preview 候选版本：{build_id}\n启动：./scripts/run-preview {build_id}\n发布：./scripts/promote {build_id}')


def candidate(build_id):
    if not build_id or Path(build_id).name != build_id or build_id in {'.', '..'}:
        raise RuntimeError('请提供完整的候选构建 ID。')
    folder = CANDIDATES / build_id
    if folder.is_symlink():
        raise RuntimeError('候选版本目录不能为符号链接。')
    manifest = read_json(folder / 'candidate.json')
    app = folder / 'Fuyu Preview.app'
    if manifest['identity']['buildID'] != build_id or manifest['identity']['channel'] != 'preview':
        raise RuntimeError('候选版本身份不匹配。')
    if inventory(app) != manifest['files'] or read_json(app / IDENTITY) != manifest['identity']:
        raise RuntimeError('候选产物校验失败，请重新构建和验收。')
    run('codesign', '--verify', '--deep', '--strict', app)
    return app, manifest['identity'], manifest['files']


class MacOS:
    def running(self, app):
        executable = str((app / EXECUTABLE).resolve())
        library = ctypes.CDLL('/usr/lib/libproc.dylib')
        library.proc_pidpath.argtypes = [ctypes.c_int, ctypes.c_void_p, ctypes.c_uint32]
        library.proc_pidpath.restype = ctypes.c_int
        pids = run('/bin/ps', '-axo', 'pid=', capture_output=True, text=True).stdout.split()
        matches = []
        for value in pids:
            buffer = ctypes.create_string_buffer(4096)
            if library.proc_pidpath(int(value), buffer, len(buffer)) > 0 and os.fsdecode(buffer.value) == executable:
                matches.append(int(value))
        return matches

    def stop(self, app, timeout):
        pids = self.running(app)
        for pid in pids:
            control = Path.home() / f'Library/Caches/Fuyu/control/{pid}.sock'
            if control.exists():
                with socket.socket(socket.AF_UNIX) as connection:
                    connection.settimeout(2)
                    connection.connect(str(control))
                    connection.sendall(json.dumps({'action': 'quit', 'executable': str((app / EXECUTABLE).resolve())}).encode() + b'\n')
                    if connection.recv(64) != b'accepted\n':
                        raise RuntimeError(f'应用拒绝退出请求：{app}')
            else:
                script = f"ObjC.import('AppKit'); var app = $.NSRunningApplication.runningApplicationWithProcessIdentifier({pid}); if (!app.isNil()) app.terminate();"
                run('osascript', '-l', 'JavaScript', '-e', script, timeout=10, capture_output=True)
        deadline = time.monotonic() + timeout
        while self.running(app):
            if time.monotonic() >= deadline:
                raise RuntimeError(f'应用尚未正常退出，请处理窗口中的任务或未保存内容提示；旧版请按 ⌘Q 退出后重试：{app}')
            time.sleep(0.3)

    def open_app(self, app, arguments, log):
        command = ['/usr/bin/open', '-n', '-a', str(app.resolve()), '--stdout', str(log), '--stderr', str(log)]
        for name in ['PATH', 'CODEX_HOME', 'CLAUDE_CONFIG_DIR']:
            if name in os.environ:
                command.extend(['--env', f'{name}={os.environ[name]}'])
        run(*command, '--args', *arguments, timeout=15, capture_output=True, text=True)

    def launch(self, app, data, identity, timeout, work):
        token = uuid.uuid4().hex
        ready = work / ('ready-' + token + '.json')
        log = work / ('startup-' + token + '.log')
        self.open_app(app, ['-data-dir', str(data), '-ready-file', str(ready), '-launch-token', token], log)
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if ready.exists():
                ack = read_json(ready)
                if ack.get('token') != token or ack.get('pid') not in self.running(app) or ack.get('buildID') != identity['buildID'] or ack.get('channel') != identity['channel'] or Path(ack.get('dataDir', '')).resolve() != data.resolve():
                    raise RuntimeError(f'启动确认不匹配：{ready}')
                time.sleep(1)
                if ack['pid'] in self.running(app):
                    return
                raise RuntimeError(f'窗口初始化后进程退出，日志：{log}')
            time.sleep(0.2)
        raise RuntimeError(f'等待窗口初始化超时，日志：{log}')

    def reopen(self, app, data, work):
        log = work / ('reopen-' + uuid.uuid4().hex[:8] + '.log')
        self.open_app(app, ['-data-dir', str(data)], log)
        time.sleep(1)
        if not self.running(app):
            raise RuntimeError(f'上一版已恢复，但重开失败，日志：{log}')


def snapshot_data(data, backup):
    backup.mkdir(mode=0o700)
    for source in data.iterdir():
        if source.name in {'fuyu.lock', 'fuyu.db', 'fuyu.db-wal', 'fuyu.db-shm'}:
            continue
        if source.is_symlink():
            raise RuntimeError(f'数据目录含符号链接，请先处理：{source}')
        if source.is_dir():
            shutil.copytree(source, backup / source.name, symlinks=True)
        else:
            shutil.copy2(source, backup / source.name)
    database = data / 'fuyu.db'
    if database.exists():
        with closing(sqlite3.connect(database.as_uri() + '?mode=rw', uri=True)) as source:
            source.execute('PRAGMA query_only=ON')
            with closing(sqlite3.connect(backup / 'fuyu.db')) as target:
                source.backup(target)
                if target.execute('PRAGMA quick_check').fetchone()[0] != 'ok':
                    raise RuntimeError('数据库备份校验失败。')
        (backup / 'fuyu.db').chmod(0o600)


def restore_data(data, backup):
    for entry in data.iterdir():
        if entry.name != 'fuyu.lock':
            if entry.is_dir() and not entry.is_symlink():
                shutil.rmtree(entry)
            else:
                entry.unlink()
    for entry in backup.iterdir():
        if entry.is_dir():
            shutil.copytree(entry, data / entry.name, symlinks=True)
        else:
            shutil.copy2(entry, data / entry.name)


def validate_destination(app, data):
    if app.suffix != '.app' or app.is_symlink() or data.is_symlink():
        raise RuntimeError('应用必须是普通 .app 目录，应用和数据目录不能是符号链接。')
    if app == PREVIEW_APP or CANDIDATES in app.parents or app in data.parents or data in app.parents or app == data:
        raise RuntimeError('正式版安装位置、候选目录与数据目录必须相互独立。')
    backups = app.parent / '.Fuyu-releases'
    if backups == data or backups in data.parents or data in backups.parents:
        raise RuntimeError('正式版数据目录与发布备份目录必须相互独立。')
    marker = data / 'release-channel'
    if data.resolve() == (SUPPORT / 'Fuyu Preview').resolve() or (marker.exists() and marker.read_text().strip() != 'stable'):
        raise RuntimeError('正式版必须使用正式版数据目录。')
    if app.exists():
        with (app / 'Contents/Info.plist').open('rb') as handle:
            if plistlib.load(handle).get('CFBundleIdentifier') != 'com.shenming.fuyu':
                raise RuntimeError('目标位置已有其他应用，请选择 Fuyu 正式版位置。')


def point_legacy_to_installed(app, transaction):
    legacy = BIN / 'Fuyu.app'
    if legacy == app or (legacy.is_symlink() and legacy.resolve() == app.resolve()):
        return
    backup = BIN / '.stable-backups' / transaction / 'Fuyu.app'
    if legacy.exists() or legacy.is_symlink():
        backup.parent.mkdir(parents=True, exist_ok=True)
        legacy.rename(backup)
    try:
        legacy.symlink_to(app, target_is_directory=True)
    except OSError:
        if backup.exists() or backup.is_symlink():
            backup.rename(legacy)
        raise
    command = BIN / 'fuyu'
    if not command.exists() and not command.is_symlink():
        command.symlink_to('Fuyu.app/Contents/MacOS/Fuyu')


def promote(build_id, app, data, check=False):
    source, identity, expected_files = candidate(build_id)
    validate_destination(app, data)
    if check:
        print(json.dumps({'candidate': identity, 'app': str(app), 'data': str(data)}, ensure_ascii=False, indent=2))
        return
    app.parent.mkdir(parents=True, exist_ok=True)
    releases = app.parent / '.Fuyu-releases'
    releases.mkdir(mode=0o700, exist_ok=True)
    with locked(releases / 'release.lock'):
        work = releases / (build_id + '-' + uuid.uuid4().hex[:8])
        work.mkdir(mode=0o700)
        stage = work / 'Fuyu.app'
        shutil.copytree(source, stage)
        if inventory(stage) != expected_files:
            raise RuntimeError('复制期间候选产物发生变化，请重新核对候选版本。')
        configure_channel(stage, identity, 'stable')
        sign(stage)
        if executable_payload(source) != executable_payload(stage):
            raise RuntimeError('晋升后的程序与候选版本不一致。')
        snapshot_data(data, work / 'data-before')
        receipt = {'buildID': build_id, 'app': str(app), 'data': str(data), 'identity': dict(identity, channel='stable'), 'state': 'prepared'}
        write_json(work / 'receipt.json', receipt)
        legacy = BIN / 'Fuyu.app'
        if app.exists():
            app.rename(work / 'previous.app')
        elif data == DEFAULT_DATA and legacy != app and legacy.exists():
            shutil.copytree(legacy, work / 'previous.app')
        try:
            stage.rename(app)
        except BaseException:
            if (work / 'previous.app').exists():
                (work / 'previous.app').rename(app)
            raise
        receipt['state'] = 'installed'
        receipt['installedFiles'] = inventory(app)
        write_json(work / 'receipt.json', receipt)
        write_json(releases / 'latest.json', {'transaction': work.name})
        try:
            register(app)
        except (OSError, subprocess.SubprocessError) as error:
            print(f'正式版已就位，启动服务注册失败：{error}。', file=sys.stderr)
        if app == DEFAULT_APP and data == DEFAULT_DATA:
            try:
                point_legacy_to_installed(app, work.name)
            except OSError as alias_error:
                print(f'正式版已更新，旧入口同步失败：{alias_error}。请从 {app} 启动。', file=sys.stderr)
        print(f'正式版已就位，将在下次启动时生效：{build_id}\n应用：{app}\n恢复资料：{work}')


def rollback(app, data, timeout, restore=False, platform=None):
    validate_destination(app, data)
    platform = platform or MacOS()
    releases = app.parent / '.Fuyu-releases'
    with locked(releases / 'release.lock'):
        name = read_json(releases / 'latest.json')['transaction']
        if Path(name).name != name or name in {'.', '..'}:
            raise RuntimeError('恢复记录路径无效。')
        work = releases / name
        receipt = read_json(work / 'receipt.json')
        previous = work / 'previous.app'
        if receipt['state'] != 'installed' or receipt['app'] != str(app) or receipt['data'] != str(data) or not previous.exists():
            raise RuntimeError('没有可用于此应用及数据目录的上一版应用。')
        if inventory(app) != receipt['installedFiles']:
            raise RuntimeError('正式版文件已变化，请先核对恢复记录。')
        platform.stop(app, timeout)
        with locked(data / 'fuyu.lock'):
            before_db = work / 'data-before/fuyu.db'
            current_db = data / 'fuyu.db'
            versions = []
            for database in [before_db, current_db]:
                if database.exists():
                    with closing(sqlite3.connect(database.as_uri() + '?mode=rw', uri=True)) as connection:
                        connection.execute('PRAGMA query_only=ON')
                        versions.append(connection.execute('PRAGMA user_version').fetchone()[0])
                else:
                    versions.append(0)
            if versions[0] != versions[1] and not restore:
                raise RuntimeError('数据格式已变化。核对升级后的新增内容后，使用 --restore-data 恢复升级前的数据。')
            snapshot_data(data, work / ('data-before-rollback-' + uuid.uuid4().hex[:8]))
            app.rename(work / 'rolled-back.app')
            try:
                previous.rename(app)
            except BaseException:
                (work / 'rolled-back.app').rename(app)
                raise
            if restore:
                restore_data(data, work / 'data-before')
        receipt['state'] = 'rolled-back'
        write_json(work / 'receipt.json', receipt)
        register(app)
        platform.reopen(app, data, work)
        print(f'已恢复上一版：{app}\n恢复记录：{work}')


def run_preview(build_id, timeout):
    source, identity, expected_files = candidate(build_id)
    platform = MacOS()
    with locked(BIN / 'preview.lock'):
        with tempfile.TemporaryDirectory(prefix='.preview-', dir=BIN) as temporary:
            stage = Path(temporary) / 'Fuyu Preview.app'
            shutil.copytree(source, stage)
            if inventory(stage) != expected_files:
                raise RuntimeError('复制期间候选产物发生变化，请重新核对候选版本。')
            platform.stop(PREVIEW_APP, timeout)
            if PREVIEW_APP.exists():
                PREVIEW_APP.rename(Path(temporary) / 'previous.app')
            stage.rename(PREVIEW_APP)
            register(PREVIEW_APP)
        logs = SUPPORT / 'Fuyu Preview/release-logs'
        logs.mkdir(parents=True, mode=0o700, exist_ok=True)
        platform.launch(PREVIEW_APP, SUPPORT / 'Fuyu Preview', identity, timeout, logs)
        print(f'Preview 已启动：{build_id}\n正式版继续运行。')


def main():
    parser = argparse.ArgumentParser(description='Fuyu 本机 Preview 构建、验收与正式版晋升')
    commands = parser.add_subparsers(dest='command', required=True)
    builder = commands.add_parser('build')
    builder.add_argument('channel', nargs='?', choices=['preview'], default='preview')
    for command in ['run-preview', 'promote', 'rollback']:
        action = commands.add_parser(command)
        if command != 'rollback':
            action.add_argument('build_id', nargs='?' if command == 'run-preview' else None)
        if command != 'promote':
            action.add_argument('--timeout', type=float, default=30)
        if command != 'run-preview':
            action.add_argument('--app', type=Path, default=DEFAULT_APP)
            action.add_argument('--data-dir', type=Path, default=DEFAULT_DATA)
        if command == 'promote':
            action.add_argument('--check', action='store_true')
        if command == 'rollback':
            action.add_argument('--restore-data', action='store_true')
    args = parser.parse_args()
    if getattr(args, 'timeout', 1) <= 0:
        parser.error('--timeout 必须大于 0。')
    if args.command == 'build':
        with locked(BIN / 'build.lock'):
            build()
    elif args.command == 'run-preview':
        build_id = args.build_id
        if build_id is None:
            entries = sorted(path.name for path in CANDIDATES.iterdir() if not path.name.startswith('.') and path.is_dir())
            if not entries:
                raise RuntimeError('请先执行 ./scripts/build preview。')
            build_id = entries[-1]
        run_preview(build_id, args.timeout)
    elif args.command == 'promote':
        promote(args.build_id, args.app.expanduser().absolute(), args.data_dir.expanduser().absolute(), args.check)
    else:
        rollback(args.app.expanduser().absolute(), args.data_dir.expanduser().absolute(), args.timeout, args.restore_data)


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError, sqlite3.Error) as error:
        print(f'Fuyu：{error}', file=sys.stderr)
        sys.exit(1)
