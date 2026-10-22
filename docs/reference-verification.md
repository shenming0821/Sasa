# Fuyu 参考项目核验记录

> 核验日期：2026-10-22  
> 用途：补全设计缺口，并为 Codex UI 的逐步实现提供可追溯依据。  
> 设计入口：[Fuyu 设计文档](design.md)

## 1. 证据范围

本次直接读取参考仓库固定提交下的文档、实现和测试源码。GitHub API 发生限流后，改用只读浅克隆获取源码。未执行这些项目的测试，也未把它们的实现直接复制进 Fuyu。

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

落实：Fuyu 将连接、thread、turn、本地交互阶段分别维护。一次执行完成后继续保留会话；失联需要重新核验，不直接写为失败或成功。

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

这些测试使用受控环境和模型响应夹具。本次只读取测试，未执行；不能据此宣称 Fuyu 或当前安装版本已通过真实运行验证。

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

落实：沿用这些交互机制组织 Fuyu UI；Murmur 的模型直连、聊天存储和取消调用不能直接替代 Codex 的原生会话、历史与 turn 中断。

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

落实：Fuyu 可以借鉴职责划分，但协议以实际 Codex 版本为准。尤其不能照搬“输出流结束即成功”的处理来判断任务成功；必须核验 turn 终态。该项目也不能证明 Fuyu 的跨进程退出、恢复和生命周期已经解决。

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
codex app-server generate-json-schema --experimental --out /tmp/fuyu-design-research/installed-schema
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

Fuyu 的具体规则依据本项目的 Codex Runtime、MyGo 原生 UI 和当前开发阶段编写。Magpie 的订阅迁移、社区作者名单、Web GUI 测试、跨平台发布流程和历史缺陷属于其自身项目背景，不作为 Fuyu 的现有机制或经历。

规范入口为根目录 `AGENTS.md`，`CLAUDE.md` 仅引用该入口。当前应用代码尚未建立，构建/测试命令明确标注可用条件；未复制上游实现或新增发布流程。

固定参考：

```text
https://github.com/yetone/magpie/blob/ea6be1592706ea4a0dd6cd526acec0b5d1fb967e/AGENTS.md
https://github.com/yetone/magpie/blob/ea6be1592706ea4a0dd6cd526acec0b5d1fb967e/docs/code-standards.md
https://github.com/yetone/magpie/blob/ea6be1592706ea4a0dd6cd526acec0b5d1fb967e/docs/subsystems/README.md
https://github.com/yetone/magpie/blob/ea6be1592706ea4a0dd6cd526acec0b5d1fb967e/LESSONS.md
```
