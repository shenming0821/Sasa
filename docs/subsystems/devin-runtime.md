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
| `Interrupt` | `session/cancel` 通知 |
| `Events()` | ← `session/update` 通知投影 |
| 审批 `Respond` | ← `session/request_permission` → `outcome` |
| `HistoryBackend` | `session/list`、`session/load`（重放更新） |
| `IdleThreadReleaser` | `session/close` + 解除附着 |
| `Reconciler` | 已捕获重放投影为 `{"thread":{id,turns}}` |

`session/prompt` 在轮次结束时才返回，`stopReason` 映射：`end_turn`→completed、`cancelled`→interrupted，其余（refusal/max_tokens/max_turn_requests/未知）→failed 并附错误。turn 身份由 `TurnMap` 在本地分配（ACP 不返回 turn ID），会话检查点按用户输入前缀匹配重放轮次并回填应用 TurnID。

`session/update` 投影：`agent_message_chunk`→消息增量；`tool_call`/`tool_call_update` 按 `kind` 映射（execute→commandExecution、edit/delete/move→fileChange、其余→dynamicToolCall），diff 内容块投影为 `changes`，文本块为聚合输出，`cognition.ai/terminal_exit` 提供退出码；`config_option_update`/`available_commands_update`/`current_mode_update`/`session_info_update` 更新会话缓存。`agent_thought_chunk`、`plan`、`usage_update` 不投影到实时流，仅在恢复重放时生成 reasoning 条目。

配置选项映射：`model`→模型、`thought_level`→推理强度、`speed`→服务档位，均按会话随 `session/new`/`session/load` 返回并支持 `session/set_config_option` 修改；`modes` 保留当前模式名到 `approvalPolicy`。未实现的 ACP 客户端能力（fs、terminal、其他请求方法）在 `initialize` 声明不支持并以 `cancelled` 兜底。

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

`internal/devin/client_test.go` 用 `FUYU_DEVIN_HELPER` 环境变量重入测试二进制充当脚本化 ACP server，覆盖握手鉴权、会话创建、消息增量、工具生命周期、审批选择与回写、中断、忙会话互斥、历史列表/重放续发、配置选项更新与未提交错误、断线终态。
