# 项目在 Finder 中打开

日期：2026-11-03。

## 行为

- 普通项目名称右键提供“在Finder中打开”和“移除项目”。
- Git 仓库下各 worktree 文件夹卡片右键提供“在Finder中打开”和“重命名”，使用卡片的实际路径；仓库总标题保留“移除项目”。
- 先检查目标为现存文件夹，再通过已有 MyGo `Shell.OpenPath` 调用 macOS NSWorkspace 打开。目录缺失、已变成文件或系统打开失败时沿用文件操作错误弹窗，显示实际路径和原因。
- “移除项目”继续只隐藏 Fuyu 项目入口，实际文件及历史会话保留。

## 自动化验证

- `./scripts/go test ./internal/ui -count=1 -timeout=180s` 通过，20.973 秒。
- `./scripts/go vet ./internal/ui`、`./scripts/build`、`git diff --check` 通过。
- 现有项目菜单、整行交互与重命名回归检查新的菜单组成，仓库总标题不增加 Finder 操作。
- `TestProjectFinderReportsMissingClickedDirectory` 通过受控 UI 分别点击普通目录、主工作区和第二个工作区的 Finder 菜单，确认错误归属点击目录且项目顺序不变；不启动真实系统应用。

## 原生验收

通过 Cmd+Q 正常退出旧 Fuyu，进程查询确认已退出，直接启动新构建。新版窗口可用。

- Playground 工作区卡片右键出现“在Finder中打开”和“重命名”；选择后 Finder 的 Playground 窗口显示 `/Users/shenming/Documents/Playground/` 的实际内容。
- Fuyu 工作区卡片执行相同操作后，Finder 打开 `/Users/shenming/Documents/Sync/AI/Fuyu/`，显示仓库文件，确认使用所点击卡片对应目录。
- 已不存在的普通测试目录 `first` 右键出现“在Finder中打开”和“移除项目”；点击 Finder 操作后显示包含完整目标路径和 `no such file or directory` 的错误，收起提示后窗口可继续操作。

真实打开覆盖两个现存主工作区；关联分支工作区的路径绑定由受控 UI 回归覆盖。没有新建验收项目或发送模型任务。
