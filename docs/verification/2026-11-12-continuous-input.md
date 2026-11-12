# 连续发送验证

## 行为与范围

Codex 运行中继续发送补充，沿用当前 turn；Claude Code 与不提供追加能力的 Runtime 排队至当前轮正常完成。两者提交后立即显示普通用户消息气泡，后台确认时合并原生记录、保持气泡身份和位置；正常发送过程无额外状态提示。暂停、失败和结果待核验时在对应消息下提供反馈与处理入口。两者共用输入框、按执行状态与输入文字切换发送/停止的单个按钮，以及每会话独立持久化。修改保留工作区，由 Preview 验收后决定后续交付。

## 协议与真实 Runtime

2026-11-12 使用本机 `codex app-server generate-json-schema` 输出的 `v2/TurnSteerParams.json` 与 `TurnSteerResponse.json`，核对 `threadId`、`expectedTurnId`、`clientUserMessageId`、`input` 及响应 `turnId`。客户端 ID 不等于原生用户消息 ID，不能用两者相等确认送达。

真实文本测试在临时工作目录启动自有 App Server，首条要求解释 Go 并发模型，在输出期间连续发送两条带独立标记的补充。原生会话 `01a1ce55-12de-7fe0-a057-388f95ed7391` 只有一个已完成 turn、三条不同原生 ID 的用户消息，最终回复同时包含两条补充指定的标记。首次测试曾因错误假定客户端 ID 等于原生 ID 而报失败；修正该假定后，使用 `ReadThread` 重新核验同一真实会话通过。另一次独立实测在 90 秒内未完成，测试超时后关闭自有进程；该次不作为成功证据。

可复核命令：

```sh
FUYU_LIVE_STEER=1 ./scripts/go test ./internal/codex -run '^TestLiveContinuousSteer$' -v -count=1
FUYU_LIVE_STEER=1 FUYU_LIVE_STEER_THREAD=<上述真实会话ID> ./scripts/go test ./internal/codex -run '^TestLiveContinuousSteer$' -v -count=1
```

以上真实证据覆盖 Codex 文本追加；真实 Claude 排队、真实图片与技能追加另行验收。原生窗口启动与键盘操作以当次 Preview 的实际观察结果报告。

## 自动化入口

`internal/codex/steer_test.go` 与协议夹具核对活动任务前置条件、客户端消息 ID、文本、技能和图片；`internal/app/followup_test.go` 核对同轮多条输入、重复/分阶段事件、快照消息身份与单轮计时。`internal/ui/followup_test.go` 覆盖连续输入与顺序、跨会话回调归属、拒绝/断连不重发、相同文本逐条确认、排队后保留新草稿、停止后暂停、重启不重放、单按钮在输入和清空文字时切换发送/停止、计时及过程折叠。

```sh
./scripts/go test ./...
./scripts/go vet ./...
./scripts/go test -race ./internal/app ./internal/codex ./internal/ui -run 'Followup|Steer|UserInputs|ComposerAndSendGates|Timing' -count=1
./scripts/build preview
./scripts/run-preview <构建ID>
```
