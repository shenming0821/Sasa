# Claude Code 技能候选验证

日期：2026-10-29。范围：Claude Code 会话中输入 `/` 显示 skill，及加载失败后的重试。后续按用户要求增加本地 `/clear`；原生目录继续只筛选技能，新增命令的实现与验证见 [/clear 验证](2026-10-29-claude-clear.md)。

## 问题与根因

触发条件：Agent 选择 Claude Code 后，在输入框输入 `/`。

原有行为：候选浮层出现但显示“没有匹配项”，看不到任何 Claude Code 命令。`internal/ui/completion.go` 的 `completionItems` 对 Claude 只遍历 `a.completion.commands`，而填充该字段的 `loadCompletionSkills` 仅在 `internal/ui/view.go` 的 `query.kind == '$'` 分支被调用。斜杠查询从不触发加载，`commands` 始终为空。

最新要求：Claude 的 `/` 只展示技能，其他命令暂不展示。

修改后：首次输入 `/` 时按当前会话 cwd 通过 `ListCommands` 在同一个探测进程中读取 initialize 命令目录及只读 `get_skills_dialog`。用技能 `name`、`display_name` 与命令名关联，排除 `state=off` 的技能，通过 `agent.Command.IsSkill` 标记，UI 只展示该标记为真的候选。`/model`、`/compact` 及 Codex 本地动作不进入 Claude 候选。缓存按会话隔离，确认后把原生 `/名称 ` 作为普通文本插入草稿，不自动发送；标题为“技能”。按最新要求，Claude 与 Codex 的技能候选在后台读取时保持安静，保留内部加载状态用于防止重复读取和避免提前显示空结果，失败仍可重试。

审核发现首次读取失败后，点击重试即使成功仍保留 `completion.error`。现开始重新读取时清除旧错误；回归测试在修复前失败于 `retry did not clear previous failure`，修复后通过。

## 协议核对

本机 CLI：Claude Code `2.1.307`，可执行文件 `/Users/shenming/.local/bin/claude`，权限 `default`，配置根 `~/.claude`。读取已安装 CLI 的协议 schema 及控制请求处理代码，并实测 `get_skills_dialog` 返回技能名称、显示名称、来源和状态；不依赖描述后缀或硬编码命令名单判断。

在项目目录中，initialize 返回 68 条命令，技能目录返回 24 条技能。保留的 `TestLiveClaudeSkillCommandCatalog` 使用临时 cwd、临时 Fuyu 检查点目录及现有 Claude 配置，新建临时项目技能 `fuyu-catalog-probe`；真实 adapter 返回 69 条命令，其中 25 条标记为技能，项目技能存在，`model`、`compact` 未标记为技能。仅执行 initialize 和只读目录请求，没有发送用户任务或调用模型，未修改原有配置和技能。

旧版本或连接拒绝 `get_skills_dialog` 时返回具体读取错误，供用户重试；技能目录为空时不回退展示其他命令。

## 自动化验证

- `TestClaudeSlashCompletionLoadsRuntimeSkills`：输入 `/` 后按会话 cwd 发起读取；技能 `/frontend-design` 可见，`/compact`、`/model`、`/new`、`/resume` 不可见，Tab 只插入 `/frontend-design `。
- `TestClaudeSlashCompletionRetryClearsError`：模拟首次失败，实际点击“重试”，加载中和成功回填后均无旧错误，技能候选可见，重试入口消失。
- `TestCommandCatalogIdentifiesInvocableSkills`：协议夹具覆盖普通技能、技能名与命令显示名不同、仅用户可调用技能、关闭技能及其他命令，保留目录、描述和参数提示。
- `TestCommandCatalogDoesNotFallBackToActions`：目录请求被拒绝时返回错误；空目录不把其他命令标记为技能。

```sh
./scripts/go test ./internal/claude ./internal/ui -run 'TestCommandCatalog|TestClaudeSlashCompletion|TestCompletion|TestComposerSlash' -count=1
./scripts/go test ./...
./scripts/go vet ./...
./scripts/go test -race ./internal/claude ./internal/ui -run 'TestCommandCatalog|TestCatalogUsesBoundConfigurationHome|TestClaudeSlashCompletion|TestComposerSlash|TestCompletion' -count=1
FUYU_LIVE_CLAUDE=1 ./scripts/go test ./internal/claude -run '^TestLiveClaudeSkillCommandCatalog$' -count=1 -v
./scripts/build
```

上述命令全部通过；全量普通测试中各包通过，UI 包 14.563 秒。真实 CLI 隔离技能测试通过，普通自动化使用受控协议夹具和内存渲染器。Go 文件通过 gofmt 检查，`git diff --check` 通过。

## 移除加载提示补验

按最新要求仅移除候选浮层中的加载文案，保留内部 `loading` 状态。以下命令通过：

```sh
./scripts/go test ./internal/ui -run 'TestClaudeSlashCompletion|TestCompletion|TestComposerSlash' -count=1
./scripts/go vet ./internal/ui
./scripts/build
git diff --check
```

正常退出旧实例，启动构建后的 Fuyu（PID 34112），确认窗口可用。在原 Claude 会话输入 `/`，原生浮层显示“技能”和键盘提示，后台读取期间没有 loading 文案，也未提前显示“没有匹配项”。观察时尚未出现列表，未据此声明列表或键盘确认验收通过。已恢复空草稿，未发送任务。

## 前序验收限制与剩余项

第一轮错误重试修复构建后，曾通过原生窗口工具正常退出旧实例 PID 4087，并确认新实例 PID 74216 的窗口可用；这一轮在用户提出“只显示 skill”之前。之后的窗口输入调用被用户中断，不作为验收通过。

技能筛选版本最初构建后，继续原生验收时，工具返回 `The Mac is locked and automatic unlock could not unlock it`。本次已补验正常退出重启、窗口可用及读取期间的浮层；技能列表、筛选、滚动及 Tab/Enter 插入的完整原生验收仍保留。真实 CLI 与组件测试不替代这一步。
