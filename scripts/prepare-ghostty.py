#!/usr/bin/env python3
import hashlib
from concurrent.futures import ThreadPoolExecutor
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parent.parent
CACHE = ROOT / '.cache/ghostty-native'
REVISION = '22efb0be2bbea73e5339f5426fa3b20edabcaa11'
SOURCE_SHA = '4487e1c07cfd96bc98120273a2a83e23d49ae37c7d8e1c049a1726459d471786'
ZIG_SHA = '3cc2bab367e185cdfb27501c4b30b1b0653c28d9f73df8dc91488e66ece5fa6b'


def fetch(url, path, digest):
    if not path.exists() or hashlib.sha256(path.read_bytes()).hexdigest() != digest:
        temporary = path.with_suffix('.download')
        subprocess.run(['curl', '-fL', '--retry', '2', '--max-time', '600', url, '-o', str(temporary)], check=True)
        if hashlib.sha256(temporary.read_bytes()).hexdigest() != digest:
            raise RuntimeError(f'Checksum mismatch: {path.name}')
        temporary.replace(path)


def replace(path, old, new):
    text = path.read_text()
    if new in text:
        return
    if text.count(old) != 1:
        raise RuntimeError(f'Ghostty source changed: {path}')
    path.write_text(text.replace(old, new))


def build_environment():
    environment = os.environ.copy()
    sdk = Path(subprocess.check_output(['xcrun', '--show-sdk-path'], text=True).strip()).resolve()
    legacy_sdk = sdk.parent / 'MacOSX26.5.sdk'
    if legacy_sdk.exists():
        sdk = legacy_sdk
    stub = sdk / 'usr/lib/libSystem.B.tbd'
    if 'arm64-macos' not in re.search(r'targets:\s*\[[^\]]+\]', stub.read_text()).group(0):
        overlay = CACHE / 'MacOSX.sdk'
        if not (overlay / '.ready').exists():
            for directory, subdirs, files in os.walk(sdk):
                origin = Path(directory)
                destination = overlay / origin.relative_to(sdk)
                destination.mkdir(parents=True, exist_ok=True)
                for name in subdirs + files:
                    source = origin / name
                    target = destination / name
                    if target.exists() or target.is_symlink():
                        continue
                    if source.is_symlink():
                        target.symlink_to(os.readlink(source))
                    elif source.is_file():
                        if source.suffix == '.tbd':
                            def targets(match):
                                value = match.group(0)
                                additions = []
                                for entry in re.findall(r'arm64e-[\w]+', value):
                                    plain = entry.replace('arm64e-', 'arm64-')
                                    if plain not in value:
                                        additions.append(plain)
                                return value[:-1] + (', ' + ', '.join(dict.fromkeys(additions)) if additions else '') + ']'
                            target.write_text(re.sub(r'targets:\s*\[[^\]]+\]', targets, source.read_text()))
                        else:
                            target.symlink_to(source)
            (overlay / '.ready').touch()
        sdk = overlay
    tools = CACHE / 'tools'
    tools.mkdir(exist_ok=True)
    wrapper = tools / 'xcrun'
    wrapper.write_text('#!/usr/bin/env python3\nimport os,sys\nif "--show-sdk-path" in sys.argv:\n print(os.environ["SDKROOT"])\nelse:\n os.execv("/usr/bin/xcrun", ["/usr/bin/xcrun", *sys.argv[1:]])\n')
    wrapper.chmod(0o755)
    environment['SDKROOT'] = str(sdk)
    environment['PATH'] = str(tools) + os.pathsep + environment['PATH']
    return environment


def compile_native(zig, source, environment):
    command = [zig, 'build', '-Doptimize=ReleaseFast', '-Dapp-runtime=none',
               '-Demit-macos-app=false', '-Demit-xcframework=false',
               '-Demit-docs=false', '-Demit-exe=false', '-Dstrip=true',
               '-j4', '--prefix', str(CACHE / 'install')]
    manifests = '\n'.join(path.read_text() for path in [source / 'build.zig.zon', *(source / 'pkg').rglob('build.zig.zon')])
    for attempt in range(3):
        result = subprocess.run(command, cwd=source, env=environment, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        if result.returncode == 0:
            return
        urls = list(dict.fromkeys(re.findall(r'\.url = "(https://[^"]+)"', result.stdout)))
        if not urls or attempt == 2:
            print(result.stdout, file=sys.stderr)
            raise RuntimeError('Native Ghostty build failed.')

        def prefetch(url):
            match = re.search(re.escape(url) + r'".*?\.hash = "([^"]+)"', manifests, re.S)
            if not match:
                raise RuntimeError(f'No pinned dependency hash: {url}')
            path = CACHE / url.rsplit('/', 1)[-1]
            subprocess.run(['curl', '-fLsS', '--retry', '2', '--max-time', '300', url, '-o', str(path)], check=True)
            actual = subprocess.check_output([zig, 'fetch', str(path)], text=True).strip()
            if actual != match.group(1):
                raise RuntimeError(f'Ghostty dependency hash mismatch: {path.name}')

        with ThreadPoolExecutor(max_workers=4) as pool:
            list(pool.map(prefetch, urls))


def prepare():
    if sys.platform != 'darwin' or platform.machine() != 'arm64':
        raise RuntimeError('The native Ghostty build currently targets macOS arm64.')
    CACHE.mkdir(parents=True, exist_ok=True)
    library = CACHE / 'libghostty.dylib'
    source = CACHE / f'ghostty-{REVISION}'
    stamp = CACHE / 'build-version'
    if library.exists() and stamp.exists() and source.exists() and (CACHE / 'install/share/terminfo').exists() and stamp.read_text() == REVISION + '-metal-source-1':
        return library, source
    archive = CACHE / 'source.tar.gz'
    fetch(f'https://codeload.github.com/ghostty-org/ghostty/tar.gz/{REVISION}', archive, SOURCE_SHA)
    if not source.exists():
        subprocess.run(['tar', '-xf', str(archive), '-C', str(CACHE)], check=True)
    else:
        subprocess.run(['tar', '-xf', str(archive), '-C', str(CACHE),
                        f'{source.name}/build.zig', f'{source.name}/src/build/SharedDeps.zig',
                        f'{source.name}/src/renderer/metal/shaders.zig',
                        f'{source.name}/src/build/GhosttyLib.zig'], check=True)
    zig = os.environ.get('FUYU_ZIG') or shutil.which('zig')
    if not zig:
        zig_archive = CACHE / 'zig-fast.tar.xz'
        fetch('https://fs.liujiacai.net/zigbuilds/zig-aarch64-macos-0.15.2.tar.xz', zig_archive, ZIG_SHA)
        zig_dir = CACHE / 'zig-aarch64-macos-0.15.2'
        if not zig_dir.exists():
            subprocess.run(['tar', '-xf', str(zig_archive), '-C', str(CACHE)], check=True)
        zig = str(zig_dir / 'zig')
    version = subprocess.check_output([zig, 'version'], text=True).strip()
    if version != '0.15.2':
        raise RuntimeError(f'Ghostty 1.3.1 requires Zig 0.15.2; found {version}.')
    replace(source / 'build.zig',
            'if (!config.target.result.os.tag.isDarwin()) {\n            libghostty_shared.installHeader();',
            'if (config.target.result.os.tag.isDarwin()) {\n            libghostty_shared.install("libghostty.dylib");\n            resources.install();\n        } else {\n            libghostty_shared.installHeader();')
    replace(source / 'src/build/GhosttyLib.zig',
            '_ = try deps.add(lib);',
            '_ = try deps.add(lib);\n    lib.linkFramework("Metal");')
    replace(source / 'src/build/SharedDeps.zig',
            'const metallib = self.metallib.?;\n        metallib.output.addStepDependencies(&step.step);\n        step.root_module.addAnonymousImport("ghostty_metallib", .{\n            .root_source_file = metallib.output,\n        });',
            'step.root_module.addAnonymousImport("ghostty_metallib", .{\n            .root_source_file = b.path("src/renderer/shaders/shaders.metal"),\n        });')
    replace(source / 'src/renderer/metal/shaders.zig',
            'const data = try macos.dispatch.Data.create(\n        @embedFile("ghostty_metallib"),\n        macos.dispatch.queue.getMain(),\n        macos.dispatch.Data.DESTRUCTOR_DEFAULT,\n    );\n    defer data.release();',
            'const data = try macos.foundation.String.createWithBytes(\n        @embedFile("ghostty_metallib"), .utf8, false,\n    );\n    defer data.release();')
    replace(source / 'src/renderer/metal/shaders.zig',
            'objc.sel("newLibraryWithData:error:"),\n        .{\n            data,\n            &err,\n        },',
            'objc.sel("newLibraryWithSource:options:error:"),\n        .{\n            data,\n            @as(?*anyopaque, null),\n            &err,\n        },')
    environment = build_environment()
    compile_native(zig, source, environment)
    shutil.copy2(CACHE / 'install/lib/libghostty.dylib', library)
    subprocess.run(['install_name_tool', '-id', '@rpath/libghostty.dylib', str(library)], check=True)
    (CACHE / 'build-version').write_text(REVISION + '-metal-source-1')
    return library, source


if __name__ == '__main__':
    native_library, ghostty_source = prepare()
    if len(sys.argv) > 1:
        contents = Path(sys.argv[1])
        frameworks = contents / 'Frameworks'
        resources = contents / 'Resources/ghostty'
        frameworks.mkdir(parents=True, exist_ok=True)
        resources.mkdir(parents=True, exist_ok=True)
        shutil.copy2(native_library, frameworks / native_library.name)
        shutil.copytree(ghostty_source / 'src/shell-integration', resources / 'shell-integration', dirs_exist_ok=True)
        themes = ghostty_source / 'zig-out/share/ghostty/themes'
        if themes.exists():
            shutil.copytree(themes, resources / 'themes', dirs_exist_ok=True)
        installed_themes = CACHE / 'install/share/ghostty/themes'
        if installed_themes.exists():
            shutil.copytree(installed_themes, resources / 'themes', dirs_exist_ok=True)
        shutil.copytree(CACHE / 'install/share/terminfo', contents / 'Resources/terminfo', symlinks=True, dirs_exist_ok=True)
        shutil.copy2(ghostty_source / 'LICENSE', contents / 'Resources/licenses/Ghostty-MIT.txt')
