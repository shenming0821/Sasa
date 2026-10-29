# Claude 本地命令恢复验证

日期：2026-10-29。环境：macOS arm64，Claude Code `2.1.307`，权限 `default`，配置根 `~/.claude`，Fuyu 使用默认应用数据目录。

## 现象与依据

用户的 Fuyu 项目会话底部持续显示“已恢复历史，但旧任务尚无确定终态；无法重新附着旧 stdio 或恢复旧审批。核验前暂停发送”。

只读核对发现末条发送为 `/model`，检查点状态为 unknown。原生 transcript 具有同一发送 UUID 对应的 `system/local_command` 记录，级别 info，命令为 model、参数为空，内容是完整的 `local-command-stdout`；历史摘要与检查点相同。先前运行时错误已显示 success、is_error=false、无 terminal_reason、进程正常退出。

原因有两处：成功判定只为 `/compact` 允许缺少 terminal_reason；历史主链选择只把 user/assistant 当作有效末尾，丢失末尾的本地 system 输出。重复核验因此始终得到 unknown。

## 修复与边界

- `local_commands.go` 核对原生本地输出的发送归属、命令和参数、级别、完整输出标签及主链末尾身份。
- `protocol.go` 仅在 result 明确成功、进程正常退出且上述本地完成证据存在时，将缺少 terminal_reason 的本地命令认定为 completed。
- `history.go` 保留本地 system 末尾与对应输出；恢复旧检查点时要求无自有活动进程、末条为 unknown、原生历史摘要未变，保存前再次核对代次和身份，原子保存后恢复输入。
- 非零退出、无 result、未知 terminal_reason、一般未知任务、inProgress、活动进程、摘要变化及不匹配的本地输出均继续保留原失败或待核验语义；不自动重发旧任务。

## 自动化和真实 CLI

```sh
./scripts/go test ./...
./scripts/go vet ./...
./scripts/go test -race ./internal/claude -run 'TestLocalCommand|TestCheckpointKeepsUnknown|TestRecorded|TestConversation' -count=1
FUYU_LIVE_CLAUDE=1 ./scripts/go test ./internal/claude -run '^TestLiveClaudeLocalCommandResume$' -count=1 -v
./scripts/build
git diff --check
```

全部通过。`TestLocalCommandMissingTerminalReasonCompletesOnlyWithEvidence` 覆盖成功与非零退出、无 result、错误 result、未知原因和缺失原生输出。`TestLocalCommandRecoveryChecksOwnershipAndStableHistory` 覆盖归属、命令、级别、末尾、活动任务、摘要变化、普通 assistant 文本及核验结果跨重开持久化。

真实 CLI 测试使用临时工作目录和临时 Fuyu 检查点目录，沿用原生登录，仅执行只读 `/model`，确认 completed 和可恢复状态；随后在测试副本中模拟旧 unknown 检查点，重建 Client，再核验恢复为 idle、历史含完成状态及本地输出。未发送模型任务，未修改原生配置。

## 用户原会话验收

1. 修改前把目标 Fuyu 检查点复制到 `/tmp/fuyu-local-command-backup.Df0UBQ/`，原生 transcript 保持原样。
2. 构建后通过原生窗口正常退出 PID 20738，启动本仓库新版 Fuyu，确认新进程 PID 28792 和原会话窗口可用。
3. 原会话自动恢复后，“重新核验原生会话”和待核验提示均消失，历史显示 `/model` 的本地输出。
4. 通过原生输入框设置临时文字“恢复验证（不发送）”，AX 确认“发送”按钮可用；没有点击发送，随后恢复原空草稿。
5. 对比备份：仅最后一条状态由 unknown 变为 completed；此前任务、历史摘要未变，原生 transcript 的实际 SHA-256 仍与摘要一致。

本次代码与验证文档保留工作区，未暂存、未提交。Claude 技能候选的原生浮层与键盘确认验收仍按其独立记录保留。
