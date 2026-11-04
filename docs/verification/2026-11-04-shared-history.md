# 共享历史目录验证

日期：2026-11-04。

## 行为

右侧会话历史的合并目录改为应用级共享的 `threadList`：条目、各来源分页 cursor、读取与错误状态只属于应用，不属于任何会话。修复切换项目或会话时列表被清空、从首页重读导致的闪动。

- 切换选中会话或项目只按当前项目/全部项目重新筛选，不发送 `ListThreads` 请求，已有行保持显示。
- 打开历史行不再按 Agent/Runtime/cwd 复制缓存；新会话直接共用目录，进行中的请求不因切换丢弃。
- 打开历史面板（侧栏切换、"会话历史"页签、`/resume`）时，未读取列表加载首页；已有列表仅刷新各来源第一页：新会话插到最前，已有条目原位更新，分页 cursor 不变；刷新与分页互斥并保留一次后续刷新。
- Codex 与 Claude 会话在任务完成终态后，仅当所属来源已读取时插入共享列表并保持最新在前。
- 无来源字段的记录固定回落为默认 Codex 来源，不再随所选会话改变归属。

## 自动化验证

新增回归：`TestOpenHistoryReusesTheSharedListAcrossAgents`（打开跨 Agent 历史行零新增请求、目录保留）、`TestSwitchingProjectsNeverReloadsTheSharedHistory`（项目间切换行保持、零请求、无"没有匹配会话"闪现）、`TestInFlightThreadListSurvivesSessionSwitch`（加载中切换结果仍写回且不重复请求）、`TestCompletedThreadJoinsTheSharedHistory`（Codex 终态后入列、未读来源不记录、不重复）、`TestHistoryHeadRefreshPrependsAndKeepsPagination`（首页刷新前置新行、原位更新、cursor 与来源分页状态不变、加载期间列表保持）。

重写原按会话缓存的 `TestOpenHistoryCarriesOnlyMatchingCache`、`TestOpenHistoryLoadsTargetWhenCacheDirectoryChanges`；`TestCompletedClaudeAppearsInMatchingHistory` 增加未读来源不记录断言；`TestSessionRenameHistorySearchAndIdentity` 改为显式行身份核验跨 Agent/Runtime 隔离。既有双来源合并、按 ID 分页去重、失败来源重试、滚动分页与历史恢复测试全部保留并通过。

以下检查全部通过：

```sh
./scripts/go test ./internal/... -count=1
./scripts/go vet ./...
./scripts/go test -race ./internal/ui -count=1 -run 'SharedHistory|SharedList|InFlightThreadList|CompletedThread|HeadRefresh|ThreadHistory|History(Combines|Keeps|Opens|Empty|Scope)|CompletedClaude|StartupAdds'
git diff --check
```

## 边界

- 目录条数仍无上限，随分页累积；本次未引入上限。
- `internal/ui/zz_dbg_test.go` 为既有调试测试，本次仅同步字段访问，未删除。
- 搜索词与项目范围仍按工作区各自保存：范围不同的项目间切换会按各自偏好改变可见行，属于既有设计，不属于闪烁。
- 原生窗口观感（多项目来回切换时右侧列表稳定）待用户复核。
