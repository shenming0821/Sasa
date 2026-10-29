# Claude 权限配置继承验证

日期：2026-10-29。

## 行为与范围

本机 `/Users/shenming/.claude/settings.json` 的 `permissions.defaultMode=bypassPermissions`。此前 Fuyu 的共用 `spawn` 入口显式传递 `--permission-mode default`，且只接受初始化回执 `current_permission_mode=default`，因此覆盖了原生权限配置。

现由 CLI 按绑定的配置根和当前 cwd 解析实际权限，不传入权限模式覆盖。保留 stdio 审批通道，初始化仍要求回执含有效的非空权限模式；未确认时关闭自有进程并在发送路径返回未提交错误。新任务、恢复后的后续任务、模型与技能目录探测均使用这一入口。用户原生配置文件保持原值，Fuyu 当前没有独立的权限模式切换入口。

## 验证记录

`TestInitializationInheritsPermissionMode` 使用临时配置、临时 cwd 与受控协议服务：default、bypassPermissions、acceptEdits、plan 均能初始化；缺字段、空字符串、空白或类型错误均阻止发送用户帧。实际捕获子进程 argv，确认保留 `--permission-prompt-tool stdio`，且没有权限模式或 bypass 参数覆盖。测试通过。

```sh
FUYU_LIVE_CLAUDE=1 FUYU_LIVE_CLAUDE_PERMISSION_MODE=bypassPermissions ./scripts/go test ./internal/claude -run '^TestLiveClaudeConfiguredPermissionMode$' -count=1 -v
```

真实 CLI `2.1.307` 使用现有原生配置，在临时 cwd 和临时 Fuyu 检查点目录中初始化，返回 `current_permission_mode=bypassPermissions`，测试通过。仅执行只读初始化，没有发送用户任务或调用模型。

以下检查全部通过：

```sh
./scripts/go test ./...
./scripts/go vet ./...
./scripts/go test -race ./internal/claude -run 'TestInitializationInheritsPermissionMode|TestCancelInitializationDoesNotSubmitOrBlockRetry' -count=1
./scripts/build
git diff --check
```

正常退出旧 Fuyu 后启动新构建，确认 PID 45578 的原生窗口可用，原 Claude 会话和空草稿保留。未通过 GUI 发送任务；本轮真实权限证据来自 adapter 的只读初始化测试，不扩展为工具自动执行的端到端验收。修改未暂存、未提交。
