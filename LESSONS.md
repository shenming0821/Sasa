# 经验记录

## 使用方式

修改代码前阅读本文。遇到已复现的问题或交付遗漏时，记录触发场景、原因、可执行规则和证据。反复适用的规则整理进[代码标准](docs/code-standards.md)，本文保留案例与链接。

实际回归与运行时观察见下文；参考项目的经验与 Fuyu 的实测结果分别记录。

## 设计阶段已确认的经验

| 场景 | 后续动作 | 依据 |
|---|---|---|
| 单独完成 UI 骨架仍无法确认 Runtime 交互可行 | 在 P1 将原生窗口与真实 Codex 任务连起来，验收文件操作、连续对话和中断 | [实施计划 P1](docs/implementation-plan.md#3-p1真实任务最小闭环) |
| 当前阶段与未来能力混在一起，容易扩大首轮范围 | 实现从当前接续点开始，完成验收后推进下一阶段 | [实施计划](docs/implementation-plan.md) |
| 恢复历史与重新接入活动任务具有不同条件 | 根据原生状态和进程归属分别验证，记录实际恢复结果 | [设计文档](docs/design.md)、[参考核验](docs/reference-verification.md) |

## 新案例格式

```text
日期与问题：
触发条件与可观察结果：
原因：
在什么条件下，执行什么动作：
证据：源码/提交/验证记录路径
规则归属：代码标准或相关子系统文档
```

## 2026-10-22：P1 实测案例

| 触发场景与原因 | 后续动作 | 证据 |
|---|---|---|
| 新提交尚未拿到 turn ID，旧 completed 可能清除 Busy | 保存已结束 turn ID，忽略其重复和晚到事件 | `internal/app/state_test.go` |
| Cmd+Enter 在输入法组合期间仍触发回调 | 在每次渲染读取 Composing，用于发送门控；在组件中复现候选输入与发送快捷键的组合 | `internal/ui/app_test.go` |
| 在键盘回调查询上一次渲染的 Element 引发过期 pass panic | 跨回调保留必要状态值；需持久定位时使用框架 Handle，避免直接持有 Element 调用方法 | `internal/ui/app.go`、[P1 验收](docs/verification/2026-10-22-p1.md) |
| turn 已完成/中断，独立后台 sleep 仍运行 | 分别以 turn 与 item 的原生事件核对终态，停止后检查实际子进程；只对实际覆盖的退出路径声明清理通过 | [P1 原生窗口验收](docs/verification/2026-10-22-p1.md) |
| main 在资源创建后 os.Exit 跳过 defer | 将有资源的工作放入返回 error 的 run()，退出码在 run 返回后设置 | `cmd/desktop/main.go` |
| Markdown 编号按比例数字测宽，实际使用 tnum 数字渲染，10 起出现句点换行 | 为当前列表设置 3 em 缩进，逐项增长验证 1–120 的编号高度；依赖修复后重新核验再移除此兼容设置 | `internal/ui/app.go`、`TestStreamListMarkersStayOnOneLine` |
| 服务端报告 willRetry 后恢复输出，旧重连提示仍停留在已完成界面 | 按 turn 跟踪临时重试提示，恢复输出或到达终态时移除对应提示，保留其他警告 | `internal/app/state.go`、`TestRetryNoticeClearsWhenOutputResumes` |
