# 开发指南

## 1. 当前状态与环境

当前已实现并完成当前环境下的真实任务、过程交互及会话恢复验收，包括多会话、SQLite 索引、原生历史恢复及窗口生命周期。实际覆盖与支持边界见 [会话持久化与恢复验证](verification/2026-10-22-p3.md)。

本机已验证环境：macOS arm64、Command Line Tools、Go `1.27.3`、Codex CLI `0.163.0`。`go.mod` 要求 Go `1.27.1`，指定工具链 `go1.27.3`；MyGo 固定为 `v0.6.3-0.20261022014156-976f17c9391f`。

Go 安装在 `~/.local/share/fuyu/toolchains/go1.27.3/`，未修改系统工具链。`scripts/go` 按 `FUYU_GO`、PATH 中的 `go`、本机专用安装路径的顺序选择；其他机器可安装 Go 后直接使用包装脚本。

构建、运行、test、vet 统一使用 `scripts/go`，并需要 Python 3 标准库。包装脚本通过 `scripts/mygo-input-style.py` 将锁定的 MyGo 复制到项目内被忽略的 `.cache/mygo-input-style/`，调整输入占位文字和光标绘制，再通过临时 `-modfile` 使用该副本；共享模块缓存、项目 `go.mod` 和 `go.sum` 保持原内容。缓存可删除后重建。Markdown 补丁将无序列表标记列限制为最多 1.4 em，有序列表保留 3 em 的编号兼容宽度；升级时核对 `ui/markdown/view.go`。原生 `go build/test` 不包含此样式补丁，界面验收以包装脚本构建为准；升级 MyGo 时须重新核对补丁，版本或源码不匹配会明确失败。

无序列表标记保留正文的行框，在框中心向下 0.08 em 处绘制直径 0.28 em 的实心圆、空心圆或方块，与中文及行内代码的可见中心对齐；15 DIP 正文下直径 4.2 DIP、下移 1.2 DIP。正文、缩进和有序列表编号保持原尺寸。

列表兼容补丁在 `ui/list.go` 的 `ListState.sync` 读取到 `FollowEnd=false` 时清除旧跟随状态，使执行过程折叠期间的临时暂停立即生效；保持首次打开与正常输出的底部跟随。升级 MyGo 时重新核对该同步入口，Fuyu 的 `TestTurnProcessDisclosureKeepsReadingPosition` 覆盖动效和即时切换下的阅读位置。

该补丁同时让多行编辑器接收 `Cmd+Enter`，通过原有编辑器插入换行，保留光标、选区与撤销逻辑；输入框视图使用普通 Enter 发送，输入法组合门控继续保留。

鼠标左键在已聚焦的文本编辑器中重新定位时，补丁重置光标闪烁计时，使新位置立即显示光标。光标颜色取控件解析后的正文色，编辑区的 GitHub 配色独立生效，普通输入继续继承应用正文色。升级 MyGo 时同时核对 `ui/input.go` 的点击定位路径及 `ui/editor_layout.go` / `ui/textarea.go` 的光标绘制；文件缓冲及普通文本区域的可控时钟与像素回归见 `TestEditorClickRestartsVisibleCaretPhase`、`TestEditorThemeSelectionAndCaretPixels`。

该隔离副本另修复 macOS 无障碍密码字段 setter 的缓存：AX value/selectedText 写入原文时，只将原文发送给编辑器，不保存在可读的 AppKit 无障碍值中。版本或目标代码不匹配时同样明确失败；升级框架后重新核对原生秘密输入和自动填充。

```sh
./scripts/go version
./scripts/build preview
./scripts/run-preview <上一步输出的构建ID>
```

`scripts/build` 默认构建 Preview，等同于 `scripts/build preview`。候选保存于 `bin/candidates/<构建ID>/Fuyu Preview.app`，记录源码提交、包含未提交和未跟踪源码的摘要、全部产物校验值；构建期间源码变化则拒绝生成候选。`run-preview` 从指定候选复制至 `bin/Fuyu Preview.app` 并正常重启 Preview；不传构建 ID 时启动最近的候选。既有 `bin/Fuyu.app` 与 `bin/fuyu` 保留，日常构建不覆盖正式版。晋升命令为 `./scripts/promote <构建ID>`，默认安装 `/Applications/Fuyu.app`。完整发布、预检和回退步骤见[双渠道与发布](subsystems/releases.md)。当前为本机发布，尚未配置公证或网络更新服务。

图标源文件为 `assets/app-icon.png`，构建时通过 macOS 自带的 `sips` 和 `iconutil` 生成 16–1024 像素的多尺寸 `Resources/Fuyu.icns`，并写入 `CFBundleIconFile`。当前图标采用用户确认的蓝色 F 与橙色 Y 组合，底色为不透明纯白 `#FFFFFF`，PNG 为 1024×1024；可编辑矢量源保存在 `assets/app-icon.svg`。Preview 图标在该矢量源上增加紫色 P 标记，窗口标题显示 Preview 与构建 ID。更新矢量源后，执行 `sips -s format png assets/app-icon.svg --out assets/app-icon.png` 导出 PNG，再重新构建以更新图标。终端启动便于查看退出错误：

```sh
"bin/Fuyu Preview.app/Contents/MacOS/Fuyu" -cwd /path/to/project -codex codex
```

启动或晋升时替换完整 `.app`，并在本机 Launch Services 工具可用时重新登记此应用，使系统重新读取图标与 bundle 元数据。图标验证包含 Finder 实际预览，不能只核对 `.icns` 文件存在。正式图标历史回归见[图标验收](verification/2026-10-23-app-icon.md)。

通过 Finder 或 Dock 启动时，Fuyu 自动发现 Codex，并为自身及其子进程补充 PATH。发现顺序为应用继承的 PATH、用户交互式登录 shell 的 PATH、当前用户的常见安装目录与系统安装目录；补充 Bun、npm 自定义前缀常用目录、pnpm、Volta、asdf 与 nvm 目录。nvm 额外检查 `NVM_DIR`、`~/.nvm` 和 `~/.local/share/nvm`；无已配置优先级时按版本号从新到旧补充。只读取安装位置，不安装或更新 Codex，也不修改系统环境或 shell 配置。

`-codex /absolute/path` 优先使用指定文件，文件失效时明确报错。路径含空格可正常使用；自动搜索排除空目录与相对 PATH 项。npm/Bun 的 `#!/usr/bin/env node` 包装入口会检查 Node.js 是否可找到，避免只修复 Codex 路径后仍无法启动。

窗口先恢复本地索引并显示，再后台发现与连接 Agent。登录 shell 环境探测最多等待 2 秒，输出最多 64 KiB，失败后继续检查常见目录；版本探测与 App Server 初始化并行，分别最多 5 秒和 60 秒。未找到 Codex、缺少 Node.js 或连接失败时，窗口显示具体原因。自定义安装位置可通过登录 shell 的 PATH 或 `-codex` 指定。实际验证见[启动路径验收](verification/2026-10-23-codex-discovery.md)。

正式版默认数据目录为 `~/Library/Application Support/Fuyu`，Preview 为 `~/Library/Application Support/Fuyu Preview`，可用 `-data-dir /absolute/path` 指定同渠道的独立目录。一个数据目录仅允许一个进程持有；同一索引绑定规范化后的原生历史根。正式版 Codex 沿用 `CODEX_HOME`（默认 `~/.codex`），Preview 使用自己数据目录下的 `runtimes/codex`。数据库损坏或高版本会明确报错并保留文件，避免覆盖原数据。详见[存储与恢复](subsystems/storage.md)。

Claude Code 使用 PATH、`~/.local/bin/claude`、Homebrew 常见目录发现。正式版配置根来自 `CLAUDE_CONFIG_DIR`，默认 `~/.claude`；Preview 使用自己数据目录的 `runtimes/claude`。应用按 Agent 独立绑定根目录，子进程显式使用这一根，切换配置根时使用独立应用数据目录。Claude 检查点保存于应用数据目录的 `claude/`。缺失 CLI 时点击对应的新建图标显示具体原因，可继续使用另一可用 Agent。

## 2. 目录与代码定位

入口为 `cmd/desktop/main.go`。现有包与状态/进程边界见[子系统索引](subsystems/README.md)和[运行时说明](subsystems/codex-runtime.md)。

构建在写完资源和 Info.plist 后，先签名内嵌 libghostty，再以当前渠道的 Bundle ID 签名整个应用（正式版 `com.shenming.fuyu`、Preview `com.shenming.fuyu.preview`），并严格核验包签名。本地默认 `FUYU_CODESIGN_IDENTITY=-`，使用 ad hoc 签名；已有 Apple 签发的代码签名证书时，通过 `FUYU_CODESIGN_IDENTITY` 指定钥匙串中的身份。Info.plist 包含局域网模型服务用途说明。macOS 按负责的 GUI 应用身份判定其子进程的局域网权限，终端 CLI 则自动获准访问；仅在 CLI 中连通不能证明 Fuyu 已获准。ad hoc 签名仍不能保证跨构建的权限身份稳定；可靠追踪需要 Apple 签发的身份。此前本机阻断与验证边界见[网络验证](verification/2026-11-02-local-network.md)。

## 3. 验证方式

主窗口终端使用 Ghostty 原生库，macOS arm64 构建启用 cgo。`./scripts/build` 通过 `scripts/prepare-ghostty.py` 构建并打包固定版本原生库、主题、shell integration 和许可证；首次构建需要网络和 Zig 0.15.2（可用 `FUYU_ZIG` 指定，未提供时下载校验后的工具链）。后续构建复用 `.cache/ghostty-native/`。终端不依赖 WebView、JavaScript、npm 或已安装的 Ghostty.app。配置复用与原生验收方式见[终端验证](verification/2026-11-01-terminal.md)。

| 改动 | 应执行的验证 |
|---|---|
| 文档、规范或模板 | 链接、内容一致性、文件可见性、最终差异 |
| Go 逻辑 | 改动文件格式、相关测试；可用后运行完整测试与 vet |
| 协议与状态 | 请求/通知夹具、错误/断线/失效分支、实际版本兼容检查 |
| goroutine / 取消 / 进程 | 并发与 race 检查、退出及清理验证 |
| 原生 UI | 组件验证加真实窗口操作，覆盖输入法、焦点、滚动和缩放 |
| Codex 任务链路 | 临时工作目录中的真实调用、文件结果、原生终态和自有进程核验 |

已执行的基础验证入口：

```sh
./scripts/go test ./...
./scripts/go vet ./...
./scripts/go test -race ./...
```

对修改过的 Go 文件运行 `./scripts/go fmt ./...`。普通测试使用受控子进程与 MyGo 内存渲染器，不启动真实模型任务。

单独执行真实 Codex 验证会使用本机已有登录和模型服务，在临时目录创建测试文件，并产生真实模型调用：

```sh
FUYU_LIVE=1 ./scripts/go test ./internal/codex -run '^TestLiveCodexLoop$' -v -count=1
```

会话恢复的真实双目录、重开恢复与小型开发任务验证：

```sh
FUYU_LIVE=1 ./scripts/go test ./internal/codex -run '^TestLiveP3DirectoriesAndRestart$' -v -count=1
```

`TestLiveCodexLoop` 验证实际文件、同 thread 的第二轮上下文、原生中断终态和 App Server 退出；它不证明所有后台命令都随 `turn/interrupt` 退出。原生 GUI、真实输入法与进程检查单独记录。

文件编辑与 Git 的真实文件监听联动与 500 文件负载分别按需执行；普通测试跳过真实调用。两者使用临时目录和内存渲染器，覆盖磁盘、缓冲和真实协议联动，原生窗口另行验收：

```sh
FUYU_LIVE=1 ./scripts/go test ./internal/ui -run '^TestLiveP4AgentWritesWhileEditing$' -v -count=1
FUYU_LIVE_BATCH=1 ./scripts/go test ./internal/ui -run '^TestLiveP4BatchWritesWhileEditing$' -v -count=1
```

## 4. 自动化与真实验收

Claude 真实文件/审批/恢复/拒绝/问答/故障隔离与双 Agent 联合验收分别显式启用：

```sh
FUYU_LIVE_CLAUDE=1 ./scripts/go test ./internal/claude -run '^TestLiveClaude' -v -count=1
FUYU_LIVE_CLAUDE=1 FUYU_LIVE_CLAUDE_COMPACT=1 ./scripts/go test ./internal/claude -run '^TestLiveClaudeFileApprovalResume$' -v -count=1
FUYU_LIVE_AGENTS=1 ./scripts/go test ./internal/ui -run '^TestLiveAgentsRunIndependently$' -v -count=1
```

`internal/claude/testdata/server.py` 是合成协议服务；`live_process_compare.py /absolute/path/to/claude` 会真实比较每轮进程与常驻进程的双轮上下文，在临时目录保存双向 wire。以上真实入口使用现有原生登录与模型服务；普通 Go 测试跳过真实调用。不要把合成夹具或内存渲染器结果当作原生窗口验收。

自动化测试使用临时目录、受控子进程和协议夹具。使用 `t.Setenv` 等测试内机制隔离需要的配置路径；如测试框架需要独立 HOME，由测试启动器设置，保持开发者现有 shell 环境。隔离时保留可用的构建缓存路径。

真实 Codex 验收使用单独的临时工作目录，记录实际版本、有效设置、目录、操作和文件结果。复用已授权的原生登录方式，保持真实配置和凭据文件不变；日志与截图在保存前脱敏。

测试结束核对自有进程和必要清理。真实服务不可用时如实记录，夹具验证结果单独保留。

## 5. 验证记录与交接

Fuyu 间歇性无法收到模型回复时，在重启或切换网络前执行 `python3 scripts/diagnose-network.py`。默认保存最近 15 分钟、网关端口 3425 的内核网络日志，以及 Fuyu 进程树、nehelper、签名、系统代理与路由快照到 `~/Library/Logs/Fuyu/network/` 的独立目录；通过 `--minutes`、`--port` 调整实际窗口和端口。脚本保留每项命令的退出结果，采集失败时仍保留其余文件；不会发送测试请求、重启应用或改变网络配置。故障会话与大致发送时间需另行记录，用于对齐原生 Runtime 和网关时间。局域网签名故障证据与边界见[网络验证](verification/2026-11-02-local-network.md)。

实际验收后按需创建 `docs/verification/`，以“日期-主题.md”记录操作、输入、期望、实际结果和证据。协议/进程测试应记录失败分支；截图只用于证明对应的实际场景。

需要交接未完成任务时，按需记录复现方法、自有进程、临时目录、已知问题和下一动作。

## 6. 文档分工

| 事实 | 维护位置 |
|---|---|
| 产品范围、状态与协议约定 | [设计文档](design.md) |
| 子系统职责与源码定位 | [子系统索引](subsystems/README.md)及后续专项文档 |
| 编码与审核规则 | [代码标准](code-standards.md) |
| 实际问题的原因与预防动作 | [经验记录](../LESSONS.md) |
| 上游源码与版本证据 | [参考核验](reference-verification.md) |

同一事实维护一份，通过链接引用。行为、契约和验收要求变化时同步更新对应资料；单纯内部整理且契约未变时按实际需要更新。
