# 项目拖拽排序与移除验证

## 项目操作统一使用右键菜单（2026-10-28）

按最新要求移除仓库及普通目录标题右侧的省略号按钮，标题右键菜单继续仅提供“移除项目”。标题填满原按钮区域，整行点击控制折叠，拖拽排序及连续悬停背景保留。此前关于更多按钮的验收记录对应历史版本。

更新既有组件测试，覆盖两类项目均无选项按钮、右键菜单内容、仓库移除、标题左右区域的连续悬停及按下背景；既有拖拽与取消测试通过。定向项目测试、完整 `./scripts/go test ./internal/ui -count=1`、`./scripts/go vet ./internal/ui`、`./scripts/build`、`git diff --check` 均通过。

首次原生验证因 Mac 锁屏暂缓。用户解锁后，已通过 Command+Q 正常退出旧实例，确认进程结束，再启动 `bin/Fuyu.app`。真实窗口及辅助功能树确认仓库和普通目录的省略号按钮均消失；分别右键 Fuyu 仓库与 live-gui 普通目录，菜单均仅显示“移除项目”。未执行实际移除，菜单已收起，窗口保留供用户使用。变更保留在工作区，未暂存或提交。

日期：2026-10-26。范围：左侧仓库与普通目录的排序、移除、重新添加和持久化。

## 实现行为

- 拖拽标题到另一个标题的上/下半部，插入线反馈放置位置；Escape 取消，点击继续展开/收起。
- 标题更多菜单和右键菜单提供上移、下移、移除项目；首尾边界禁用相应动作。
- Git 仓库和普通目录共用顺序，搜索不改变完整项目顺序。
- 移除整个侧栏项目，保留磁盘目录、worktree、会话、草稿、标签和当前选择；刷新及重启保持移除状态。
- 显式重新添加恢复原项目，放到末尾；已有可见项目重复导入保留位置。普通目录后台识别为仓库时继承位置和移除状态。

## 自动化结果

以下均通过：

```sh
./scripts/go fmt ./internal/ui ./internal/storage
./scripts/go test ./internal/ui -run 'TestProjectHeadingDragAndCancel|TestProjectMenuAndFilteredOrder|TestProjectRemovalRefreshRestartAndReadd|TestRemovedFolderPromotionDuringRefresh|TestDirectoryPromotionPreservesOrder' -count=1
./scripts/go test ./...
./scripts/go vet ./...
./scripts/go test -race ./internal/ui ./internal/storage
./scripts/build
git diff --check
```

新增 `internal/ui/project_order_test.go` 五项行为测试：

| 场景 | 结果 |
|---|---|
| 在真实 MyGo 组件中将目录拖到仓库前、将仓库拖到目录后 | 顺序符合放置方向；拖拽未触发折叠 |
| 拖拽时 Escape 取消，随后普通点击标题 | 顺序保持；普通点击仍折叠 |
| 更多菜单/右键上移与下移、首项禁用上移、搜索过滤后移动 | 菜单动作生效；未匹配项目仍在完整顺序中 |
| 菜单移除仓库 | 整个仓库及工作区从界面消失，目录分组未回添其会话 |
| 扫描进行中移除仓库，移除普通目录后再扫描 | 扫描结束仍隐藏 |
| 移除后的磁盘、会话、草稿、标签及当前选择 | 真实临时目录、worktree 和标记文件存在；原状态保留 |
| SQLite 保存并重新 AttachStore，刷新，再显式添加仓库/目录 | 移除状态恢复；添加到末尾，复用原草稿；再次恢复顺序一致 |
| 普通目录扫描进行中移除，随后完成 Git 识别 | 识别结果保持隐藏；显式重新添加恢复 |
| 已排序普通目录在后台识别为仓库 | 继承原位置 |

测试使用临时 SQLite 数据库、临时真实 Git 仓库和 worktree；没有操作用户项目或调用真实模型。

## 原生窗口复核

构建后复制应用为独立 bundle `com.shenming.fuyu.project-preview`，使用隔离数据目录 `/var/folders/8t/brc73fcs07d1slb8w2wdx66h0000gn/T/fuyu-project-preview-3926224703/data`。测试项目为同级临时目录 Alpha、Beta、Gamma，初始顺序 Alpha → Beta → Gamma。

通过原生 UI 工具实际操作：

1. 将 Gamma 标题拖到 Alpha 上半部，顺序变为 Gamma → Alpha → Beta，原折叠状态保持。
2. 打开 Gamma 更多菜单，看到“上移”禁用、“下移”“移除项目”可用。
3. 点击“下移”，顺序变为 Alpha → Gamma → Beta。
4. 正常退出隔离实例，再用同一数据目录启动，原生窗口仍显示 Alpha → Gamma → Beta，Alpha 草稿保留。
5. 正常退出测试实例，确认进程结束；原用户实例保持运行。

本次真实窗口复核覆盖拖拽、菜单和退出恢复；移除及其数据保留由自动化组件、临时磁盘与 SQLite 测试验证。原生测试以 `-codex /missing/fuyu-preview-codex` 阻止模型连接，界面连接失败提示属于测试设置，不作为真实 Codex 验收。文件编辑与 Git 其他待验收项沿用原记录。

## 标题与选项整体悬停修正

按用户反馈，将标题、展开箭头和箭头右侧的选项按钮交给外层圆角行统一绘制悬停背景；标题与菜单按钮保留独立点击和焦点，鼠标或键盘聚焦任一区域时共同显示箭头和选项。

`TestProjectHeadingUsesOneHoverBackground` 覆盖仓库及普通目录，核对选项在标题右侧、鼠标经过标题/选项区域时两侧背景像素相同、按下保持同一背景。相关项目与拖拽回归、完整 `./scripts/go test ./internal/ui`、`./scripts/go vet ./internal/ui`、`./scripts/build` 和 `git diff --check` 通过。

构建后已正常退出并重启实际 Fuyu，通过原生窗口打开 Fuyu 项目更多菜单，观察到标题、展开箭头与右侧选项处于同一连续圆角背景，菜单可用且没有触发展开/收起；随后收起菜单交给用户验证。窗口同时出现独立的项目扫描超时提示（Git 子进程被扫描上下文取消），保留上次列表；本次记录不将该提示视为扫描成功。

## 菜单精简

按最新用户要求，项目更多菜单和标题右键菜单移除“上移”“下移”及分隔线，仅保留“移除项目”。排序继续使用标题拖拽。前述包含菜单排序的记录为此前版本验收，本段为当前行为。

`TestProjectMenuAndFilteredOrder` 已调整为检查更多菜单与右键菜单精确包含唯一一项“移除项目”，并继续验证搜索后的完整顺序和移除行为。相关项目测试、`./scripts/go vet ./internal/ui`、`./scripts/build`、`git diff --check` 均通过。已正常退出并重启实际 Fuyu，打开项目选项菜单，原生辅助功能树确认只出现“移除项目”，随后收起菜单交还用户。

## 移除无目录占位分组

按用户要求，项目列表仅收录有工作目录的会话；空 cwd 会话不再生成“未选择项目”分组，适用于新建与重启恢复后的数据以及搜索结果。此前无法移除是因为它作为空 cwd 的特殊占位项被排除在移除操作之外；现在直接在项目分组入口过滤，保留原会话、草稿及标签，不修改存储记录。

新增 `TestProjectNavigationOmitsUnboundSessions` 覆盖空目录会话无分组、普通项目继续呈现、搜索不会回添占位，以及原草稿/打开标签保留并可从标签进入。首次测试误用了子串匹配（“展开项目 ”同时匹配真实项目标题），改为精确匹配文本后通过。完整 `./scripts/go test ./internal/ui`、`./scripts/go vet ./internal/ui`、`./scripts/build`、`git diff --check` 均通过。

已正常退出并重启 Fuyu。原生窗口重启前存在无目录的“展开项目 ”按钮，重启后该按钮消失，五个原有实际项目与会话标签 2、3 保留；实际辅助功能树确认修复生效，窗口留给用户验证。
