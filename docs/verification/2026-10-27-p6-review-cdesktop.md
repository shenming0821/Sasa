# P6 接入方案复核：以 cdesktop Claude executor 为依据

复核日期：2026-10-27。结论：**Agent 抽象、原生历史归属、夹具先行的方向成立，但现方案不能直接作为实现契约。必须先修正授权范围、turn 标识、初始化字段来源和 JSONL 恢复算法；进程常驻与 schema 不升版都应改为有验收条件的选择。**

本次只读 Fuyu 和参考源码，未启动 Agent、未进行真实 Claude 验收，也未修改 Fuyu 文件。报告中的“建议”均为待实施项。

证据基线：

- Fuyu：`/home/ming/Documents/AI/Fuyu`，`main@4d9784b8ebfc0ebc3810c8b4618303e8e3116291`；开始核对时工作区、暂存区均为空。
- cdesktop：`/tmp/ref-cdesktop`，`75bd015e88f9797d785c9fabcfca03c7151fd5cc`，参考工作区为空；本报告的“当前实现”均指此本地快照，不声称核验了远端最新 HEAD。
- SDK 类型：`/tmp/p6-source-typescript.d.ts`，SHA-256 `64a59ddbcae4dc05da268dd3e009431c67e91a23616bce38f122b9247650271f`。原方案标为 SDK 0.3.311，本次依据文件内容核对，未独立核验 npm 包版本与安装包运行代码。
- 原方案：`/tmp/p6-agent-analysis.md`，SHA-256 `57ed945f6673849b28fab64a418726d68004e2f27d50edb2a993cf08324f1ecb`。用户称“昨天”，但所给文件署名为 **2026-10-27**；本次以此文件版本为准。
- 仓库实施计划实际仍处于 P4 验收，P6.0～P6.4 是本次评审的拟议阶段，不是已批准开始的实现任务。依据：`docs/implementation-plan.md:7`。

引用约定：`C/` = `/tmp/ref-cdesktop/crates/`，`F/` = `/home/ming/Documents/AI/Fuyu/`，`T:` = `/tmp/p6-source-typescript.d.ts:`；冒号后均为源码行号。

## 1. cdesktop Claude executor 架构摘要

### 1.1 进程模型：每次执行 spawn，后续轮 resume

调用链是 `ClaudeCode::spawn` → 构造命令 → `spawn_internal`。后续消息走 `spawn_follow_up`，加入 `--resume <session_id>` 后再次调用 `spawn_internal`；需要回退时额外加入 `--resume-session-at <message_uuid>`。所以 **stream-json 是双向传输格式，不意味着该应用复用常驻 Claude 进程**。依据：`C/executors/src/executors/claude.rs:379`、`:391`、`:660`。

启动使用 `-p --verbose --output-format=stream-json --input-format=stream-json --include-partial-messages --replay-user-messages`，stdin/stdout/stderr 都为 pipe，指定 cwd，建立进程组并设置 `kill_on_drop(true)`。正文由 stdin 的 user 帧发送，而非拼入命令参数。依据：`C/executors/src/executors/claude.rs:160`、`:197`、`:693`；`C/executors/src/executors/claude/protocol.rs:198`。

协议 reader 遇到首个 `result` 就退出，不继续读下一轮。外层发送任务也只提交一次用户消息；从对象所有权看，reader 结束后 peer/stdin 被释放，随后由进程管理回收执行进程。这支持“每轮执行”的判断，但**不应把 reader 收到 result 等同于已经确认 OS 进程退出**。依据：`C/executors/src/executors/claude/protocol.rs:49`、`:90`；`C/executors/src/executors/claude.rs:735`、`:760`。

### 1.2 帧处理：原始日志与显示投影分离

`ProtocolPeer` 以 `BufReader.read_line` 读取 JSON 行，先将每帧写入日志，再解析 `CLIMessage`。控制请求走审批/hook 回调，`result` 结束本次读取；`control_response` 和 `control_cancel_request` 虽有类型声明，但此 reader 没有对应处理分支。stdin 写入由 mutex 串行化，写完整 JSON 后追加换行并 flush。依据：`C/executors/src/executors/claude/protocol.rs:55`、`:79`、`:82`、`:189`；`C/executors/src/executors/claude/types.rs:12`。

显示侧通过独立 `ClaudeLogProcessor` 消费 `MsgStore.history_plus_stream()`，拼接跨 chunk 的完整行，转换为统一 `NormalizedEntry`/JSON Patch。它维护 streaming message、content block 和 tool ID 映射，以便用最终消息替换流式显示；`user.message.content` 内的 `tool_result` 按 `tool_use_id` 更新工具状态。依据：`C/executors/src/executors/claude.rs:415`、`:825`、`:1459`、`:1594`、`:1783`。

恢复相关元数据也从帧中提取：session ID 不采用 system/init，而从 assistant/user/result 等帧取得；assistant 的 transcript UUID 暂存，到 result 才提交为恢复检查点，避免选到尚有未完成工具调用的位置。依据：`C/executors/src/executors/claude.rs:876`、`:883`、`:954`。

### 1.3 审批桥接：hook → can_use_tool → 统一审批服务

启用 approvals/plan 时，cdesktop 用 `--permission-prompt-tool=stdio`，启动参数先给 bypass 模式，然后发送控制请求设置实际模式。`initialize` 注册 `PreToolUse` hooks：部分只读工具自动允许，其他操作返回 hook `permissionDecision:"ask"`，由 CLI 再发 `can_use_tool`。这是该产品为统一审批定制的权限组合，不能直接照搬为 Fuyu 的默认安全策略。依据：`C/executors/src/executors/claude.rs:169`、`:208`、`:230`、`:747`；`C/executors/src/executors/claude/client.rs:323`。

`can_use_tool` 使用 `tool_use_id` 创建审批，写入 `ApprovalRequested`，等待共享 `ExecutorApprovalService`，再记录结果并以原协议 `request_id` 回 `control_response`：允许返回 `updatedInput`；拒绝返回原因及 `interrupt`；超时为 deny + interrupt=true。`ExitPlanMode` 的允许响应还返回 `updatedPermissions`，把 session 模式设为 bypass。依据：`C/executors/src/executors/claude/client.rs:60`、`:101`、`:109`；`C/executors/src/executors/claude/protocol.rs:114`、`:168`。

`AskUserQuestion` 单独进入问答服务，回答写回 `updatedInput.answers`，并非普通 allow/deny 卡片，也不是依赖新的 `request_user_dialog`。依据：`C/executors/src/executors/claude/client.rs:145`、`:203`、`:279`。

### 1.4 中断：取消 token 触发协议 interrupt，外层限时回收

reader 的取消分支只发送一次 interrupt，随后继续等 result。审批服务也收到相同取消 token，使等待用户的操作有机会退出；外层停止流程等待最多 5 秒，再清理进程组。依据：`C/executors/src/executors/claude/protocol.rs:59`；`C/executors/src/executors/claude/client.rs:89`；`C/local-deployment/src/container.rs:1466`。

需要保留限制：reader 在控制请求分支中同步 await 用户审批，期间不能继续读取其他帧或进入顶层取消分支；取消能否及时前进依赖审批等待响应 token。它没有实现 interrupt 回执关联、still_queued 检查或 control_cancel_request 撤销处理。依据：`C/executors/src/executors/claude/protocol.rs:82`、`:121`；`C/executors/src/executors/claude/types.rs:12`。这属于静态可见的实现局限，不是本次复现出的运行故障。

### 1.5 恢复：Runtime 续接和 UI 历史是两条链

- Runtime：新进程 `--resume`；可附 `--resume-session-at`。源码明确说明后者截断会话历史到指定消息，因此它不能等同于“创建独立分叉”。依据：`C/executors/src/executors/claude.rs:401`。
- UI 历史：cdesktop 保存自己的执行 stdout/stderr 日志，离线加载日志后重建 `MsgStore` 并重新 normalize；**该实现不是直接读取 `~/.claude/projects/...` 来展示历史**。依据：`C/services/src/services/execution_process.rs:195`、`:282`；`C/services/src/services/container.rs:852`、`:885`、`:901`。
- 重放过滤：normalizer 忽略 `isReplay` user 帧，避免恢复上下文被再次显示。依据：`C/executors/src/executors/claude.rs:1525`。

### 1.6 与 Codex executor 的统一方式

两种 Agent 共享 executor trait、执行子进程结果、审批服务、模型/权限选项结构和规范化日志；统一的是应用能力和展示结果，协议仍各自实现。依据：`C/executors/src/executors/mod.rs:228`、`:329`；`C/executors/src/model_selector.rs:15`、`:53`、`:72`。

Codex 每次执行也启动自己的 App Server；后续执行在本快照里实际用 `thread/fork`，不是保持原 thread 的 `thread/resume`。Claude 则 `--resume` 保持 session，二者原生 ID 连续性并不一致。依据：`C/executors/src/executors/codex.rs:246`、`:592`、`:610`、`:639`。Fuyu 目前“一个自有常驻 App Server 服务多个 thread”的行为不同，无需为模仿参考项目而改掉。

模型目录在本快照的两个 executor 内都是静态基线：Claude 的 `DEFAULT_MODEL_IDS`，Codex 的 `discover_options` 列表；Claude 动态发现的是命令、插件、agents。不能引用 cdesktop 证明“它已经用 initialize 动态读取模型”。另有独立 provider HTTP 模型拉取服务，属于不同路径。依据：`C/executors/src/executors/claude.rs:277`、`:436`、`:540`；`C/executors/src/executors/codex.rs:321`；`C/services/src/services/model_fetch.rs:47`。

审批同样只统一用户意图：Codex 把 Approved 映射为 Accept，带原因的 Denied 可映射为 Cancel 并另排反馈；Claude 把原因直接放入 deny.message。不能把各协议的枚举或 scope 直接做类型别名。依据：`C/executors/src/executors/codex/client.rs:678`、`:705`；`C/executors/src/executors/claude/client.rs:130`。

## 2. 对原方案六项设计决策的逐条复核

| 决策 | 判断 | 需要的修订 |
|---|---|---|
| turn 合成 | 方向成立，标识选择错误 | Send 前分配应用 turn ID；不能等待 result 或使用 assistant message.id |
| 审批 scope 降级 | 必须重写 | 单次授权、session 规则、模式切换分别建模；删除“本会话允许＝切换权限模式” |
| JSONL 原生历史 | 归属成立，读取算法不足 | 重建 transcript 会话链；不能按物理行倒序直接拼消息 |
| streaming 常驻 | 可选，参考实现不能背书 | 先通过多轮/中断/尾随帧验收，再决定常驻还是每轮重建 |
| schema 不升版 | 仅增加 payload 字段时成立 | Runtime 身份与 thread 唯一约束必须一并决策 |
| 夹具先行 | 保留并扩大 | 成功路径之外加入乱序、撤销、损坏、超量和版本降级 |

### 2.1 turn 合成：保留，但必须在发送前拥有稳定 ID

原方案的 `result.message.id` 不符合给定类型：result 顶层是 `uuid`，并无 `message.id`；assistant 的 `message.id` 则属于模型消息，可能跨多个已完成 content block 重复出现。依据：`T:3694`、`T:6008`、`T:6108`。

Fuyu 的 Send 立即返回 Turn，状态机又以 Turn.ID 拒绝不匹配的完成事件，不能等最后一帧才决定 ID。依据：`F/internal/ui/app.go:24`；`F/internal/app/state.go:250`。建议发送前生成唯一应用 turn ID 和 user UUID，建立 `(Agent, SessionID, ProcessGeneration, TurnID)` 关联；有能力时用 `user_message_uuid(s)` 校验 result 归属，旧版通过“每 session 同时只允许一个在途发送”关联。依据：`T:5976`、`T:5980`。

分开保存应用 TurnID、原生 session ID、transcript UUID、模型 message.id、tool_use_id 和 request_id；应用 turn 完成只投影一次。cdesktop 的“assistant UUID 等 result 再成为检查点”值得采用，但检查点不是 TurnID。依据：`C/executors/src/executors/claude.rs:883`。

还要保留三件事：result.subtype、is_error、terminal_reason；`subtype:"success"` 不能单独作为成功条件。SDK 明确允许 success 携带 API 错误，result 后也可能继续出现通知。依据：`T:6003`、`T:5990`。中断请求成功、收到中断回执、收到本轮终态、OS 进程退出、后台工具退出分别记账。

### 2.2 审批 scope：删除整会话模式切换的伪等价

“Claude 只有单次 allow/deny + permission mode”是事实错误。cdesktop 类型已支持 `updatedPermissions` 与 `destination:session`，并实际用于 ExitPlanMode；给定 SDK 还支持 addRules/replaceRules/removeRules、目录增删，以及 user/project/local/session/cliArg 等 destination。依据：`C/executors/src/executors/claude/types.rs:89`、`:117`、`:137`；`C/executors/src/executors/claude/client.rs:111`；`T:2598`。

建议 P6 首版默认只展示已验证的“本次允许 / 拒绝”；若真实请求提供可验证的 permission_suggestions，再提供“在本会话允许此规则”，并展示工具、命令或目录范围。保持原始规则结构，不从自然语言说明猜规则。缺少精确映射的 Codex turn scope 直接不展示；永久授权首版暂缓。

**删除原建议按钮“本次允许并切换会话权限模式”。** 用户对单个动作的批准不构成放开整个 session 的授权。权限模式如需提供，应是独立设置，有自己的实际生效反馈。

另有进程模型关联：`destination:session` 的内存权限能否跨新进程 `--resume` 保留，本次源码不能证明。采用每轮 spawn 时必须真实验证；若不保留，应用可显式维护会话规则并在下一轮重新应用，或者不承诺跨轮授权。不能因 destination 名字叫 session 就推断其持久化语义。

### 2.3 JSONL 历史：保留原生权威，但不是“倒序读行”

cdesktop 自己保存日志的做法不能替 Fuyu 的原生 transcript reader 提供正确性保证。SDK `getSessionMessages` 的说明明确要求沿 **parentUuid 重建 conversation chain**，还支持纳入 system 消息；compaction 包含 preserved_segment/preserved_messages 的重连信息。依据：`T:909`、`T:925`、`T:3844`。

建议实现独立 transcript reader：先识别记录类型和有效会话分支，再做消息/工具投影，最后按完整消息或应用 turn 分页。文件倒序扫描最多作为查找记录的优化，不能替代分支、去重、compaction 和工具配对。工具结果可能在另一条 user 记录中，分页也不能把 tool_result 当成人类输入。依据：`C/executors/src/executors/claude.rs:1594`。

历史展示、可继续执行、活动任务终态是不同证据等级。transcript 能显示文字，不代表旧进程已退出、未决审批不存在或获得写入权；给定类型明确说 pending_permission_requests 即使为空，也不穷尽从旧 worker 继承的审批。依据：`T:433`。

首版恢复限定为 Fuyu 索引内的已知 session；全盘发现外部 session、外部 writer 接管、任意 transcript 分叉另列能力，未验证前保留只读或禁止续发。若考虑用 SDK 的 getSessionMessages 做辅助进程，需另评 Node 依赖，不能把它写成纯 Go 已具备的 CLI RPC。

### 2.4 进程模型：将“建议常驻”改为阶段性决策

给定 SDK 明确支持 streaming-input 多轮 result；cdesktop 却在首个 result 后停止读取。这说明两种策略均有实现空间，不能从 stream-json 参数推出必须常驻。依据：`T:6003`、`T:6093`；`C/executors/src/executors/claude/protocol.rs:90`。

建议 P6.0 同时验证 A：每轮 spawn + resume；B：每会话常驻并连续发送至少两轮。**P6.2 的保守基线可先取 A，B 在多轮、中断、审批撤销和空闲退出全部通过后启用。**这是缩小首版状态复杂度的工程建议，不是 SDK 要求。若 A 不能达到会话授权、后台任务或延迟目标，验收后选 B，不同时维护两套未经验证的生产路径。

采用 B 时 reader 不能遇 result 就 break，还要继续接收任务通知、后续 init、控制响应及后台工具状态。采用 A 时 result 后也应有有界排空/关闭流程，避免立即关闭 stdout 遗失尾随通知。依据：`T:6003`。

必须新增“会话进程正常结束”与“连接异常”的区分。Fuyu 当前 `fuyu/disconnected` 会令状态失联并拒绝后续事件，不能把 Claude 每轮预期 EOF 直接送进这条分支；一个 Claude 进程失败也不能使其他会话断线。依据：`F/internal/app/state.go:74`、`:83`。

### 2.5 schema：payload 可扩展，不代表存储整体无需迁移

原方案关于 Session 可加 Agent 字段的判断成立，但存储描述需更精确：每个 Session 序列化到 `sessions.payload`；Snapshot 其他字段写 settings，保存时 `snapshot.Sessions=nil`。旧字段缺失默认 Codex 可沿用。依据：`F/internal/storage/store.go:20`、`:301`、`:316`。

尚未覆盖两个真实约束：

1. `runtime_identity` 为 `id=1` 单行、`codex_home NOT NULL`，`BindRuntime` 只绑定一个 Codex 根目录；不能直接“新插一行 claude_home”。依据：`F/internal/storage/store.go:149`、`:237`。
2. `sessions.thread_id TEXT UNIQUE` 不含 Agent。加入 Agent JSON 字段不会改变这个跨 Agent 唯一性约束。依据：`F/internal/storage/store.go:131`、`:310`。

建议 **P6.3 使用 v4**：增加 agent 归属，原生非空 ID 唯一性改为 `(agent, native_session_id)`，Runtime 绑定支持按 agent 记录规范化历史根目录；旧记录全部迁移为 codex，保留原有 home 校验。不是为“多一个字段”升版，而是维护运行时归属和索引契约。

若坚持 v3，也存在可行替代：payload 增 Agent；数据库 thread_id 只存带 agent 命名空间的索引键；Claude home 绑定放 settings JSON，保留原生 ID 单独字段。但必须明确编码/解码、旧数据兼容与旧程序误读风险。当前旧程序会忽略未知 JSON 字段，可能将 Claude 会话当 Codex 打开；v4 可利用已有高版本拒绝机制阻止误读。依据：`F/internal/storage/store.go:122`、`:277`。因此“不升版”只能是完整设计后的选择，不能预先定论。

### 2.6 夹具先行：保留，增加来源与负路径

保留 P6.0，但真实录制与合成异常输入分别标来源，记录 CLI 版本、可执行路径、SDK 类型摘要、cwd、权限模式和 capability。SDK 包版本不等于本机 CLI 版本，原方案的“2.1.268+”只能作为相关字段的已知版本说明，不能证明全部功能都在该版本可用。依据：`T:433`、`T:6221`、`T:6280`。

夹具应记录双向帧，验证输出响应、状态转换和资源回收，不只断言解析成功。真实 transcript 另录，不用 stdout JSON 伪装 transcript。cdesktop 自带测试只能说明其投影意图，不算 Fuyu 已通过验收。

## 3. 新发现的坑与参考实现自身的限制

### 3.1 初始化：请求写出不等于握手成功

cdesktop 的 initialize/set_permission_mode 只发送 JSON；启动任务随后直接发用户输入，reader 没有关联控制响应。Fuyu 应有 request_id → pending 表、超时、错误响应处理，并在权限设置确认后才提交有副作用的任务。依据：`C/executors/src/executors/claude/protocol.rs:203`；`C/executors/src/executors/claude.rs:747`。可借鉴其 Codex peer 已有的 register/resolve/request 机制，而非照抄 Claude peer 的省略。依据：`C/executors/src/executors/codex/jsonrpc.rs:180`、`:209`。

原方案另有明确字段错误：**models/account 在 initialize 控制响应中，system/init 是当前 model、tools、commands、版本与 capabilities 等会话元数据。**依据：`T:4768`、`T:6211`。不要在首次用户任务才出现的 init 上阻塞发送前模型菜单。

### 3.2 审批等待不能堵住 reader；撤销与重复必须处理

cdesktop reader 内 await 审批会暂停后续帧读取，且忽略 control_cancel_request。Fuyu 应把用户等待放到独立任务，reader 持续路由响应、撤销、终态和中断；只在写 stdin 时串行化。审批表按 `(process generation, request_id)` 去重，pending 回放和 live 帧只显示一次，撤销后迟到点击不能回响应。依据：`C/executors/src/executors/claude/protocol.rs:82`、`:121`；`T:435`、`T:437`。

cdesktop 对非 AskUserQuestion 且缺 tool_use_id 的请求甚至直接 allow，permission_suggestions 被命名为未使用参数，blocked_paths 被忽略。Fuyu 应按 request_id 仍可审批；缺少工具关联只降低展示能力，不能自动扩大授权。依据：`C/executors/src/executors/claude/client.rs:279`、`:309`；`C/executors/src/executors/claude/protocol.rs:114`。

### 3.3 AskUserQuestion 与新 dialog 不是同一个协议

应先实现 cdesktop 已展示的 AskUserQuestion → updatedInput.answers 路径，并保留原始问题文本及答案映射。`request_user_dialog` 是另一个可扩展 dialog_kind 通道，需要在 initialize 声明自己能呈现的 supportedDialogKinds；遇到未声明 kind 不能套用“所有未知请求都回错误/取消”，SDK 说明该错误会被忽略，而取消是实际的用户决策。依据：`C/executors/src/executors/claude/client.rs:203`；`T:5264`。

P6 首版可不声明任何未实现 dialog kind；若实际 CLI 发来意外 dialog，显示不支持并保留停止入口。初始化必须据实际能力协商，不把“2.1.268+”等同于 UI 已支持所有提问。

### 3.4 Slash commands 不是 Codex Skill 的别名

cdesktop 动态探测用单独进程运行 `-p --verbose --output-format=stream-json --max-turns 1 -- /`，读取 init 的 slash_commands/plugins/agents，120 秒超时并终止探测；再合并内置命令和本地描述。依据：`C/executors/src/executors/claude/slash_commands.rs:194`、`:245`、`:268`、`:301`。

它还扫描项目/全局 `.claude/commands`、`skills/*/SKILL.md` 及插件命名空间。该扫描只是简化的描述补充器，不是完整配置解析器。Fuyu 不能把 Claude 命令变成 Codex 的 `type=skill,path=...` 输入；应保留命令名/参数、命令来源、工作目录与可见能力。依据：`C/executors/src/executors/claude/slash_commands.rs:84`、`:116`；`F/internal/ui/models.go:13`。

处理 `/compact`、`/context` 等命令时，既有 `<local-command-stdout>` 包装，也有 SDK 独立 local command output 消息类型。终端专属命令应按 terminal_slash_commands 过滤或另行支持。依据：`C/executors/src/executors/claude.rs:1582`；`T:5587`、`T:6238`。程序拼装 prompt 时不能盲目开 client_composed：类型说明它还会跳过部分 turn-start 上下文附加，而不仅关闭 slash 解析。依据：`T:1917`。

### 3.5 Onboarding 与登录态：参考项目有补丁，不代表应该自动修改

cdesktop 启动前会把 `~/.claude.json.hasCompletedOnboarding` 写为 true，源码解释第三方 provider 可能卡在交互 onboarding；该操作不代表完成认证。它的 availability 也只是检查 `.claude.json` 修改时间。依据：`C/executors/src/executors/claude/onboarding.rs:1`、`:17`、`:55`；`C/executors/src/executors/claude.rs:641`。

Fuyu 建议只读检查“可执行程序存在 / 握手成功 / 账号或 provider 已配置 / 实际调用可用”，分开呈现。卡 onboarding、认证失败、网络失败分别报错，提供用户在终端完成设置的动作；不自动改用户配置。Codex 对照路径会检查 get_account，Claude 也应有自己的认证判据，不能共用“文件存在＝登录成功”。依据：`C/executors/src/executors/codex.rs:598`；`T:4776`。

### 3.6 环境：配置覆盖顺序、凭据来源和 Runtime 根目录

cdesktop 支持 runtime env → profile env → provider env 的覆盖，Claude 最后还可移除 ANTHROPIC_API_KEY；多仓库附加目录显式以 --add-dir 传入。依据：`C/executors/src/env.rs:163`、`:175`；`C/executors/src/executors/claude.rs:672`、`:700`、`:707`。

Fuyu 应复用可执行路径/Node/PATH 探测，但按子进程构造环境，不把 Claude 的配置写进全局进程环境。核验 CLAUDE_CONFIG_DIR、API key/provider 设置、cwd/符号链接/worktree 与附加目录；绑定实际规范化历史根，别只绑定固定 ~/.claude。CLAUDE_CONFIG_DIR 会影响本地 transcript 写入，类型已有说明。依据：`T:6856`。

嵌套启动相关 CLAUDECODE 等变量应列入 P6.0 实测项目；本次在所查 executor/env 路径未发现专门过滤，不能把清除哪些变量写成已核验规则。报告也未读取用户当前凭据值。

### 3.7 大输出与 JSONL 边界：参考实现没有替我们兜底

cdesktop protocol 的 read_line 和 normalizer 的 buffer 没有本地明确字节上限，normalize 还会收集完整行；不能据此宣称已解决超长工具输出。Fuyu 当前 Codex 路径已有单帧 16 MiB、事件队列 4 MiB/4096 条的限制，应抽出同等级预算并按 Claude 实际帧验证。依据：`C/executors/src/executors/claude/protocol.rs:55`；`C/executors/src/executors/claude.rs:852`；`F/internal/codex/client.go:171`、`:238`。

区分 wire 限制、原始输入解析、展示裁剪和总队列预算；不能先把原始帧字符串截断再 JSON 解码。生命周期/审批/控制响应不可作为普通文字丢弃；缺口后的核验走 Claude transcript 能力，不能调用 Codex thread/read。

补测分片换行、CRLF、UTF-8 跨 chunk、无尾换行、半写尾行、坏行、合法未知帧、文件截断/增长/替换、重复 UUID、超大 tool_result 和图片 base64。cdesktop 对 normalizer 最后残余 buffer 直接当 system 文字，不能作为 Fuyu 历史正确性策略。依据：`C/executors/src/executors/claude.rs:933`。

### 3.8 子代理、压缩和用量：不可混进主 turn 的简单文本流

cdesktop 处理 task_started/task_progress 等事件，并在 stream_event 的 usage 中排除 parent_tool_use_id 非空的子代理，但其 assistant 结构未保存 parent_tool_use_id，streaming_message_id 也只有一个当前指针。它对 compact_boundary 则直接忽略。说明它有部分兼容，不代表完整支持并发嵌套消息及压缩历史。依据：`C/executors/src/executors/claude.rs:1341`、`:1342`、`:1788`、`:1837`、`:2347`。

Fuyu 应在投影键中保留父工具/agent 关联，至少把子代理工具挂到父任务，不把子代理文本当主回答，也不把其结束当主 turn 结束。SDK 的 forwardSubagentText 决定是否包含子代理全文，默认不能假设一定有。依据：`T:1910`、`T:3702`。

compaction 不等于清空聊天：UI 可以显示压缩标记，但历史 reader 必须理解重连元数据。用量也需区分每轮 usage 和跨轮累计 modelUsage/total_cost_usd；不能把连续 result 的累计值相加。依据：`T:3844`、`T:5950`、`T:5958`。

### 3.9 中断、恢复与模型缓存还有跨组件遗漏

- interrupt 回执可能先于或晚于异常 result，still_queued 不一定只含客户端提交 UUID；能力支持时发送 cancel_queued=true，但终态仍独立等待。依据：`T:4826`、`T:4842`。
- 原方案的 pending_permission_requests 不足以确认“无待办”，缺失字段更不能当空数组。恢复后先核验进程代次、session 和原生状态，禁止自动重发上轮输入。依据：`T:433`。
- Fuyu 模型缓存和 backend 现在在 root App 上，而不是按 agent/session/cwd 区分；仅把类型移到 agent 包会使两种 Agent 的模型列表串用。需统一改路由、事件归属、缓存键、历史入口、运行详情及错误文案。依据：`F/internal/ui/app.go:31`、`:47`；`F/internal/ui/models.go:22`。
- `ResumeThread` 与 `ListTurns` 目前承担恢复门控，`State.Apply` 接受的仍是 Codex 事件名。类型别名不会自动消除这些语义。应定义应用事件与恢复结果的能力边界，再由各 adapter 投影。依据：`F/internal/ui/history.go:15`、`:27`；`F/internal/app/state.go:97`。

## 4. P6.0～P6.4 的具体增补项

以下为修订建议，均未勾选完成；实施仍需按仓库当前阶段另行推进。

### P6.0 协议与历史证据先行

1. 固定可执行路径和实测 CLI 版本，保存类型文件摘要；录制 initialize 完整 request/response、system/init、首个 user/assistant/result，区分模型目录来源与会话当前模型。
2. 录制一次允许、拒绝含反馈、审批超时、审批时停止、control_cancel_request、重复请求和 pending 回放；验证 AskUserQuestion，多余 dialog kind 不声明。
3. 对比“每轮 spawn+resume”与“同进程两轮以上”，包括空闲、第二轮中断、result 后通知、session 权限跨轮存续；选定 P6.2 唯一生产基线并记录原因。
4. 分开录制真实 transcript：两轮上下文、工具配对、手动/自动 compact、正常续接、异常中断后的恢复；确认 parentUuid、目录编码、CLAUDE_CONFIG_DIR 和 worktree 行为。
5. 合成负路径夹具覆盖超大帧、半行、坏行、未知 subtype、子代理交错、result success+is_error、终态重复/迟到、EOF 无 result、控制响应乱序与写入不确定。

通过门槛：形成版本/能力矩阵与双向协议断言；每项注明真实录制或合成。仅有“解析能过”不能结束 P6.0。

### P6.1 Agent 抽象与 Codex 等价回归

1. 定义 AgentKind、SessionKey、应用 TurnID、ProcessGeneration、Capabilities、应用事件、审批意图、历史分页/恢复核验结果；原始协议帧留在各 adapter 内。
2. 把生命周期、审批、模型/命令目录、原生历史、缺口核验、占用/分叉按能力组织，允许实现明确“不支持”，不要求 Claude 伪造 Codex RPC。
3. 路由按 session 选择 backend；断线和重启按实际影响域处理；缓存至少按 agent/runtime 配置/cwd 区分，选中模型保存 agent 归属。
4. 从 Codex client 抽出可复用的限额、请求关联和进程管理，保持其现有常驻 App Server 行为；消除 root 模型缓存、硬编码错误文案和全局失联假设。
5. 对 Codex 的发送、审批、停止、历史恢复、分叉和多会话运行现有相关测试，确认语义等价后再叠 Claude。

通过门槛：Codex 行为回归通过，且 UI/app 不再需要用 Codex 专属 RPC 去修复 Claude 状态。

### P6.2 Claude adapter 与失败状态机

1. 实现有界 framing、独立 reader、带超时的串行 writer、双向 request_id 关联；完成握手和权限确认后提交输入。
2. 发送前分配稳定 turn/user UUID；每 session 一个在途 turn；处理 delta、分块 assistant、最终消息和 result 的覆盖/去重，以及原生终态分类。
3. 实现审批状态表、取消/过期/重复提交保护、AskUserQuestion、单次 allow/deny；只有真实验证后才启用 session 规则授权。缺 tool ID 不自动 allow。
4. 实现 interrupt 回执与终态分离、队列取消能力探测、限时优雅停止及自有进程组回收；预期进程退出不作为全局断线。
5. 实现 transcript 分支重建、compaction、消息/工具关联和稳定分页；读取损坏/尾部未完成时显示具体不确定性，禁止假成功。
6. 子代理至少有父任务归属；无完整嵌套 UI 时显示有用摘要。保留 result 后通知与后台工具状态；未知合法帧不破坏会话，未知交互按能力规则处理。

通过门槛：P6.0 正负夹具通过，证明审批等待不阻塞控制通道、晚到旧轮事件不结束新轮、一个 Claude session 故障不污染其他会话。

### P6.3 会话入口、身份与存储

1. 在新会话创建前选择 Agent；已有原生 session 不原地改 Agent。每个会话保存 AgentKind、原生 ID、选中模型与必要运行时归属；旧会话默认 Codex。
2. 完成 v4 迁移设计与实现：按 Agent 的 Runtime 根目录绑定、原生 ID 联合唯一约束、旧值回填、一致性备份、失败回滚和高版本拒绝；若选择 v3 替代设计，必须先通过第 2.5 节全部约束验证。
3. 历史列表按 Agent 路由，模型/命令菜单按能力展示，区分“当前配置”与“运行时已确认值”；缺能力时不显示不可执行入口。
4. 未登录/onboarding/CLI 缺失给出对应入口；恢复未知任务不自动重发；分叉、外部占用和后台任务停止未实现时明确边界。
5. 防止旧程序误开 Claude 会话，验证同一原生 ID 在不同 Agent 下的存储行为，以及重启后仍选择正确 adapter。

通过门槛：旧数据无损打开、混合 Agent 会话重开不串线、错误 home 不被误认成历史丢失。

### P6.4 真实 Claude 与双 Agent 联合验收

1. Claude 真实文件任务：读取、审批写入、拒绝与反馈、连续两轮上下文、模型选择、问答；验证发生的文件副作用与 UI 终态一致。
2. 分别在流式输出、等待审批、启动/发送窗口中停止；检查回执、result、进程组与后台任务，不以一个“停止成功”标记替代其余证据。
3. 正常重开、执行中异常退出再开、含工具结果和 compact 的历史恢复；验证未确认任务不自动重发，分页无重复/分支污染。
4. Codex 与 Claude 同时运行，切换会话和模型；使单个 Claude 进程故障，确认其他 Claude/Codex 会话、缓存、审批和历史仍归属正确。
5. 以受控输入验证大输出/损坏历史，再记录实际 CLI 的能力、版本、配置根路径和限制；分别汇报通过、失败、未验证、暂缓。
6. 后续真正修改应用时，按仓库流程完成相关验证与构建，再正常退出并重启 Fuyu 确认新窗口；本轮只读评审不触发构建或重启。

通过门槛：真实 CLI 与 GUI 验收均有来源记录；session 规则、分叉、外部会话写入权和后台任务全停，凡未验证者保持明确限制，不计入完成项。

## 5. 本轮验证边界

已完成：对照所给 P6 文件、cdesktop Claude/Codex executor 及其服务层日志/停止链路、SDK 类型、Fuyu Backend/状态/历史/模型/存储源码进行只读核验，并确认六项决策中需要修订的事实与边界。

未执行：真实 Claude stream-json、登录/权限配置操作、真实 JSONL 样本解析、会话恢复/分叉、GUI、构建和测试。未把参考项目测试算作 Fuyu 验收；未核验用户本机 Claude CLI 版本或当前生效权限参数。

首要修订顺序：**先纠正审批 scope 与 turn/握手契约，再做 transcript 恢复证据；以这些结果决定进程模型和存储迁移，最后进入 UI 接入。**
