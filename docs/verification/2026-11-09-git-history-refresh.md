# 最近提交刷新按钮

日期：2026-11-09。

## 原因与修改

最近提交区域只能依赖提交、远程等动作后的自动重读，无法在打开面板后直接获取最新提交。

标题行改为与文件分组相同的结构：整行悬停/聚焦高亮，左侧折叠按钮占据剩余宽度，右侧保留 24 DIP 动作槽，常驻显示刷新图标按钮，点击重新读取第一页提交并清理已移除提交的展开缓存。按钮沿用顶部工具栏同一刷新图标，Git 忙碌或历史读取期间禁用。

## 验证结果

- 新增 `TestGitHistoryHeaderRefreshReloadsNewCommits` 通过：展开历史后外部新增提交，点击常驻的“刷新提交历史”，列表更新为新提交在前；等待条件同时覆盖初始状态读取完成，避免与刷新按钮的加载禁用态竞争。
- `./scripts/go test -race ./internal/ui -run 'Git' -count=1` 通过（51.957 秒），覆盖历史吸底、独立滚动与既有 Git 交互回归。
- `./scripts/build preview` 生成候选 `20261109T083001Z-c349f5423f-39a852`，`./scripts/run-preview` 重启 Preview，原生窗口标题核对为对应构建 ID。
- 点击热区沿用分组标题既有结构，未单独做原生像素验收；按钮在初始读取与 Git 忙碌期间禁用为逻辑入口状态，未逐状态截图。

`internal/ui/app.go`、`internal/ui/models.go` 存在本轮开始前未提交修改，`./scripts/go fmt` 顺带修正其缩进；`internal/ui/message_edit*.go` 的并行修改与本次无关，未触碰。
