# Claude CLI 基础协议证据

真实录制于 Linux，日期为 2026-10-27（两份 JSON 输出）及 2026-10-28（普通文本对照），时区 Asia/Shanghai。Linux 编写，Mac 待验证。此目录是 Claude 协议与历史核验基础证据，不代表 Claude adapter 或完整协议验收已完成。

## 版本与环境

- 调用路径：`/home/ming/.local/bin/claude`；实际文件：`/home/ming/.local/share/claude/versions/2.1.311`。
- `claude --version` 实际输出：`2.1.311 (Claude Code)`；`codex --version`：`codex-cli 0.164.0`。
- SDK 类型来源：用户提供的 `/tmp/p6-source-typescript.d.ts`；SHA-256：`64a59ddbcae4dc05da268dd3e009431c67e91a23616bce38f122b9247650271f`。未用 SDK 包版本推断 CLI 能力。
- 每次调用使用独立 `/tmp/fuyu-p6-*` 工作目录，实际 cwd 见 `recording.json`。保留用户原有配置，不读取或复制凭据值。
- `CLAUDE_CONFIG_DIR`：未设置；实际 init 中的 `memory_paths.auto` 位于 `/home/ming/.claude/projects/`。`CLAUDECODE`、`CLAUDE_CODE_ENTRYPOINT`：均未设置；未据此声称已验证嵌套环境过滤。
- 请求参数 `--dangerously-skip-permissions`；实际 `permissionMode=bypassPermissions`。本次只有问候任务，不涉及工具审批或文件修改。
- 实际 init 的 `model=zhipu/glm-5.3`，assistant 的 `model=glm-5.3`，result 的 `modelUsage` 键为 `zhipu/glm-5.3`。这是已有 provider 配置下的 Claude CLI 真实运行，未验证 Anthropic 直连模型。

## 录制命令

实际使用 Python `subprocess.run` 参数数组、pipe stdin、独立临时 cwd，stdout/stderr 按原始字节保存，单次超时 120 秒。以下为等价 shell 命令；从隔离临时目录运行，并将输出路径换成仓库绝对路径：

```sh
echo '{"type":"user","message":{"role":"user","content":[{"type":"text","text":"只回复：你好"}]}}' |
  /home/ming/.local/bin/claude --print --output-format stream-json --input-format stream-json --verbose --max-turns 1 --dangerously-skip-permissions > hello-stream-json.jsonl 2> hello-stream-json.stderr.txt

echo '只回复：你好' |
  /home/ming/.local/bin/claude --print --output-format stream-json --input-format text --verbose --max-turns 1 --dangerously-skip-permissions > hello-text.jsonl 2> hello-text.stderr.txt

echo '只回复：你好' |
  /home/ming/.local/bin/claude --print --input-format text --max-turns 1 --dangerously-skip-permissions > hello-plain.txt 2> hello-plain.stderr.txt
```

`hello-user.jsonl` 保存第一条命令的实际 stdin（含末尾换行）。三次均为退出码 0；版本、日期、命令参数、cwd、字节数与原始证据 SHA-256 见 `recording.json`。stdout/stderr 未改写；已核对未包含凭据内容。stderr 保留 CLI 对自定义模型名称的警告，普通文本版本另有上下文窗口默认 200k 的提示，不能据此宣称模型目录探测成功。

## 夹具与帧序列

| 文件 | 来源与实际序列 |
|---|---|
| `hello-user.jsonl` | 真实 stdin，单条 user 帧 |
| `hello-stream-json.jsonl` | 真实 stdout，41 帧：system/init → 37 个 system/thinking_tokens → 2 个 assistant → result/success |
| `hello-text.jsonl` | text stdin 的真实 stream-json stdout，34 帧：system/init → 30 个 system/thinking_tokens → 2 个 assistant → result/success |
| `hello-plain.txt` | text stdin、默认 text stdout，实际为 `你好` 加换行 |
| `hello-*.stderr.txt` | 各次调用的真实 stderr |
| `recording.json` | 录制元数据与原始证据摘要 |

两份 JSON 录制中，两个 assistant 帧共用同一个模型 `message.id`，但各有独立顶层 UUID；内容分别为 thinking 和 text。result 的顶层 UUID 又不同，`subtype=success`、`is_error=false`、`terminal_reason=completed`、`result=你好`。这些标识没有被当作应用 TurnID。所有帧属于同一 session；两次独立录制的 session 不同。

system/init 实测包含当前 model、tools、slash_commands、terminal_slash_commands、版本及 capabilities。已报告 `interrupt_receipt_v1` 和 `interrupt_cancel_queued_v1` 等能力，但未执行中断，因此能力声明不代表中断路径已通过。此输入没有主动发送 initialize 控制请求，没有 initialize 的模型目录响应，也没有自动回放 user 帧；不能添加虚构帧使序列显得完整。

## 验证边界与补录

`internal/claude/fixture_test.go` 只断言这些真实样本的身份、序列、分块消息与终态，不启动 CLI 或模型，不宣称已实现生产协议解析器。stdout 夹具不是原生 transcript。

以下均为 **待 Mac 真实验收补录**：

- initialize 完整控制 request/response、模型目录和权限确认。
- 审批允许、拒绝反馈、超时、审批时停止、control_cancel_request、重复请求和 pending 回放。
- AskUserQuestion 与不支持的 dialog；不声明未实现的 dialog kind。
- 每轮 spawn+resume 与同进程两轮以上、第二轮停止、空闲退出、result 尾随通知。
- session 规则跨进程保留；未经验证不承诺持久授权。
- 独立录制原生 transcript，覆盖 parentUuid、工具配对、compaction、正常与异常恢复、worktree 和配置根变化。

有界 framing、半行/坏行、未知 subtype、子代理交错、重复/迟到终态、EOF 无 result、超大图片/工具输出及控制响应乱序等合成异常夹具，随 Claude 运行时接入解析器补齐并单独标注来源。本轮不把这些项目计为完成。

## Mac 任务生命周期补录（2026-10-28）

`live_tasks.py /Users/shenming/.local/bin/claude [subagent|background|subagent-tool]` 在独立临时目录录制真实任务，当前实测版本 `2.1.307 (Claude Code)`。CLI 使用 stream-json 输入/输出、`--permission-prompt-tool stdio --permission-mode default --model default`，initialize 后发送受控任务，未使用 bypassPermissions。实际配置根为 `/Users/shenming/.claude`，实际 provider/model 沿用本机配置，不代表 Anthropic 直连。

每行保存 `direction` 与原生 `frame`。录制器先解析 stdout 再进行 JSON 序列化，因此这些记录保留字段值而不是原始 stdout 字节。仓库只选取 assistant/user 工具消息和 system/task_* 通知；完整 wire 在本轮临时目录中。所选片段不含 initialize 中的 account 等信息，不用于证明完整协议或原生 transcript。

| 片段 | 来源会话与内容 | SHA-256 |
|---|---|---|
| `mac-subagent-events.jsonl` | `dcf5240f-abd3-4aba-b2aa-c2c8d20ee575`；5 帧，前台 Agent 启动、更新、完成及工具结果 | `64778caebbbe49cdd2a7f61a6a377dc4540a511fcd0f3025809fd6ca116d6444` |
| `mac-background-events.jsonl` | `7b399caa-b830-446e-962a-c2dbe7d4c01c`；9 帧，后台 Bash 与完成通知；实际 `background-proof.txt` 等于 `FUYU_BACKGROUND_OK` 加换行 | `b7e84eabc90b96f6207a53a8d0fbd28a5c69146bca205487f3d43a5b8ceccb44` |
| `mac-subagent-tool-events.jsonl` | `38823aa7-883e-442d-bcdd-9af39eb11d8a`；8 帧，Agent 中的 Bash 带有真实 `parent_tool_use_id` | `1a8fe8f86b5f6a9b6a1746cce0805271413f9acb0d0d3110801488b48bd978e2` |

三次原生进程退出码均为 0、result 为 `success/is_error=false/terminal_reason=completed`；所选片段中的 task_notification 均在 result 前到达，不将本次录制声明为 result 后通知的真实证据。`tasks_test.go` 回放这些片段并验证所有工具终态；result 后通知另以合成输入验证。

`server.py` 的碎片化写入、超长帧、半行/坏行、未知 result、撤销及重复请求均为合成异常场景。`adapter_test.go` 验证不确定终态保持 unknown 且禁止重发、撤销后的重复请求不重新弹出、迟到回答不能执行副作用；这些通过结果不替代真实 CLI 的相应录制。

## Mac 控制协议与原生历史补录（2026-10-28）

`live_controls.py /Users/shenming/.local/bin/claude [allow|deny|question|interrupt]` 使用同版本、default 模型及 default 权限，在独立临时 cwd 先 initialize 再发送任务。允许操作实际写出 `FUYU_CONTROL_OK\n`；拒绝、问答和中断均没有 `approved.txt`。中断得到原生 `control_cancel_request`、interrupt success 回执以及 `error_during_execution/is_error=true/terminal_reason=aborted_tools`，进程实际退出码为 1。首次录制器把所有非零退出视为录制失败，已按原生结果修正并重录中断；不把这一录制器断言当成 adapter 缺陷。

`mac-control-*.jsonl` 保留双向控制帧和 result 的投影。initialize 响应只保留版本、能力、模型目录和 `current_permission_mode`，未复制 account、路径和其他非验收字段；result 只保留类型、错误标记、原因和会话标识。原始完整解析 wire 仍在独立临时目录、文件权限 0600，不以仓库片段冒充完整响应。录制时模型目录有 6 项，这只用于固定样本断言；生产目录仍动态读取。

| 文件 | 真实会话 | SHA-256 |
|---|---|---|
| `mac-control-allow.jsonl` | `e0eaccc6-161b-4e3d-978d-01b144b79516` | `3aff35a7f93f589650bddcb3596b4b82f839b929c93cc68af2860af55e04ea72` |
| `mac-control-deny.jsonl` | `1a879442-cb7b-4cd0-9bdc-929392e93cd8` | `fd72538e7d786b390cf3bc14a92bd4dcbefbe46767ae856cdd22b856004b9b50` |
| `mac-control-question.jsonl` | `fb4a55d5-98ec-4565-87d8-ec8edadd0718` | `1c02651dca828c2acaef127d08d6e083658ab6e0ffb15446d3f959dc9184240d` |
| `mac-control-interrupt.jsonl` | `8a6389a0-25ea-4dca-9a44-ebbd749ddaa4` | `2facee081c35b3e53c420a7254198e7bf51cecc5a0666ca810a2511a0537592b` |
| `mac-transcript-compact.jsonl` | `76f2a4ae-871c-4fcf-8dcc-a721c4723e48` | `95cb7520db554c649283c28c6b1103afeae151e69329454626295927b2bceb5c` |
| `mac-transcript-interrupted.jsonl` | `8a6389a0-25ea-4dca-9a44-ebbd749ddaa4` | `2cfc92723d2948b000c411b518580e493c726af34de06e4bb8d18f6664f646a5` |

`replay_controls.py` 以真实控制字段构造隔离子进程，request_id 和 session_id 按该次测试重新关联；逐字段比较 adapter 的本次允许、拒绝反馈、AskUserQuestion answers 与实测响应，并回放原生撤销和非零中断退出。它是实测帧驱动的受控回放，不是真实模型调用。

两份 `mac-transcript-*.jsonl` 来自原生 `projects/*/<session>.jsonl`，与 stdout 分开采集。仅保留历史解析所需的会话、父链、压缩、消息字段，以及维持父链的 attachment/progress 标识；其余 payload 未复制。`recorded_history_test.go` 使用这些原生字段与重建的测试检查点核验分支、compact 及中断恢复，不把重建检查点声称为原持久化文件。原会话的真实重开/compact 续发另由 live 验证证明。

`startup_server.py` 为合成握手等待，使用可观察文件同步。初始化取消测试验证没有用户帧、没有失联/假终态事件、未发送检查点回滚、显式重试可完成；UI 测试另外覆盖读取默认模型时取消、Send 尚未返回时取消、取消与发送成功竞争以及切换会话后的草稿归属。
