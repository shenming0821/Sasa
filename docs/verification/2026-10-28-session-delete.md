# 删除会话验证

日期：2026-10-28。环境：macOS arm64，分支 `p6-claude-adapter`，基线 `e3c7bcb`。本轮增加 Fuyu 内删除与原生历史隐藏，保留 CLI 原始记录、检查点、项目文件和共享图片副本。

## 自动化

使用临时 SQLite、受控后端与 MyGo 内存渲染器，实际执行删除：

- `TestDeleteSessionRemovesTabsAndSurvivesRestart`：当前、后台、已关闭、最后一条会话；标签与索引移除、其他草稿保留、重启不恢复被删记录、历史标记按 Agent/Runtime 隔离。
- `TestDeleteSessionConfirmationAndRecheck`：确认、取消、打开对话框失败、确认期间开始运行或身份改变；核对确认与取消按钮的索引映射。
- `TestDeleteSessionProtectsPendingWork`：运行、恢复、待核验、分叉、持久化活动标记、图片导入和恢复阻塞时保留原会话。
- `TestDeleteSessionFailureKeepsOriginalState`：数据库写入不可用时保留原会话、草稿、标签和历史展示。
- `TestDeleteSessionCommitsIndexAndHistoryFilterTogether`：设置验证失败时回滚索引删除；成功时索引和隐藏标记同时提交。
- `TestDeleteHistoryMenusFilterAndLateOpen`：历史菜单与标签右键入口，确认期间打开并运行的会话受到保护，删除后已有缓存中的目标不再显示，其他记录保留。
- `TestDeleteClaudeSessionKeepsOtherAgentAndRuntimeHistory`：相同原生 ID 的 Claude 与 Codex 会话分别归属，隐藏仅影响对应 Runtime。
- `TestDeleteLastSessionKeepsActiveUnsavedFile`：删除最后一条聊天时，当前编辑文件和未保存状态保留。

针对性普通测试已通过。首次完整 `./scripts/go test -race ./... -count=1` 中，其他包通过，UI 的既有 `TestGitHunkStageConfirmsUnsavedEditorBeforeUsingDisk` 在第 668 行出现 `P4 state did not settle`，与 P6 前轮记录的偶发超时一致。单独复跑及最终验证结果见下方补记。

## 初版原生窗口

首次 `./scripts/go vet ./...`、`./scripts/build` 和 `git diff --check` 通过。通过 Cmd+Q 正常退出，确认旧 Fuyu 进程已退出，再打开新 bundle 并读回原生窗口：

1. 当前 Claude 会话 40 及历史列表保留；右侧每条历史出现“历史会话操作”省略号菜单。
2. 点击 thread `166d0e64-85d6-4658-adf4-31b4f271b834` 的操作菜单，显示“删除会话…”。
3. 点击该项，原生确认 sheet 展示目标标题、草稿和标签移除范围、不可撤销性，以及保留项目文件和 CLI 原始记录的说明。
4. 点击“取消”，sheet 关闭，会话 40、`FUYU_MODEL_PREF_OK` 回复和对应历史仍然显示。
5. 右键会话标签 40，确认存在“删除会话…”入口，随后 Escape 收起菜单。

原生检查只执行打开确认与取消，未实际删除用户环境的会话；实际删除、失败回滚和重启不恢复由隔离自动化覆盖。未增加真实模型调用，未改变 Runtime 协议。

## 最终补记

- `./scripts/go test -race ./internal/ui -run 'TestDelete' -count=1` 通过，包含最后补充的 Claude 边界和未保存文件保护用例（3.433s）。
- 包含既有 Git 测试的针对性 race 复跑仍在该 Git 用例第 668 行超时；该命令中的 storage 通过。
- 使用 `git archive HEAD` 将未修改的基线 `e3c7bcb` 导出到 `/tmp/fuyu-delete-baseline.6htN18`，在该目录单独执行 `./scripts/go test -race ./internal/ui -run '^TestGitHunkStageConfirmsUnsavedEditorBeforeUsingDisk$' -count=1`，同样第 668 行超时（7.616s）。此失败在未修改基线上复现，本轮保留该既有问题，不宣称完整 race 通过。
- 首次完整普通 `./scripts/go test ./... -count=1` 全包通过（UI 15.289s）；最后补充用例后的完整普通复跑中，相同 Git 用例第 668 行超时（UI 20.419s），其他包通过。
- 最终 `./scripts/go test ./... -skip '^TestGitHunkStageConfirmsUnsavedEditorBeforeUsingDisk$' -count=1` 全包通过（UI 16.905s），仅跳过上述已在基线复现的用例。
- 最终 `./scripts/go vet ./...`、`./scripts/build`、`git diff --check` 通过。再次通过 Cmd+Q 正常退出、核对旧进程退出、打开最终 bundle，读回会话 40、原回复和历史省略号菜单，窗口可用。
- 原生验证未删除现有数据。本轮代码与文档已提交到 `p6-claude-adapter`，未推送。

## 同日交互调整

按用户要求，会话历史行通过右键菜单提供操作，整行保持打开会话用途；标签、目录会话及历史右键菜单统一显示“删除会话”。macOS 确认框将取消放左侧、删除放右侧，Escape 对应取消。

原生 NSAlert 会结合默认操作安排按钮位置，单独交换 `Buttons` 数组无法保证左右顺序。最终使用 `Buttons=[删除会话,取消]`、`DefaultButton=0`、`CancelButton=1`，返回索引 0 确认删除，索引 1 取消。Enter 对应确认删除，Escape 对应取消。

`./scripts/go test ./internal/ui -run 'TestDelete|TestThreadHistory|TestResponsivePanels|TestProjectNavigation' -count=1` 通过（1.649s），覆盖确认/取消的返回索引、菜单文字、历史行操作按钮移除以及原有分页与导航。`./scripts/build` 通过。

正常退出并确认旧进程结束后打开最终 bundle，原生读回历史行已仅保留打开会话入口；右键历史行及会话标签均显示“删除会话”。打开标签的确认框并截图，实际按钮为左“取消”、右“删除会话”；按 Escape 关闭确认框后，会话 40、原回复与对应历史保留。原生验证未实际删除会话。上方初版原生证据仅记录当时界面，当前交互以本节为准。
