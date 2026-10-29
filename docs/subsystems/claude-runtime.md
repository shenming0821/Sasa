# Claude Code 运行时

## 入口与会话

`cmd/desktop/main.go` 发现本机 Claude CLI，使用 `CLAUDE_CONFIG_DIR`（未设置时为 `~/.claude`）规范化路径注册独立后端，检查点保存于应用数据目录的 `claude/`。子进程显式使用这一配置根，历史读取与实际执行保持一致。发现/启动错误属于 Claude，不切换到 Codex。

输入区 Agent 菜单由 `internal/ui/agent_picker.go` 实现，与模型入口并列。空白会话切换保留文字和图片草稿，清空模型与命令缓存；已有消息、任务或恢复状态时，菜单提示使用另一 Agent 新建会话，原会话与任务保持独立。最近选择写入应用索引，会话历史保留 Agent/Runtime 归属。

## 协议与进程

`client.go` 管理会话检查点，`protocol.go` 管理每轮子进程与 stream-json。使用 `--permission-prompt-tool stdio --permission-mode default`，发送 initialize 并核验返回的 `current_permission_mode=default` 后再发送用户帧。模型和命令目录来自 initialize；模型选择通过 `--model` 传递，命令保留原生 `/名称 参数` 文本。

Claude 的 `/` 候选仅显示技能。`catalog.go` 在同一探测进程中继续发送只读 `get_skills_dialog`，用技能的 `name`、`display_name` 与 initialize 的命令名关联，为 `agent.Command.IsSkill` 标记有效且未关闭的技能；UI 仅展示该标记为真的命令，保留原生命令名和参数提示。技能目录读取失败时返回错误，由候选入口提供重试；目录为空时不回退展示其他命令。该能力已核对本机 CLI `2.1.307`，旧版本或其他连接拒绝该请求时会明确显示读取错误。

当前实现每轮 spawn，后续使用原 session UUID resume；该策略已通过真实上下文与重开续发验证。两种策略的真实双轮对照均通过，本次记录中第二轮分别约 1.943 秒与 2.034 秒；这只是单次测量，不作为性能统计。首版保留每轮进程基线，并暂不开放会话授权规则；常驻进程的第二轮中断/空闲/授权矩阵未用于生产验收。result 后关闭 stdin 并有界排空 stdout，自有进程组由 `internal/runtime` 回收，模型/命令探测进程也纳入 Client 关闭范围。

应用 TurnID、发送 user UUID、transcript UUID 与模型 message.id 分开维护。reader 路由控制响应、审批和撤销，等待用户期间继续读取。writer 串行化并限制单次写入 10 秒，控制请求最多 30 秒。协议帧上限 16 MiB；显示队列上限 4096 条/4 MiB，优先移除文字增量并发送缺口标记；控制事件无法保留时明确断开并关闭自有进程，保留未知结果供核验。

初始化阶段取消时，关闭并等待该次自有进程，回滚尚未写入用户帧的检查点，返回 `agent.NotSubmittedError`；这种退出不投影成任务失败或断线。输入帧开始写入后，读取失败继续保留 unknown，不能用请求上下文取消推断未执行。输入区在尚无原生 turn ID 的准备阶段也提供停止操作；若发送已完成，则改发原生 interrupt 并等待终态。

普通成功需 result 的 subtype、is_error、terminal_reason 与正常退出共同确认。`/compact` 的本机原生结果为 success、is_error=false、无 terminal_reason；仅在明确发送该命令且收到 compact_boundary、进程正常退出时认定完成。实测停止返回 `error_during_execution + is_error=true`，terminal_reason 可能为 `aborted_tools` 或 `aborted_streaming`；这两种终态仅在收到对应 interrupt 成功回执时映射 interrupted，无回执仍判失败。无 result、坏帧与不确定写入保留 unknown 检查点并阻止重发。

## 审批与投影

`projection.go` 将工具与文本投影到应用已有卡片，按稳定块 ID 更新；子代理工具通过 `parentToolUseId` 投影到应用 `Tool.ParentToolID`，命令与其他工具统一保留关联。界面标明“子任务”，不同父任务的命令分别分组；子代理文字不重复显示在主任务正文。can_use_tool 提供本次允许、拒绝、拒绝并停止，拒绝可附加反馈。AskUserQuestion 保留问题与选项，支持多选及其他回答，返回原问题文本到 answers 的映射。请求按进程和 request_id 去重，撤销、已提交和终态后迟到响应失效。

当前不提供永久规则、session 规则或权限模式切换。后台工具收到 task_notification 时更新状态，任务结束后的迟到通知只更新对应工具，不重启任务。真实前台和后台 Bash 的中断均已核验：本机 CLI 返回 interrupted，记录的 Bash PID 不存在，延迟文件写入未发生。该证据仅覆盖已执行的 Bash 场景，不推断所有分离进程或外部任务都已停止。

## 历史与恢复

`history.go` 只读取 Fuyu 创建并持有检查点的会话，原生记录位于配置根的 `projects/*/<session>.jsonl`。读取上限 64 MiB，核验文件身份、大小、修改时间和完整尾行；坏行、缺父链、替换或正在写入均返回不确定错误。沿 parentUuid 选择有效主链，补回同一模型消息的并行工具调用与结果，排除 sidechain/team/meta。分页以完整 turn 为单位，每页 20 轮，游标绑定内容摘要。

检查点终态与 transcript 内容摘要共同决定可续发状态；旧任务 unknown/inProgress 或外部改变记录时保留历史展示并阻止续发。不自动重发旧用户输入或恢复旧审批。已确认 interrupted 的任务中，同一原生 promptId 的标准中断标记归属原任务，不新建待完成的用户轮次；未知终态、不同 promptId 或实际发送的同文输入继续按普通输入核验。compact 支持字符串 summary、preservedSegment/preservedMessages 重连与完整 turn 分页；原生 local command 的 promptId 关联发送检查点，重复摘要与 hook 命令不作为新用户问题显示。真实 compact 后历史及上下文续发已通过，更多复杂嵌套压缩仍保留回归范围。摘要核验不能替代外部 writer 锁；当前不提供外部会话接管、Claude 分叉或强制解除未知任务入口。

运行详情展示 system/init 的实际模型与 CLI 版本，模型选择在内部保留原生别名；恢复时从检查点读取对应实际值。审批的应用 request ID 编码原 session/进程代次，回传时还原 native request_id，避免两个独立进程的相同 native ID 串用。

模型菜单及输入区使用 initialize 的 resolvedModel 显示具体模型名称，发送时保留原生别名；目录移除 default 和额外的 `[1m]` 上下文预设，按具体模型去重。模型目录加载前显示“选择模型”，避免短暂泄漏 opus/sonnet 等别名。每个 Claude 配置根单独记住上次选择或成功发送的模型，保存在 SQLite 设置的 LastClaudeModels 中；新建会话和切回 Claude 时沿用，现有会话保留自身选择。旧设置没有该字段时，从已有 Claude 会话选择回填，并优先采用上次活动会话。没有使用记录时选择目录中的首个具体模型，Codex 模型默认行为保持原有规则。

## 验证

`adapter_test.go`、`queue_test.go` 与合成 `testdata/server.py` 验证审批不堵 reader、取消/停止、跨会话失败隔离、重复响应、问答、多选、目录绑定、坏帧、未知终态、历史分支与分页、队列上限。真实测试需显式 `FUYU_LIVE_CLAUDE=1`，使用临时 cwd 与现有原生登录。已取得证据和未验收项见 [P6 验证记录](../verification/2026-10-28-p6-agent.md)。
