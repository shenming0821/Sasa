# Codex 原生任务链路

## 入口与资源归属

`cmd/desktop/main.go` 校验初始目录并启动自有 App Server。`run()` 返回前执行 `Client.Close()`，随后 main 才根据错误设置退出码。Fuyu 不连接或终止共享 Codex daemon，不改动用户登录、模型或权限配置。

桌面入口先调用 `internal/runtime/codex.go` 的 `ResolveCodex`：根据当前用户目录、继承环境和有时限的交互式登录 shell 探测定位可执行文件，并返回包含 Node.js 安装目录的 PATH。仅在 UI 与 App Server 创建前更新 Fuyu 进程的 PATH；保留其余环境、`CODEX_HOME` 与用户原生配置。显式路径失效时直接报错，不替换为另一安装。脚本包装入口单独检查 Node.js 依赖；自动搜索不使用项目当前目录。版本探测与 App Server 初始化分别采用 5 秒和 60 秒截止时间，普通 RPC 截止时间保持原约定。安装方式与回归证据见[启动路径验收](../verification/2026-10-23-codex-discovery.md)。

`internal/runtime/` 通过参数数组启动进程，为其建立独立进程组。stdio 关闭后读取者负责清理自有组并回收 leader；正常关闭先关闭 stdin，等待最多 2 秒，超时再终止自有组。隔离测试确认：忽略 EOF 的 leader 能强制退出并回收；另建进程组的后代仍可能残留。当前只保证原进程组清理，不保证所有后代清理，也不以模糊名称匹配终止进程。测试自行按精确 PID 清理受控后代。

## 协议客户端

`internal/codex/client.go` 使用 `codex app-server --listen stdio://`，完成 `initialize` / `initialized`，关联客户端请求 ID，独立处理通知、服务端请求、响应与错误。协议按本机 0.163.0 生成的 JSON schema 核验。

读取与事件消费分离，UI 暂停消费期间，RPC 响应仍可返回。初始化声明 `experimentalApi=true`，使用当前版本的原生提问协议。单条协议输入上限 16 MiB；UI 每条文字消息显示上限 2 MiB。

`internal/codex/display.go` 在事件入队前裁剪最终工具输出和文件内容，保留原生 ID、状态、退出码与截断标记，防止最终聚合事件绕过显示上限。事件队列限制为 4 MiB / 4096 条；超限先移除可丢弃的文字/工具增量，并产生 `fuyu/eventGap`。生命周期和审批事件不静默丢弃；仅控制事件就超过容量时明确断开自有连接，提示结果需要核验。

`internal/codex/interactions.go` 注册原生请求，`request.go` 解码 thread/turn/item ID、审批选项与问题结构。单个请求上限 256 KiB，同时最多保留 16 个待解决请求。请求 ID 保留字符串或 int64 类型；响应前进行一次性占用，写入失败也不能盲目再次批准。响应写入截止时间为 10 秒。`serverRequest/resolved` 按 thread/request ID 释放；turn 完成时按 thread/turn 释放。

命令审批保留 `availableDecisions` 的顺序和原始 JSON，包括命令规则与网络策略修订。文件审批提供原生 accept / acceptForSession / decline / cancel，并展示已有文件摘要和请求写入范围。`grantRoot` 在 0.163.0 schema 中为实验字段，不能据本地按钮点击宣称服务端已经持久授权。额外权限只提交请求的 profile，scope=`turn` 或 `session`；拒绝提交空权限和 scope=`turn`。

用户提问按原生 question ID 映射答案，保留 options、isOther、isSecret 和 isBlocking；取消发送空 answers。MCP elicitation 不在当前设计范围，返回 `decline` 并显示原因；其他未支持请求返回 -32601，无效/过大的已知交互返回 -32602。

## 状态与 UI

`internal/app/state.go` 在 UI 线程投影 thread、turn、消息与连接状态。增量与最终文本按 item ID 合并；已结束 turn 的重复事件不能结束下一次提交。连接失效后保持失联，晚到事件不恢复执行。

`internal/ui/app.go` 通过 `Window.Update` 将后台结果投递到主线程。MyGo 的 Update **异步返回**，不能将其当成同步屏障。事件按 40 ms / 最多 256 条批量应用，等待本批主线程回调完成后再派发下一批，同时限制 UI 的待执行工作量；使用原生虚拟列表、稳定消息 ID 与跟随末尾。

工具投影位于 `internal/app/tools.go`，卡片位于 `internal/ui/activity.go`。命令默认显示最多四行并使用省略号结束；含命令的卡片通过“展开详情”查看完整命令及输入/输出。详情使用最高 180 DIP 的独立滚动区，卡片裁切绘制边界，长脚本不会覆盖元数据和后续消息。纯输出卡片保留“展开输出”入口。命令、文件、MCP/动态工具按 item ID 合并，命令输出可折叠，展开区域最高 180 DIP。单项输出/文件内容裁剪为 256 KiB；总显示缓存按字节估算限制为 8 MiB / 1000 条消息，优先移除较早且非活动交互的显示项并提示。该上限控制显示数据，不能作为整个进程的 RSS 上限。

`internal/app/snapshot.go` 在增量缺口后以 `thread/read(includeTurns=true)` 核验相同 thread/turn 的原生终态和最终 items；错误或不匹配时保留“不完整”，提供重新核验入口。该路径用于当前连接的缺口校准；重启后的历史恢复由 `thread/resume` 与分页 `thread/turns/list` 完成，见[会话管理](sessions.md)。

`internal/codex/history.go` 为外部 writer 冲突提供精确 RPC 分类，要求错误码为 `-32600`，消息中的 thread ID 与请求一致。此时以 `thread/read(includeTurns=false)` 和 `thread/turns/list` 读取未加载会话，不取得原 thread 写入权。“创建聊天分叉”使用 `thread/fork(threadId, excludeTurns=true, deferGoalContinuation=true)`，保留原生配置和独立 ID；直接使用返回状态加载历史，不再次 resume 新分叉或自动发起 turn。参数由本机 0.163.0 的 experimental JSON schema 核验；真实只读接口及受控分叉验收分别见[占用交互记录](../verification/2026-10-23-occupied-session.md)。

交互状态位于 `internal/app/interactions.go`：待处理、提交中、已响应、已解决、已取消、已失效、发送待核验。任务停止、结束或断线会清除回答并关闭入口；响应失败保留错误和核验提示，禁止重复提交。秘密回答只传入协议响应，不写入时间线。构建包装对 MyGo 的 macOS AX setter 做隔离修复，防止自动填充将秘密原文留在无障碍值中。

待办按钮定位最早待处理交互；自己提交的响应解决后定位后续流程，其他请求的解决事件保留阅读位置。输入区始终独立于工具卡片和表单滚动。

具体视图位于 `internal/ui/view.go`：紧凑目录工具栏，消息与一体输入区共享 800 DIP 最大宽度，用户气泡右对齐；运行参数在只读弹层展示，完成回复可复制 Markdown。离开末尾时，返回按钮浮在输入区上方 20 DIP，底部 80 DIP 叠加从透明到主题背景色的渐变；装饰层透传输入，列表视口不随按钮显隐变化，末尾固定内容留白保证最后一条消息可读。可重试错误的对应提示在同 turn 恢复输出或到达终态时清除，其他错误保留。

`internal/ui/thinking.go` 从当前 Busy、连接、错误、停止状态和最后一条用户消息之后的可见内容派生等待提示。正文或工具尚未出现时，在列表内追加独立稳定键的“正在思考”行；空 assistant 事件不生成空白列表行。提示为 UI 投影，不写入消息或复制结果。文字渐变通过局部绘制和组件 Loop 驱动，不增加计时 goroutine；离开可见列表或提示收起后不再请求其动画帧，ReduceMotion 时保持静态。

普通 Enter 发送，`⌘ Enter` 换行。渲染时读取输入框组合状态，在键盘回调中使用该值；回调不能保留并操作上一渲染 pass 的 Element。组件测试覆盖组合输入期间的发送抑制；本机真实拼音输入已验证组合期间 Enter 提交拼音原文而不发送、空格选出中文、组合结束后 Enter 发送，以及中文正文的 `⌘ Enter` 换行。独立候选浮窗的外观与其他输入法兼容性未覆盖，详见 P1 验收末节。

Markdown 的列表缩进设为 3 em，兼容锁定版本按比例数字测宽、实际用 tnum 渲染造成的编号换行。逐项增长的组件回归覆盖 1–120；不是任意位数编号的上游修复。

创建会话被明确 RPC 错误拒绝且尚未调用 turn/start 时，保留草稿并允许重新发送。提交结果不确定或连接丢失时，保留输入、禁止重发，并提示重新启动及核验记录。原生任务失败提供重试当前任务入口，可重试错误随同一 turn 的输出恢复而清除。停止 RPC 成功后继续等待原生 `turn/completed`，只在终态为 `interrupted` 时显示中断；它不代表独立后台命令已退出。工具完成状态只由 item 事件决定，turn 结束不会批量改写工具状态；已知工具的晚到终态仍可更新卡片，并清除对应的未结束警告。

## 当前边界

P3 提供多个独立会话，共享一个 Fuyu 自有 App Server；各会话独立绑定 cwd 和原生 thread/turn，异步回调固定所属会话。历史来自原生接口，应用索引只保存界面元数据，详见[会话管理](sessions.md)与[存储与恢复](storage.md)。

关闭窗口保存状态并隐藏，Dock 菜单或再次激活应用可打开原窗口。退出时列出全部活动任务，选择停止后逐个中断，最多等待 10 秒；未确认时提供返回核验或强制退出。连接丢失保留未确认任务，不能代替原生终态。进程清理仍只作用于自有原进程组。最终原生验收覆盖与支持边界见 [P3 记录](../verification/2026-10-22-p3.md)。
