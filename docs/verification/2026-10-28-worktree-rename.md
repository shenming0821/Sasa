# Worktree 标题重命名验证

日期：2026-10-28。范围：项目列表内主工作区和关联 worktree 的显示名称。

## 行为与边界

右键工作区卡片选择“重命名”，编辑弹窗预填当前名称。保存按钮或 Enter 提交，留空恢复文件夹名称；取消、Escape、Command+W 或点弹窗外部放弃编辑。保存失败时保留旧名称、草稿与弹窗。别名按绝对路径保存在 settings JSON，刷新和重启保留，可用于搜索。名称只改变卡片标题，路径、分支、会话归属和草稿保持原状。

## 自动化验证

`internal/ui/worktree_rename_test.go` 使用临时 Git 仓库、真实 Git worktree、隔离 SQLite 和 MyGo 内存渲染器，覆盖菜单、取消、键盘保存、两端空白、恢复默认、搜索、会话路径与草稿保留、同名工作区独立保存、后台刷新、重启恢复、快照隔离、存储失败回滚及失效路径拒绝。真实窗口操作单独记录。

首次定向测试发现 Command+W 未在模态作用域内被接收，已在弹窗内部处理，回归通过。

以下检查通过：

```sh
./scripts/go fmt ./internal/ui ./internal/storage
./scripts/go test ./internal/ui -run 'TestWorktreeRename' -count=1
./scripts/go test ./...
./scripts/go vet ./...
./scripts/go test -race ./internal/ui -run 'TestWorktreeRename' -count=1
./scripts/build
git diff --check
```

本轮全量普通测试各包均通过，包括先前记录中曾超时的 Git 包；此前失败证据继续保留，不能据本次通过推断已修复历史不稳定问题。

## 原生验证

构建完成后，通过 Command+Q 正常退出旧 Fuyu，确认原进程结束，再打开 `bin/Fuyu.app`。新进程 PID 63238 存在，但原生窗口工具多次读取超时，重置连接后仍超时，无法确认窗口可操作性，未执行真实重命名或修改用户显示名称。应用清单确认 Fuyu 正在运行；进程采样保存在 `/tmp/fuyu-worktree-rename-sample.txt`，主线程采样停在系统 `open`，具体原因未确认。本轮保留当前进程，不继续重复重启。

后续先确认实际窗口是否可操作，再右键展开的工作区卡片验证保存、取消、搜索、刷新和正常重启后的恢复，验收后恢复原显示名称。普通测试与 SQLite 恢复测试不代替此项原生验收。
