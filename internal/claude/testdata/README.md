# Claude CLI 基础协议证据

真实录制于 Linux，日期为 2026-10-27（两份 JSON 输出）及 2026-10-28（普通文本对照），时区 Asia/Shanghai。Linux 编写，Mac 待验证。此目录是 P6.0 基础证据，不代表 Claude adapter 或完整协议验收已完成。

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

有界 framing、半行/坏行、未知 subtype、子代理交错、重复/迟到终态、EOF 无 result、超大图片/工具输出及控制响应乱序等合成异常夹具，随 P6.2 解析器补齐并单独标注来源。本轮不把这些项目计为完成。
