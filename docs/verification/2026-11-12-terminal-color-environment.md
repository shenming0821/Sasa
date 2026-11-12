# 终端彩色输出环境隔离

## 问题与原因

正在运行的正式版进程 PID 59047 的启动环境为 `NO_COLOR=1`、`TERM=dumb`、`COLORTERM` 空字符串。Ghostty 会覆盖终端的 `TERM` 与 `COLORTERM`，但继承的 `NO_COLOR=1` 会继续传给 shell，使命令关闭自动彩色输出。用户确认在 fish 中执行 `set -e NO_COLOR` 后恢复颜色。此次未追溯最初将该变量传入正式版的具体启动者。

本机 PTY 对比中，同一条 `rg --color=auto` 命令在 `NO_COLOR=1` 时输出不含 ANSI 颜色序列，移除该变量后出现颜色序列；父进程的 `TERM=dumb` 不能直接视为 Ghostty 内 shell 的实际值。

## 修改

`internal/terminal/bridge_darwin.m` 为每个新 surface 设置 `NO_COLOR` 空字符串覆盖，使继承的非空禁色设置失效。主进程环境保持原值，其他 Runtime 继续使用原有环境。既有终端需自行清除该变量或重新创建；shell 配置与具体命令仍可主动设置非空 `NO_COLOR`。

## 自动化与原生验证

- `FUYU_LIVE_TERMINAL=1 ./scripts/go test -tags fuyu_terminal_probe ./internal/terminal -count=1 -v`：通过。
- 原生测试使用隔离 HOME/XDG 配置、真实 AppKit/Metal surface、PTY 与 `/bin/sh`；父进程注入 `NO_COLOR=1`，读取实际 shell 输出确认 `NO_COLOR` 空字符串、`COLORTERM=truecolor`，同时确认父进程 `NO_COLOR=1` 保持原值。
- 新增检查在未应用修复时以 `terminal inherited NO_COLOR="1"` 失败，应用修复后通过；原有中文/ANSI、键盘、输入法提交、缩放、双终端隔离及进程清理检查通过。
- `./scripts/go vet ./internal/terminal`：通过。
- 测试日志包含 macOS `sandbox_extension_issue_file_to_process ... Operation not permitted` 提示；实际窗口、PTY 和上述检查均完成。

## Preview 与交付

Preview 候选 `20261112T093630Z-ce686fcea1-b92625` 已通过 `./scripts/build preview` 构建并用 `./scripts/run-preview 20261112T093630Z-ce686fcea1-b92625` 启动，已读取真实窗口确认构建身份。首次构建因其他 UI 文件的并行修改触发源码变化保护，重试构建成功；候选包含当时工作区的其他 UI 修改，这些修改不属于此次终端修复。

在该 Preview 中打开真实 fish 终端，命令输出确认 `NO_COLOR=<>`、`COLORTERM=<truecolor>`；截图中 RED、GREEN、BLUE 分别呈红、绿、蓝，提示符和输入语法着色正常。污染环境下的恢复由上述隔离原生 Probe 覆盖。

用户随后授权完整发布。发布前当前源码的 `./scripts/go test ./...`、`./scripts/go vet ./...` 与 16 项 `test_release.py` 测试通过；终端原生回归再次通过。标题栏并行修改的旧快照曾有 3 个侧栏按钮测试失败，更新后的当前源码全量测试通过。本次完整发布范围包含当时全部未提交文件；正式版构建身份与安装结果以发布脚本的候选清单和安装回执为准，安装于下次启动生效。

未验证 Agent 真实任务，因为终端环境修复仅作用于终端 surface 的环境覆盖。
