# 会话按需加载与标签关闭验证

日期：2026-10-23。

## 行为与范围

原实现启动后为所有本地索引会话显示标签，并逐个请求原生恢复。本轮将历史索引与已打开标签区分：重启恢复上次已打开标签和选中项，并只恢复选中会话的原生历史；点击其他已恢复标签、项目会话或原生历史时才加载对应历史。索引仍读取元数据，未打开会话不加载原生消息正文。

每个标签右侧显示独立关闭按钮。关闭保留侧栏记录、草稿、阅读和工具状态；运行中的任务与审批继续归属原会话，退出保护继续包含它们。关闭当前标签优先选择左邻，再选右邻；关闭最后一项显示空白主窗口，不创建新会话。已加载会话在当前进程中保留状态，重新打开不重复恢复；已打开标签与选中项写入 settings JSON，重启恢复；已关闭项保持关闭。旧记录缺少打开列表时兼容为单标签恢复。

未打开的历史保留未确认 Active/TurnID 和阅读数据；保存不会用空白投影覆盖它们。标签关闭不会调用原生中断、删除历史或释放任务归属。

## 自动化验证

`internal/ui/tabs_test.go` 使用临时 SQLite 索引、受控恢复后端和 MyGo 组件测试，不读取真实用户配置或发送模型任务。

- `TestStartupLoadsOnlySelectedTabAndOpensHistoryOnDemand`：三个索引记录仅显示选中标签；启动只请求其恢复；点击另一项才请求恢复；关闭重开保留草稿且不重复请求；保存保留未加载记录的未确认任务与阅读状态；再次启动恢复已打开标签，旧版记录首次加载仍只打开选中项。
- `TestCloseTabsPreservesBackgroundWorkflowAndLastTabFallback`：关闭后台标签不切换当前选择；审批与原生终态仍回到原会话；退出保护保留活动任务；重新打开保留草稿和工具展开；关闭当前项切换邻居，关闭最后一项显示空白窗口并保留历史。
- `TestClosingRecoveringTabDoesNotReopenOnLateResult`：恢复期间关闭标签，晚到的恢复结果不重新打开标签或抢占选择。
- `TestTabCloseButtonLayout`：640、1000 DIP 窗口关闭按钮可见，点击区域至少 24×24 DIP，与标题不重叠。
- `TestRestartRestoresOpenTabsAndSelectionLazily`：重启保留打开列表与选中项，关闭项保持关闭；启动只请求选中历史，点击恢复的后台标签后才加载；再次关闭并重启不重新打开该项。
- `TestOpenTabSettingsCompatibilityAndLaunchDirectory`：覆盖旧记录、跨项目与空白草稿、失效标签 ID、已关闭的选中项及显式 cwd 启动；保存列表排除失效 ID。
- 存储回归将 `OpenTabs` 纳入重开和事务回滚断言，沿用 schema 2。
- 既有导航回归继续覆盖多标签横向滚动、选中标签滚入视口与侧栏切换。

已通过：`./scripts/go test ./...`、`./scripts/go vet ./...`、`./scripts/go test -race ./...`、`./scripts/build`。

## 界面检查与边界

已检查 MyGo 无头渲染截图 `/tmp/fuyu-session-tabs-preview.png`，两个标签的关闭图标在标题右侧独立显示，沿用现有主题和输入框样式。截图采用受控会话标题，属于组件渲染证据。

本轮未执行真实原生窗口点击、实际 Codex 恢复或真实任务的关闭标签验收。新应用包为 `bin/Fuyu.app`；重新启动后可核对已打开标签与选中项是否恢复、后台标签点击后加载、关闭项下次保持关闭，以及后台任务继续运行。

## 关闭最后标签为空白窗口

`TestLastClosedTabRestoresEmptyWorkspace` 验证最后标签关闭不新增会话、空打开列表和 Selected=0 的持久化、重启保持空白、草稿保留、从侧栏重新打开和从空白窗口主动新建。既有关闭后台任务回归继续核对事件与审批归属。
