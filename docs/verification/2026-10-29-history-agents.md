# 双 Agent 会话历史验证

日期：2026-10-29。

## 行为

会话历史同时展示默认 Codex 与已注册 Claude Code Runtime 返回的记录，每行标题前显示对应的 12 px SVG 图标；Claude 使用既有橙色，Codex 跟随主题。当前项目和全部项目按钮下方删除重复的项目名。项目筛选、标题和目录搜索覆盖合并列表，打开、重命名和删除继续使用记录自身的 Agent、Runtime 与 thread ID。

每个来源独立分页、标记读取完成；相同 ID 的不同来源分别保留，同来源的分页重叠记录更新原行。来源读取失败时保留其他成功结果、显示具体来源错误，允许重试且不会重读已结束的来源。打开另一种 Agent 的历史按记录归属创建会话，避免继承默认 Agent 的模型和连接错误。

## 自动化与原生验证

受控组件与后端测试通过：双来源同 ID 保留、同来源跨页去重与标题更新、独立分页到末尾、空来源不重复请求、单一来源失败后保留成功结果及重试、两种图标尺寸、当前项目/全部项目筛选、搜索、隐藏标记隔离、重命名目标与打开归属、默认 Claude 模型和错误不泄漏到 Codex、独立历史面板移除项目名。原有分页与历史恢复测试继续通过。测试使用受控后端，不发送真实模型请求。

以下检查全部通过：

```sh
./scripts/go test ./...
./scripts/go vet ./...
./scripts/go test -race ./internal/ui -run 'TestHistory|TestThreadHistory|TestOpenHistory|TestCompletedClaude|TestNewClaude' -count=1
./scripts/build
git diff --check
```

完整测试 UI 包用时 17.479 秒，定向 race 用时 2.263 秒。日志位于 `/tmp/fuyu-mixed-history-full.log`、`/tmp/fuyu-mixed-history-vet.log`、`/tmp/fuyu-mixed-history-race.log`、`/tmp/fuyu-mixed-history-build.log`。

通过正常退出流程关闭原实例，再启动最终构建，实例 PID 57753。原生无障碍树同时列出四条当前项目 Claude 记录与多条 Codex 记录，来源 Tooltip 分别为 Claude Code 与 Codex。截图确认 Claude 为橙色图标、Codex 为主题图标，当前项目/全部项目下方直接进入列表，重复的 Fuyu 文案已删除。用户原会话 61 保持选中、历史面板展开，没有新建测试标签或修改草稿。

本轮原生检查覆盖实际双来源列表、图标、布局与窗口重启；分页错误、同 ID 隔离及历史打开和重命名归属使用受控测试覆盖，未重复真实模型续聊。
