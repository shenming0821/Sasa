# 终端文字与显示缩放验证

## 问题与定位

用户反馈工作区终端文字大小观感不一致、模糊，与独立 Ghostty.app 不一致。

沿 `Session.Present` → `fuyu_terminal_new` → `FuyuGhosttyView.syncSize` 核对：surface 初始化时传入窗口缩放率，后续仅更新 Ghostty 内容缩放和像素尺寸，没有同步承载 IOSurface 的 `CALayer.contentsScale`。窗口 backing 改变后，图层与渲染分辨率可能不同步。上游同版本 `macos/Sources/Ghostty/Surface View/SurfaceView_AppKit.swift` 的 `viewDidChangeBackingProperties` 明确在禁用隐式动画的事务内同步该图层属性。

本次补齐图层缩放同步，并通过 `convertSizeToBacking` 取得实际像素尺寸，再计算 surface 内容缩放和设置像素宽高。保留 Ghostty 自身配置与字体选择。

## 本机字体核验

- Ghostty.app：1.3.1；Fuyu 打包源码：1.3.1，固定 revision `22efb0be2bbea73e5339f5426fa3b20edabcaa11`。
- 本机 `+show-config`：字体 `M PLUS Code Latin`，默认字号 `13 pt`，主题 `Gitlab Light`。
- 独立 Ghostty.app 和 Fuyu 实际 dylib 的 `+show-face --string='ABC中文提交代码'` 均选到英文 `M PLUS Code Latin`、中文 `PingFang SC`；普通、粗体、斜体的中文查询一致。dylib 通过临时 C 启动器调用自身 `ghostty_init`/`ghostty_cli_try_action` 查询，未改用户配置。
- 上述是配置与字体发现诊断，不代表已读取用户原有 Devin surface 的逐字符字体或 ANSI 状态。

## 原生回归

使用既有 `fuyu_terminal_probe` 隔离 Probe.app、HOME/XDG 配置、受控 `/bin/sh` 和实际 Metal/dylib。

新增回归先确认初始图层与窗口缩放一致，再把图层设成不同缩放，触发真实 `viewDidChangeBackingProperties`，断言缩放恢复且没有图层动画；隐藏、恢复及尺寸变化后再次核对。

- 修复前：同一回归失败，报 `terminal layer scale did not follow backing change without animation`。
- 修复后：通过；原有输入、中文/ANSI、可变预编辑提交、PTY 尺寸 87×26 → 112×39、独立 shell、关闭进程与退出通知同时通过。
- 原生测试仍出现既有 `sandbox_extension_issue_file_to_process ... Operation not permitted` 诊断，未阻止上述断言。

## Preview 主窗口

已构建并正常启动候选 `20261105T042759Z-e91ad14387-aefecb`，正式版继续运行。在隔离 Preview 添加 Fuyu 工作区，新建终端，以 `printf` 输出中英数字混排的普通、粗体、淡色与斜体，真实窗口显示正常，保留窗口供用户比较。

当前证据支持修复图层缩放不同步这一缺陷；用户截图中原 Devin 会话的同内容重放、与 Ghostty.app 的截图对照、真实跨不同 DPI 屏幕移动仍待验收。本次未向 Devin 发送模型任务。

用户随后反馈正式版也恢复正常。只读核对证实正式版进程自 2026-11-05 12:15 持续运行，程序仍为 2026-11-04 23:09 的旧产物，两侧加载各自包内的 dylib，用户 Ghostty 配置未修改。因此本轮修复并未同步到当时的正式版；旧版恢复的具体触发未捕获，原截图根因尚不能归结为已修复的缩放缺陷。

## 检查

- `./scripts/go test ./...`：通过，UI 整包约 24.7 秒。
- `./scripts/go vet ./...`：通过。
- `./scripts/go test -race ./internal/terminal ./internal/ui -run 'Terminal' -count=1`：通过。
- `FUYU_LIVE_TERMINAL=1 ./scripts/go test -tags fuyu_terminal_probe ./internal/terminal -run '^TestLiveNativeMetalTerminal$' -count=1 -v`：最终复跑通过。
- `./scripts/go fmt ./internal/terminal`、`git diff --check`：通过。
- `./scripts/build preview`、`./scripts/run-preview 20261105T042759Z-e91ad14387-aefecb`：通过。
