# Agent 抽象层

## 职责与入口

`internal/agent/` 保存应用需要的身份、任务、事件、审批意图和可选能力契约。`internal/ui/app.go` 的 `Backend` 是 `agent.Backend` 的兼容别名；UI 与 `internal/app/` 使用 agent 类型。`internal/codex/` 保留协议编解码、原生 RPC 参数、审批响应与子进程读取行为；既有导出类型通过别名兼容原测试和调用者。

当前入口启动 Codex 并注册可用的 Claude Code，连接失败保留对应错误。标签栏行尾由 `internal/ui/agent_picker.go` 提供两个 16 px 图标新建动作，始终在当前目录创建独立会话，原草稿、模型、缓存和任务保留。输入区保留模型入口；连接不可用时显示具体错误并保留当前会话。`internal/ui/agent_icons.go` 复用 Orca 的 `OpenAIIcon`、`ClaudeIcon` SVG 路径，会话页签标题前显示所属 Agent 的 12 px 图标。会话视图的目录工具栏已移除；侧栏折叠时，对应恢复按钮放在标签栏。新建会话记住最近选择，历史按原 Agent 恢复。Claude 的原生协议与验证边界见 [Claude 运行时](claude-runtime.md)。

## 身份、路由和缓存

`AgentKind` 的空值通过 `Normalized()` 解释为 Codex，未知值明确报错。`SessionKey` 由 Agent、Runtime 配置标识和原生 session ID 组成，`ProcessGeneration` 区分同一会话的进程重建。原生 ID 相同的不同 Agent 仍是两个会话。

`internal/ui/agents.go` 的 `RegisterBackend` 在恢复索引和 `Run` 前注册额外连接；连接由创建者管理生命周期，不能重复注册或在窗口运行后替换。`sessionBackend` 按会话 Agent/Runtime 选择已注册连接；默认 Codex 使用已有共享 backend，缺失 Claude 连接返回不可用。后台动作先通过 `forSession` 固定所属会话，注册表在运行期间保持不变。

每条事件的 `Method` 保持字符串，`Params` 保持 `json.RawMessage`；原始 wire 帧仍由 adapter 处理。Codex 现有方法名、参数与原生 turn ID 直接透传，默认事件由接收循环补充 Codex/Runtime 影响域。UI 按 Agent/Runtime/session/generation 筛选；旧测试的无来源事件仅用于默认 Codex。Claude 单进程异常携带 session 和 generation，预期退出不投影为 `fuyu/disconnected`，未知退出阻止所属会话续发；显示控制队列耗尽才升级为 Claude 后端范围故障。

模型目录、加载状态和原生历史目录缓存存放在 session，目录与 Runtime 改变后失效，异步回调核对 Agent/Runtime/cwd 后写回原 session；模型选择的持久化包含 `ModelAgent`。历史目录合并默认 Codex 与已注册 Claude 来源，`ThreadMeta.Agent/Runtime` 在 UI 取得原生列表后补充，不进入原生协议 JSON；各来源的 cursor 与读取完成状态独立保存，按 Agent/Runtime/thread ID 去重。技能候选也由各 session 按 cwd 保存，Claude 原生命令保留名称和参数。`agent.CacheKey` 明确 Agent/Runtime/cwd 的缓存边界，Claude Runtime 为实际生效的规范化配置根。

从历史列表创建会话时，按该行的 Agent/Runtime 定位后端，不继承新会话默认 Agent 的模型或连接错误。仅在来源缓存键与目标 Agent/Runtime/cwd 完全一致时复制已有列表、各来源分页状态和错误状态；列表与来源状态使用独立容器，进行中的请求仍归原会话。缓存键不同时重新加载双来源目录；选择已存在的会话时也核对并加载目标列表，已有完整缓存由加载入口直接复用。受控回归见 `TestOpenHistoryCarriesOnlyMatchingCache`、`TestOpenHistoryLoadsTargetWhenCacheDirectoryChanges`、`TestHistoryOpensTheRowAgentInsteadOfDefault` 和 `TestResponsivePanelsPreserveComposerAndNavigation`。

## 能力与历史核验

`backend.go` 的最小 Backend 只有 StartThread、Send、Interrupt、Events。模型/技能/命令目录、图片模型发送、原生历史、占用元数据、分叉、缺口核验、原生响应与通用审批意图分别使用可选接口；实现可以返回 `ErrUnsupported`，调用者通过能力断言限制入口。`Capabilities` 为能力描述，不替代实际接口与真实验收。

`NotSubmittedError` 只用于后端能够证明用户输入尚未尝试写入时的失败。UI 在初始化或读取默认模型期间可取消准备，恢复草稿并保留会话可用性；已经尝试写入的失败继续按结果不确定处理。取消与成功返回竞争时，UI 取得原生 turn ID 后继续请求原生中断，等待实际终态，不把取消上下文等同于任务停止。退出流程同样覆盖尚无 turn ID 的准备请求。

历史展示、可续发、旧任务终态、写入权和 pending 认知分别建模。`RecoveryResult` 明确保留这些证据及不确定性；Codex `RespondHistory` 复用 resume + 分页读取，并检查页内终态，不因能展示文字就允许续发。它不是跨所有历史页的审计，UI 现有恢复仍保留分页、占用和旧提交门控。

`Thread`、`HistoryTurn` 等兼容结构是应用投影，不要求 Claude 实现同名 RPC。`Interaction`、`DecodeInteraction` 和 `RPCError` 保留既有 Codex 投影约定用于无行为变化的过渡；Claude 必须使用独立 wire 解码及审批映射，不能把其原生枚举做别名。slash commands 保留命令名称、参数、来源和 cwd，不映射为 Codex skill 路径。

## Turn 与审批

`TurnMap.Begin` 在发送前生成应用 TurnID 及独立 user UUID，每 session 仅允许一个在途发送；即使进程重建，未核验旧任务仍阻止重复发送。`Bind` 分开关联 native turn、transcript UUID 和模型 message.id；`Lookup` 供归属核验使用，`Complete` 以 generation + 应用 ID 保护重复和迟到终态。完成后下一次 Begin 替换当前映射，不将内存表当作持久历史。Codex 保持原生 ID 路径；Claude 已接入 Begin/Bind/Complete，并独立持久化发送和终态检查点。

`ApprovalIntent` 只表达本次 allow/deny、可选反馈和停止。`RuleSuggestion` 保留原始对象，只有精确建议已经验证且 destination=session 时才允许附带，拒绝不能扩大权限；`setMode` 不作为审批规则。`PermissionModeChange` 是独立设置类型，永久规则首版不开放。Claude 使用可选 Respond 接口映射单次审批/反馈/问答，应用请求身份编码 session/generation/native request ID；缺工具 ID 仍需审批。pending 去重/撤销、迟到响应及写入不确定保护均由 Claude adapter 实现；未开放 session 规则。

## 资源与兼容

`agent.RequestTable` 提供带缓冲、非阻塞、按 ID 关联的响应表；Codex Call 超时或结束时移除等待者，reader 路由响应不等待 UI。原有关闭、写入截止时间、事件队列策略不变。协议帧 16 MiB、事件队列 4 MiB/4096 条、交互 256 KiB/16 条的限额统一由 agent 常量提供；显示投影与裁剪也已移入 agent，Codex 保留包装函数。

子进程及进程组仍由 `internal/runtime/` 管理；未增加全局进程清理或修改 Codex 常驻拓扑。SQLite v4 的身份及迁移约定见[存储与恢复](storage.md)，旧程序利用版本拒绝机制防止将 Claude 会话误读为 Codex。

## 验证入口

新增测试：`internal/agent/agent_test.go`、`internal/app/agent_test.go`、`internal/codex/agent_test.go`、`internal/storage/agent_test.go`、`internal/ui/agents_test.go`、`internal/claude/fixture_test.go`。原 Codex/app/UI 发送、审批、停止、恢复、分叉、多会话测试保留，用于 Mac 等价回归。

真实基础证据见[夹具说明](../../internal/claude/testdata/README.md)，实现与验证边界见 [P6 验证](../verification/2026-10-28-p6-agent.md)。参考项目和静态检查均不能替代 Fuyu 的 Go 测试、真实 Runtime 与原生窗口验收。
