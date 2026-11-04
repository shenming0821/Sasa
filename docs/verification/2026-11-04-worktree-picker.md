# Worktree 文件夹点击进入选择面板

## 行为与原因

按用户要求，点击侧栏 worktree 文件夹在该目录没有打开页签时不再自动按上次使用的 Agent 新建会话，改为显示面板的“选择面板内容”入口（新建 Codex 会话、新建 Claude 会话、新建终端）。已有打开页签或已保存的多分屏布局时，仍恢复上次选择。新建 Worktree 弹窗创建成功后同样进入新目录的选择面板，不再直接进入空白会话。

`openWorktree` 在 `restoreFolderTab` 未命中（无打开页签、无多分屏布局）时调用新的 `openWorkspacePicker`：选中工作区并保留聚焦面板的空选择态，不创建会话、不启动 Runtime。关闭的会话、草稿与后台任务保持不变，历史入口仍可主动重开。

## 自动化

- `TestFolderClickShowsPickerWithoutOpenTabs`（原 `TestFolderClickCreatesSessionAfterLastTabClosed`）：覆盖空白会话与已有标题/thread 两种已关闭会话；点击工作区后不创建会话、显示选择面板，关闭会话草稿保留；经“新建 Codex 会话”进入该目录，再次点击恢复打开页签，显式重开历史保留状态。
- `TestRepositoryImportGroupsBranchesAndSessions`：点击无页签 worktree 先断言选择面板与原会话保持，再经面板入口创建会话并核对 cwd、草稿隔离与多会话归属。
- `TestWorktreeCreationMenuFormAndNavigation`、`TestWorktreeCreationRefreshFailureRetriesOnlyDiscovery`：创建成功与“重试刷新”成功后进入新目录选择面板，原会话与草稿保留，不新增会话。
- `./scripts/go test ./...` 通过；`./scripts/go vet ./...`、`./scripts/build`、`git diff --check` 通过。
- 环境变量门控的 `TestLiveProjectWorktreesReadOnly` 已同步改为点击后显式建会话，本轮未实跑真实仓库。

## 原生窗口

本轮未做原生验收；构建后需人工核对：点击无页签 worktree 显示三个新建入口，入口创建会话归属该目录，已有页签的 worktree 点击仍恢复上次选择。
