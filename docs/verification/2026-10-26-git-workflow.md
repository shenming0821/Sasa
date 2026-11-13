# Git 工作流扩展验证

日期：2026-10-26。本文保留当时的验证范围与结果，当前行为见[文件、编辑器与 Git](../subsystems/editor.md)。

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
4. 本文只验收新增 Git 工作流；文件编辑与 Git 既有原生验收剩余项仍保留在实施计划和原文件编辑与 Git 验证中。

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

本次补齐此前剩余原生检查；新增 Git 工作流验收完成。浅/深/窄渲染、写操作、错误重试及保护分支的详细自动化证据见上表；真实托管远程身份交互与原文件编辑与 Git 的其他验收边界保持独立。

## Git 列表交互优化

后续调整：增删数字移动到悬停操作之后、行尾状态字母之前，保留 0 值隐藏规则。操作提示只显示动作名称，控件的无障碍标签保留路径用于区分目标文件；“丢弃改动”入口、确认和结果提示统一为“撤销改动”。更新布局回归断言，核对操作区、统计、状态的排列；`./scripts/go test ./internal/ui -run 'TestGit' -count=1`、构建和差异检查通过，已正常重开原应用。

文件行保留 28 DIP，文件名 12 DIP、目录 10 DIP 并优先收缩目录。行尾固定字母状态与含义提示，增删数直接显示且分别隐藏 0 值；悬停操作依次为打开、丢弃、暂存，固定占位并共享整行背景。当前 Diff 区分工作区和暂存侧持续标记；分组数量移至右侧中性徽标，批量动作悬停或聚焦显示。

完整 `./scripts/go test ./...`、`./scripts/go vet ./...` 和 `./scripts/build` 通过。新增 `TestGitFileRowHoverKeepsGeometryAndActiveComparison` 覆盖同文件两侧的联动判定、悬停前后位置稳定、三个操作入口、背景连续、状态行尾位置和 0 值隐藏；`TestGitGroupHoverKeepsCountAndBulkActionPositions` 覆盖标题批量入口和徽标位置稳定。浅/深色、260/360 DIP 含增删数及悬停截图位于 `/tmp/fuyu-git-row-20261026/`，已检查最窄浅色与 360 DIP 深色结果。

原 `bin/Fuyu.app` 已正常退出并重开，实际窗口确认新文字层级、字母状态、数量徽标和点击后的选中底色。辅助按钮的连续悬停与同文件两侧选中由组件验证覆盖。原生新打开 `git_panel.go` 差异标签停留在加载状态，本次未将其正文加载计为通过；已关闭该新增标签，保留重开前的六个标签。未对 Fuyu 执行暂存、丢弃或提交。

## Git 分组与吸底历史

基线 `fa35179`，开始时工作区和暂存区均干净。文件分组顺序调整为冲突、更改、已暂存的更改；所有空组整体隐藏，移除空组占位。最近提交移出文件滚动区并固定在剩余内容区底部；展开后整个历史区最多占该区域 45%，内部列表最高 260 DIP，滚动状态与文件列表分开且归属各自工作区。折叠后仅保留底部标题。

自动化验证：

- 更新 `TestGitPanelUsesContinuousCompactRows` 的顺序与折叠断言，保留 26 DIP 标题、28 DIP 文件行及 8 DIP 组间距。
- 新增 `TestGitPanelHidesEmptyGroups`，覆盖全部为空、仅工作区更改、仅已暂存三种状态，核对标题和占位均隐藏以及底部位置。
- 新增 `TestGitHistoryStaysAtBottomWithIndependentBoundedScroll`，在 320/500/900 DIP 高度下覆盖冲突顺序、文件列表滚动、历史展开/折叠、45% 与 260 DIP 上限、历史独立滚动、上方空间保留及分页按钮可达。
- `./scripts/go test ./...` 通过，UI 包耗时 13.567 秒；`./scripts/go vet ./...` 通过。指定的分块暂存未保存保护测试在定向执行和完整测试中均通过，本次未复现其偶发失败。
- 根目录 `./scripts/go build` 返回 `no Go files in .../Fuyu`；根目录没有 Go 入口。随后 `./scripts/go build ./...` 和 `./scripts/go build -o bin/Fuyu.app/Contents/MacOS/Fuyu ./cmd/desktop` 均通过，全程使用包装脚本。`git diff --check` 通过。

原应用已正常退出并重开，保留原数据目录。实际窗口确认空的已暂存分组隐藏、最近提交在底部展开，提交列表内部滚动后上方六个文件及历史标题保持原位。短窗口尺寸及冲突/已暂存的组合由组件验证覆盖，未在原仓库制造这些 Git 状态，未执行真实远程操作。提交前补跑定向 `./scripts/go test -race ./internal/ui -run 'TestGitHistoryStaysAtBottom|TestGitPanelHidesEmptyGroups|TestGitHistoryCommit|TestGitWorkflowStagesOnlyChosenFileAndCommitsIndex' -count=1` 通过。修改已提交。

## 最近提交的变更文件树

每条提交左侧显示展开/收起箭头，点击提交行切换文件列表，首次展开异步读取变更文件。读取失败可重试，空提交显示结果提示；成功结果按工作区和提交缓存，刷新历史时清理已移出列表的缓存。点击文件打开该提交中对应文件的历史 Diff，连续选择时以最后一次请求为准。保留吸底位置、独立滚动与高度上限。

自动化验证：

- `TestGitHistoryCommitExpandCollapseAndOpenChosenFile` 使用临时 Git 仓库，覆盖按需读取、展开不打开标签、收起隐藏、缓存复用、指定文件 Diff、连续选择及工作区状态隔离。
- `TestGitHistoryCommitReadFailureRetryAndEmptyCommit` 覆盖失败重试、历史刷新清理缓存与真实空提交；既有提交工作流测试更新为先展开再选择文件。
- `./scripts/go test ./...`、`./scripts/go vet ./...` 均通过；定向 `./scripts/go test -race ./internal/ui -run 'TestGitHistoryCommit|TestGitWorkflowStagesOnlyChosenFileAndCommitsIndex' -count=1` 通过。本次未重复执行全量 race。
- `./scripts/go build -o bin/Fuyu.app/Contents/MacOS/Fuyu ./cmd/desktop` 和 `git diff --check` 通过。

原 `bin/Fuyu.app` 已正常重开。原生窗口展开 `fa35179` 后列出八个变更文件，收起后文件行消失；点击 `docs/subsystems/editor.md` 后，主窗口正文实际加载该文件的历史差异。保留原数据目录、标签与草稿，未在 Fuyu 仓库执行 Git 写操作。代码和文档已提交。

## 更改行尾共用区域

“更改”文件行的增删数及状态字母与三个操作按钮合并到同一行尾区域。默认显示变更情况，悬停或键盘聚焦时替换为打开文件、撤销改动、暂存按钮；移出且无键盘焦点时恢复变更情况。共用区域至少 72 DIP，较大数字按文字测宽增加占位，切换显示时保持文件名位置和行高。已暂存、冲突组保留既有排列。

更新 `TestGitFileRowHoverKeepsGeometryAndActiveComparison`，在 260/360 DIP 下验证互斥显示、行尾边界、连续跨按钮悬停、选中背景、文件名位置和移出恢复。新增 `TestGitWorkingTreeTrailingAreaSwapsLargeCountsAndKeyboardActions`，覆盖七位增删数、窄行、键盘聚焦替换与移出后焦点保留。

`./scripts/go test ./...` 通过，UI 包耗时 15.113 秒；`./scripts/go vet ./...`、桌面入口构建和差异检查通过。本轮未执行 race。原 `bin/Fuyu.app` 已正常退出并重开，实际窗口确认默认行尾增删数和字母显示；精确鼠标悬停替换由组件输入回归覆盖，本轮不单列为原生验收通过。原有数据及空白主窗口保留，未对 Fuyu 执行 Git 写操作；代码和文档已提交。

### 文件操作单一文字提示

修复共享按钮和文件行分别调用 `Tooltip` 导致同时显示两个气泡的问题，改为创建按钮时传入最终动作文字。打开文件、撤销改动、暂存、取消暂存、标记已解决的气泡只显示动作，不显示路径。新增 `TestGitFileActionsShowOneActionOnlyTooltip`，对更改、已暂存与冲突组的每个操作悬停一秒，断言只新增一个动作文字提示。

完整 `./scripts/go test ./...`、`./scripts/go vet ./...` 和桌面入口构建通过，UI 测试耗时 14.185 秒。原应用已正常退出并重开，气泡的数量与内容由组件回归验证；本轮未单列原生悬停验收，未执行 race。代码和文档已提交。

### 文件名优先显示

目录改用零 flex basis，只使用文件名自然宽度以外的剩余空间。文件名需要截断时目录宽度为零，避免同时显示文件名和路径的省略号；文字入口右内边距从 8 DIP 缩至 2 DIP，让文字更接近行尾打开文件图标。

新增 `TestGitFileRowPrioritizesFilenameBeforeDirectory`，在 240/320/520 DIP 和更改/已暂存分组下核对文件名未完整时目录不占宽度、悬停前后位置稳定、截断文件名接近操作区。完整 `./scripts/go test ./...`（UI 包 14.059 秒）、`./scripts/go vet ./...`、桌面构建通过。原应用已正常重开，实际窗口中的 `2026-10-26-git-workflow.md` 完整显示后，路径在剩余空间中截断；短文件名旁正常显示目录。本轮未执行 race，代码和文档已提交。
