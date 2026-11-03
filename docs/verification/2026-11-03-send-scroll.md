# 发送消息后滚动到底部

2026-11-03：发送入口此前仅追加消息，向上阅读历史后发送仍保留旧位置。普通发送在本地检查和状态保存通过后清除临时滚动保持，调用 MyGo `ScrollToEnd` 并恢复跟随；文字、图片与失败重试共用入口。历史消息编辑保持既有定位规则。

通过：

- `TestSendReturnsToLatestAndResumesFollowing`：使用受控后端，覆盖历史阅读后发送、临时保持状态下发送、新回复底部跟随及再次向上阅读后的流式保持。
- `./scripts/go test ./internal/ui`、`./scripts/go vet ./internal/ui`。
- `./scripts/go test -race ./internal/ui -run 'TestSendReturnsToLatestAndResumesFollowing|TestReadingWhileMessageGrows|TestTurnProcessDisclosureKeepsReadingPosition|TestMessageEdit' -count=1`：同时回归历史编辑与执行过程折叠的阅读位置。
- `./scripts/build`、`git diff --check`，新增 Go 文件通过 gofmt 格式检查。
- 通过原生 Cmd+Q 正常退出旧进程 PID 17773，重新打开后 PID 19087；已确认新版窗口、输入框及历史面板可用。

组件输入与受控后端不代表真实模型验收；真实模型发送后的原生滚动待用户验收。文字、图片和失败重试的入口复用由源码核对，图片发送未另做原生操作。
