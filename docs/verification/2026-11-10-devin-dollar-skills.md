# Claude/Devin `$` 技能候选

## 范围

非 Codex 会话（Claude、Devin）输入 `$` 于行首时弹出技能候选，仅列出 `IsSkill` 原生命令；确认后把草稿替换为 `/名称 ` 文本，由运行时原生 slash 命令机制调用技能。句中 `$` 不触发（与 `/` 触发形状一致）；Codex 行为不变，仍可在任意位置 `$` 引用技能并随发送附带。

## 自动化验证

- `TestDevinDollarCompletionInsertsNativeSkillCommand`：Devin 会话 `$` 读取命令目录、仅显示 IsSkill 项、Tab 插入 `/review `、句中 `$r` 不弹候选。
- 回归：`./scripts/go test ./internal/ui -count=1` 全量通过（含 Claude `/`、Codex `$`、文件 `@` 既有用例）。

## 真实 Runtime 证据

对 `devin acp`（CLI 3000.12.43）手工 initialize + authenticate + session/new，`available_commands_update` 返回 53 条命令，`_meta["cognition.ai/category"]` 分布为 Account=4 / System=3 / Session=14 / **Skills=32**，确认 `IsSkill` 分类标记真实存在。

同一探针确认两个时序事实：`available_commands_update` 可能先于 `session/new` 响应到达（此时应用侧会话尚未注册）；`session/load` 只重放内容类更新（message/tool/session_info），不推命令广告。早前实现两种时序下都丢弃该更新，且空结果被补全目录缓存，导致 `$`/`/` 永久"没有匹配项"。修复为：未附着/恢复中的会话仍应用命令广告，广告到达时按后端范围发 `fuyu/commands` 事件，UI 失效目录并重新读取。

## 自动化验证（补充）

- `TestCommandsAdvertisedBeforeSessionRegistration`：命令更新先于响应到达时 `ListCommands` 仍返回两条命令（技能标记与参数提示正确），且发出 `fuyu/commands`。
- `TestDevinCommandsEventRefreshesEmptyCatalog`：`$` 先读到空目录，`fuyu/commands` 事件后自动重读并显示技能候选。
- `./scripts/go test -race ./internal/devin ./internal/ui -count=1` 全量通过。

## 未验证项

- 原生窗口中的实际显示与键鼠交互，以 Preview 构建人工确认为准。
- Claude 会话的 `$` 与 `/` 共用同一命令目录读取路径，自动化已覆盖 Devin 分支；Claude 侧行为等价，未单独实跑。
