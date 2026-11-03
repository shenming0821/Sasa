# 主窗口大小恢复

2026-11-03：主窗口启用 MyGo 固定依赖版本的 `StateKey = "main"`，`PathUserData` 指向当前 Store 目录。正常退出保存 `window-state.json`，下次启动恢复窗口大小与位置，并沿用 MyGo 的最大化、全屏和显示器可见性处理。没有可读记录时使用原默认尺寸。

## 验证与接续

- 通过：`./scripts/go test ./internal/ui -run '^TestQuit' -count=1`、`./scripts/go vet ./internal/ui`、`./scripts/build`、`git diff --check`。
- 原生通过：旧版正常退出后启动新版，通过 macOS 辅助功能将主窗口设置为 1455×979；正常退出后 `window-state.json` 中的 `main.width/height` 为 1455/979，再次启动读取实际窗口尺寸仍为 1455×979，进程可见。最终窗口保持原使用尺寸，供用户继续验证。
- 位置观察：保存坐标为 374,248，重开后辅助功能返回 172,308；本轮仅确认尺寸恢复，位置在当前 macOS 的实际表现仍需单独核验。
- 最大化/全屏、移除显示器及异常终止场景尚未进行原生验收；异常终止不保证保存最后一次调整。
