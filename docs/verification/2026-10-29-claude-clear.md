# Claude /clear 验证

日期：2026-10-29。

## 行为

Claude `/` 候选保留技能，并增加本地 `/clear`，支持名称筛选。目录尚未读取或读取失败时也可显示 `/clear`。确认候选只插入草稿，再次发送时保存旧历史，关闭旧标签并打开空上下文的新会话，保留工作目录、Claude 后端和选中模型；下次任务分配新的原生会话。旧历史可继续恢复。

运行中、恢复中或待核验等不可发送状态沿用发送保护。附带图片或携带参数时显示具体错误并保留草稿；保存旧历史失败时保留原会话。该命令在 UI 本地执行，不将 `/clear` 当作模型任务发送。

## 原生协议核对

Claude CLI `2.1.307` 的本地命令描述为开始空上下文的新会话、旧会话保留以便恢复。本轮在临时 cwd，用随机新 session UUID 执行只读初始化和 `/clear`；实际收到 `command_lifecycle`、`conversation_reset`，随后 `system/init` 与成功 `result` 使用不同的新 UUID，进程退出码为 0。没有发送模型任务，没有操作用户已有会话。该观察用于确认身份变化边界，Fuyu 通过自己的会话管理完成相同的上下文隔离。

## 自动化与窗口验证

新增组件回归覆盖 Tab 插入、Enter 执行、空上下文、保留旧历史与持久化、保留 Agent/Runtime/cwd/model，以及忙碌、恢复、待核验、图片、参数和保存失败分支。以下检查全部通过：

```sh
./scripts/go test ./...
./scripts/go vet ./...
./scripts/go test -race ./internal/ui -run 'TestClaudeClear|TestClaudeSlashCompletion|TestComposerSlash' -count=1
./scripts/build
git diff --check
```

正常退出旧进程后启动新构建，核对新 PID 为 68278。原生窗口读取、截图及工具重连均返回 `timeoutReached`，因此没有进入临时空白会话执行 `/clear`，也未修改用户当前草稿或发送任务。保留新实例，最终原生候选与执行仍待验收；组件测试不替代这一项。代码保留工作区，未暂存、未提交。
