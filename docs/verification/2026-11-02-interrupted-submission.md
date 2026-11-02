# 快速中断后的消息送达

日期：2026-11-02。

## 故障与原因

原始 Fuyu 会话中的第一条 `say ok` 于北京时间 17:27:30 建立轮次，约 1.9 秒后收到原生 `interrupted` 终态。只读 `thread/read(includeTurns=true)` 返回该轮 `items: []`，后续“继续”的模型上下文没有这条输入。第二条 `say ok` 包含原生用户条目并正常回复 `ok`。

Fuyu 在本地 `Begin` 时直接创建普通完成态用户气泡，并将轮次建立当成任务开始；原先既不处理 `userMessage` 确认，也不在快速中断后核验输入是否进入原生记录，导致本地显示与实际上下文不一致。现有记录能证明本轮被中断及输入缺失，无法单凭该记录确定当时的点击来源。

## 实现

- Codex 本地输入以普通用户气泡呈现，在后台等待同轮原生用户条目确认送达。
- 轮次终止后仍未确认输入时读取完整原生记录，暂停下一次发送；空条目轮次恢复原文字与图片，并将气泡标记“未发送”。
- 已送达的输入不恢复为待重发内容；已有新草稿时保留新文字与图片。
- 核验失败、缺少轮次或完整条目时保留不确定性，允许重新核验；不自动重新执行任务。
- 明确未提交的准备阶段取消与失败同样显示未发送；切换会话后回调仍只更新原会话，恢复草稿沿用既有存储。

## 自动化验证

- `./scripts/go test ./...`：通过。
- `./scripts/go vet ./...`：通过。
- `./scripts/go test -race ./internal/app ./internal/ui ./internal/codex`：通过；后续小改再次执行消息送达、准备取消相关定向 race。
- `./scripts/build`：通过。
- `TestInterruptedSubmissionReconcilesDelivery`：完整条目含有/缺少用户输入，空草稿/已有新草稿，文字和图片恢复、避免重复输入以及清除旧提示。
- `TestSubmissionRequiresNativeUserItem`：轮次建立不等于输入送达，同轮用户条目确认后不误判未发送。
- `TestIncompleteSubmissionSnapshotKeepsUncertainty`：缺少条目、非完整视图、无效条目和其他轮次均不误恢复。
- `TestInterruptedDeliveryCheckPreservesSessionAndDraft`：核验期间暂停发送、核验失败后的重新核验、切换会话、气泡状态和恢复草稿持久化。
- 既有准备期间取消、发送交付竞争及其他 UI 回归通过。

上述 UI 断言使用 MyGo 内存渲染器，不代表原生窗口验收。

## 真实 Runtime 验证

实际 CLI：`codex-cli 0.165.0`。临时目录真实调用的模型值为 `codex/gpt-6.1-sol`，审批策略为 `never`，沙箱为 `dangerFullAccess`。使用现有原生配置与模型服务，自有 App Server 在测试退出时关闭。

`TestLiveCodexInterruptedHistoryDelivery` 通过环境变量指定原始故障 thread/turn，只读原生历史：确认空条目终态经修复后的投影标记为未发送，并恢复 `say ok`。没有向原始会话发送消息或修改原生记录。

`TestLiveCodexEarlyInterruptDelivery` 在系统临时目录创建新会话，观察到 `turn/started` 后立即请求停止。本次原生输入已写入，修复正确保持已送达且不恢复待重发内容；下一轮 `say ok` 正常获得 `ok` 和原生用户输入确认。快速中断能否赶在输入写入前取决于 Runtime 调度，本次临时会话没有复现空输入分支，该分支另由原始故障记录及受控回归验证。

真实调用初次验证发现 `turn/start` 响应后立即请求停止可能返回 `no active turn to interrupt`；测试随后按原生 `turn/started` 发起中断。此现象不作为本轮已解决行为的证据。

## 限制与接续

Mac 当前锁屏，Computer Use 无法读取或操作 Fuyu 窗口；保留现有应用进程，未强制终止。解锁后正常退出并重启构建产物，确认窗口可用，再验证真实窗口快速停止后的气泡标记、输入恢复、已有草稿保护和后续重新发送。

原生 Runtime 取消发生在输入写入之前时，该输入仍不会出现在模型上下文中。本轮修复使界面与原生记录一致，并保留可重新发送的内容。

## 送达确认提示精简

按用户要求，移除用户气泡下方的“等待送达确认”文字，待确认输入保持普通气泡；后台核验、明确未发送的标记及草稿恢复沿用原逻辑。

- `./scripts/go test ./internal/app ./internal/ui -run 'Test(Interrupted.*|Submission.*|IncompleteSubmission.*|.*Bubble.*)' -count=1`：通过。
- `./scripts/go vet ./internal/ui`、`./scripts/build`：通过。
- 正常退出时原生提示当前会话仍有 1 个任务运行，已保留退出确认框；本轮结束后需重启加载新版，尚未完成新版原生窗口验收。
