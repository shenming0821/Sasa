# 开发指南

## 1. 当前状态与环境

当前已实现并完成当前环境下的 P1/P2/P3 验收，包括多会话、SQLite 索引、原生历史恢复及窗口生命周期。进度见[实施计划](implementation-plan.md)，实际覆盖与支持边界见 [P3 验证](verification/2026-10-22-p3.md)。

本机已验证环境：macOS arm64、Command Line Tools、Go `1.27.3`、Codex CLI `0.163.0`。`go.mod` 要求 Go `1.27.1`，指定工具链 `go1.27.3`；MyGo 固定为 `v0.6.3-0.20261022014156-976f17c9391f`。

Go 安装在 `~/.local/share/fuyu/toolchains/go1.27.3/`，未修改系统工具链。`scripts/go` 按 `FUYU_GO`、PATH 中的 `go`、本机专用安装路径的顺序选择；其他机器可安装 Go 后直接使用包装脚本。

构建、运行、test、vet 统一使用 `scripts/go`，并需要 Python 3 标准库。包装脚本通过 `scripts/mygo-input-style.py` 将锁定的 MyGo 复制到项目内被忽略的 `.cache/mygo-input-style/`，调整输入占位文字和光标绘制，再通过临时 `-modfile` 使用该副本；共享模块缓存、项目 `go.mod` 和 `go.sum` 保持原内容。缓存可删除后重建。原生 `go build/test` 不包含此样式补丁，界面验收以包装脚本构建为准；升级 MyGo 时须重新核对补丁，版本或源码不匹配会明确失败。

该补丁同时让多行编辑器接收 `Cmd+Enter`，通过原有编辑器插入换行，保留光标、选区与撤销逻辑；输入框视图使用普通 Enter 发送，输入法组合门控继续保留。

P2 另在该隔离副本中修复 macOS 无障碍密码字段 setter 的缓存：AX value/selectedText 写入原文时，只将原文发送给编辑器，不保存在可读的 AppKit 无障碍值中。版本或目标代码不匹配时同样明确失败；升级框架后重新核对原生秘密输入和自动填充。

```sh
./scripts/go version
./scripts/build
open bin/Fuyu.app
```

`scripts/build` 构建本地开发 `.app`，输出 `bin/Fuyu.app`；原有 `bin/fuyu` 入口通过符号链接指向包内同一可执行文件，每次构建自动同步，使用同一默认数据目录。包含 bundle 元数据和应用图标；尚未配置正式发布签名、公证或安装器。图标源文件为 `assets/app-icon.png`，构建时通过 macOS 自带的 `sips` 和 `iconutil` 生成 16–1024 像素的多尺寸 `Resources/Fuyu.icns`，并写入 `CFBundleIconFile`。当前图标采用用户确认的蓝色 F 与橙色 Y 组合，底色为不透明纯白 `#FFFFFF`，PNG 为 1024×1024；可编辑矢量源保存在 `assets/app-icon.svg`。更新矢量源后，执行 `sips -s format png assets/app-icon.svg --out assets/app-icon.png` 导出 PNG，再重新构建以更新图标。终端启动便于查看退出错误：

```sh
bin/Fuyu.app/Contents/MacOS/Fuyu -cwd /path/to/project -codex codex
```

构建完成后更新 `.app` 外层目录的修改时间，并在本机 Launch Services 工具可用时重新登记此应用，使系统重新读取图标与 bundle 元数据。原地覆盖包内文件可能保留外层目录时间，导致 Finder 和启动后的应用继续沿用默认图标缓存。图标验证包含 Finder 实际预览，不能只核对 `.icns` 文件存在。实际回归见[图标验收](verification/2026-10-23-app-icon.md)。

通过 Finder 或 Dock 启动时，Fuyu 自动发现 Codex，并为自身及其子进程补充 PATH。发现顺序为应用继承的 PATH、用户交互式登录 shell 的 PATH、当前用户的常见安装目录与系统安装目录；补充 Bun、npm 自定义前缀常用目录、pnpm、Volta、asdf 与 nvm 目录。nvm 额外检查 `NVM_DIR`、`~/.nvm` 和 `~/.local/share/nvm`；无已配置优先级时按版本号从新到旧补充。只读取安装位置，不安装或更新 Codex，也不修改系统环境或 shell 配置。

`-codex /absolute/path` 优先使用指定文件，文件失效时明确报错。路径含空格可正常使用；自动搜索排除空目录与相对 PATH 项。npm/Bun 的 `#!/usr/bin/env node` 包装入口会检查 Node.js 是否可找到，避免只修复 Codex 路径后仍无法启动。

登录 shell 环境探测最多等待 2 秒，输出最多 64 KiB，失败后继续检查常见目录；版本探测最多 5 秒，App Server 初始化单独允许 60 秒。未找到 Codex、缺少 Node.js 或连接失败时，窗口显示具体原因。自定义安装位置可通过登录 shell 的 PATH 或 `-codex` 指定。实际验证见[启动路径验收](verification/2026-10-23-codex-discovery.md)。

默认数据目录为 `~/Library/Application Support/Fuyu`，可用 `-data-dir /absolute/path` 隔离测试实例。一个数据目录仅允许一个进程持有；同一索引绑定规范化后的 `CODEX_HOME`（默认 `~/.codex`），切换原生历史目录时使用独立数据目录。数据库损坏或高版本会明确报错并保留文件，避免覆盖原数据。详见[存储与恢复](subsystems/storage.md)。

## 2. 目录与代码定位

入口为 `cmd/desktop/main.go`。现有包与状态/进程边界见[子系统索引](subsystems/README.md)和[运行时说明](subsystems/codex-runtime.md)。

## 3. 验证方式

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

P3 的真实双目录、重开恢复与小型开发任务验证：

```sh
FUYU_LIVE=1 ./scripts/go test ./internal/codex -run '^TestLiveP3DirectoriesAndRestart$' -v -count=1
```

P1 的 `TestLiveCodexLoop` 验证实际文件、同 thread 的第二轮上下文、原生中断终态和 App Server 退出；它不证明所有后台命令都随 `turn/interrupt` 退出。原生 GUI、真实输入法与进程检查单独记录。

P4 的真实文件监听联动与 500 文件负载分别按需执行；普通测试跳过真实调用。两者使用临时目录和内存渲染器，覆盖磁盘、缓冲和真实协议联动，原生窗口另行验收：

```sh
FUYU_LIVE=1 ./scripts/go test ./internal/ui -run '^TestLiveP4AgentWritesWhileEditing$' -v -count=1
FUYU_LIVE_BATCH=1 ./scripts/go test ./internal/ui -run '^TestLiveP4BatchWritesWhileEditing$' -v -count=1
```

## 4. 自动化与真实验收

自动化测试使用临时目录、受控子进程和协议夹具。使用 `t.Setenv` 等测试内机制隔离需要的配置路径；如测试框架需要独立 HOME，由测试启动器设置，保持开发者现有 shell 环境。隔离时保留可用的构建缓存路径。

真实 Codex 验收使用单独的临时工作目录，记录实际版本、有效设置、目录、操作和文件结果。复用已授权的原生登录方式，保持真实配置和凭据文件不变；日志与截图在保存前脱敏。

测试结束核对自有进程和必要清理。真实服务不可用时如实记录，夹具验证结果单独保留。

## 5. 验证记录与交接

实际验收后按需创建 `docs/verification/`，以“日期-主题.md”记录操作、输入、期望、实际结果和证据。协议/进程测试应记录失败分支；截图只用于证明对应的实际场景。

实现完成或中断前，按[实施计划](implementation-plan.md#6-中断后如何接续)更新接续点与工作记录。复现方法、当前自有进程、临时目录、已知问题和下一动作应可由另一位开发者直接接续。

## 6. 文档分工

| 事实 | 维护位置 |
|---|---|
| 当前阶段、待办、工作记录 | [实施计划](implementation-plan.md) |
| 产品范围、状态与协议约定 | [设计文档](design.md) |
| 子系统职责与源码定位 | [子系统索引](subsystems/README.md)及后续专项文档 |
| 编码与审核规则 | [代码标准](code-standards.md) |
| 实际问题的原因与预防动作 | [经验记录](../LESSONS.md) |
| 上游源码与版本证据 | [参考核验](reference-verification.md) |

同一事实维护一份，通过链接引用。行为、契约和验收要求变化时同步更新对应资料；单纯内部整理且契约未变时按实际需要更新。
