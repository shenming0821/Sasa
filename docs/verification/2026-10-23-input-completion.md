# 输入框命令与技能候选验收

日期：2026-10-23。

## 原因与参考

Fuyu 原输入框只处理普通文字、发送和换行，没有 CLI 的候选展示层。本机 `codex-cli 0.163.0` 交互检查确认：输入 `/` 显示内置命令，继续输入筛选；输入 `$` 显示技能名称和简介，Enter 插入技能引用，随后继续编辑，不立即发送。检查后退出 CLI，未发送模型任务。

原生协议依据为该版本 `codex app-server generate-json-schema` 生成的 `SkillsListParams`、`SkillsListResponse` 与 `TurnStartParams.UserInput`。`skills/list` 接收当前目录的 `cwds` 和 `forceReload=true`；技能输入为 `type=skill`、`name`、`path`。

## 实现范围

- 候选浮层位于输入框上方，使用原生主题；名称、简介、筛选结果、加载状态、错误与重试均可见。列表内部滚动，输入区不挪位。
- `/` 当前提供 `/new`、`/resume`、`/status`、`/skills`，分别接已有的新会话、右侧历史、运行详情和技能选择。不表示已经实现 CLI 全部命令，模型切换、权限调整等入口仍按后续能力规划。
- `$` 从当前目录的原生 `skills/list` 读取技能，优先展示 interface 的名称和简述，筛除 disabled 技能；本地不自行扫描或猜测技能安装目录。
- 支持上下选择、Enter / Tab 确认、Esc 或点击外部关闭；查询变化后恢复展示。输入法组合期间保留原有中文输入行为。
- 技能选择只替换光标前的当前 token，保留其余正文，并把焦点与光标放回输入框。命令选中直接执行本地动作，技能选中只插入引用，不发送。
- 发送时保留文本，并为已选且仍存在于草稿的技能加入原生 skill 输入，携带准确 name/path。删除引用后不再附带它；重复引用去重。
- 技能缓存、请求结果与选择归属各自会话；异步结果不会覆盖切换后的会话。已选技能的名称与路径随草稿保存在应用索引，重开仍能显式调用；目录变化后不沿用旧目录引用。

## 验证结果

- `TestComposerSlashCompletionDoesNotSubmitTask`：输入 `/` 展示、筛选和执行本地命令；最小窗口面板位于输入框上方且在窗口内。
- `TestComposerSkillCompletionKeyboardEscapeAndClick`：技能过滤、Esc 关闭、编辑重开、Tab 确认、鼠标选中、焦点与插入后继续输入。
- `TestCompletionQueryUsesCaretAndKeepsOtherText`：中文 rune 偏移、中间插入、保留后缀、排除路径与嵌入 token、避免技能前缀误匹配和重复发送。
- `TestCompletionKeyboardSelectionAndIMEGate`：上下选择、中文输入法组合时隐藏候选并保留 Enter 语义。
- `TestSkillMenuAllowsScrollingAndRevealsKeyboardSelection`：深色主题、最小窗口、20 个技能的鼠标滚动不被选中项拉回，键盘循环选中项自动滚入可见区。
- `TestSkillLoadingAndSubmissionStayWithTheirSession`：目录请求、异步会话归属、显式技能与发送终态回调。
- `TestSkillReferencesSurviveDraftRestartAndFollowEdits`：真实 SQLite 保存/读取草稿引用，删除引用后清理保存项，切换目录不携带旧路径。
- `TestSkillsListAndExplicitSkillInput`：受控 RPC 校验 cwds、forceReload、原生错误条目以及 turn/start 文本与 skill 的完整结构。
- 既有发送、换行、中文输入、最小窗口、历史恢复测试通过。

只读真实接口核验：

```sh
FUYU_LIVE_SKILLS=1 FUYU_SKILLS_CWD=/Users/shenming/Documents/Sync/AI/Fuyu ./scripts/go test ./internal/codex -run '^TestLiveSkillsReadOnly$' -count=1 -v
```

首次请求返回 0 个技能与 1 个原生错误条目；再次核验返回 41 个技能、41 个 enabled、0 个错误。首次错误的具体原因未取得，不推断为没有安装技能。产品界面会展示原生错误并提供重试，不缓存成成功的空列表。接口核验仅初始化 App Server 和读取技能，未创建 thread/turn 或调用模型。

已通过完整 test、vet、race 与构建，并检查输入候选的内存渲染预览。测试以受控数据和 MyGo 内存渲染为主，真实读取技能单独记录；实际 Fuyu 原生窗口的键盘、鼠标及输入法体验待重启后复验。
