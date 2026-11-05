# Git 面板紧凑布局

日期：2026-10-26。

## 调整

原列表将标题与文件行放在同一个 `Gap(14)` 的 Column 下，文件行自身为 36 DIP，连续文件实际步进为 50 DIP。现在每个分组使用独立的零间距 Column，文件行 28 DIP、标题 26 DIP，组间 8 DIP。

分支工具栏由 42 DIP 调为 36 DIP，上游行底部留白由 10 DIP 调为 4 DIP。分组名称使用 12 DIP 粗体、数量独立使用 11 DIP 次要色；文件名保持 12 DIP，目录为 10 DIP 次要色，右侧 Git 原始状态使用固定 18 DIP 宽度的 10 DIP 等宽字。沿用主题色和全局列表交互，继续打开对应只读 Diff。

## 验证

- `TestGitPanelUsesContinuousCompactRows`：260 / 360 DIP 面板下，文件行高及连续步进均为 28 DIP，标题高 26 DIP，组间 8 DIP；长文件名不改变行边界，折叠后无残留文件占位。
- 既有 `TestP4Git*`：状态读取、工作区与暂存区 Diff、索引监听、后台刷新位置与折叠保持通过。
- `./scripts/go test ./...`、`./scripts/go vet ./...`、`./scripts/go fmt ./...`、`./scripts/go test -race ./internal/ui -count=1`（27.81 s）、`./scripts/build`：通过。
- 原 `bin/Fuyu.app` 正常退出并重开；原生截图确认右侧 Git 面板使用紧凑连续列表，文件名、目录、状态及分组数量均可见，当前显示真实工作区修改。

本轮改动已提交；此项不代表文件编辑与 Git 全部验收完成。
