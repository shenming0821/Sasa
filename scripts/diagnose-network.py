#!/usr/bin/env python3
import argparse
import datetime
import json
import os
from pathlib import Path
import subprocess
import tempfile


def capture(directory, name, command):
    with (directory / name).open('w') as output:
        try:
            result = subprocess.run(command, stdout=output, stderr=subprocess.STDOUT, timeout=30)
            return {'file': name, 'exit_code': result.returncode}
        except (OSError, subprocess.TimeoutExpired) as error:
            output.write('\nCapture failed: ' + str(error) + '\n')
            return {'file': name, 'error': str(error)}


def main():
    parser = argparse.ArgumentParser(description='保存 Sasa 网络故障现场，不改变网络或应用状态。')
    parser.add_argument('--minutes', type=int, default=15, help='回溯日志分钟数，默认 15')
    parser.add_argument('--port', type=int, default=3425, help='模型网关端口，默认 3425')
    arguments = parser.parse_args()
    if not 1 <= arguments.minutes <= 120 or not 1 <= arguments.port <= 65535:
        parser.error('minutes 取值 1–120，port 取值 1–65535')

    os.umask(0o077)
    started = datetime.datetime.now().astimezone()
    parent = Path.home() / 'Library/Logs/Fuyu/network'
    parent.mkdir(parents=True, exist_ok=True)
    directory = Path(tempfile.mkdtemp(prefix=started.strftime('%Y%m%d-%H%M%S-'), dir=parent))
    print('正在保存故障现场：' + str(directory), flush=True)
    results = []
    process_rows = []
    try:
        process_output = subprocess.check_output(
            ['ps', '-axo', 'pid=,ppid=,comm='], text=True, timeout=10)
        for line in process_output.splitlines():
            fields = line.split(None, 2)
            if len(fields) == 3:
                process_rows.append({'pid': int(fields[0]), 'ppid': int(fields[1]), 'executable': fields[2]})
    except (OSError, subprocess.SubprocessError) as error:
        results.append({'file': 'processes.json', 'error': str(error)})
    application_pids = {row['pid'] for row in process_rows
                        if row['executable'].endswith(('/Sasa.app/Contents/MacOS/Sasa', '/Sasa Preview.app/Contents/MacOS/Sasa', '/Fuyu.app/Contents/MacOS/Fuyu', '/Fuyu Preview.app/Contents/MacOS/Fuyu'))}
    owned_pids = set(application_pids)
    while True:
        descendants = {row['pid'] for row in process_rows if row['ppid'] in owned_pids}
        expanded = owned_pids | descendants
        if expanded == owned_pids:
            break
        owned_pids = expanded
    (directory / 'processes.json').write_text(json.dumps(
        [row for row in process_rows if row['pid'] in owned_pids], ensure_ascii=False, indent=2))

    cutoff = started - datetime.timedelta(minutes=arguments.minutes)
    log_command = ['/usr/bin/log', 'show', '--start', cutoff.strftime('%Y-%m-%d %H:%M:%S'),
                   '--end', started.strftime('%Y-%m-%d %H:%M:%S'), '--style', 'ndjson', '--info']
    kernel_predicate = 'process == "kernel" AND eventMessage CONTAINS[c] ":' + str(arguments.port) + '"'
    helper_predicate = ('process == "nehelper" AND (eventMessage CONTAINS[c] "fuyu" '
                        'OR eventMessage CONTAINS[c] "sasa" OR eventMessage CONTAINS[c] "a.out")')
    results.append(capture(directory, 'kernel-network.ndjson', log_command + ['--predicate', kernel_predicate]))
    results.append(capture(directory, 'network-policy.ndjson', log_command + ['--predicate', helper_predicate]))
    for name, command in [
        ('system.txt', ['sw_vers']),
        ('proxy.txt', ['scutil', '--proxy']),
        ('network-state.txt', ['scutil', '--nwi']),
        ('routes.txt', ['netstat', '-rn', '-f', 'inet']),
    ]:
        results.append(capture(directory, name, command))
    application_paths = {row['executable'].rsplit('/Contents/MacOS/', 1)[0]
                         for row in process_rows if row['pid'] in application_pids}
    for index, application_path in enumerate(sorted(application_paths)):
        results.append(capture(directory, 'signature-' + str(index) + '.txt',
                               ['codesign', '-dvv', application_path]))

    metadata = {'started': started.isoformat(), 'finished': datetime.datetime.now().astimezone().isoformat(),
                'minutes': arguments.minutes, 'port': arguments.port,
                'application_pids': sorted(application_pids), 'captures': results,
                'note': '本脚本未发起连接测试；终端请求不能替代 GUI 子进程的权限判定。'}
    (directory / 'capture.json').write_text(json.dumps(metadata, ensure_ascii=False, indent=2))
    failures = [result for result in results if result.get('error') or result.get('exit_code', 0) != 0]
    if failures:
        print('部分采集失败，请查看 capture.json；已保留其余证据。', flush=True)
    if not application_pids:
        print('未找到正在运行的 Sasa / Fuyu，已保存系统日志；下次请在重启前采集。', flush=True)
    print('采集完成：' + str(directory), flush=True)
    print('文件仅保存到本机。保留故障会话，记录是哪条消息及大致发送时间。', flush=True)
    return 1 if failures else 0


if __name__ == '__main__':
    raise SystemExit(main())
