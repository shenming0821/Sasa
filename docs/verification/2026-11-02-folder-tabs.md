# 按文件夹展示页签

2026-11-02 实现顶部混合页签按文件夹过滤。每个 worktree 独立分组，子目录会话归入所属 worktree；普通目录各自成组。所有页签继续保存在同一集合，切换仅改变展示与选择，保留后台任务、草稿、文件编辑及终端 shell。

各文件夹记住上次选中页签，在 settings JSON 的 `FolderSelectedTabs` 中持久保存，保持数据库 schema 4。恢复时验证归属和打开状态，兼容旧设置；当前文件夹为空时保留空工作区。关闭当前页签先选择同组前一页，没有前一页则选择后一页，最后一页关闭后不切换文件夹。文件重命名同步迁移选择 ID，后台识别出 worktree 后重新核对当前分组。

worktree 名称和普通目录标题保持原文字起点，6 DIP 主题成功色绿点通过 8 DIP 宽的绝对定位容器绘制在左侧既有缩进中，不占用文字行空间；状态说明为“有打开的页签”。普通目录标题继续只控制展开/收起，worktree 卡片恢复本组选择，具体会话和历史入口仍直接进入指定会话。

## 自动化验证

名称位置调整：`TestFolderTabsIndicatorPreservesOriginalNameOrigin` 覆盖 worktree/普通目录与浅深色四组，确认侧栏默认尺寸下名称起点仍为原来的 X=37 DIP，绿点位于名称左侧，绿点消失后文字位置和宽度均保持原值。`./scripts/go test ./internal/ui -run 'Test(FolderTabsIndicator|RepositoryNavigationLayout|WorktreeCardHover)' -count=1`、UI vet 及应用构建通过。原生位置复验见下方补充。

以下检查通过：

- `./scripts/go test ./...`，全量通过，UI 包 21.225 秒。
- `./scripts/go vet ./...`。
- 补齐 Diff 与目录发现用例后，`./scripts/go test ./internal/ui -run '^TestFolderTabs' -count=1` 和对应 `-race` 检查均通过。
- `./scripts/go test -race ./internal/ui ./internal/storage -run 'Test(FolderTabs|CloseTabs|CloseMenu|CloseShortcut|RestartRestoresOpenTabs|ProjectNavigation|RepositoryImport|P4HistoryFilter|VersionTwoBackupAndMixedTabs|EmbeddedTerminal)' -count=1`，UI 与存储均通过。
- `./scripts/build`。
- `git diff --check`。

`folder_tabs_test.go` 使用临时数据目录、受控后端及 MyGo 内存渲染器，覆盖 A 两页/B 三页过滤、同组前后回退与末页空工作区、后台任务及草稿保留、同仓库独立 worktree、子目录归属、会话/文件/Diff/终端混合选择、脏文件取消关闭、每组选择及空工作区重启恢复、旧设置与失效 ID、隐藏分组历史延迟加载、浅深色绿点像素与标题几何稳定、普通目录标题折叠行为。已有跨文件夹导航测试同步使用文件夹切换入口；后台会话关闭仍核对审批归属和完成事件。

## 原生窗口验收

构建后通过 Cmd+Q 正常退出原实例，窗口恢复到新版 PID 67531。首次辅助功能读取两次超时，随后同进程窗口可正常读取；采样主线程处于 AppKit 事件循环，未据超时判定应用崩溃。

在默认数据目录中使用已有 Fuyu 与 Playground 工作区，分别打开两页与三页，原生截图确认 Playground 仅显示三页，两处工作区名称前均有绿点；切回 Fuyu 后确认仅显示两页。验收只读取已有历史并创建空白会话，没有发送模型任务。

再次 Cmd+Q 正常退出，确认进程退出后直接启动已构建程序，PID 73542 窗口可用，Fuyu 两页恢复。后续检测到用户在窗口中继续操作，停止原生操作并保留其当前状态。原生重启后上次选中页的完整核验、深色绿点及真实后台模型任务切换未继续执行；每组选择、深色绿点和后台状态保留已有自动化覆盖。

名称位置修正后通过 Cmd+Q 正常退出，确认旧进程退出后直接启动最终构建，PID 91022 窗口可用。原生截图确认工作区 Fuyu 名称恢复与仓库 Fuyu 名称相同的文字起点，绿点位于左侧缩进中，分支和会话摘要位置保持原布局；普通目录名称与文件夹图标恢复原位置。本次没有切换会话或发送任务，保持重启前的空工作区。
