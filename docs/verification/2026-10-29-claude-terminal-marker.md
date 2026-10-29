# Claude 已结束任务的中断标记恢复

## 现场与原因

普通目录 `fuyu-p6-native-Z4rXN0` 下的会话 34 显示“旧任务待核验”，输入暂停，删除菜单禁用。原生 AX 确认会话已能进入，重新核验后仍停留在待核验。

只读检查实际 Claude 检查点与原始历史：最后一轮检查点状态为 `failed`，原始记录摘要与检查点一致；末尾 `[Request interrupted by user]` 与最后一次用户输入具有相同 `promptId`。历史解析仅在 `interrupted` 检查点下识别该附加标记，因此误建了一轮 `inProgress`，触发恢复和删除保护。

## 修改与验证

- 已确认 completed、failed、interrupted 的任务均将同 promptId 的标准中断标记归入原任务，保留原终态。
- unknown、不同 promptId、具有自身发送检查点的同文输入继续核验，保留现有恢复与删除保护。
- 扩展 `TestInterruptedTranscriptMarkerKeepsConfirmedTurn`，修复前 failed/completed 两例均出现额外 inProgress 轮次，修复后通过；不同 promptId 与 unknown 分支继续保持未完成。
- `./scripts/go test ./internal/claude ./internal/ui -count=1`、对应 vet、上述回归的 race 检查、构建与差异检查通过。
- 未修改实际会话检查点、原始历史或删除会话，未发送模型请求。

## 原生复核

首次正常退出时 Mac 锁定，原生工具无法解锁。用户要求继续后正常退出旧实例，确认进程已结束，再启动新构建，PID 87529。

- 会话 34 自动恢复后历史可读，“旧任务待核验”及暂停发送提示消失，最后一轮保留失败结果并提供“重试本次任务”。
- 原生右键菜单确认“删除会话”可用；未点击删除，也未执行实际删除。
- 切换到会话 61 后再进入会话 34，正常显示原历史。
- 后续原生状态显示该会话新增重试结果 `FUYU_MODEL_OK`；本轮验证未主动点击重试或发送模型任务。
