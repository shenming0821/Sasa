# Claude Code 运行时

## 入口与会话

`cmd/desktop/main.go` 发现本机 Claude CLI，使用 `CLAUDE_CONFIG_DIR`（未设置时为 `~/.claude`）规范化路径注册独立后端，检查点保存于应用数据目录的 `claude/`。子进程显式使用这一配置根，历史读取与实际执行保持一致。发现/启动错误属于 Claude，不切换到 Codex。

标签栏行尾的 Codex 和 Claude Code 图标由 `internal/ui/agent_picker.go` 实现，点击后在当前目录新建对应 Agent 的空白会话，原会话的文字/图片草稿、模型、上下文和运行中任务保留。会话页签标题前用小图标显示 Agent，原 Claude 文字前缀由图标替代；会话视图移除上方目录工具栏。输入区保留模型选择；连接不可用时显示具体原因并保留当前会话。最近选择写入应用索引，会话历史保留 Agent/Runtime 归属。

## 协议与进程

关闭会话标签后，UI 保留 30 秒再调用 `ReleaseIdleThread`：活动和未知任务保留，空闲进程停止计时、退出并等待清理完成；已按 2 分钟超时回收的进程也可进入缓存回收。原生历史可读取且恢复状态为 idle 时，清理关闭会话的消息及渲染缓存，保留草稿、标题、阅读位置和折叠状态；重开先恢复历史再允许发送。历史缺失、变化或读取失败时保留显示缓存，按现有 1 分钟间隔重试。进程自身仍保持 2 分钟空闲超时、最多 4 个空闲进程的上限。验证见[Claude 关闭会话回收](../verification/2026-11-02-claude-memory.md)。

`client.go` 管理会话检查点，`protocol.go` 与 `session_process.go` 管理任务进程、独立轮次与 stream-json。使用 `--permission-prompt-tool stdio`，权限模式由 CLI 按原生配置与当前 cwd 决定；新任务、恢复后的后续任务和模型/技能目录探测共用此启动入口。发送 initialize 并核验返回的 `current_permission_mode` 为非空字符串后再发送用户帧，缺失或格式错误时终止初始化并保持未发送状态。模型和命令目录来自 initialize；模型选择通过 `--model`、推理强度选择通过 `--effort` 传递，命令保留原生 `/名称 参数` 文本。`PinnedEffort` 检测配置根与项目 `.claude` settings `env.CLAUDE_CODE_EFFORT_LEVEL` 及启动环境变量：env 层锁定实测覆盖 `--effort`，模型菜单据此提示选择不会生效。2026-10-29 本机 `permissions.defaultMode=bypassPermissions`，真实 CLI 初始化确认 `current_permission_mode=bypassPermissions`，验证见[权限配置继承](../verification/2026-10-29-claude-permission-mode.md)。

Claude 的 `/` 原生目录候选仅显示技能，另由 UI 提供本地 `/clear`。`catalog.go` 在同一探测进程中继续发送只读 `get_skills_dialog`，用技能的 `name`、`display_name` 与 initialize 的命令名关联，为 `agent.Command.IsSkill` 标记有效且未关闭的技能；UI 展示该标记为真的命令，保留原生命令名和参数提示。技能目录读取失败时返回错误，由候选入口提供重试；目录为空时不回退展示其他原生命令，本地 `/clear` 保持可见。该能力已核对本机 CLI `2.1.307`，旧版本或其他连接拒绝该请求时会明确显示读取错误。

本机 CLI 的 `/clear` 会发出 `conversation_reset` 并更换 session UUID。Fuyu 发送入口在本地处理这一命令：保存并保留旧会话历史，切换到相同 cwd、后端和模型的空白会话，后续任务再分配新原生 UUID，避免继续持有已被重置的旧身份。见 [/clear 验证](../verification/2026-10-29-claude-clear.md)。

普通成功轮次在完整原生 success/completed result 后保存终态并保留进程，下一轮复用其 reader/writer 和已初始化上下文，分配新的应用轮次身份。空闲 2 分钟回收，最多 4 个空闲进程；模型、推理强度或关联配置变化时先回收再重新 initialize/resume。slash 命令、失败、中断和仍有后台工具的结果沿用排空并退出的路径。目录探测独立于任务进程。策略、摘要核验和退出边界见[使用性能](performance.md#claude-连续对话进程)。真实同 PID 双轮上下文、重启续聊、同 PID 允许/拒绝/提问和第二轮运行中断通过，见[性能收尾验证](../verification/2026-10-30-performance-completion.md)。

应用 TurnID、发送 user UUID、transcript UUID 与模型 message.id 分开维护。reader 路由控制响应、审批和撤销，等待用户期间继续读取。writer 串行化并限制单次写入 10 秒，控制请求最多 30 秒。协议帧上限 16 MiB；显示队列上限 4096 条/4 MiB，优先移除文字增量并发送缺口标记；控制事件无法保留时明确断开并关闭自有进程，保留未知结果供核验。

初始化阶段取消时，关闭并等待该次自有进程，回滚尚未写入用户帧的检查点，返回 `agent.NotSubmittedError`；这种退出不投影成任务失败或断线。输入帧开始写入后，读取失败继续保留 unknown，不能用请求上下文取消推断未执行。输入区在尚无原生 turn ID 的准备阶段也提供停止操作；若发送已完成，则改发原生 interrupt 并等待终态。

普通可复用轮次以完整 result 的 success、is_error=false、terminal_reason=completed 确认任务完成；进程可以继续等待下一轮。本地命令和非复用路径继续结合正常退出核验。`/compact` 的本机原生结果为 success、is_error=false、无 terminal_reason；仅在明确发送该命令且收到 compact_boundary、进程正常退出时认定完成。实测停止返回 `error_during_execution + is_error=true`，terminal_reason 可能为 `aborted_tools` 或 `aborted_streaming`；这两种终态仅在收到对应 interrupt 成功回执时映射 interrupted，无回执仍判失败。无 result、坏帧与不确定写入保留 unknown 检查点并阻止重发。

`/model` 等本地命令也可能成功但没有 terminal_reason。此时要求成功 result、正常进程退出，并在原生历史主链末尾找到同一发送 UUID 的 `system/local_command`、`level=info`、匹配的命令与参数，以及完整的 `local-command-stdout` 输出，才认定完成；一般成功判定及失败保护保持上述规则。

模型、默认模型与技能目录通过 `catalog_cache.go` 按 cwd 共享探测进程、在途请求及结果；Client 自身绑定可执行文件与配置根。模型在 initialize 后立即可用，技能接口的延迟或失败独立反馈；initialize 报告的逐模型 `supportedEffortLevels` 进入模型目录，模型菜单据此渲染“推理强度”选项，未选择沿用 CLI 原生默认（本机 CLI 2.1.307 `claude --help` 确认 `--effort low/medium/high/xhigh/max`）。缓存、刷新与退出回收约定见[使用性能](performance.md#窗口启动与模型目录)。

## 审批与投影

`projection.go` 将工具与文本投影到应用已有卡片，按稳定块 ID 更新；子代理工具通过 `parentToolUseId` 投影到应用 `Tool.ParentToolID`，命令与其他工具统一保留关联。界面标明“子任务”，不同父任务的命令分别分组；子代理文字不重复显示在主任务正文。can_use_tool 提供本次允许、拒绝、拒绝并停止，拒绝可附加反馈。AskUserQuestion 保留问题与选项，支持多选及其他回答，返回原问题文本到 answers 的映射。请求按进程和 request_id 去重，撤销、已提交和终态后迟到响应失效。

当前不提供永久规则、session 规则或权限模式切换。后台工具收到 task_notification 时更新状态，任务结束后的迟到通知只更新对应工具，不重启任务。真实前台和后台 Bash 的中断均已核验：本机 CLI 返回 interrupted，记录的 Bash PID 不存在，延迟文件写入未发生。该证据仅覆盖已执行的 Bash 场景，不推断所有分离进程或外部任务都已停止。

## 历史与恢复

`history.go` 只读取 Fuyu 创建并持有检查点的会话，原生记录位于配置根的 `projects/*/<session>.jsonl`。读取上限 64 MiB，核验文件身份、大小、修改时间和完整尾行；坏行、缺父链、替换或正在写入均返回不确定错误。沿 parentUuid 选择有效主链，补回同一模型消息的并行工具调用与结果，排除 sidechain/team/meta。分页以完整 turn 为单位，每页 20 轮，游标绑定内容摘要。

检查点终态与 transcript 内容摘要共同决定可续发状态；旧任务 unknown/inProgress 或外部改变记录时保留历史展示并阻止续发。不自动重发旧用户输入或恢复旧审批。已确认 completed、failed 或 interrupted 的任务中，同一原生 promptId 的标准中断标记归属原任务，保留检查点终态，不新建待完成的用户轮次；未知终态、不同 promptId 或实际发送的同文输入继续按普通输入核验。compact 支持字符串 summary、preservedSegment/preservedMessages 重连与完整 turn 分页；原生 local command 的 promptId 关联发送检查点，重复摘要与 hook 命令不作为新用户问题显示。真实 compact 后历史及上下文续发已通过，更多复杂嵌套压缩仍保留回归范围。摘要核验不能替代外部 writer 锁；当前不提供外部会话接管、Claude 分叉或强制解除未知任务入口。

旧版本误判的本地命令可在恢复时按上述完成证据修复：`ResumeThread` 仅在没有自有活动进程、历史摘要与检查点相同，且末条 unknown 命令具有匹配的本地完成记录时，原子保存 completed 后恢复输入；保存前再次核对代次、任务身份、摘要和运行状态。inProgress、其他未知任务、归属不符、错误输出或摘要变化继续保持待核验。主链保留末尾的 system 本地输出，并将其投影到对应任务；这不授予外部会话接管或任意解除未知状态的能力。实测见[本地命令恢复验证](../verification/2026-10-29-claude-local-command-recovery.md)。

运行详情展示 system/init 的实际模型与 CLI 版本，模型选择在内部保留原生别名；恢复时从检查点读取对应实际值。审批的应用 request ID 编码原 session/进程代次，回传时还原 native request_id，避免两个独立进程的相同 native ID 串用。

模型菜单及输入区使用 initialize 的 resolvedModel 显示具体模型名称，发送时保留原生别名；目录移除 default 和额外的 `[1m]` 上下文预设，按具体模型去重。模型目录加载前显示“选择模型”，避免短暂泄漏 opus/sonnet 等别名。每个 Claude 配置根单独记住上次选择或成功发送的模型，保存在 SQLite 设置的 LastClaudeModels 中；新建会话和切回 Claude 时沿用，现有会话保留自身选择。旧设置没有该字段时，从已有 Claude 会话选择回填，并优先采用上次活动会话。没有使用记录时选择目录中的首个具体模型，Codex 模型默认行为保持原有规则。

## 验证

`adapter_test.go`、`queue_test.go` 与合成 `testdata/server.py` 验证审批不堵 reader、取消/停止、跨会话失败隔离、重复响应、问答、多选、目录绑定、坏帧、未知终态、历史分支与分页、队列上限。真实测试需显式 `FUYU_LIVE_CLAUDE=1`，使用临时 cwd 与现有原生登录。已取得证据和未验收项见 [Claude 接入与双 Agent 支持验证记录](../verification/2026-10-28-p6-agent.md)。
