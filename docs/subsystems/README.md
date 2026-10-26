# 子系统索引

## 当前阶段

P1 已建立下列源码包，P2 补充任务交互，P3 加入多会话、SQLite 索引、原生历史恢复和窗口驻留，当前环境验收已完成。实际协议、状态和生命周期见[Codex 运行时](codex-runtime.md)、[会话管理](sessions.md)与[存储与恢复](storage.md)。

| 子系统 | 责任与边界 | 源码或规划路径 | 当前参考 |
|---|---|---|---|
| 项目管理 | 本地仓库导入、Git worktree 发现、分支和会话导航 | `internal/project/`、`internal/ui/projects.go` | [项目管理](projects.md) |
| 文件与编辑器 | 文件树、编辑保存、草稿恢复与只读 Git 差异；P4 验收进行中 | `internal/editor/`、`internal/workspace/`、`internal/git/`、`internal/ui/editor_*.go` | [文件、编辑器与 Git](editor.md) |
| 原生 UI | 会话侧栏、消息、输入、状态和交互入口；会话绑定的异步派发 | `internal/ui/` | [会话管理](sessions.md)、[实施计划](../implementation-plan.md) |
| 应用状态 | thread/turn 投影、活动会话与用户动作协调 | `internal/app/` | [设计](../design.md) |
| Codex 客户端 | 双向 RPC、消息分类、请求关联与协议事件 | `internal/codex/` | [协议核验](../reference-verification.md) |
| 进程管理 | 自有 App Server 启停、cwd、退出检测与取消 | `internal/runtime/` | [生命周期设计](../design.md) |
| 存储与恢复 | 应用索引、界面状态与原生历史核验，P3 引入 | `internal/storage/` | [存储与恢复](storage.md)、[P3 计划](../implementation-plan.md) |

## 何时建立专项文档

一个子系统已经有实际实现，且职责、状态或兼容规则需要独立说明时，在本目录建立专项文档并从表中链接。

专项文档写清：

- 负责的行为、调用入口和与其他组件的边界。
- 权威数据、缓存与资源归属。
- 主要路径、状态转换、失败和兼容行为。
- 实际源码文件、关键符号和可复核的验证入口。

计划中的变化放在实施计划或设计方案，当前行为根据实际实现维护。

## 修改与审核

契约、状态、资源归属或错误处理变化时，同一轮更新所属文档和验证。审核沿入口追踪到实际结果，核对说明、源码与证据是否一致。

内部整理保持已有契约时，可只说明保持的行为和验证结果。文档中的“支持”应有实际实现和对应验证依据。
