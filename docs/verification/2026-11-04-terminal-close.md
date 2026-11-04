# 关闭终端不再二次确认

## 行为与原因

按用户要求，关闭终端页签（⌘W、页签关闭按钮、面板“关闭窗口”）直接结束其中的 shell 和任务，不再弹出“关闭终端并结束其中的 shell 和任务？”确认框。`closeTerminalWithResult` 移除 `quitPrompt` 分支：已在关闭中的页签保持 `done(false)` 防重入，其余一律 `finishCloseTerminal` 并回报已关闭；同步删除仅供该弹窗错误展示的 `terminalError` 状态与提示条。退出应用时运行中的终端仍列入“退出 Fuyu”任务确认清单，该流程不变。

## 自动化

- `TestPaneCloseTerminalClosesShellWithoutPrompt`（原 `TestPaneCloseTerminalCancelAndConfirm`）：设置非空窗口进入原生应用分支，`quitPrompt` 置为报错桩，面板“关闭窗口”直接结束 shell、移除页签与面板，断言未触发确认且未置 `quitDialog`。再次审核发现原测试无窗口，旧实现同样会跳过确认，因此补上窗口条件；使用临时 Go overlay 替换为旧 `terminal.go`，该测试因未直接关闭而失败，当前实现通过。
- 其余终端关闭路径（页签按钮、`TestCloseShortcutAndMenuCloseTerminalTabs`、重启恢复）沿用既有断言，`a.win` 为 nil 时本就直关，行为不变。
- `./scripts/go test ./internal/ui/ ./internal/app/`、`./scripts/go vet ./...`、`./scripts/build`、`git diff --check` 结果见实施计划接续点。
- 再次审核执行 `./scripts/go test ./...`、`./scripts/go vet ./...`、UI 命令组/终端/面板关闭/退出相关定向 `-race`、`./scripts/build` 与 `git diff --check`，均通过。

## 原生窗口

构建后通过应用菜单正常退出旧版，直接启动新版 PID 25310，确认窗口及原会话恢复。新建验证终端 `terminal-1` 并运行 `sleep 120`：进程树记录 login=25516、fish=25518、sleep=25625；通过 ⌘W 关闭后无确认弹窗，三者均已退出，验证终端已移除，原会话 192 已恢复。

页签关闭按钮、面板“关闭窗口”与应用退出确认本轮由自动化及调用链覆盖，未重复原生操作；命令组真实运行状态与失败计数的最终观感仍待用户验收。
