# Fuyu 设计文档 v2.2

> 更新日期：2026-10-22  
> 当前交付目标：Codex 原生 UI 交互  
> 原始方案：`/Users/shenming/Downloads/Fuyu 设计文档v2.0.md`  
> 参考源码与核验边界：[参考项目核验](reference-verification.md)  
> 实施顺序、验收与接续状态：[实施计划](implementation-plan.md)

## 1. 当前产品范围

Fuyu 使用 Go + MyGo Native UI，为 Codex 原生 Runtime 提供桌面交互。第一阶段围绕一个用户选择的本地工作目录，逐步完成创建会话、连续对话、流式输出、工具展示、审批、回答问题、中断及历史恢复。

多 Agent、Repository → Worktree 管理、文件浏览和 Git Review 保留为后续方向。当前只实现 Codex，不为了未来的 Adapter 提前建立完整插件框架。

### 1.1 当前需要实现

- macOS 原生窗口、会话侧栏、消息时间线、输入区和连接状态。
- 选择本地目录，在创建会话前展示实际工作目录。
- 使用本机已安装的 Codex 与其现有凭据、项目规则和配置。
- 通过 App Server 结构化协议创建/读取/恢复 thread，启动和中断 turn。
- 显示文字流、工具生命周期、命令输出、文件修改摘要和错误。
- 将原生审批与用户提问转换为可操作的界面。
- 切换会话时保留草稿、滚动位置及后台任务。
- 保存 Fuyu 的会话索引和界面状态，重启后核验原生历史及运行状态。

### 1.2 后续阶段

| 能力 | 安排 |
|---|---|
| Claude Code、OpenCode 及统一 Adapter 框架 | Codex 交互稳定后单独规划 |
| Repository / Worktree 自动发现和管理 | Codex 第一版验收后 |
| 文件树、代码只读查看、完整 Git Diff | 后续代码审查阶段 |
| Commit Graph、Blame、暂存与提交 | 后续 Git 功能阶段 |
| 原生终端、远程 Daemon、退出后持续运行 | 后续运行管理阶段 |
| Windows / Linux | macOS 交互验证后 |

首版允许选择非 Git 目录。会话绑定绝对工作目录，未来通过迁移补充 repository_id / worktree_id。

## 2. 从参考项目落实的决定

| 问题 | 已核验依据 | Fuyu 决定 |
|---|---|---|
| 流式聊天如何实现 | MyGo `examples/markdown-chat/main.go` | 使用原生消息列表、流式 Markdown 与完成态 Markdown |
| 后台事件如何更新 UI | MyGo `docs/ui/views.md`；Murmur `app.go` | 协议读取放在后台，界面状态通过 `Window.Update` 更新 |
| 滚动如何保持 | MyGo `docs/ui/list.md`；Murmur `view.go` | 稳定消息 ID；跟随末尾，用户向上滚动后保持当前位置 |
| Session 与 Run 如何区分 | Codex `v2/thread.rs`、`v2/turn.rs` | Session 对应 thread，Run 对应 turn，连接状态独立保存 |
| 是否只有批准/拒绝 | Codex `common.rs`、`v2/item.rs` | 审批与用户提问分别建模，保留原生请求 ID |
| 恢复运行中任务是否可行 | Codex `thread_resume.rs` 相关测试 | 原进程仍存活时核验并重新接入；进程已结束时恢复历史 |
| RPC 如何处理双向请求 | cdesktop Codex `jsonrpc.rs`、`client.rs` | 区分响应、服务端请求、通知和错误，分别路由 |
| 关闭窗口如何继续驻留 | Murmur `tray.go` | 区分窗口关闭与应用退出；后台继续依赖 Fuyu 进程存活 |

以上是源码存在的机制及 Fuyu 的设计选择，不表示 Fuyu 已经实现或通过运行验收。具体定位和固定提交见核验文档。

## 3. 最小架构

```text
MyGo UI
  会话列表 / 时间线 / 输入区 / 审批与问题表单
       |
Application Controller
  会话选择 / 单次执行 / 请求状态 / 恢复核验
       |
Codex Client
  双向 JSON-RPC / 请求关联 / 事件解码 / 连接生命周期
       |
Fuyu 启动并持有的 Codex App Server 子进程
       |
Codex 原生 Runtime、配置与历史
```

UI 不解析协议 JSON，也不直接写子进程 stdin。Controller 接收类型明确的事件并更新视图模型。

第一步由应用持有一个 App Server 连接，先完成单个 thread 的真实任务闭环。第三步再加入多个 thread 的管理与切换；单个 thread 同时只允许一个活动 turn，跨会话切换不改变 cwd，也不取消活动 turn。

### 3.1 本地接入约定

- 使用参数数组启动 `codex app-server --listen stdio://`，由 Fuyu 持有其 stdin/stdout/stderr 和退出句柄。
- 读取 stdout 上的逐行 JSON 消息，stderr 单独处理；日志不能混入协议流。
- 启动后发送 `initialize`，收到成功响应后发送 `initialized`，再允许业务请求。
- 为客户端请求建立 pending 表；服务端请求使用独立的待处理表，不能混用 ID 空间。
- stdin 写入串行化；读取循环持续处理消息，用户审批期间也保持读取。
- 首版不接管已有 Codex Daemon，不执行其启停或升级命令。若安装版本改变进程拓扑，先验证实际进程归属，再开放任务运行。
- 启动时读取并显示 Codex 实际版本；协议字段与安装版本生成的 schema 对齐。
- 保留 Codex 配置继承；仅覆盖用户在 Fuyu 明确选择的设置。界面同时显示实际生效的模型、工作目录和权限设置。

### 3.2 协议操作与界面行为

| 操作/事件 | 用途 | 界面行为 |
|---|---|---|
| `initialize` / `initialized` | 连接握手 | 成功后输入区进入可发送状态 |
| `thread/start` | 新建会话 | 保存 thread ID 和响应中的实际配置 |
| `thread/list` | 发现历史 | 分页显示，并按实际 cwd 提供分组/筛选 |
| `thread/read` | 读取历史元数据 | 查看历史不直接等同于恢复活动运行 |
| `thread/resume` | 加载/重新接入会话 | 核验状态并建立后续事件接收 |
| `thread/turns/list` / `thread/items/list` | 分页历史 | 按安装版本能力加载，保持滚动位置 |
| `turn/start` | 发送本轮输入 | 响应表示创建执行，完成状态由后续事件确认 |
| `turn/started` / `turn/completed` | 本轮生命周期 | 更新活动状态与最终结果 |
| `item/started` / `item/completed` | 消息、工具生命周期 | 按 item ID 更新已有卡片 |
| `item/agentMessage/delta` | 助手文字增量 | 追加到对应 item，完成态使用完整快照校准 |
| `item/commandExecution/outputDelta` | 命令输出增量 | 追加到相应工具卡片，支持折叠和容量限制 |
| `thread/status/changed` | thread 原生状态变化 | 更新原生状态和等待标识 |
| `item/commandExecution/requestApproval` | 命令审批 | 显示命令、cwd、原因及实际允许的决定 |
| `item/fileChange/requestApproval` | 文件修改审批 | 结合对应 item 显示修改范围并响应 |
| `item/permissions/requestApproval` | 额外权限申请 | 按具体权限结构展示和响应 |
| `item/tool/requestUserInput` | 用户问题 | 根据问题 ID、选项和输入属性生成表单 |
| `serverRequest/resolved` | 请求已解决 | 关闭对应待处理入口，禁止重复提交 |
| `turn/interrupt` | 请求中断 | 显示“正在停止”，等待实际终态 |

此表列出设计基线。实验字段、分页模式及未知请求的兼容性按固定安装版本验证，不能由方法名推断能力。

## 4. 状态与数据所有权

### 4.1 三种状态分别维护

| 对象 | 状态 |
|---|---|
| Fuyu 连接 | stopped / starting / connected / disconnected / stopping |
| Codex thread 原生状态 | notLoaded / idle / systemError / active |
| Codex turn 原生状态 | inProgress / completed / interrupted / failed |
| Fuyu 本地交互状态 | submitting / waitingApproval / waitingUserInput / cancelling / reconciling |

thread 的 active flags 包含 waitingOnApproval 和 waitingOnUserInput，可组合展示。请求的 `isBlocking` 决定提问是否阻塞当前执行，不能把所有提问都视为停顿。

一次 turn 完成后，thread 仍可继续接受下一轮输入。断线表示运行状态尚待确认；不能据此写入 completed 或 failed。审批被拒绝也不必然代表 turn 结束，按原生决定语义和实际事件更新。

### 4.2 首版数据模型

```text
SessionIndex
  id, native_thread_id, codex_home_identity, cwd, title, created_at, last_opened_at
  native_thread_status, last_verified_at
RunProjection
  session_id, native_turn_id, native_status, local_phase, started_at, ended_at
ItemProjection
  session_id, native_turn_id, native_item_id, kind, content, status
PendingInteraction
  connection_epoch, native_rpc_id, method, native_thread_id, native_turn_id
  native_item_id, kind, payload, local_state
ViewState
  session_id, draft, scroll_anchor, expanded_items
ConnectionState
  epoch, process_identity, state, codex_version
```

- Codex 原生 thread / turn / item 和配置是会话内容、执行结果的权威来源。
- Fuyu 保存索引、显示偏好和可重建的投影缓存。缓存不用于覆盖原生历史。
- `codex_home_identity` 用于区分不同 Codex 数据目录；不保存凭据内容。
- SQLite 在“重启恢复”步骤引入；前面可使用内存状态，不能提前宣称支持重启恢复。
- PendingInteraction 的有效性由活动连接和服务端请求决定。重启后保留的旧记录只能作为核验线索。
- 需要记录原始事件时默认脱敏并限制容量；秘密输入和凭据不进入日志、缓存或数据库。

### 4.3 历史、增量与去重

1. 使用 thread ID、turn ID、item ID 合并历史快照与实时投影。
2. 消息 delta 只在其所属连接内按接收顺序应用；最终 item 快照校准内容。
3. 本地递增序号仅表示本地接收顺序，不能视为 Codex 服务端重放游标。
4. 重新连接时先缓冲活动事件，再获取恢复快照；以快照覆盖已确认部分，按协议顺序应用后续事件。若无法建立可靠边界，重新读取原生快照，不盲目拼接旧 delta。
5. 同一请求在有效连接内只能响应一次。收到 resolved 或 turn 结束后，将对应入口失效。
6. `turn/start` 响应丢失时先核验原生状态和历史，不自动重发，避免重复执行。

## 5. 审批与用户提问

### 5.1 审批卡片

在原会话时间线中显示请求，侧栏标记等待交互；切换会话后请求仍归属原 thread。

卡片显示请求类型、实际工作目录、命令或修改目标、请求原因和允许的决定。`accept`、`acceptForSession`、`decline`、`cancel` 等选项按具体请求及 schema 展示：

- `decline` 表示拒绝该操作，Agent 可以继续当前 turn。
- `cancel` 表示拒绝并中断当前 turn。
- 会话级或持久化规则选项必须明确其作用范围，并仅在原生请求允许时显示。

用户点击后禁用重复提交，发送对原生服务端请求 ID 的 RPC response。传输失败时显示未确认，核验请求是否仍然有效；不能直接显示“已批准”。

### 5.2 用户问题表单

保留原生 `questions[].id`，按其 question / options / isOther / isSecret 渲染。响应使用原生 `answers` 映射，将每个问题 ID 对应到答案数组，不能转换成一个普通聊天消息代替。

秘密输入使用掩码且不持久化答案。`isBlocking` 缺省值、实验能力及选项限制以固定版本为准。首版仅支持该版本明确验证的表单结构。

MCP elicitation 等其他服务端交互按实际启用功能单独声明支持状态。收到未支持的请求时显式提示并使用协议允许的拒绝/错误路径；不虚构回答，也不静默等待。

## 6. 中断、退出与恢复

### 6.1 中断当前 turn

使用当前真实的 threadId / turnId 发送 `turn/interrupt`。成功响应只说明中断请求被接受，界面等待 completed 通知或重新查询确认最终状态。

中断等待审批的执行时，待处理审批随原生 resolved/终态失效。已完成 turn 的中断错误按原生结果显示，不改写已有成功记录。

若中断超时，显示任务状态并提供明确的强制停止操作。共享 App Server 的强制停止会影响所有会话，界面列出全部受影响会话后再执行。

### 6.2 窗口关闭与应用退出

- 关闭窗口：隐藏窗口并保持 Fuyu 进程存活，任务继续运行；提供可重新打开窗口的菜单栏/托盘入口。
- 退出应用且无活动任务：保存索引与界面状态，结束 Fuyu 持有的 App Server。
- 退出应用且有活动任务：显示受影响会话，提供“取消退出”和“停止任务并退出”。
- 选择停止：逐个请求活动 turn 中断，等待核验，再清理自有子进程；超时后显示剩余进程和强制停止影响。
- 应用意外退出：下次启动进入 reconciling，核验历史与进程归属，保留未能确认的状态。

第一版承诺的后台运行依赖 Fuyu 进程仍在运行。独立 Daemon 的持续运行安排在后续阶段。

### 6.3 恢复流程与边界

```text
启动 Fuyu
  -> 读取本地索引，显示“正在核验”
  -> 启动受管连接并完成握手
  -> 用原生接口读取 thread 元数据及历史
  -> 需要继续会话时执行 thread/resume
  -> 合并快照、活动事件及有效待处理请求
  -> 根据原生状态显示可继续 / 运行中 / 已中断 / 待确认
```

恢复必须区分：

| 情况 | 处理 |
|---|---|
| 仅切换会话，原连接仍有效 | 保持订阅与运行，只切换视图 |
| 原 App Server 存活，连接可重新建立 | 按原生 resume 行为重新接入并核验活动 turn |
| 原进程已结束，历史已落盘 | 恢复历史并允许新 turn，不承诺继续原来的进程执行 |
| 原生历史含未正常结束的 turn | 使用服务端读/恢复结果；不能凭旧缓存显示仍在运行 |
| 存在有效待审批请求 | 使用服务端重新发出的请求及当前 ID，重建操作入口 |
| thread 尚未持久化或不存在 | 显示不可恢复原因，允许新建会话，保留可用索引供核对 |

当前源码的测试覆盖了接入运行中的 thread、命令/文件审批重放及历史未完成 turn 的处理。这是版本相关的能力，必须在实际安装版本做集成验证。

stdio 管道断开时，首版不能保证重新附着原进程；若无法安全重连，显示失联并处理自有进程，再恢复历史。需要真正的传输重连时，另行验证可重连的本地 transport。

## 7. 界面与逐步实施

```text
Fuyu
├── 左侧：当前目录 / Codex 会话列表 / 新建会话
├── 顶栏：会话名 / 实际 cwd / Codex 连接状态
├── 中央：用户消息 / 助手 Markdown / 工具卡片 / 交互卡片
└── 底部：多行输入 / 发送或停止 / 错误与恢复提示
```

上述布局是 Codex 日常使用版本的目标。第一步只做目录选择、消息区、输入区、发送、停止和运行状态；会话侧栏在第三步加入。Git 面板、Agent 切换器、远程主机选择等入口在对应功能实现时加入。

| 步骤 | 交付内容 | 验收方式 |
|---|---|---|
| P1 真实任务最小闭环 | 原生窗口、目录选择、真实 Codex 对话、文字流、最小运行状态与中断 | 在临时目录完成简单文件操作、第二轮对话和真实中断；验证中文输入、复制、滚动及代码块 |
| P2 任务过程与交互 | 工具卡片、命令输出、审批、用户问题、错误与停止结果 | 工具和交互对应原生事件；批准、拒绝、回答、中断结果可核对 |
| P3 日常使用与恢复 | 会话列表、SQLite 索引、分页历史、草稿与滚动恢复、窗口驻留、退出清理 | 两个目录的会话切换、退出重开和异常恢复；区分历史恢复与活动执行恢复 |
| P4 代码审查 | 任务关联的变更文件列表和只读 Diff | 用户可定位并检查本次任务影响的文件；已有变更与 Agent 变更的归属边界明确 |

每次完成一个步骤的实际验收和问题修复，再推进下一步。P1 把原生窗口和真实 Runtime 尽早连起来；P3 完成后形成 Codex 日常使用版本，P4 再扩展代码审查。Repository / Worktree 导航、完整文件浏览与 Commit Graph 在 P4 之后另行规划。

具体待办、证据要求和中断接续入口统一维护在[实施计划](implementation-plan.md)，本节只保留阶段概览。当前为 P1，核心链路已实现，剩余原生交互验收见实施计划。

2026-10-22 用户确认参考其提供的 ChatGPT 窗口截图，P1 界面按以下约定实现：

- 顶部为紧凑工具栏，展示项目短名与任务状态；点击项目名查看完整 cwd，新会话入口保留活动任务门控。
- 消息与输入框共享居中、最大 800 DIP 的阅读宽度；两侧最少留白 24 DIP，随窗口收缩。
- 用户消息使用右侧浅色气泡，助手回复采用开放式 Markdown，完成后提供整段 Markdown 复制。
- 底部为圆角一体输入区，文本自动增长至 6 行；模型简显，发送/停止共用右下角位置。
- 版本、完整模型、审批和沙箱值放入只读“运行详情”；未建立 thread 时明确显示尚待确认。
- 使用 Enter 发送、Cmd+Enter 换行，保留输入法组合门控；返回最新消息按钮浮在消息区底部，与输入区保持 20 DIP 间距。上滚离开末尾时，底部 80 DIP 由透明渐变到主题背景色；装饰层透传滚动。按钮显隐不改变列表视口，列表末尾固定保留内容留白，保证最后一条消息可完整阅读。
- 输入占位文字和实际输入正文均为 14 DIP。占位文字使用浅灰色，浅色主题为次级文字色的 40% 不透明度，深色主题为 65%；正文保持原色。光标使用正文色，宽度为 2 DIP，沿用原生位置与闪烁逻辑。占位颜色与光标样式通过锁定 MyGo 的构建补丁实现，维护方式见[开发指南](development.md)。
- 当前请求尚无正文或工具输出时，消息区显示浅灰色“正在思考”，文字上以 1.8 秒周期从左向右扫过灰色渐变。默认浅色主题范围为 `#A3A3A9 → #8B8B92`，默认深色主题为 `#717178 → #888890`，按主题背景与次级文字色混合，保持柔和对比。正文或工具出现、停止、报错、断线及任务结束时收起。该提示表示等待回复，不表示收到模型推理内容；系统减少动态效果时显示静态文字。
- 对话定位刻度与预览仍是后续候选，不在本轮增加会话历史功能。截图只作为视觉依据，不将未观察的动态交互描述为参考应用实测结果。

### 7.1 第一版验收边界

- Codex 原生 Runtime 完成真实连续对话和工具执行。
- 审批与提问正确返回原生请求，结果可核对。
- 中断、退出及恢复显示实际状态，不将连接丢失视为成功。
- 会话切换保留输入与阅读位置，后台运行保持原目录。
- 每项验证记录真实 Codex / 协议夹具 / UI 样例的来源，区分通过、未验证和暂缓。

## 8. 工程与版本准备

```text
cmd/desktop/
internal/ui/
internal/app/
internal/codex/
internal/runtime/
internal/storage/       # P3 引入
docs/
  README.md
  design.md
  implementation-plan.md
  reference-verification.md
```

第一步只创建实际需要的包。当前参考 MyGo 提交的 go.mod 要求 Go 1.27.1；初始化和依赖应固定到经过构建验证的版本，避免持续使用 `@latest`。

设计阶段的本机只读检查快照（2026-10-22，工程初始化之前）：

- `codex --version`：`codex-cli 0.163.0`。
- App Server 帮助包含 `--listen stdio://` 与 schema 生成子命令。
- 已在临时目录生成该安装版本的实验 schema，核对恢复、提问、审批和中断字段。
- 当次 shell PATH 下找不到 `go`；P1 开始前需要重新检查并准备兼容工具链。

以上是工程初始化前的环境与协议结构核验快照。此后已建立 P1 可运行实现，当前构建、真实任务与原生窗口结果见[P1 验证记录](verification/2026-10-22-p1.md)；运行中重连与任务恢复仍待后续阶段。
