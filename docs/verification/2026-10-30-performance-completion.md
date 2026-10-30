# 对话性能项收尾验证

## 范围与实现

| 项目 | 当前行为 | 主要证据 |
|---|---|---|
| 窗口先显示 | 本地索引恢复后创建窗口，后台发现并连接 Agent，版本探测并行 | 前一阶段的隔离原生阻塞初始化验证，见[启动记录](2026-10-30-startup-catalog.md) |
| 模型和技能预加载 | 同 Agent/Runtime/cwd 共享结果与在途请求，Claude 共用一次目录探测 | `catalog_cache_test.go`、`model_catalog_test.go`，前一阶段真实双 Agent 目录验收 |
| Claude 连续对话 | 普通成功轮次复用进程，保持独立 TurnID/UUID/代次；2 分钟空闲回收、最多 4 个空闲进程 | `session_process_test.go` 及下方真实 Runtime 验证 |
| 首条输出和交互 | 首条非空输出、审批/提问、开始/结束、断线和错误立即派发，后续文字继续 40 ms/256 条合并 | `event_batch_test.go`，使用不触发的计时通道证明即时事件不等待定时器 |
| 重启后技能候选 | 成功元数据原子落盘，窗口创建后异步恢复，提供上次候选并强制原生校验 | `storage/catalog_test.go`、`completion_disk_test.go` |

进程复用保留原生配置继承。本地 slash 命令、失败、中断及仍有后台工具的结果继续排空并退出。模型、CLI、权限、凭据和直接关联的 CLAUDE/MCP 配置变化后重新初始化。

## 自动化

- `./scripts/go test ./...` 通过。
- `./scripts/go vet ./...` 通过。
- `./scripts/go test -race ./internal/claude ./internal/ui ./internal/storage -count=1` 通过。
- `./scripts/build` 通过。
- `git diff --check` 通过。

受控协议回归覆盖同 PID 双轮、历史恢复、应用重开、模型切换、配置变更、空闲进程提前退出、空闲超限回收、超时回收路径、旧审批失效、原生请求 ID 复用、提问、中断后续发、未知退出阻止重发。历史变化测试确认仅记账追加可以校准摘要，真实用户消息追加仍阻止发送。

技能磁盘缓存回归覆盖最多 32 项、单项最大 1 MiB、损坏缓存忽略、失败替换保留旧文件、重启后旧候选可见、失败刷新保留旧候选及成功刷新替换。

## 真实 Claude Runtime

测试采用本机 CLI 2.1.307、现有原生连接及临时工作目录。没有读取或改写用户工程文件，没有新增记忆文件。

### 双轮与重开

`FUYU_LIVE_CLAUDE=1 ./scripts/go test ./internal/claude -run '^TestLiveClaudeFileApprovalResume$' -count=1 -v` 通过。

同 PID 57188 完成文件任务和第二轮上下文回答，原生历史包含两轮；关闭 Client 后重开为 idle，第三轮继续保留上下文。首次实验发现 CLI 退出会补写记账信息，造成原始摘要变化；修复为完整对话投影一致时才能更新摘要，重跑通过。未将这类变化直接视为任意外部修改可接受。

### 允许、拒绝与提问

`FUYU_LIVE_CLAUDE=1 ./scripts/go test ./internal/claude -run '^TestLiveClaudeReusedApprovals$' -count=1 -v` 通过。

同 PID 65565 依次完成无工具预热、原生允许、原生拒绝和 AskUserQuestion。允许后 `allowed.txt` 精确为 `FUYU_ALLOWED`，拒绝后 `denied.txt` 不存在。为覆盖真实审批，该测试使用临时 CLI 包装入口指定 `--permission-mode default`；生产入口继续继承用户原生配置，未改写用户设置。

### 中断

`FUYU_LIVE_CLAUDE=1 ./scripts/go test ./internal/claude -run '^TestLiveClaudeInterruptRunningTool$' -count=1 -v` 通过。

前台 Bash、后台 Bash、同一进程的第二轮 Bash 三种场景均收到 interrupted。分别确认 Bash PID 64845、65014、65526 不存在，延迟写入的 `forbidden.txt` 没有产生。第二轮测试额外断言实际复用预热进程；这是已执行场景的证据，不推断所有分离进程均已停止。

### 分段耗时样本

`FUYU_LIVE_CLAUDE=1 ./scripts/go test ./internal/claude -run '^TestLiveClaudeProcessLatency$' -count=1 -v` 通过。三轮使用同一条无工具提示，记录发送准备、首条可见内容和完成时间：

| 场景 | PID | 发送准备 | 首条可见内容 | 完成 |
|---|---|---|---|---|
| 首次启动 | 78160 | 643.1 ms | 2.690 s | 2.766 s |
| 同进程续聊 | 78160 | 5.95 ms | 0.975 s | 1.051 s |
| 回收后 resume | 78346 | 367.4 ms | 1.538 s | 1.645 s |

发送准备包含本地检查点及初始化，至用户帧写入返回为止。首条可见内容为协议事件观测时间，不包含原生 UI 绘制。该表是单次样本，模型服务、上下文和缓存状态仍有差异，不能作为固定收益或长期统计。

## 最终原生交接

最终测试与构建完成时曾因锁屏无法读取窗口。解锁后通过原生窗口确认用户没有打开的标签和活动任务，正常退出旧实例 PID 691，确认进程已退出。

最终构建使用隔离数据目录 `/private/var/folders/8t/brc73fcs07d1slb8w2wdx66h0000gn/T/fuyu-final-cache-462oi0xw` 验收磁盘缓存。预置一项 `cached-skill` 技能元数据，模拟 Codex 记录 initialize 请求后持续等待、不返回初始化结果。首次窗口可见且输入可编辑，输入 `$` 后原生候选显示 `cached-skill`，发送保持禁用。随后正常退出并以相同隔离目录重启，实例 PID 95355 在恢复 `$` 草稿后直接显示该候选；同时确认模拟进程 PID 95468 存活、日志只有 initialize 请求，因此候选来自磁盘而非本次 Runtime 响应。正常退出后确认两个 PID 均已消失，没有发送模型任务。

最后启动默认数据目录的最终实例 PID 96169。原生 AX 与截图确认窗口可用、Codex 和 Claude 历史均已加载，恢复用户原来的无打开标签状态。隔离草稿和缓存留在临时数据目录，未写入用户正常会话。最终原生交接完成。
