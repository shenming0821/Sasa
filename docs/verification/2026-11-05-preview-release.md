# 2026-11-05 双渠道与候选晋升验证

## 范围与来源

实现正式版与 Preview 的独立应用身份、应用数据和 Agent 历史根，固定候选构建、按构建 ID 晋升、本机升级备份与回退。源码入口见[双渠道与发布](../subsystems/releases.md)。已有全文搜索改动保留在工作区，本轮候选会包含当前工作区功能代码。

## 已执行

- Go 全量测试通过，包含原有 UI 与工作区回归。
- Python 10 项临时目录发布测试通过：原候选程序晋升、篡改拒绝、复制期间变化拒绝、退出取消、数据锁拦截、启动失败恢复应用/SQLite/草稿、同版本回退保留新草稿、跨版本回退要求显式数据恢复、Preview/候选/备份目录目标拒绝、首次正式安装同步旧入口并保留旧应用。
- `internal/release` 测试覆盖包身份与渠道校验、首次配置复制与后续保留、不复制历史、凭据权限、拒绝原生目录符号链接、跨渠道与未标记数据库拦截、启动确认 token/PID/数据目录。
- 新增真实 Unix socket 控制通道回归：仅正确程序路径的 quit 请求触发退出回调；拒绝其他路径和其他动作、拒绝覆盖活动监听、退出清理 socket。应用端派发到主线程调用原有退出入口，现有生命周期回归继续覆盖任务/文件/保存失败门控。
- 最终 Go 全量 test、vet 与差异检查通过；发布身份、配置隔离、socket 和 UI 退出相关定向 race 通过。UI 整包 race 首轮通过；当前最终代码两次整包运行均在 `TestGitHunkStageConfirmsUnsavedEditorBeforeUsingDisk` 出现 `P4 state did not settle` 超时，该用例定向 race 重跑通过。未修改该用例或 Git 业务代码，整包 race 未全绿，超时原因待独立排查。

## 本机原生验收

- 固定候选：`20261105T025851Z-3b76ebface-a87969`；源码提交 `89b64da83b67d8035267eec99a62fa2858d4ce87`，源码摘要 `3b76ebface131a11dd5caa0fd69b36985fee0b4773151765ec7055a6b4ef14ac`。该候选包含当时未提交源码；随后发布工具与验证文档的增补不改变候选内已编译程序。
- `sips`/`iconutil` 生成双图标、完整包签名与严格核验通过；候选 Preview 的 `-version` 与启动确认匹配构建 ID。
- 实际窗口确认 Preview 标题含渠道与构建 ID、原正式版 Fuyu 窗口仍保留原项目与页签。旧正式版 PID 43370 始终未退出或替换；Preview 首次新版 PID 96018，经 socket 请求正常退出并重开为 PID 96736，启动确认和控制 socket 均存在。
- 实际读取 Preview SQLite 的 `runtime_identity`：Codex=`/Users/shenming/Library/Application Support/Fuyu Preview/runtimes/codex`，Claude=`/Users/shenming/Library/Application Support/Fuyu Preview/runtimes/claude`。与正式原生根分离。
- 在 `bin/release-verification-9w7diyr3/Applications/Fuyu.app` 与独立 `data/` 实际执行签名、晋升、窗口启动确认、正常退出、上一版回退及重开；升级前数据库 schema=4，回退保留升级后新增的 `verification-draft.txt`。原始正式版与 Preview 在测试后继续运行。隔离旧版测试进程通过 ⌘Q 正常退出。
- 晋升前后移除签名的程序 SHA-256 均为 `6091f24f90b12f19b76c813fcd3ba2ebb993f7773c090cdbb1d9a9a0e0027dfb`，验证未重新编译功能程序。真实恢复资料保留于上述测试目录的 `.Fuyu-releases/`。
- 原生测试发现旧版不响应 `NSRunningApplication.terminate`，⌘Q 可正常退出。因此新版增加私有 Unix socket 正常退出通道；旧正式版首次迁移可能需要用户先按 ⌘Q，脚本超时保留现场。

## 未验收与后续

- 正式版真实用户数据晋升须在 Preview 验收通过后由用户要求发布。
- Codex/Claude 真实模型请求、钥匙串登录与跨构建局域网权限尚未验收；配置与原生历史隔离的自动检查不替代这些验收。
- Finder/Dock 实际图标观感、真实活动任务/未保存文件下的发布阻塞提示尚未原生复验；后者由既有生命周期自动化与共享退出调用链覆盖。
