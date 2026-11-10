# Devin 完整文件快照验证

## 原因与修复

本机 Devin 原生会话记录中，编辑开始通知的 diff 只有空 newText 占位，完成通知才携带完整文件 oldText/newText。原适配器只投影开始通知，遗漏完成快照，并将 diff 一律当作无位置区域，导致变更卡片统计不完整。

实时和历史回放均接收完成快照，使用带 `snapshot:true` 标记的文件完整替换补丁。汇总校验连续快照的完整行数、内容和末尾换行，按本轮初始与最终版本计算净差异。命令执行附带的文件快照通过独立文件变更条目参与汇总。统计仍仅读取协议内容。

## 自动化验证

`internal/devin/changes_test.go` 覆盖实时通知与历史回放的一致性、重复编辑、改回原文、重复行、删除后恢复、末尾换行、空文件、命令附带编辑、新增及删除、缺失快照、占位内容、前后内容和行数冲突。原有 region 保守合并测试继续保留。

以下检查通过：

- `./scripts/go test ./internal/devin ./internal/app ./internal/ui`
- `./scripts/go test -race ./internal/devin ./internal/app`
- `./scripts/go vet ./internal/devin ./internal/app ./internal/ui`
- `git diff --check`

针对本机 `aboard-agenda` 会话，从原生数据库只读提取 15 次带 diff 的调用，使用临时检查重放到应用统计，并以每个文件首次 oldText 和最后 newText 在临时目录生成 Git 对照；原文件及工作区 Git 均未参与统计。实时与历史回放均得到以下精确结果，6 个文件全部与对照一致：

| 文件 | 新增 | 删除 |
| --- | ---: | ---: |
| panes.go | 122 | 14 |
| panes_view.go | 56 | 0 |
| navigation.go | 2 | 0 |
| empty_tabs.go | 1 | 0 |
| panes_test.go | 128 | 0 |
| sessions.md | 1 | 1 |

原始会话内容和临时检查不进入仓库。此检查使用已有真实协议记录，未提交新模型任务。

## Preview 与边界

`./scripts/build preview` 构建候选 `20261110T081548Z-ecf83a2c16-0d0d3b`，经 `./scripts/run-preview 20261110T081548Z-ecf83a2c16-0d0d3b` 启动成功，启动回执和原生无障碍树均确认对应版本窗口已恢复并可呈现内容。构建包含当时工作区另一路 Git 面板改动。

原生 UI 本次确认窗口可用；尚未通过真实 Runtime 重新加载会话并点击变更卡片执行端到端验收。未执行正式版发布。缺失或冲突快照、旧区域记录、协议缺口、显示裁剪及计算预算限制继续标为统计不完整。
