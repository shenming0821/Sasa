# Sasa 参考项目核验记录

2026-11-03 分屏只读参考：本地 Mayoi 快照 `/tmp/sasa-mayoi-diff-20261030`，固定提交 `cd08688ade5ece3411d9bb812228c7d30492526a`，核对 `cmd/mayoi/columns.go` 与 `tabs.go` 的列页签、分割、资源归属与空列收敛；其该快照主要为左右列，不据此宣称具备 Sasa 的上下嵌套布局。Orca 使用本机已解包的 `/tmp/orca-asar/app/out/shared/rpc-contract/workspace-layout-params.js` 和 `workspace-layout-terminal-tab-params.js`，参考 `splitPane`、`movePane`、`splitGroup` 与布局比例的命令职责；未核定该解包产物的版本。Sasa 自行实现按目录保存的二叉分屏树和面板焦点，复用固定 MyGo 的 `SplitPercent` / `SplitVerticalPercent`；未移植参考项目源码，验收以 Sasa 自身测试为准。

> 核验日期：2026-10-22  
> 用途：补全设计缺口，并为 Codex UI 的逐步实现提供可追溯依据。  
> 当前功能与实现入口：[子系统索引](subsystems/README.md)

## 1. 证据范围

本次直接读取参考仓库固定提交下的文档、实现和测试源码。GitHub API 发生限流后，改用只读浅克隆获取源码。未执行这些项目的测试，也未把它们的实现直接复制进 Sasa。

| 项目 | 固定提交 | 本次用途 |
|---|---|---|
| openai/codex | `2a77e82f09212c9e1bbf02656afca5d8172c5f9e` | App Server 协议、thread/turn 状态、恢复与审批测试 |
| egoist/mygo | `976f17c9391fb86f9afeba7a2d3b27b54e6d671a` | 原生聊天、主线程更新、列表与 Markdown |
| qsnh/murmur | `650019b39363f5a722b2e7c706453a7ce6df2cec` | 原生聊天状态、逐会话滚动、窗口驻留 |
| cdesktop-ai/cdesktop | `75bd015e88f9797d785c9fabcfca03c7151fd5cc` | Codex 双向 RPC、握手和历史读取的实现参考 |

官方 Codex 文档页面未成功取得正文，本记录以官方 Codex 源码和本机安装包生成的 schema 为依据。固定提交是研究快照，不能视为与本机二进制完全一致的版本。

Mayoi 的文件树、Diff 和 Git 功能已移到后续范围，本次没有对其实现做专项核验。Claude Code、OpenCode 同样不在本次协议核验范围内。

## 2. Codex：会话与执行的状态

来源：

- `openai/codex/codex-rs/app-server-protocol/src/protocol/v2/thread.rs`
- `openai/codex/codex-rs/app-server-protocol/src/protocol/v2/turn.rs`

`ThreadStatus` 定义 notLoaded、idle、systemError、active；active 的 flags 包含 waitingOnApproval、waitingOnUserInput。`TurnStatus` 定义 completed、interrupted、failed、inProgress。

落实：Sasa 将连接、thread、turn、本地交互阶段分别维护。一次执行完成后继续保留会话；失联需要重新核验，不直接写为失败或成功。

固定源码：

```text
https://github.com/openai/codex/blob/2a77e82f09212c9e1bbf02656afca5d8172c5f9e/codex-rs/app-server-protocol/src/protocol/v2/thread.rs
https://github.com/openai/codex/blob/2a77e82f09212c9e1bbf02656afca5d8172c5f9e/codex-rs/app-server-protocol/src/protocol/v2/turn.rs
```

## 3. Codex：读取与恢复的边界

`ThreadReadParams` 提供 threadId 和 includeTurns；源码对分页 thread 建议读取元数据后使用 `thread/turns/list`、`thread/items/list`。`ThreadResumeParams` 支持 threadId、excludeTurns 等字段，响应包含 thread、实际 cwd、模型、审批与沙箱信息及版本相关的分页字段。

测试来源：

`openai/codex/codex-rs/app-server/tests/suite/v2/thread_resume.rs`

| 测试函数 | 证明的源码覆盖范围 |
|---|---|
| `thread_resume_rejoins_running_paginated_thread_with_initial_page` | 恢复接口重新接入仍在运行的分页会话 |
| `thread_resume_can_skip_turns_when_thread_is_running` | 活动会话可按协议选择不返回历史 turns |
| `thread_resume_replays_pending_command_execution_request_approval` | 重新接入时重放待处理命令审批 |
| `thread_resume_replays_pending_file_change_request_approval` | 重新接入时重放待处理文件审批 |
| `thread_resume_and_read_interrupt_incomplete_rollout_turn_when_thread_is_idle` | 原生读取/恢复对闲置会话中的未完成历史有专门处理 |

落实：区分同一活动 Runtime 的重新接入与 Runtime 已结束后的历史恢复。审批重放使用本次连接实际收到的请求 ID；不提交磁盘缓存中的旧请求。

这些测试使用受控环境和模型响应夹具。本次只读取测试，未执行；不能据此宣称 Sasa 或当前安装版本已通过真实运行验证。

固定源码：

```text
https://github.com/openai/codex/blob/2a77e82f09212c9e1bbf02656afca5d8172c5f9e/codex-rs/app-server/tests/suite/v2/thread_resume.rs
https://github.com/openai/codex/blob/2a77e82f09212c9e1bbf02656afca5d8172c5f9e/codex-rs/app-server/tests/suite/v2/thread_read.rs
```

## 4. Codex：审批、用户提问与中断

方法与通知定义：`codex-rs/app-server-protocol/src/protocol/common.rs`。

具体请求、答案、决定类型：`codex-rs/app-server-protocol/src/protocol/v2/item.rs`。

- 命令审批与文件审批是服务端请求，客户端回复其 RPC ID。
- 命令审批的 decline 表示拒绝操作并允许 turn 继续；cancel 表示拒绝并中断 turn。
- `item/tool/requestUserInput` 在源码中标为实验能力；questions 包含 id、header、question、options、isOther、isSecret，响应为按问题 ID 组织的 answers。
- 当前类型还包含 isBlocking，缺省解析为 true；不能把 autoResolutionMs 当作当前阻塞判断依据。
- `serverRequest/resolved` 带 threadId 和 requestId，用于结束待处理交互入口。
- 还有额外权限请求、MCP elicitation 等服务端请求。通用“批准/拒绝”弹窗不能自动覆盖它们。

中断相关测试：`codex-rs/app-server/tests/suite/v2/turn_interrupt.rs`。

- `turn_interrupt_rejects_completed_turn`：已完成 turn 的中断会报错。
- `turn_interrupt_resolves_pending_command_approval_request`：中断与待审批请求清理有关联。

落实：请求状态单独维护，发送响应后仍需处理失效/解决通知；拒绝操作、中断执行和连接断开使用不同状态。

固定源码：

```text
https://github.com/openai/codex/blob/2a77e82f09212c9e1bbf02656afca5d8172c5f9e/codex-rs/app-server-protocol/src/protocol/common.rs
https://github.com/openai/codex/blob/2a77e82f09212c9e1bbf02656afca5d8172c5f9e/codex-rs/app-server-protocol/src/protocol/v2/item.rs
https://github.com/openai/codex/blob/2a77e82f09212c9e1bbf02656afca5d8172c5f9e/codex-rs/app-server-protocol/src/protocol/v2/notification.rs
https://github.com/openai/codex/blob/2a77e82f09212c9e1bbf02656afca5d8172c5f9e/codex-rs/app-server/tests/suite/v2/turn_interrupt.rs
```

## 5. MyGo / Murmur：原生聊天交互

MyGo：

- `examples/markdown-chat/main.go` 使用原生列表、输入区、流式/完成态 Markdown。
- `docs/ui/views.md` 明确后台 goroutine 通过 `Window.Update` 在主线程更新状态。
- `docs/ui/list.md` 提供稳定行 key、FollowEnd、跳转末尾和加载较早消息的机制。
- `go.mod` 在本次固定提交声明 `go 1.27.1`。

Murmur：

- `app.go` 的状态更新入口调用 `Window.Update`，流式调用在后台执行。
- `store.go` 为 Chat 保存独立列表状态和取消函数。
- `view.go` 的消息列表使用会话 ID 作为 key，并设置 FollowEnd。
- `tray.go` 区分应用退出与窗口关闭，窗口关闭时隐藏窗口。

落实：沿用这些交互机制组织 Sasa UI；Murmur 的模型直连、聊天存储和取消调用不能直接替代 Codex 的原生会话、历史与 turn 中断。

固定源码：

```text
https://github.com/egoist/mygo/blob/976f17c9391fb86f9afeba7a2d3b27b54e6d671a/examples/markdown-chat/main.go
https://github.com/egoist/mygo/blob/976f17c9391fb86f9afeba7a2d3b27b54e6d671a/docs/ui/views.md
https://github.com/egoist/mygo/blob/976f17c9391fb86f9afeba7a2d3b27b54e6d671a/docs/ui/list.md
https://github.com/qsnh/murmur/blob/650019b39363f5a722b2e7c706453a7ce6df2cec/app.go
https://github.com/qsnh/murmur/blob/650019b39363f5a722b2e7c706453a7ce6df2cec/store.go
https://github.com/qsnh/murmur/blob/650019b39363f5a722b2e7c706453a7ce6df2cec/view.go
https://github.com/qsnh/murmur/blob/650019b39363f5a722b2e7c706453a7ce6df2cec/tray.go
```

## 6. cdesktop：双向 JSON-RPC 的工程参考

来源：

- `crates/executors/src/executors/codex/client.rs`
- `crates/executors/src/executors/codex/jsonrpc.rs`

client 实现 initialize → initialized 握手、thread/start、turn/start 和 thread/read。JSON-RPC 层区分响应、错误、服务端请求与通知，以 pending 表关联客户端请求，并在连接结束时清理 pending。

落实：Sasa 可以借鉴职责划分，但协议以实际 Codex 版本为准。尤其不能照搬“输出流结束即成功”的处理来判断任务成功；必须核验 turn 终态。该项目也不能证明 Sasa 的跨进程退出、恢复和生命周期已经解决。

固定源码：

```text
https://github.com/cdesktop-ai/cdesktop/blob/75bd015e88f9797d785c9fabcfca03c7151fd5cc/crates/executors/src/executors/codex/client.rs
https://github.com/cdesktop-ai/cdesktop/blob/75bd015e88f9797d785c9fabcfca03c7151fd5cc/crates/executors/src/executors/codex/jsonrpc.rs
```

## 7. 本机协议结构核验

实际安装版本：`codex-cli 0.163.0`。

只读版本/帮助检查与临时 schema 生成：

```sh
codex --version
codex app-server --help
codex app-server generate-json-schema --help
codex app-server generate-json-schema --experimental --out /tmp/sasa-design-research/installed-schema
```

生成结果核对了：

- ThreadResumeParams：threadId、excludeTurns、cwd 等字段。
- ThreadReadParams：threadId、includeTurns。
- ToolRequestUserInputParams：threadId、turnId、itemId、questions、isBlocking。
- ToolRequestUserInputResponse：answers。
- CommandExecutionRequestApprovalParams / FileChangeRequestApprovalParams。
- ServerRequestResolvedNotification：threadId、requestId。
- TurnInterruptParams：threadId、turnId。

schema 生成只证明安装包导出这些结构，不能证明实验能力默认开启，也不能证明真实服务端、模型或配置一定会触发相应交互。实现时应固定版本并完成真实握手和集成验证。

当前 PATH 找不到 Go。未安装工具链、修改用户 Codex 配置、启动/停止现有 Daemon，也未执行真实模型任务。

## 8. 仍需通过实现验证的项目

- MyGo 在本机的构建、中文输入法、长消息渲染和窗口行为。
- 安装版本的实际握手、有效配置与实验能力开关。
- 工具执行、权限审批、用户提问是否能在相应配置下真实触发。
- 共享 App Server 的进程归属及多 thread 生命周期。
- 断流后快照与活动事件的合并边界，不能只靠本地序号声称可靠重放。
- 应用退出、异常退出、残留进程和原生历史恢复。
- 后续如需复用第三方代码，再核对相关文件的许可和署名要求。

## 9. Magpie：项目规范的参考

用户指定参考 `yetone/magpie` 的 `AGENTS.md`。本次从远程确认 main 提交为 `ea6be1592706ea4a0dd6cd526acec0b5d1fb967e`，读取其 Agent 指南、代码标准、子系统文档索引，以及经验记录中与配置、测试、并发和交付有关的内容。

采用的组织方式：

- Agent 指南作为短入口，链接代码规则、子系统约定和经验记录。
- 说明行为变化时追踪完整路径，同步维护对应文档和验证证据。
- 测试按行为和风险选择，隔离真实用户配置，并明确资源清理责任。
- 已发生的问题以条件、动作、原因和证据沉淀，通用规则归入代码标准。

Sasa 的具体规则依据本项目的 Codex Runtime、MyGo 原生 UI 和当前开发阶段编写。Magpie 的订阅迁移、社区作者名单、Web GUI 测试、跨平台发布流程和历史缺陷属于其自身项目背景，不作为 Sasa 的现有机制或经历。

规范入口为根目录 `AGENTS.md`，`CLAUDE.md` 仅引用该入口。规范建立时应用代码尚未创建；随后真实任务闭环已建立可运行实现，实际构建/测试命令见[开发指南](development.md)。本节仅记录规范参考来源。

固定参考：

```text
https://github.com/yetone/magpie/blob/ea6be1592706ea4a0dd6cd526acec0b5d1fb967e/AGENTS.md
https://github.com/yetone/magpie/blob/ea6be1592706ea4a0dd6cd526acec0b5d1fb967e/docs/code-standards.md
https://github.com/yetone/magpie/blob/ea6be1592706ea4a0dd6cd526acec0b5d1fb967e/docs/subsystems/README.md
https://github.com/yetone/magpie/blob/ea6be1592706ea4a0dd6cd526acec0b5d1fb967e/LESSONS.md
```

## 10. cdesktop：已确认采用的五项设计参考

2026-10-22，用户确认记录以下五项，作为后续实现与审核的检查依据。源码核对版本为 `75bd015e88f9797d785c9fabcfca03c7151fd5cc`；本次仅进行源码分析，未运行 cdesktop。下表描述 Sasa 待落实的动作，实际实现与覆盖边界见相关子系统说明。

| 参考点 | 在什么条件下，执行什么动作 | 落实阶段与验收 |
|---|---|---|
| 审批独立状态 | 收到审批或用户提问时，保存原生请求 ID 及 thread/turn/item 归属；分别处理待回答、提交中、已解决、取消和失效。任务结束或连接失效时更新入口，防止重复响应；等待用户期间保持协议读取可用 | 任务过程与交互：覆盖批准、拒绝、回答、中断、晚到结果与重复点击 |
| 明确滚动意图 | 用户位于末尾时跟随流式输出；用户向上阅读时保持阅读锚点；用户主动返回最新消息时恢复跟随。历史补入和内容高度变化时按相同规则处理 | 真实任务闭环：验收真实窗口的流式阅读；会话持久化与恢复：补齐会话切换和历史加载时的阅读恢复 |
| 结构化工具状态 | 展示工具事件时，以原生 item ID 合并结构化的工具类型、输入/输出、审批及执行状态；依据 item 终态更新工具结果，独立于 turn 终态。第二种 Agent 接入时再按实际差异提取公共模型 | 任务过程与交互：核对命令、文件修改、失败、拒绝与超时；保留必要原生字段 |
| 总量受控的事件缓冲 | 处理持续输出时，限制事件队列和显示缓存的总容量，检测消费者落后及事件缺口；发生缺口时通过快照重新核验，无法补齐时明确显示结果不完整 | 任务过程与交互：大量输出期间输入、停止和阅读可用；超限后仍能核对终态。容量值在实现时确定并实测，避免直接复制参考项目阈值 |
| 应用历史与 Runtime 历史分工 | 实现持久化时，应用保存会话索引、草稿、阅读位置等自身数据；对话历史和任务状态向 Codex 原生接口核验。恢复时分别判断可读历史、可继续对话和可重新接入活动任务 | 会话持久化与恢复：覆盖正常重开、异常退出、失联与状态校准，保持原始凭据和配置 |

架构保持：继续采用 Go + MyGo 原生 UI，当前连续对话复用同一 App Server 和原生 thread；用户明确要求从历史分支会话时再引入 fork。上述参考不引入 Web UI、多 Agent 框架或每轮重启进程的要求。

进程清理单独核验：可以参考 cdesktop 的分级信号与执行监控，但进程组清理本身不能证明独立后台进程组已退出。Sasa 继续分别记录原生中断终态、工具终态和实际进程退出结果。

固定源码路径（均相对于上述 cdesktop 提交）：

```text
crates/services/src/services/approvals/executor_approvals.rs
crates/executors/src/executors/codex/client.rs
packages/web-core/src/features/workspace-chat/model/conversation-scroll-commands.ts
crates/executors/src/logs/mod.rs
crates/utils/src/msg_store.rs
crates/services/src/services/execution_process.rs
crates/executors/src/executors/codex.rs
crates/local-deployment/src/container.rs
crates/utils/src/process.rs
```

## 11. 任务过程与交互实现时的再次核验

实现任务过程与交互时，再次读取 cdesktop 固定提交 `75bd015e88f9797d785c9fabcfca03c7151fd5cc` 的审批桥接、Codex 客户端和消息存储源码。参考其设计后按 Sasa 的 Go/MyGo 与原生协议独立实现，未运行 cdesktop，也未将其测试作为本项目证据。

| 固定源码 | 读到的机制 | Sasa 的落实 |
|---|---|---|
| `crates/services/src/services/approvals/executor_approvals.rs` | 创建请求与等待结果分开，waiter 按 ID 保存，处理取消和移除 | `internal/codex/interactions.go` 独立登记原生请求，等待用户时读取和其他 RPC 继续；一次性响应，resolved/turn 完成释放 |
| `crates/executors/src/executors/codex/client.rs` | 双向请求、审批/提问和原生事件分别处理 | 保留四类已支持请求及原生响应值；UI 状态和工具状态分别投影 |
| `crates/utils/src/msg_store.rs` | 记录每条消息的估算字节数，限制历史总量，识别 broadcast lag | 队列 4 MiB / 4096 条，显示缓存 8 MiB / 1000 条；超限显式 gap，终态后读取原生快照校准。阈值按 Sasa 场景选取 |

固定来源：

```text
https://github.com/cdesktop-ai/cdesktop/blob/75bd015e88f9797d785c9fabcfca03c7151fd5cc/crates/services/src/services/approvals/executor_approvals.rs
https://github.com/cdesktop-ai/cdesktop/blob/75bd015e88f9797d785c9fabcfca03c7151fd5cc/crates/executors/src/executors/codex/client.rs
https://github.com/cdesktop-ai/cdesktop/blob/75bd015e88f9797d785c9fabcfca03c7151fd5cc/crates/utils/src/msg_store.rs
```

原生协议按本机 `codex-cli 0.163.0` 执行下列官方命令生成，再核对 `ServerRequest.json`、`ClientRequest.json` 和 `ServerNotification.json`：

```sh
codex app-server generate-json-schema --experimental --out /tmp/sasa-p2-schema
```

已确认 `serverRequest/resolved` 的关联字段为 threadId/requestId；question 按 ID 映射并含 isOther/isSecret/isBlocking；命令审批有原始 availableDecisions 和策略修订对象；文件新增/删除 diff 为原始文件内容。

## 12. 原生历史与生命周期核验

按本机 `codex-cli 0.163.0` 生成 `/tmp/sasa-p3-schema`，核对当前安装版本的 `ThreadResumeParams`、`ThreadResumeResponse`、`ThreadTurnsListParams`、`ThreadTurnsListResponse` 和 thread/list 相关定义。

```sh
codex app-server generate-json-schema --experimental --out /tmp/sasa-p3-schema
```

采用 `thread/resume` 的 `excludeTurns=true`，不覆盖原生 cwd/model/权限配置；从响应读取真实设置和 thread.status。`thread/turns/list` 使用 `itemsView=full`、`sortDirection=desc`、`limit=20` 和不透明 nextCursor；thread/list 按 `updated_at` 降序，每页20条。完整历史由原生记录提供，应用 SQLite 只保存界面元数据和原生 ID。实际字段及调用见 `internal/codex/history.go`。

窗口生命周期按固定 MyGo 提交的 `Window.OnClose` / `Hide` / `Show`、`App.OnActivate`、`Dock.SetMenu`、`App.OnBeforeQuit` 核对实现；SQLite 固定 `modernc.org/sqlite v1.60.1`。本节记录本机 schema 与固定依赖核验，不将上游示例当作本应用运行证据。

## 13. Mayoi：轻量原生编辑器参考

2026-11-02 编辑器主题继续核对固定提交 `cd08688ade5ece3411d9bb812228c7d30492526a`，参考 `internal/highlight/highlight.go` 的 token/颜色分离与特殊文件名识别，以及 `vue.xml` 和 `TestVueDirectiveOverLines` 的指令引号边界。Sasa 保留自己的有界异步高亮流程，保存原始 Chroma token 类型，绘制时使用当前依赖 Chroma v2.27.0 的 `github` / `github-dark` 样式；在 Vue 规则副本中等效收紧指令正则，并另外委派 script/style 的嵌入语言。未复制 Mayoi 高亮器或构建运行 Mayoi。

2026-10-30 为 Diff 代码展示只读核对 Mayoi 提交 `cd08688ade5ece3411d9bb812228c7d30492526a`，本地参考 `/tmp/sasa-mayoi-diff-20261030`。重点核对 `cmd/mayoi/diffrows.go` 的 `diffRow`、`lineRows`、`foldRows`，以及 `diffview.go`、`diffhunks.go`：代码内容、旧/新行号、增删类型与修改块身份分开保存，行内和并排布局各有行映射。Sasa 按该职责分离方式实现自己的 `diff_rows.go`，继续复用自己的补丁快照、token 校验、后台高亮和滚动组件；未直接移植源码，未构建或运行 Mayoi。当前采用完整可核验上下文，缺少源版本的历史采用省略标记，未引入 Mayoi 的折叠展开或逐字符差异能力。

2026-10-24 按用户提供的仓库克隆并只读核对，固定提交 `2650b9bfa90c87ffe0d48fffb140dd2fc428ff1c`，本地参考为 `/tmp/sasa-mayoi-reference-20261024`。Mayoi 使用 Go + MyGo 原生 UI，许可为 MIT（Copyright 2026 chencheng）。本轮未构建或运行 Mayoi，未移植代码。

| 固定源码（相对 Mayoi 仓库） | 核对结果与 Sasa 设计用途 |
|---|---|
| `cmd/mayoi/document.go`、`history.go` | 文档拥有 TextBuffer、保存基线和历史；视图独立保留选择/滚动，供混合标签设计参考 |
| `cmd/mayoi/editor.go` | TextAreaBufferBase、TrackLines、可见区域 TextRanges；供原生编辑器和行号/高亮参考 |
| `internal/highlight/highlight.go` | Chroma 后台 worker、版本与行文本校验、可见区域优先；供异步高亮参考 |
| `internal/workspace/tree.go` | 按目录读取、fsnotify、事件合并和 Close 资源释放；供文件树参考 |
| `internal/git/git.go`、`cmd/mayoi/scm.go` | porcelain v2 NUL 状态、staged/unstaged 区分、源代码管理展示；只采用本轮只读范围 |
| `docs/ARCHITECTURE.md`、`go.mod`、`LICENSE` | 架构说明、实际依赖分叉和 MIT 许可；性能描述属于上游自述 |

Mayoi 当前声明 MyGo v0.6.0，但通过 replace 实际使用 `github.com/sorrycc/mygo v0.4.9-0.20261022125358-4bd079439ee7`；Sasa 使用 `github.com/egoist/mygo v0.6.3-0.20261022014156-976f17c9391f` 及本项目输入补丁。已在 Sasa 本机依赖源码核对 TextBuffer/Snapshot、TextAreaBufferBase、TextRanges、TrackLines 存在；编辑、绘制和中文输入行为兼容性待原生编辑兼容验证，不据此直接替换依赖或复制整个应用。

进一步核对 Mayoi 的 `go.mod` 提交记录：`34c00c4946c33cc388f65e47e4118d9fdc19e83f` 于 2026-10-21 升级官方 MyGo 0.6.0 并删除分叉；`046b499868f4d21d44ac6f00d6e309d8565dbebb` 随后说明为保留 Linux Pango Tab 宽度与旧 Cairo 文字渲染修复而恢复分叉，等待上游 #614 发布。该记录解释当时的依赖选择，不表示未来同步有保证，也不证明 macOS 编辑必须使用分叉。Sasa 文件编辑与 Git 默认保留官方依赖，缺口按实际兼容验证处理。

固定来源：[Mayoi 源码](https://github.com/sorrycc/Mayoi/tree/2650b9bfa90c87ffe0d48fffb140dd2fc428ff1c)。当前文件编辑与 Git 行为见[编辑器子系统](subsystems/editor.md)。

### 2026-10-26 Git 工作流补充核对

本轮刷新本地参考至 `2ba263790582069d766860103ffac45c7142e399`，核对 `cmd/mayoi/` 中的 `scm.go`、`git.go`、`scmpanels.go`、`diffview.go`、`diffhunks.go`、`aicommit.go`、`mergeview.go`。参考其暂存/提交/远程、分页历史、分块差异、冲突源版本与提交说明生成的工作流结构。Sasa 保留自己的工作区、异步派发、文件保存与 Codex 进程归属；Git 操作通过显式参数和临时仓库独立验证，未将 Mayoi 的测试视为 Sasa 验收。当前 Git 工作流见[编辑器子系统](subsystems/editor.md)。

## Mayoi COMMITS 移植（2026-11-18）

固定源码为 Mayoi `6f2190e9297bbdd187ae0d28a80c191b2243d41b`，本地参考 `/tmp/mayoi-branch-review-20261117`。实际构建并运行 Mayoi，核对 Git 侧栏的 COMMITS、全部分支及双端点比较。Sasa 直接移植 `cmd/mayoi/graph.go` 的图布局与对应测试，并适配 `scm.go`、`committree.go`、`coauthors.go`、`commitactions.go`、`internal/git/git.go` 与 `internal/git/commitactions.go` 的引用解析、文件树、悬停统计、菜单、补丁及远端选择逻辑。MIT 原文保留在 `THIRD_PARTY_NOTICES/Mayoi-LICENSE`，适配文件标明来源。

实现沿用 Sasa 的工作区、异步取消、文件保存、Diff 和冲突状态；范围为本地 Git COMMITS，未加入 GitHub PR、CI 状态或远程头像。自动化通过临时 Git 仓库和 MyGo 原生测试器独立核验，不以 Mayoi 自身运行结果替代 Sasa 验收。任务目录完成自动化后仍需主目录集成和公共 Preview 验收。
