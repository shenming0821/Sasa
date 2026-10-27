# Agent 抽象层

## 职责与入口

`internal/agent/` 保存应用需要的身份、任务、事件、审批意图和可选能力契约。`internal/ui/app.go` 的 `Backend` 是 `agent.Backend` 的兼容别名；UI 与 `internal/app/` 使用 agent 类型。`internal/codex/` 保留协议编解码、原生 RPC 参数、审批响应与子进程读取行为；既有导出类型通过别名兼容原测试和调用者。

当前生产入口仍只启动 Codex。`internal/claude/` 当前只有真实协议证据及样本断言，Claude adapter 和新建会话 Agent 选择入口属于 P6.2/P6.3 待办。Linux 编写，Mac 待验证，不能将类型迁移视为 Codex 等价回归已通过。

## 身份、路由和缓存

`AgentKind` 的空值通过 `Normalized()` 解释为 Codex，未知值明确报错。`SessionKey` 由 Agent、Runtime 配置标识和原生 session ID 组成，`ProcessGeneration` 区分同一会话的进程重建。原生 ID 相同的不同 Agent 仍是两个会话。

`internal/ui/agents.go` 的 `RegisterBackend` 在恢复索引和 `Run` 前注册额外连接；连接由创建者管理生命周期，不能重复注册或在窗口运行后替换。`sessionBackend` 按会话 Agent/Runtime 选择已注册连接；默认 Codex 使用已有共享 backend，缺失 Claude 连接返回不可用。后台动作先通过 `forSession` 固定所属会话，注册表在运行期间保持不变。

每条事件的 `Method` 保持字符串，`Params` 保持 `json.RawMessage`；原始 wire 帧仍由 adapter 处理。Codex 现有方法名、参数与原生 turn ID 直接透传，默认事件由接收循环补充 Codex/Runtime 影响域。UI 按 Agent/Runtime/session/generation 筛选；旧测试的无来源事件仅用于默认 Codex。会话进程异常须携带 session 和 generation，不能让 Claude 单进程 EOF 变成其他会话的全局失联。预期退出不投影为 `fuyu/disconnected`；Claude 的退出分类尚待 adapter 实现。

模型目录、加载状态和原生历史目录缓存存放在 session，目录与 Runtime 改变后失效，异步回调写回原 session；模型选择的持久化包含 `ModelAgent`。技能候选也由各 session 按 cwd 保存。`agent.CacheKey` 明确 Agent/Runtime/cwd 的缓存边界，Runtime 标识必须来自实际生效配置，P6.3 接线时补验。

从历史列表创建会话时，仅在来源缓存键与目标 Agent/Runtime/cwd 完全一致时复制已有列表、分页游标和错误状态；列表使用独立切片，进行中的请求仍归原会话。不同缓存键不复用。受控回归见 `TestOpenHistoryCarriesOnlyMatchingCache` 和 `TestResponsivePanelsPreserveComposerAndNavigation`。

## 能力与历史核验

`backend.go` 的最小 Backend 只有 StartThread、Send、Interrupt、Events。模型/技能/命令目录、图片模型发送、原生历史、占用元数据、分叉、缺口核验、原生响应与通用审批意图分别使用可选接口；实现可以返回 `ErrUnsupported`，调用者通过能力断言限制入口。`Capabilities` 为能力描述，不替代实际接口与真实验收。

历史展示、可续发、旧任务终态、写入权和 pending 认知分别建模。`RecoveryResult` 明确保留这些证据及不确定性；Codex `RespondHistory` 复用 resume + 分页读取，并检查页内终态，不因能展示文字就允许续发。它不是跨所有历史页的审计，UI 现有恢复仍保留分页、占用和旧提交门控。

`Thread`、`HistoryTurn` 等兼容结构是应用投影，不要求 Claude 实现同名 RPC。`Interaction`、`DecodeInteraction` 和 `RPCError` 保留既有 Codex 投影约定用于无行为变化的过渡；Claude 必须使用独立 wire 解码及审批映射，不能把其原生枚举做别名。slash commands 保留命令名称、参数、来源和 cwd，不映射为 Codex skill 路径。

## Turn 与审批

`TurnMap.Begin` 在发送前生成应用 TurnID 及独立 user UUID，每 session 仅允许一个在途发送；即使进程重建，未核验旧任务仍阻止重复发送。`Bind` 分开关联 native turn、transcript UUID 和模型 message.id；`Lookup` 供停止/归属核验使用，`Complete` 以 generation + 应用 ID 保护重复和迟到终态。完成后下一次 Begin 替换当前映射，不将内存表当作持久历史。Codex 保持原生 ID 路径；此工具在 P6.2 接入 Claude。

`ApprovalIntent` 只表达本次 allow/deny、可选反馈和停止。`RuleSuggestion` 保留原始对象，只有精确建议已经验证且 destination=session 时才允许附带，拒绝不能扩大权限；`setMode` 不作为审批规则。`PermissionModeChange` 是独立设置类型，永久规则首版不开放。请求身份含 session/generation/request ID；缺工具 ID 不构成自动允许的理由。pending 去重/撤销与写入不确定保护由各 adapter 实现，Claude 该部分尚未实现。

## 资源与兼容

`agent.RequestTable` 提供带缓冲、非阻塞、按 ID 关联的响应表；Codex Call 超时或结束时移除等待者，reader 路由响应不等待 UI。原有关闭、写入截止时间、事件队列策略不变。协议帧 16 MiB、事件队列 4 MiB/4096 条、交互 256 KiB/16 条的限额统一由 agent 常量提供；显示投影与裁剪也已移入 agent，Codex 保留包装函数。

子进程及进程组仍由 `internal/runtime/` 管理；未增加全局进程清理或修改 Codex 常驻拓扑。SQLite v4 的身份及迁移约定见[存储与恢复](storage.md)，旧程序利用版本拒绝机制防止将 Claude 会话误读为 Codex。

## 验证入口

新增测试：`internal/agent/agent_test.go`、`internal/app/agent_test.go`、`internal/codex/agent_test.go`、`internal/storage/agent_test.go`、`internal/ui/agents_test.go`、`internal/claude/fixture_test.go`。原 Codex/app/UI 发送、审批、停止、恢复、分叉、多会话测试保留，用于 Mac 等价回归。

真实基础证据见[夹具说明](../../internal/claude/testdata/README.md)，实现与验证边界见 [P6 验证](../verification/2026-10-28-p6-agent.md)。参考项目和静态检查均不能替代 Fuyu 的 Go 测试、真实 Runtime 与原生窗口验收。
