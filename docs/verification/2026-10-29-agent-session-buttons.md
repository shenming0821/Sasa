# Agent 双图标新建会话验证

日期：2026-10-29。

## 行为与素材

会话视图删除原目录工具栏，标签栏行尾并排放置 Codex 和 Claude Code 图标，替代原通用新会话按钮。新建图标为 16 px；会话页签标题前显示所属 Agent 的 12 px 图标，替代 Claude 文字前缀。左右侧栏折叠后的恢复按钮沿用标签栏位置。悬停两个新建图标时分别提示“使用 Codex 新建会话”“使用 Claude Code 新建会话”。输入区移除 Agent 下拉，保留模型选择。点击任一图标均在当前 cwd 创建独立空白会话并聚焦输入；原会话的草稿、模型、上下文与活动任务保留。连接不可用时显示对应错误，当前会话和默认 Agent 保留。最近选择继续持久化。

SVG 路径取自本机 Orca 源码 `src/renderer/src/components/status-bar/icons.tsx` 的 `OpenAIIcon` 和 `ClaudeIcon`，`src/renderer/src/lib/agent-catalog.tsx` 确认其分别用于 Codex 与 Claude。Claude 显式指定源码颜色 `#D97757`；Codex 跟随当前主题文字色。MyGo Icon 默认统一使用文字色，首次原生检查发现 Claude 被染成灰色后，增加显式着色和像素回归，最终窗口已确认正确橙色。

## 自动化

定向组件回归通过，覆盖 480 和 1000 宽窗口、相同及不同 Agent、空白草稿与运行中任务保留、正确 Runtime 归属、Agent 下拉移除、模型入口保留、后端缺失错误、默认选择持久化，以及 Claude 模型偏好与历史缓存隔离。空工作区覆盖双图标显示、当前工作区目录绑定与连接错误可见。像素回归核对 Claude 原始橙色。测试使用 MyGo 内存渲染器及受控后端，不发送真实模型请求。

最终检查全部通过：

```sh
./scripts/go test ./...
./scripts/go vet ./...
./scripts/go test -race ./internal/ui -run 'TestAgentSession|TestAgentChoice|TestClaudeModelChoice|TestClaudeGeneration|TestNewClaudeSession|TestCompletedClaude' -count=1
./scripts/build
git diff --check
```

## 原生窗口

正常退出旧进程后启动新构建，在真实窗口确认右上角两个图标与模型入口，输入区的 Agent 下拉已移除。点击 Codex 图标创建空白会话 52，输入临时草稿 `FUYU_UI_DRAFT_CHECK`；点击 Claude 图标创建空白会话 53，其输入框为空且后端为 Claude Code。返回会话 52，草稿保持原文。清空临时草稿后再次点击 Codex 图标，新建独立会话 54。

验收没有发送模型任务。已关闭三个验收空白标签，原会话 50（Claude · hi）保持打开，临时草稿已清空。图标颜色修正、完整检查与构建完成后，再次正常退出并启动最终实例 PID 39101；原会话恢复，截图确认 Codex 为主题文字色、Claude 为橙色，新版本窗口可用。

运行中任务保留、后端不可用与窄窗口由受控组件测试覆盖；本轮不重复真实任务执行、原生极窄窗口或后台工具退出验收。本轮修改纳入本次提交，未推送。

## 单行标签栏调整与复验

按用户追加要求删除会话视图首行目录工具栏，将两个 Agent 新建按钮移至标签栏行尾，替代原通用新会话按钮。按钮点击区域为 30×30 px，图标为 16 px；会话页签标题前显示 12 px Agent 图标。文件与差异页签图标保持原样，文件视图保留其文件工具栏。

全量测试、vet、相关 race、构建和差异检查均通过：

```sh
./scripts/go test ./...
./scripts/go vet ./...
./scripts/go test -race ./internal/ui -run 'TestAgentSession|TestSessionTabsShowMatchingAgentIcons|TestTab|TestSidebarToggles|TestResponsive|TestP4ClickingPartlyVisibleTab' -count=1
./scripts/build
git diff --check
```

全量测试 UI 包用时 15.228 秒，相关 race 用时 5.451 秒。日志分别位于 `/tmp/fuyu-tabbar-full-test.log`、`/tmp/fuyu-tabbar-vet.log`、`/tmp/fuyu-tabbar-race.log` 和 `/tmp/fuyu-tabbar-build.log`。

正常退出旧实例后启动最新构建，最终实例 PID 70108。原生截图确认首行目录工具栏已移除、双图标位于标签栏行尾、原会话 50 的标题前显示小尺寸橙色 Claude 图标。分别点击 Codex 和 Claude Code 新建入口创建临时会话 55、56，确认两种页签图标与输入框 Agent 归属正确，新标签可滚动进入可视区域。两个临时页签均已关闭，原会话 50（hi）恢复，未输入草稿或发送模型任务。

侧栏恢复与窄宽度行为有组件测试覆盖，本轮没有新增原生极窄窗口验收。本轮修改纳入本次提交，未推送。
