# Git 工作流扩展验证

日期：2026-10-26。范围见 [Git 工作流设计](../git-workflow-design.md)。

## 结果与边界

全部十项工作流能力已实现，真实临时仓库、MyGo 内存渲染与交互、真实 Codex 提交信息生成通过。完整 test/vet/race 通过；最后的历史文件打开与后台刷新按钮修正后，重新执行完整 test/vet、UI race 和构建通过。

原 `bin/Fuyu.app` 使用原数据目录和最新构建，未启动第二个应用或使用另一数据目录。此前构建已核对紧凑 Git 列表、行内/并排 Diff、分支选择器和组折叠；最终原生复核期间曾持续读取超时，接续后恢复可读。本次实际完成历史分页、提交内文件切换、打开实际文件、窄区回退、并排双向滚动联动及修改块导航，详见末节。新增 Git 工作流验收完成；写操作由隔离仓库的实际 Git 与 MyGo 组件交互验证。

本轮未对 Fuyu 仓库执行暂存、提交、丢弃、切分支或远程写入；所有 Git 写验证均使用测试创建的临时仓库及本地 bare remote。原窗口的五个已有文件/Diff 标签保留；未发送现有聊天草稿。

## 逐项验证

| 能力 | 操作与实际结果 | 对应测试 |
|---|---|---|
| 精确暂存/取消暂存 | 中文、空格、换行和前导 `-` 文件名、unborn HEAD、同文件 staged/unstaged 分离；磁盘和未选文件保留 | `TestStageUnstageCommitAndHistoryKeepDiskAndOtherChanges`、`TestRenameUnstagePreservesMovedFile` |
| 多选批量操作 | Cmd 多选、Shift 连选、取消选择及分组隔离；点击“暂存所选”后实际 index 含所选两文件 | `TestGitSelectionAndDraftsAreSeparatedByWorkspaceAndIndexSide`、`TestGitBatchCommitAndRemoteWorkflowThroughPanel` |
| 提交与草稿 | 只提交 index，未暂存字节保留；UI 提交后核对实际 HEAD；显式全部暂存并提交；按工作区恢复提交草稿 | `TestGitWorkflowStagesOnlyChosenFileAndCommitsIndex`、`TestGitBatchCommitAndRemoteWorkflowThroughPanel`、`TestGitSelectionAndDraftsAreSeparatedByWorkspaceAndIndexSide` |
| 状态与布局 | 可读状态、增删统计、紧凑行与固定辅助区域；260/360 DIP、浅/深色、长名称、多选批量入口边界通过 | `TestGitPanelUsesContinuousCompactRows`、`TestGitPanelLightDarkNarrowWorkflowLayout`；另在原生窗口核对真实列表 |
| 分块 Diff | 两块独立暂存/取消，过时 index/磁盘拒绝；新增/删除/无末尾换行成功正反应用；重命名保留整文件操作 | `TestHunkStageUnstageAndStaleReview`、`TestNewDeletedAndMissingNewlineHunks`、`TestGitDiffHunkActionsAndSplitLayout` |
| Diff 导航与联动 | 前后块、所选块操作正确；并排任一侧滚动联动，文本点击更新块选择；历史按文件切换，打开实际工作区文件 | `TestGitSplitScrollAndSelectionFollowEitherPane`、`TestGitHistoryPerFileNavigationAndPagination`；此前构建原生并排显示通过 |
| 远程 | 本地 bare remote 真实发布、fetch、push、ahead/behind；pull ff-only 成功，分叉拒绝且本地 HEAD 保留；UI 发布/推送后核对远程 hash | `TestRemoteActionsAndWorktreeBranches`、`TestPullFastForwardAndDivergencePreserveLocalWork`、`TestGitBatchCommitAndRemoteWorkflowThroughPanel` |
| 历史 | 初始提交文件与 Diff、20 条分页追加至 23 条；单提交两文件独立显示 | `TestStageUnstageCommitAndHistoryKeepDiskAndOtherChanges`、`TestGitHistoryPerFileNavigationAndPagination` |
| 丢弃保护 | tracked 恢复至 index；untracked 保存到回收目录并核对正文；确认后磁盘变化拒绝；未保存编辑拒绝丢弃 | `TestDiscardAndConflictOperations`、`TestGitDiscardRechecksConfirmationAndProtectsUnsavedEditor` |
| 三方冲突 | diff3、CRLF、中文与块外字节解析；完整/不完整标记均拦截；真实 merge/rebase/cherry-pick 解决后继续成功；中止 merge 成功 | `TestConflictBlocksAndResolutionContinuation`、`TestDiscardAndConflictOperations` |
| 冲突 UI 闭环 | 点击冲突文件看到三个源版本，接受两者，保存并标记，实际 index 正文正确；点击继续后 HEAD 含解决结果 | `TestGitConflictAcceptSaveMarkAndContinue` |
| 分支 | 新建/切换成功，占用 worktree 拒绝原地切换；UI 未保存文档先保存再切换，缓冲保留 | `TestRemoteActionsAndWorktreeBranches`、`TestGitSaveThenBranchSwitchAndQuitPreparationGate`；此前构建原生选择器展开通过 |
| Codex 草稿 | 差异与近期格式进入生成提示；晚到结果保留手输草稿；相同 porcelain 状态但字节变化也拒绝旧结果；上下文追加会话草稿且不发送 | `TestGitMessageGenerationAndCodexDraftPreserveUserInput`、`TestGitMessageRejectsSameStatusWithChangedBytes` |
| Codex CLI 与取消 | 受控可执行文件核对 ephemeral/read-only 参数、stdin 字面量、异常返回；大输入阻塞时取消；真实 CLI 返回中文提交信息 | `TestGenerateMessageUsesReadOnlyEphemeralCLIAndStdin`、`TestGenerateMessageCancellationBoundsBlockedInput`、`TestGenerateMessageFailureIsNotAnEmptySuccess`、`TestLiveGitCommitMessage` |
| 归属与退出 | 单工作区重复写入拒绝；切换后回调归原工作区；退出等待保存准备与写入，已接受的保存后动作继续，新动作拒绝 | `TestGitUnsavedGuardAndWriteOwnership`、`TestGitSaveThenBranchSwitchAndQuitPreparationGate` |
| Git 子进程 | 等待 pre-commit hook 的后代时取消，在 3 秒内返回，暂存内容保留 | `TestCancelGitHookReturnsWithoutWaitingForDescendant` |
| 分块暂存的未保存保护 | 修改前复现直接暂存绕过确认；修正后选择磁盘版本仅暂存原磁盘块，编辑器草稿保留；保存后旧块失效并拒绝写入 index | `TestGitHunkStageConfirmsUnsavedEditorBeforeUsingDisk` |
| 占用分支入口 | 从分支选择器点击其他 worktree 的分支，打开其规范化目录；原分支、聊天和提交草稿保留 | `TestGitOccupiedBranchOpensItsWorktreeAndPreservesOriginalDraft` |
| 提交失败重试 | 隔离 hook 拒绝提交后，HEAD、index 和消息保持；移除测试 hook 后点击提交成功并显示摘要 | `TestGitCommitFailureKeepsMessageAndIndexForRetry` |

真实模型验证复用本机既有登录；在隔离临时目录运行只读 ephemeral CLI，未创建应用会话、未修改配置和凭据。实际返回 `fix: 将超时时间从 1 调整为 5`，测试耗时 27.69 秒。

## 已执行命令

```sh
./scripts/go test ./...
./scripts/go vet ./...
./scripts/go test -race ./...
FUYU_LIVE=1 ./scripts/go test ./internal/codex -run '^TestLiveGitCommitMessage$' -count=1 -v
FUYU_UI_EVIDENCE=/tmp/fuyu-git-workflow-ui-20261026 ./scripts/go test ./internal/ui -run '^TestGit' -count=1
./scripts/build
```

最后两项 UI 修正后再次执行 `./scripts/go test ./...`、`./scripts/go vet ./...`、`./scripts/go test -race ./internal/ui` 与 `./scripts/build`，均退出 0。`git diff --check` 通过，暂存区保持为空。

内存渲染的浅/深色、260/360 DIP 图片位于 `/tmp/fuyu-git-workflow-ui-20261026/`，已逐一检查最窄浅/深色图，文件名为主、目录为次，提交和批量按钮可读，无边界溢出。该目录是本轮证据缓存，不作为产品资源。

## 此前接续与原生检查阻塞记录

1. 保留当前原应用与现有标签；先恢复原生控件读取，再检查最终构建的历史列表、提交文件选择、“打开文件”定位和并排滚动。读取超时期间不连续重启应用。
2. 最后构建进程启动后无崩溃报告，原生 AX/截图读取仍超时。诊断阶段的 `lldb`、`debugserver` 和等待中的 `sample` 进程已停止；系统调试授权界面属于受限应用，工具拒绝读取，未尝试更改系统权限或输入凭据。如系统仍有该授权提示，由用户取消。
3. 本轮 HTTP/SSH 身份交互、真实托管远程推送和其他 Git 配置组合未作为验收；远程数据链路已通过本地 bare remote 实测。二进制/特殊冲突文件由外部处理，超容量显式报错。
4. 本文只验收新增 Git 工作流；P4 既有原生验收剩余项仍保留在实施计划和原 P4 验证中。

### 本次继续核验

继续核对同一进程 PID 46263，原生 AX 与截图读取仍返回 `timeoutReached`，未因超时关闭或重启应用。独立核查发现分块暂存绕过未保存确认，已用新增测试先复现失败再修复；同时补验占用分支的实际 UI 入口和 hook 失败后显式重试。最终原生复核仍保留待办，当前不能宣告完整验收完成。

本次补丁后完整 test、vet、UI race、构建与 `git diff --check` 均退出 0；UI 普通测试 11.321 秒，UI race 53.223 秒。最终 bundle 已更新，当前运行实例尚未加载该补丁；待原生读取恢复后正常退出并重开原应用再验收。

再次接续时，同一 PID 46263 仍存在，`getAXStateAndScreenshot()` 再次返回 `timeoutReached`，工作区和暂存状态未变，`git diff --check` 通过。同一原生验收阻塞已跨三轮持续；本轮未重启应用、未修改系统授权，任务标记为阻塞而非完成。恢复条件为原窗口重新可被工具读取；随后加载最终补丁并执行上述原生待办。

### 最终原生复核完成

2026-10-26 接续后，原窗口的 AX 与截图均恢复可读。进程 PID 12496 的启动时间为 13:40:31，原 bundle 二进制构建时间为 11:43:24，当前实例已加载最终补丁。本次未因观察超时重复重启应用，也未更改系统授权。

实际操作与结果：

- 展开最近提交后出现 20 条真实提交，点击“加载更多提交”追加 8 条，总计 28 条。
- 打开 `e469f34de3b5a99e6d619af210f311ccaec4735e`，文件菜单列出该提交的五个文件；从 `docs/implementation-plan.md` 切到 `internal/ui/editor_view.go` 后，路径和正文同步变为所选文件的历史差异，共四个修改块。
- 点击“打开文件”得到真实路径 `internal/ui/editor_view.go` 的可编辑标签，正文为当前工作区源码，保存按钮因没有编辑而禁用。
- 两侧栏展开时正文较窄，选择并排后仍用行内显示；收起左侧栏后显示原版本和新版本两栏。分别在左栏、右栏向上滚动，两侧均同步移动，块标题和上下文行对齐。
- 点击“上一个修改”，计数从 `4/4` 变为 `3/4`，两栏同时定位到 `@@ -329,7 +329,7 @@` 并显示选中块背景。
- 关闭本次新增的普通文件与历史 Diff 标签，恢复左侧栏、收起最近提交并返回 Git 列表；原有五个文件/Diff 标签保留，未编辑或发送聊天草稿，Fuyu index 未被改动。

本次补齐此前剩余原生检查；新增 Git 工作流验收完成。浅/深/窄渲染、写操作、错误重试及保护分支的详细自动化证据见上表；真实托管远程身份交互与原 P4 的其他验收边界保持独立。
