# 开发指南

## 1. 当前状态与环境

当前已实现 P1 的首个可运行版本，进度见[实施计划](implementation-plan.md)，验收证据见[本轮记录](verification/2026-10-22-p1.md)。

本机已验证环境：macOS arm64、Command Line Tools、Go `1.27.3`、Codex CLI `0.163.0`。`go.mod` 要求 Go `1.27.1`，指定工具链 `go1.27.3`；MyGo 固定为 `v0.6.3-0.20261022014156-976f17c9391f`。

Go 安装在 `~/.local/share/fuyu/toolchains/go1.27.3/`，未修改系统工具链。`scripts/go` 按 `FUYU_GO`、PATH 中的 `go`、本机专用安装路径的顺序选择；其他机器可安装 Go 后直接使用包装脚本。

构建、运行、test、vet 统一使用 `scripts/go`，并需要 Python 3 标准库。包装脚本通过 `scripts/mygo-input-style.py` 将锁定的 MyGo 复制到项目内被忽略的 `.cache/mygo-input-style/`，调整输入占位文字和光标绘制，再通过临时 `-modfile` 使用该副本；共享模块缓存、项目 `go.mod` 和 `go.sum` 保持原内容。缓存可删除后重建。原生 `go build/test` 不包含此样式补丁，界面验收以包装脚本构建为准；升级 MyGo 时须重新核对补丁，版本或源码不匹配会明确失败。

该补丁同时让多行编辑器接收 `Cmd+Enter`，通过原有编辑器插入换行，保留光标、选区与撤销逻辑；输入框视图使用普通 Enter 发送，输入法组合门控继续保留。

```sh
./scripts/go version
./scripts/build
open bin/Fuyu.app
```

`scripts/build` 构建未签名的本地开发 `.app`，输出 `bin/Fuyu.app`，包含 bundle 元数据；尚未涉及发布、签名、公证或安装器。终端启动便于查看退出错误：

```sh
bin/Fuyu.app/Contents/MacOS/Fuyu -cwd /path/to/project -codex codex
```

通过 Finder 启动时，`codex` 必须在应用继承的 PATH 中；定位失败时可从终端使用 `-codex` 指定绝对路径。首次连接失败会在窗口中显示具体错误。

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

该测试验证实际文件、同 thread 的第二轮上下文、原生中断终态和 App Server 退出；它不证明所有后台命令都随 `turn/interrupt` 退出。原生 GUI、真实输入法与进程检查单独记录。

## 4. 自动化与真实验收

自动化测试使用临时目录、受控子进程和协议夹具。使用 `t.Setenv` 等测试内机制隔离需要的配置路径；如测试框架需要独立 HOME，由测试启动器设置，保持开发者现有 shell 环境。隔离时保留可用的构建缓存路径。

真实 Codex 验收使用单独的临时工作目录，记录实际版本、有效设置、目录、操作和文件结果。复用已授权的原生登录方式，保持真实配置和凭据文件不变；日志与截图在保存前脱敏。

测试结束核对自有进程和必要清理。真实服务不可用时如实记录，夹具验证结果单独保留。

## 5. 验证记录与交接

实际验收后按需创建 `docs/verification/`，以“日期-主题.md”记录操作、输入、期望、实际结果和证据。协议/进程测试应记录失败分支；截图只用于证明对应的实际场景。

实现完成或中断前，按[实施计划](implementation-plan.md#7-中断后如何接续)更新接续点与工作记录。复现方法、当前自有进程、临时目录、已知问题和下一动作应可由另一位开发者直接接续。

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
