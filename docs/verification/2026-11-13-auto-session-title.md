# 辅助模型与自动会话标题验证

## 交付范围

任务目录：`Sasa.worktrees/auto-session-title`，分支：`task/auto-session-title`。

设置 → 通用增加 Claude Code 配置、辅助模型和自动生成会话标题开关。配置与模型默认未选择，开关默认关闭，不修改聊天模型偏好。首次完整问答结束后空闲 60 秒生成；后续同时满足 30 分钟、5 轮新增完整问答，再空闲 2 分钟检查。未改变原标题的检查也重新计数，无新问答不调用。

手动命名保护原标题；右键重新生成跳过自动频率与开关限制，成功后恢复自动管理。关闭开关、改名、删除、切换模型、新任务及会话身份变化使相应旧结果失效。关闭后重新启用不补扫历史。已关闭且释放 Runtime 的会话只读获取正文，不重新打开会话或恢复任务。

标题与检查记录保存在现有 SQLite JSON 快照中，无需数据库版本迁移；显示优先级为手动标题、自动标题、原摘要。页签、项目行及历史搜索共用标题。后台任务不改变草稿、消息、未读或完成通知。

## 自动化

全量测试、静态检查、相关并发检查、桌面程序构建及差异检查均通过。执行：

```sh
./scripts/go test ./...
./scripts/go vet ./...
./scripts/go build -o .cache/auto-title/Sasa-dev ./cmd/desktop
./scripts/go test -race ./internal/ui ./internal/claude \
  -run 'TestAutomaticTitle|TestTitle|TestManualTitle|TestAuxiliaryTitle|TestSessionRename|TestSessionDelete' -count=1
git diff --check
```

覆盖：

- 首次空闲、后续冷却与轮数同时满足、输入重置空闲、其他会话运行不阻塞、无变化检查消耗额度、失败不循环重试。
- 两类 Agent 的新成功终态计数，失败、中断和重复终态不计数；本地模型切换等命令不作为完整问答。
- 开关校验、配置及模型选择、保存恢复、聊天模型隔离、关闭与重新开启不补扫历史。
- 手动命名、手动生成、全局单请求、手动优先、跨会话归属、迟到结果、保存失败及退出取消。
- 尚未打开历史的只读生成、标题搜索、Runtime 归属隔离、重启保留，以及关闭并释放 Runtime 后的自动生成。
- 输入正文长度上限、工具及围栏代码过滤、无效标题；辅助进程参数隔离、无会话检查点、坏 JSON、错误结果、缺失成功状态、超大输出、取消和 Client 关闭。

设置界面已使用 MyGo 内存渲染器检查；可通过 `FUYU_AUXILIARY_SCREENSHOT=<PNG路径>` 配合 `TestTitleSwitchAndModelSelectionAreIndependent` 导出截图。此截图及组件操作不等同原生窗口验收。

## 真实 Claude Code

执行：

```sh
FUYU_LIVE_CLAUDE_TITLE=1 FUYU_LIVE_TITLE_MODEL=haiku \
  ./scripts/go test ./internal/claude -run '^TestLiveClaudeAuxiliaryTitle$' -v -count=1
```

实测 CLI 为 `2.1.307`，使用本机已有 `~/.claude` 配置。本轮 `haiku` 别名配置的实际模型为 `winning/deepseek-v4-1-flash-261017`，不据此推断其他配置的映射、价格或 Anthropic 直连行为。

两次独立调用均返回“修复会话切换草稿丢失”，方法耗时分别约 2.37 秒和 1.62 秒。请求使用临时 cwd、空工具集、空 MCP、禁用技能及 hooks、自定义标题系统提示词和 `--no-session-persistence`。没有调用聊天发送入口，完成后辅助进程退出。

第二次调用以约 100 毫秒间隔采样辅助子进程树：最高采样 RSS 约 **246.4 MiB**，最高采样 CPU 约 **62%**。不包含 Go 测试主进程；RSS 不等于物理 footprint，采样最大值不保证捕获瞬时峰值。本次只证明一次真实 CLI 调用的资源水平，未证明 GUI 聊天同时运行时的性能。

低频标题之间通常间隔至少 30 分钟，辅助进程采用每次调用结束立即回收，不常驻等待下一次请求。全局最多 1 个标题请求；正文最多 6000 字，单次超时 90 秒。

## 集成验收

2026-11-13 将 21 个任务文件接入主目录，保留未暂存状态；任务工作区继续保留。未提交、登记合入或发布正式版。

主目录重新执行 `./scripts/go test ./internal/ui ./internal/claude ./internal/storage` 全部通过，`git diff --check` 通过。通过标准入口构建并启动公共 Preview：

```sh
./scripts/build preview
./scripts/run-preview 20261113T142748Z-e2fd77d739-2b45f4
```

启动回执确认构建 ID 为 `20261113T142748Z-e2fd77d739-2b45f4`，源码摘要为 `e2fd77d739c37bb68fb1cd3bb6c3434825f8766213da945385dba95a3a2efd93`，Preview PID 为 `17337`，使用隔离数据目录 `~/Library/Application Support/Fuyu Preview`。正式版 PID `6094` 继续运行。只读检查 Preview 数据库确认：辅助配置为空、辅助模型为空、辅助模型名称为空、自动生成会话标题为 `false`（关闭）。未修改用户配置或已有会话。

当前执行环境缺少 macOS 辅助功能和屏幕录制权限：原生操作返回 `osascript is not allowed assistive access (-25211)`，截图返回 `could not create image from display`。因此启动回执只作为应用初始化完成的证据，不能据此认定原生窗口交互通过。回执与权限阻塞记录保存于主目录 `.cache/auto-title-acceptance/result.json`。

原生 UI 验收尚未完成：设置菜单选择与重启保存、两类 Agent 实际问答后的 60 秒空闲触发、手动重新生成与改名保护、标题在页签和历史中的展示，以及前台聊天与标题调用并行时的响应仍待实测。30 分钟与 5 轮门槛已有确定性自动化覆盖，未在原生窗口等待完整周期。真实 CLI 调用使用开发机配置，未验证 Preview 隔离配置的真实调用。自动化、真实 CLI、Preview 启动与原生交互结论分别记录。
