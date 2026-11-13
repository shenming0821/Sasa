# 子系统索引

## 当前阶段

下列源码包覆盖真实任务、过程交互、多会话、SQLite 索引、原生历史恢复和窗口驻留，当前环境验收已完成。实际协议、状态和生命周期见[Codex 运行时](codex-runtime.md)、[会话管理](sessions.md)与[存储与恢复](storage.md)。

| 子系统 | 责任与边界 | 源码或规划路径 | 当前参考 |
|---|---|---|---|
| 项目管理 | 本地仓库导入、Git worktree 发现、分支和会话导航 | `internal/project/`、`internal/ui/projects.go` | [项目管理](projects.md) |
| 文件与编辑器 | 文件树、编辑保存、草稿恢复、Git 差异和显式工作流操作 | `internal/editor/`、`internal/workspace/`、`internal/git/`、`internal/ui/editor_*.go`、`internal/ui/git_*.go` | [文件、编辑器与 Git](editor.md) |
| 原生 UI | 会话侧栏、消息、输入、状态和交互入口；会话绑定的异步派发 | `internal/ui/` | [会话管理](sessions.md)、[本轮文件变更](turn-changes.md) |
| 主窗口分屏 | 按目录保存的嵌套布局、面板页签、焦点与终端可见性 | `internal/ui/panes.go`、`internal/ui/panes_view.go` | [会话管理](sessions.md#主内容区分屏)、[分屏验证](../verification/2026-11-03-split-panes.md) |
| 图片附件 | 图片导入、私有副本、草稿、发送、历史恢复及预览 | `internal/attachment/`、`internal/ui/images.go` | [图片附件](attachments.md) |
| 应用状态 | thread/turn 投影、活动会话与用户动作协调 | `internal/app/` | [会话管理](sessions.md)、[Codex 运行时](codex-runtime.md) |
| Agent 抽象 | 会话身份、能力、应用事件、审批意图和任务映射；当前 Codex 兼容接入 | `internal/agent/`、`internal/ui/agents.go` | [Agent 抽象层](agent.md) |
| Codex 客户端 | 双向 RPC、消息分类、请求关联与协议事件 | `internal/codex/` | [协议核验](../reference-verification.md) |
| Claude 客户端 | stream-json 控制协议、单轮进程、审批、问答与自有会话恢复 | `internal/claude/` | [Claude 运行时](claude-runtime.md) |
| 进程管理 | 自有 App Server 启停、cwd、退出检测与取消 | `internal/runtime/` | [Codex 运行时](codex-runtime.md)、[Claude 运行时](claude-runtime.md) |
| 原生终端 | 当前工作区终端标签、Ghostty AppKit/Metal 视图、本机配置、PTY 与进程生命周期 | `internal/terminal/`、`internal/ui/terminal.go`、`scripts/prepare-ghostty.py` | [终端验证](../verification/2026-11-01-terminal.md) |
| 存储与恢复 | 应用索引、界面状态与原生历史核验 | `internal/storage/` | [存储与恢复](storage.md) |
| 双渠道与发布 | Preview 候选构建、渠道数据隔离、正式版晋升和恢复 | `internal/release/`、`scripts/release.py` | [双渠道与发布](releases.md) |

## 何时建立专项文档

一个子系统已经有实际实现，且职责、状态或兼容规则需要独立说明时，在本目录建立专项文档并从表中链接。

专项文档写清：

- 负责的行为、调用入口和与其他组件的边界。
- 权威数据、缓存与资源归属。
- 主要路径、状态转换、失败和兼容行为。
- 实际源码文件、关键符号和可复核的验证入口。

技能目录共享缓存、延迟保存、自适应后台维护、消息投影和文件事件合并见[使用性能与后台维护](performance.md)。

当前行为根据实际实现维护。

## 修改与审核

契约、状态、资源归属或错误处理变化时，同一轮更新所属文档和验证。审核沿入口追踪到实际结果，核对说明、源码与证据是否一致。

内部整理保持已有契约时，可只说明保持的行为和验证结果。文档中的“支持”应有实际实现和对应验证依据。

应用级外观、字体与完成通知见[设置与完成通知](settings.md)。

Agent 抽象、Claude adapter 与 Agent 选择入口已实现于工作区；完整 race、真实双 Agent 并行、子代理/后台工具和前后台 Bash 停止已通过。用户重新授权后的原生文件审批、拒绝反馈、多选问答、模型选择、Agent 切换、手动压缩续聊和停止已取得证据；流式停止后的历史中断标记修复见 Claude 接入与双 Agent 支持验证记录。覆盖边界见 [Claude 接入与双 Agent 支持验证记录](../verification/2026-10-28-p6-agent.md)。
