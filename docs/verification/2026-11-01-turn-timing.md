# 每轮处理用时验证

日期：2026-11-01。

## 实现范围

- 发送起计时，包含准备发送、审批、重试、工具与流式输出，底部每秒更新“已处理”。
- 原生完成、失败、中断以及连接断开时冻结；终态值显示在本轮最后回复上方，无回复时置于本轮最后记录上方。断线仍保留结果待核验提示。
- 本地保存原生 turn ID 对应的起止时刻；历史恢复与分页重新关联用户消息。缺失计时的旧记录和重启前未取得结束时刻的记录不推算用时。
- 使用主题次要文字和细分隔线，保持既有消息、工具和导航稳定身份，空闲不每秒刷新。

## 自动化验证

协议与状态测试覆盖发送起点、原生开始不重置、完成/失败/中断冻结、断线、缺失开始事件、下一轮独立计时。组件测试使用 MyGo 内存渲染器与受控协议事件，覆盖浅深色、首次等待、流式持续计时、完成替换、秒/分/小时边界、空闲调度、最终回复归属、会话隔离和本地存储后重建原生消息 ID 的恢复。

验证结果：

- `./scripts/go test ./...`、`./scripts/go vet ./...`、`./scripts/build` 与 `git diff --check` 通过。
- `./scripts/go test -race ./internal/app ./internal/ui ./internal/storage` 中 app/storage 通过；UI 的 `TestGitHunkStageConfirmsUnsavedEditorBeforeUsingDisk` 在整包运行期间出现 `P4 state did not settle`，整包 UI race 本次未通过。
- `./scripts/go test -race ./internal/ui -run '^(TestTiming|TestMaintenance|TestGitHunkStageConfirmsUnsavedEditorBeforeUsingDisk)' -count=1 -v` 全部通过，包含超时的 Git 用例单独复验；保留整包运行失败记录，未修改该 Git 用例。
- 新增用时回归单独运行通过；浅深色显示来自 MyGo 内存渲染器，未调用真实模型。

## 原生窗口

构建后通过原生 Cmd+Q 正常退出原进程 PID 6232，随后重新打开，确认新版 PID 79575。原生截图确认主窗口可用；点击“使用 Codex 新建会话”后，任务输入框、模型选择和发送入口可见，焦点进入输入区，无未保存或活动任务提示。

真实模型任务中的进行中计时、完成后展示及跨重启视觉恢复留待用户实际任务验收。当前组件结果来源为内存渲染器，不作为真实模型运行时长或原生完整交互验收。
