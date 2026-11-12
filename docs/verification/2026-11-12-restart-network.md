# 重启后 Codex 无响应复发验证

## 故障与定位

2026-11-12 下午，正式版构建 `20261112T084708Z-f90344e624-a6f176` 的会话 269 再次出现发送后无响应。实际模型为 `devin/swe-2-flash`，推理强度为 `max`；16:57:05 发送，原生 Runtime 于 16:57:24 开始，等待 16 分 13 秒后停止。

本次发现提交 `e310967` 新增的窗口重启入口直接执行当前程序并立即 `os.Exit(0)`，绕过 Launch Services 和正常退出清理。故障正式版 PID 51599 不在当前应用启动任务列表中，其 jetsam coalition 28265 已 terminated、active count 为 0。正式版与 Preview 的主程序 UUID 已不同，不能将本次复发归为此前跨渠道 UUID 共用问题。

保留故障进程时，内置终端访问模型端点返回 HTTP 401，TCP 建连耗时 0.004303 秒；同一应用内独立原生 CLI 探针 45 秒无回复，应用外同一原生 CLI 使用 `devin/swe-2-flash` 于 28.23 秒回复。应用内探针使用 GLM 模型，因此这组探针不构成完全相同请求的对照。当前采集未取得与本次请求匹配的 NECP 丢包证据，不能断言具体内核丢包原因。

正常退出后，通过 Launch Services 启动原正式版，PID 59047 获得有效应用启动任务。保留原会话、原模型及推理强度，于 17:15:28 发送固定无工具提示，3 秒收到 `FUYU_LAUNCH_RECOVERY_OK`。结合源码中确定的启动绕过路径，本次修复针对窗口重启入口；网络请求本身仍可能受服务端与网络状态影响。

原始诊断保存在 `~/Library/Logs/Fuyu/network/20261112-165850-nwrccmez/`，其中 `launch-coalitions-51599.txt` 记录故障启动域；补充采集在 `20261112-170245-wgxlcupy/`。

## 修改行为

窗口重启沿用正常退出流程，处理任务停止、文件保存、草稿持久化与 Runtime 清理。用户取消或保存失败时取消重启意图。完成清理后启动短期辅助进程，等待旧进程实际退出，最多 30 秒，再通过 `open -n -a` 启动同一应用包，保留工作区、数据目录、PATH、CODEX_HOME 与 CLAUDE_CONFIG_DIR 的当前值。正式版发布仍延迟至下次启动生效。

## 自动化结果

- `./scripts/go test ./...`：通过。
- `./scripts/go vet ./...`：通过。
- `./scripts/go test -race ./internal/ui -run 'TestRestart|TestP4CanceledExitAfterTaskStop|TestQuit|TestP4ExitWaits' -count=1`：通过。
- `git diff --check`：通过。

新增回归覆盖渠道环境与应用包路径、等待旧 PID 退出、特殊字符参数原样传递、确认退出后仅启动一次及启动失败传播；既有退出取消回归同时检查清除重启意图并保留草稿。

## 原生 Preview 与真实 Runtime

通过 `./scripts/build preview` 构建候选 `20261112T091331Z-a44fcd2d18-a8276c`，通过 `./scripts/run-preview` 启动并确认窗口可用。

连续两次实际点击“重启 Fuyu”，窗口及草稿恢复，替换进程分别为 PID 59685、60172，均通过 Launch Services 登记，进程组与主 PID 一致。第一次重启后的 resource 与 jetsam coalition 均 active。第二次重启确认旧 Preview 及其直接 Codex、Devin 子进程均退出。

会话 50 保持 Preview 原模型 `devin/swe-2`、推理强度 `high`。17:16:55 发送后 4 秒收到 `FUYU_BUTTON_RESTART_1_OK`；17:18:34 发送后 5 秒收到 `FUYU_BUTTON_RESTART_2_OK`。两条回复均在原生窗口可见。随后第三次重启生成 PID 60534，窗口可用且前述回复保留；未追加第三次请求。

活动任务期间的原生取消退出操作未完成独立验收；取消路径以自动化回归结果为准。正式版旧进程已恢复收发，候选的重启修复通过 Preview 验证；晋升保留运行中的正式版，新代码于下次启动加载。
