# 会话占用交互验收

日期：2026-10-23。范围：参考用户提供的 ChatGPT 截图，处理外部 writer 占用时的历史阅读、底部提示卡、重试与独立分叉。

## 协议依据

本机 `codex-cli 0.163.0` 通过 `codex app-server generate-json-schema --experimental` 生成 schema，确认：

- `thread/read(includeTurns=false)` 读取元信息，历史按 `thread/turns/list(itemsView=full, limit=20, sortDirection=desc)` 分页。
- `thread/fork` 支持 `threadId`、`excludeTurns=true`、`deferGoalContinuation=true`，返回新 thread 与模型、目录、审批策略及沙箱。
- 新分叉直接使用返回状态加载历史，等待用户发送任务。

对已被当前 Codex 进程占用的原生会话，只执行真实 `thread/read` 和 `thread/turns/list`：元信息成功，状态为 `notLoaded`、内嵌 turns 为 0；最新页成功返回 20 个 turns，并有下一页。探测进程正常退出，原 writer 保持运行。此次探测没有创建真实分叉、发送模型任务或更改原会话写入权。

## 自动化验证

通过命令：

```sh
./scripts/go test ./...
./scripts/go vet ./...
./scripts/go test -race ./...
./scripts/build
git diff --check
```

新增回归覆盖：

1. 精确识别对应 thread 的原生 writer 冲突；其他错误仍显示恢复失败。
2. 占用时加载历史并替换输入区，两个操作在 1000 和 640 DIP 窗口内可见；外部事件不会赋予 Busy 或控制权。
3. 占用释放后重试恢复原 thread、输入区和草稿；历史读取失败保留卡片及已加载历史。
4. 分叉建立独立 ID，原会话和草稿保留，重复点击受限，切换会话后的异步结果保持原归属。
5. 明确拒绝和连接丢失分开处理；结果不确定时禁止再次创建，避免重复分叉。
6. 新身份在历史回调前已写入 SQLite；重启后重新核验，草稿和原索引保留。分叉历史加载不调用额外的 resume。
7. Python 协议夹具覆盖占用错误、只读元信息、分页、独立分叉、原 writer 保留和解除占用后重试。

## 原生窗口验证

使用隔离 bundle ID `com.shenming.fuyu.occupied`、独立 SQLite 和本仓库 Python 协议夹具。测试根目录指针为 `/tmp/fuyu-occupied-root`，本次值：

```text
/var/folders/8t/brc73fcs07d1slb8w2wdx66h0000gn/T/fuyu-occupied-aof1y6b4
```

| 操作 | 观察 |
|---|---|
| 打开占用中的历史 | 消息可读；底部浅灰圆角卡显示锁图标、粗体标题、灰色说明及两个文字按钮，输入区收起 |
| 保持占用并点击重试 | 保留卡片与历史 |
| 点击创建聊天分叉 | 出现独立标签，历史保留，输入区恢复，中文草稿为“保留原会话的中文草稿” |
| 切回原标签 | 原历史及占用卡保留 |
| 移除夹具占用标记后重试 | 原标签恢复输入，中文草稿保留 |
| 重开 640×480 隔离窗口，保持右侧历史展开 | 历史与会话并排，卡片说明和两个按钮完整可见；原会话与分叉索引均恢复 |

640×480 使用 Go build overlay，仅改变隔离预览的初始窗口尺寸；正式构建保持 1000×760、最小 640×480。截图在本轮工具会话内核对。

最终构建再次完成 GUI 分叉，协议日志在 `thread/fork` 后只有新 ID 的 `thread/turns/list`，未调用额外的 `thread/resume` 或 `turn/start`。SQLite 同时保留原会话和两个主动创建的分叉，各自草稿相同。测试实例及自有协议进程已退出，用户原 Fuyu 实例保持运行。

## 边界与交付

原生 GUI 的分叉和解锁流程使用受控夹具，不代表真实 Codex 分叉或模型执行已验收。真实 Runtime 本轮只验证占用中的只读接口；原有真实会话与其他应用进程保持运行。独立分叉与原会话共享工作目录，分叉的独立性指会话身份与对话历史，并未创建新的 Git worktree。

正式 `bin/Fuyu.app` 已构建。用户原有 Fuyu 窗口保留；重启正式应用后使用新构建。提交包含实现、测试、子系统说明、实施计划和本验收记录；提交后以 Git 状态核对工作区与暂存区均干净。
