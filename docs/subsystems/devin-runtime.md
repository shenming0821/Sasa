# Devin 运行时

## 接入形态

`internal/devin/` 把 `devin acp` 子进程适配为通用 Agent 后端。ACP（Agent Client Protocol）是换行分隔的 JSON-RPC over stdio，一个进程承载全部 Devin 会话，不需要进程池或会话级检查点。

启动链路与 Claude 相同：`cmd/desktop/main.go` 的 worker 解析可执行文件（PATH、`~/.local/bin/devin`、Homebrew 路径），读取凭据后调用 `devin.New` 同步完成 `initialize` + `authenticate` 握手，握手失败的连接不注册、错误原样展示。Runtime 身份为 Devin 数据根（`~/.local/share/devin` 或 `DEVIN_DATA_DIR`），随渠道数据目录隔离；会话检查点存于 `<dataDir>/devin/`。

## 凭据

`authenticate` 需要 API key，ACP 模式不复用 CLI 本地登录态。`ResolveAPIKey` 按顺序取 `DEVIN_API_KEY` 与 Devin 凭据文件 `credentials.toml` 中的 `windsurf_api_key`（Devin Desktop/Windsurf 登录产物）。两者都缺省时握手报错并提示设置环境变量或登录。

## 协议映射

| Fuyu | ACP |
|---|---|
| `StartThread` | `session/new`（cwd、mcpServers） |
| `Send`/`SendWithImages`/`SendWithOptions` | `session/set_config_option`（变更项）+ `session/prompt` |
| `SetPermissionMode` | `session/set_mode`，以 `current_mode_update` 为生效确认 |
| `Interrupt` | `session/cancel` 通知 |
| `Events()` | ← `session/update` 通知投影 |
| 审批 `Respond` | ← `session/request_permission` → `outcome` |
| `HistoryBackend` | `session/list`、`session/load`（sessionId + cwd + mcpServers，重放更新） |
| `IdleThreadReleaser` | `session/close` + 解除附着 |
| `Reconciler` | 已捕获重放投影为 `{"thread":{id,turns}}` |

`session/load` 的 `cwd` 与 `mcpServers` 为必填；目录依次取 `session/list` 分页索引与会话检查点 `state.Cwd`，恢复成功回写检查点，仍无法取得时明确报错而不发送请求。会话被其他进程持有时返回 `-32015 session_locked`（含 `lockHolderPid`），`agent.IsActiveWriter` 归入"已在另一个应用中打开"占用态，仅提供重试；`session/close` 的 `_meta` `cognition.ai/force` 接管能力未接入。

`session/prompt` 在轮次结束时才返回，`stopReason` 映射：`end_turn`→completed、`cancelled`→interrupted，其余（refusal/max_tokens/max_turn_requests/未知）→failed 并附错误。turn 身份由 `TurnMap` 在本地分配（ACP 不返回 turn ID），会话检查点按用户输入前缀匹配重放轮次并回填应用 TurnID。

`session/update` 投影：`agent_message_chunk`→消息增量；`tool_call`/`tool_call_update` 按 `kind` 映射（execute→commandExecution、edit/delete/move→fileChange、其余→dynamicToolCall）。开始和更新通知中的 diff 内容块投影为 `changes`：完整文件 oldText/newText 转成带文件起始行号和末尾换行标记的统一补丁，kind 标记 `snapshot:true`；完成通知的内容替换开始时的占位快照。命令执行附带的 diff 另发文件变更条目，保留命令输出。缺失内容保持不完整；历史回放执行同样的投影。文本块为聚合输出，`cognition.ai/terminal_exit` 提供退出码；`config_option_update`/`available_commands_update`/`current_mode_update`/`session_info_update` 更新会话缓存。`agent_thought_chunk`、`plan`、`usage_update` 不投影到实时流，仅在恢复重放时生成 reasoning 条目。

配置选项映射：`model`→模型、`thought_level`→推理强度、`speed`→服务档位，均按会话随 `session/new`/`session/load` 返回并支持 `session/set_config_option` 修改；`modes` 保留当前模式名到 `approvalPolicy`。权限模式（accept-edits/smart/ask/plan/bypass 等）经 `modes.availableModes` 与 `mode` 配置选项取得展示名，`session/set_mode` 切换并以 `current_mode_update` 确认为准；空响应不证明生效，未知 id 会被服务端静默回退，因此本地校验可选集合并比对确认值。每次附着（`session/new`/`session/load`）默认申请 `bypass`，未广告或失败时保留服务端实际模式；`fuyu/runtime` 事件携带 `policy`/`mode`/`modes` 供运行详情的模式切换 chips 与外部切换（如 `/bypass` 命令）同步显示。`model` 选项值是 `族-基线强度` 形式的 resolved ID（如 `swe-2-high`，裸族名会被服务端拒绝），尾缀属于 `thought_level` 可选值时投影为该模型的默认推理强度；`speed` 的 `standard` 并入应用内置基线档位，选择基线档时按 `standard` 写回。选项展示与 chips 顺序保留服务端返回顺序。未实现的 ACP 客户端能力（fs、terminal、其他请求方法）在 `initialize` 声明不支持并以 `cancelled` 兜底。

## 事件与故障语义

单一进程无进程代次，出站事件不携带 generation；UI 按 Agent/Runtime/session ID 路由。显示队列上限与 Codex/Claude 一致（4 MiB/4096 条），溢出先丢消息增量并发 `fuyu/eventGap`，控制事件也放不下时升级为 `fuyu/disconnected`。进程意外退出把所有在途 turn 投影为 failed、会话标记需重新 `session/load`，随后发 `fuyu/disconnected`。

审批按选项收窄映射：`accept`→`allow_once`，`acceptForSession`→`allow_always`（缺省回退 `allow_once`），`decline`→`reject_once`/`reject_always`，`cancel`→`outcome cancelled`；无匹配选项时按取消处理。turn 结束或断连时未决审批以 cancelled 回写并投影 `serverRequest/resolved`，不会遗留等待中的原生请求。

## 边界

- 鉴权依赖本机凭据文件或 `DEVIN_API_KEY`，不支持浏览器 PKCE 登录流程。
- `session/list`/`session/load` 覆盖 Devin 原生会话索引（含其他客户端创建的会话）；`session/delete` 未接入 UI。
- 历史重放是单次 `session/load` 捕获的快照；`session/close` 释放后重新加载。
- 图片输入声明于 initialize 能力，实际以 `image` 内容块发送（base64 + MIME 嗅探）。
- 计费与模型可用性由 Devin 账户决定，与本地 API key 模式不同。
- Devin 会话可在其他客户端继续；检查点以用户输入前缀匹配，外部新增轮次不会错绑应用 TurnID。

## 验证入口

`internal/devin/client_test.go` 用 `FUYU_DEVIN_HELPER` 环境变量重入测试二进制充当脚本化 ACP server，覆盖握手鉴权、会话创建、消息增量、工具生命周期、审批选择与回写、中断、忙会话互斥、历史列表/重放续发、配置选项更新与未提交错误、断线终态、权限模式默认 bypass 与会话切换（含未知模式/会话报错）。UI 侧 chips 入口与错误展示见 `internal/ui/view_test.go` 的 `TestRuntimeDetailsPermissionModes`。
