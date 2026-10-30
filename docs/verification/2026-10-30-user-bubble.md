# 用户气泡留白调整

日期：2026-10-30。

按用户参考图将 `message` 中用户纯文本气泡的上下内边距从 16 调整为 8 DIP，左右从 20 调整为 16 DIP。字号 15 DIP、行高 24 DIP、字重 400、圆角 20 DIP、最大宽度 85% 和右对齐保持原值。文字、换行、选择及图片附件展示沿用现有行为。

`./scripts/go test ./internal/ui -run 'TestUserBubble|TestConversationLayout|TestConversationNavigation' -count=1`、UI vet、构建与差异检查通过。既有用户长文本回归按新的上下留白调整最小高度约束，覆盖浅深色、320/640/1000 DIP 窗口、中文/英文/路径与原始换行。

正常退出旧实例后重启新版 PID 26074，CoreGraphics 确认 1000×760 主窗口可见。原生截图 `/tmp/fuyu-user-bubble-20261030/native-light.png` 核对现有单行用户消息的浅灰气泡与紧凑内边距。保留当前会话供用户查看；原生深色及长消息观感本次未单独验收。
