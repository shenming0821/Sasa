# 经验记录

## 2026-11-02：计时状态与位置统一

当同一计时信息同时具有进行中和结束态时，将它们关联到同一用户问题并共用渲染位置与间距；布局验证同时覆盖初始等待、命令输出后和结束切换。仅调整完成态会遗漏单独位于列表末尾的进行中计时；在相同夹具中切换状态，核对计时与首条命令的位置保持一致，并通过隔离原生窗口复验。证据：`TestTimingLayoutBelowQuestionBeforeCommands` 与[用时验证](docs/verification/2026-11-01-turn-timing.md)。

## 2026-10-30：Diff 代码与补丁身份分离

当 Diff 需要以编辑器形式呈现时，将代码正文、旧/新行号、增删类型与原始修改块身份分开保存，跳转与块操作通过行映射定位。补丁文件头和块头用于比较与操作，正文与复制内容显示真实代码；补齐未修改代码时先验证对应源版本与补丁上下文一致。参考 Mayoi 行模型后的实现与临时仓库回归见 [Diff 代码展示验证](docs/verification/2026-10-30-diff-source-view.md)。

## 2026-10-29：Claude 本地命令的完成与历史末尾

当原生本地命令返回 success、正常退出但没有 terminal_reason 时，结合本次发送 UUID、命令参数及原生 `system/local_command` 的 info 输出核验完成。选取历史主链末尾时纳入本地命令的 system 输出，避免只保留 user/assistant 而丢失完成证据。恢复旧 unknown 检查点时，同时核对历史摘要、运行状态和任务归属，并持久化核验结果；一般未知任务继续等待核验。实际 `/model` 误判、隔离测试与原会话恢复证据见[本地命令恢复验证](docs/verification/2026-10-29-claude-local-command-recovery.md)。

## 使用方式

修改代码前阅读本文。遇到已复现的问题或交付遗漏时，记录触发场景、原因、可执行规则和证据。反复适用的规则整理进[代码标准](docs/code-standards.md)，本文保留案例与链接。

实际回归与运行时观察见下文；参考项目的经验与 Fuyu 的实测结果分别记录。

## 设计阶段已确认的经验

| 场景 | 后续动作 | 依据 |
|---|---|---|
| 单独完成 UI 骨架仍无法确认 Runtime 交互可行 | 在 P1 将原生窗口与真实 Codex 任务连起来，验收文件操作、连续对话和中断 | [实施计划 P1](docs/implementation-plan.md#3-p1真实任务最小闭环) |
| 当前阶段与未来能力混在一起，容易扩大首轮范围 | 实现从当前接续点开始，完成验收后推进下一阶段 | [实施计划](docs/implementation-plan.md) |
| 恢复历史与重新接入活动任务具有不同条件 | 根据原生状态和进程归属分别验证，记录实际恢复结果 | [设计文档](docs/design.md)、[参考核验](docs/reference-verification.md) |

## 2026-10-26：图标按钮重复提示

Git 文件行通过共享按钮注册默认提示后，再调用 `Tooltip` 设置动作文字，悬停时同时出现两个气泡。MyGo 的 `Tooltip` 每次调用都构建提示，不覆盖之前的提示。当图标按钮需要动作专用提示时，在创建共享按钮时传入最终文字，只注册一次。回归 `TestGitFileActionsShowOneActionOnlyTooltip` 覆盖更改、已暂存与冲突组的操作按钮，悬停后只新增一项动作文字提示。

## 2026-10-26：Diff 宽度与横向滚动

行内及并排 Diff 的文本区域使用固定最小 600 DIP 宽度，宽窗口出现空白；长行区域又被水平容器的默认 flex shrink 压回视口宽度，导致没有横向溢出和滚动条。新建 TextBuffer 的版本从 0 开始，单独比较默认值同为 0 的高亮版本还会跳过首次宽度计算。构建横向代码视图时，正文宽度取可用区与内容宽度的较大值，并设置 `Shrink(0)`；尚未计算宽度时启动计算，替换缓冲时清除旧宽度。后续中文长行反馈证实“字符数 × 8”的估算仍会截断行尾；需要滚动到完整行尾时，使用实际字体排版测宽，并计入内边距，分别核对行内与并排两侧的末端可见范围。回归通过实际异步宽度计算覆盖 480/1000/1800 DIP、短长行、行内/并排、滚轮和滚动条拖动，以及长行刷新为短行后的滚动归零。证据：`internal/ui/diff_layout_test.go`、[Diff 验证](docs/verification/2026-10-26-diff-layout.md)。

## 2026-10-28：原生确认框按钮位置

macOS 的 NSAlert 会根据默认操作安排按钮位置，仅交换 `Buttons` 数组不能保证最终左右顺序。在要求“取消”位于左侧、“删除会话”位于右侧时，将删除配置为确认操作，取消配置为 Escape，并同步核对返回索引；通过真实窗口截图确认按钮位置，再验证取消保持原数据。当前删除弹窗使用 `Buttons=[删除会话,取消]`、`DefaultButton=0`、`CancelButton=1`。证据：[删除验证](docs/verification/2026-10-28-session-delete.md)的同日交互调整。

## 新案例格式

```text
日期与问题：
触发条件与可观察结果：
原因：
在什么条件下，执行什么动作：
证据：源码/提交/验证记录路径
规则归属：代码标准或相关子系统文档
```

## 2026-10-23：输入事件与原生元素有效期

MyGo 的 Element 只在当前构建阶段有效。输入候选处理键盘时，在构建阶段读取组合输入和光标状态，回调通过 TextKey 读取、替换文本；跨阶段访问控件时使用原生 Handle。直接在 OnTextKey 中调用捕获 Element 的方法会触发有效期 panic。依据：输入候选实现时复现该异常，调整后已有输入测试及 `internal/ui/completion_test.go` 通过。

## 2026-10-23：内容宽度与 Grow

命令组箭头需要紧随文字时，标题使用自然宽度并允许在空间不足时收缩。MyGo 的 `Grow(0)` 仍设置零宽 flex basis，先 `Grow(1)` 再 `Grow(0)` 会把短标题压到零宽。按条件创建布局时，仅对需要填满剩余空间的分支调用 `Grow`；验证短标题实际宽度、相邻箭头位置和长短状态切换。依据：`TestCommandGroupTitleKeepsContentWidthAndAdjacentArrow` 修复前在 320 / 800 DIP 下复现宽度为 0，修复后通过。

## 2026-10-23：右侧历史布局

用户确认参考 Orca 的右侧历史侧栏时，在默认窗口下仍应将历史放在窗口内部，与聊天并排。根据截图实现导航布局时，先核对默认尺寸下的面板占位方式；展开历史为会话区让出宽度，空间不足时优先收起项目导航。组件验证同时断言历史、正文和发送按钮的实际边界。依据：`internal/ui/navigation_test.go`、[导航验收](docs/verification/2026-10-23-navigation.md)。

## 2026-10-23：外部会话占用

`thread/resume` 报告已有 active writer 时，表示另一进程持有写入权。此时以原生只读接口加载历史，并提供重试或独立分叉；保留原会话及草稿。仅管理 Fuyu 自有任务，不因外部占用创建 Busy 状态或提交不确定标记。分叉时明确延后目标自动续跑，直接使用 fork 返回状态分页，等待用户发送下一项任务。依据：`internal/ui/occupied_test.go`、`internal/codex/history_test.go`、[占用交互验收](docs/verification/2026-10-23-occupied-session.md)。

## 2026-10-23：工具卡片文字越界

长多行命令使用 `Text.MaxHeight(72)` 时只限制布局占位，实际文字仍继续向下绘制，覆盖卡片元数据及后续消息。限制文字预览时使用 `MaxLines`，并通过独立滚动详情提供完整内容；卡片同时裁切绘制边界。回归应读取实际渲染像素，检查命令与元数据之间的间距及卡片下方留白，单独检查元素高度无法发现该问题。依据：[布局验收](docs/verification/2026-10-23-tool-layout.md)。

## 2026-10-23：桌面启动环境

Finder / Dock 启动的应用可能缺少终端配置的 PATH。仅将 Codex 改为绝对路径还不足以启动 npm/Bun 包装脚本，因为 `env node` 仍依赖子进程 PATH。桌面启动时根据当前用户恢复安装目录与登录 shell 路径，并将相同环境用于版本探测、App Server 和后续工具；保留显式路径和原生配置。回归使用精简 PATH、临时安装目录和真实初始化分别核验，不能用终端中的 `codex --version` 代替桌面启动验证。依据：[启动路径验收](docs/verification/2026-10-23-codex-discovery.md)。

## 2026-10-23：应用图标缓存

原地构建 `.app` 时，包内图标与 `CFBundleIconFile` 已正确更新，但外层目录时间保留旧值，Finder 实测仍显示默认图标。构建完成后更新包修改时间并重新登记当前应用，再通过 Finder 实际预览及应用启动核验。图标资源格式检查和用户实际可见效果分别验证。依据：`scripts/build`、[图标验收](docs/verification/2026-10-23-app-icon.md)。

## 2026-10-22：P1 实测案例

| 触发场景与原因 | 后续动作 | 证据 |
|---|---|---|
| 新提交尚未拿到 turn ID，旧 completed 可能清除 Busy | 保存已结束 turn ID，忽略其重复和晚到事件 | `internal/app/state_test.go` |
| Cmd+Enter 在输入法组合期间仍触发回调 | 在每次渲染读取 Composing，用于发送门控；在组件中复现候选输入与发送快捷键的组合 | `internal/ui/app_test.go` |
| 在键盘回调查询上一次渲染的 Element 引发过期 pass panic | 跨回调保留必要状态值；需持久定位时使用框架 Handle，避免直接持有 Element 调用方法 | `internal/ui/app.go`、[P1 验收](docs/verification/2026-10-22-p1.md) |
| turn 已完成/中断，独立后台 sleep 仍运行 | 分别以 turn 与 item 的原生事件核对终态，停止后检查实际子进程；只对实际覆盖的退出路径声明清理通过 | [P1 原生窗口验收](docs/verification/2026-10-22-p1.md) |
| main 在资源创建后 os.Exit 跳过 defer | 将有资源的工作放入返回 error 的 run()，退出码在 run 返回后设置 | `cmd/desktop/main.go` |
| Markdown 编号按比例数字测宽，实际使用 tnum 数字渲染，10 起出现句点换行 | 为有序列表保留 3 em 缩进，逐项增长验证 1–120 的编号高度；无序列表通过本地补丁限制为最多 1.4 em，避免圆点两侧留白过大；依赖修复后重新核验再移除此兼容设置 | `internal/ui/app.go`、`TestStreamListMarkersStayOnOneLine` |
| 服务端报告 willRetry 后恢复输出，旧重连提示仍停留在已完成界面 | 按 turn 跟踪临时重试提示，恢复输出或到达终态时移除对应提示，保留其他警告 | `internal/app/state.go`、`TestRetryNoticeClearsWhenOutputResumes` |

## 2026-10-22：P2 实测案例

| 触发场景与原因 | 后续动作 | 证据 |
|---|---|---|
| MyGo ButtonBase 的 Label 仅提供无障碍名称，原生窗口按钮有热区但看不到文字 | 构建自定义操作按钮时显式添加 Text 子节点，并在原生窗口核对可见文字 | `internal/ui/activity.go`、[P2 验收](docs/verification/2026-10-22-p2.md) |
| 原生兼容通知的 error 为字符串，统一解码成结构体产生假警告 | 在解析前筛选应用实际投影的方法；兼容通知不进入无关的强类型结构 | `TestUnprojectedCompatibilityNoticeDoesNotBecomeParseFailure` |
| 原生新增文件的 diff 为原文，按 unified diff 计数得到 +0 | 根据 change kind 区分原文与统一差异；新增/删除统计原文行数 | `internal/app/tools.go`、真实 denied.txt 显示 +1 −0 |
| 仅限制协议队列，异步 Window.Update 仍可积压 UI 回调 | 后台每批派发后等待主线程回调完成或应用退出，再消费下一批事件 | `internal/ui/app.go` |
| 响应后表单收起，高度变化使下一步偏离阅读位置 | 用户自己的响应解决后定位后续流程；其他 resolved 继续保留阅读锚点 | `TestOwnResponseContinuesWorkflowWhileUnrelatedResolutionPreservesReading`、最小原生窗口 |
| 密码字段使用 AX setValue 后，AppKit 缓存 setter 原文；后续投影仍为空而未刷新缓存 | 对原生密码字段的 AX value/selectedText setter 隔离修复，缓存空值，仅将原文传给编辑器动作；同时核对圆点、AX 和响应映射 | `scripts/mygo-input-style.py`、P2 原生 setValue 回归 |
| turn 已中断后工具终态晚到，旧警告仍显示“尚未报告结束” | 按原生 item/turn 接收已知工具的晚到终态，最后一个工具结束时仅清除对应警告 | `TestToolLifecycleOutputAndTurnRemainIndependent` |
| 原生云配置加载失败，thread/start 返回明确 RPC 错误，尚未提交 turn | 会话创建被明确拒绝时保留草稿并允许重发；提交结果不确定时禁止自动重发，显示核验动作 | `TestSubmissionFailuresSeparateRejectedCreationFromUncertainExecution` |

## 2026-10-22：P3 会话隔离

| 触发场景与原因 | 后续动作 | 证据 |
|---|---|---|
| 会话切换后，异步发送/停止/审批/快照仍通过当前 State 写回，可能覆盖另一个会话 | 启动异步工作时固定所属会话，回调仅修改该会话；thread/start 的所属状态先写入主线程，再提交 turn/start | `internal/ui/app.go`、`TestSwitchWhileStartingThreadKeepsSubmissionAndEffectiveSettings` |
| 用整个 App 的副本绑定会话，vet 发现 Markdown Options 内含 sync.Map，复制会复制锁 | 构造会话回调接收者时仅传递会话、连接和派发资源，含锁的渲染配置保留原实例 | `internal/ui/sessions.go`、[P3 首步验证](docs/verification/2026-10-22-p3-sessions.md) |

## 2026-10-22 至 2026-10-23：P3 持久化与恢复

| 触发场景与原因 | 后续动作 | 证据 |
|---|---|---|
| 一条长回复占据多屏，仅保存 item ID 会回到回复开头 | 保存首可见原生 ID 和条目内相对偏移，分页找回锚点后再恢复偏移 | `TestRestartRestoresOffsetInsideLongNativeMessage`、[P3 验证](docs/verification/2026-10-22-p3.md) |
| 新页尚未加载旧工具，直接重建展开字典会丢失旧页设置；分页改变首条可见任务导致标题跳变 | 保存时合并未加载条目的设置，标题优先使用已保存摘要 | `TestCheckpointPreservesUnloadedToolExpansionAndStableTitle` |
| 同一目录有多个会话，启动参数 cwd 匹配遍历覆盖上次选中项 | 先保留匹配目录的选中会话，再查找其他匹配会话 | `TestStartupKeepsSelectedSessionWhenDirectoryMatches` |
| 发送未取得 thread/turn ID 就异常退出，空闲界面可能覆盖原有不确定标记 | 持久保留提交不确定性，读取原生历史并显式确认后才允许继续 | `internal/ui/recovery_test.go` |
| 恢复快照可能已包含缓冲 delta，重放会重复正文 | 控制事件与增量分别处理，缺口读取原生权威条目校准 | `internal/ui/history.go`、`internal/ui/recovery_test.go` |
| 锚点在更早页或超出显示容量，恢复标志会造成无效刷新 | 自动分页寻找锚点，到达容量或历史末尾时结束恢复并提示实际边界 | `TestRecoveryAutomaticallyLoadsSavedReadingAnchor`、`TestHistoryCapacityEndsUnresolvedAnchorRestoration` |
| 中断 RPC 返回成功或连接丢失，均不能证明任务已停止 | 逐个等待原生终态，断线保留未确认目标，超时后由用户选择返回核验或强退 | `internal/ui/lifecycle_test.go` |

## 2026-10-23：工作区卡片悬停

当一个列表项包含主入口与辅助操作时，由共同外层绘制完整悬停和选中背景，主入口使用透明按钮，辅助操作仅显示小范围反馈。仅把子按钮放进同一圆角容器，仍会因子按钮各自绘制 hover 造成割裂；用文字区、操作区的背景像素和跨区域悬停验证。当前工作区卡片已移除辅助操作，全宽点击与背景回归见 `TestWorktreeCardHoverAndClickCoverFullWidth`。

## 2026-10-24：Git 面板后台刷新跳动

2026-10-29 补充：暂存时即使不插入 loading 文案，共用的 busy 状态仍会让分支工具栏、提交输入框和操作按钮整体禁用再恢复，造成短暂变淡。快速索引操作将写入互斥与视觉禁用分开：在逻辑入口拦截重复操作，保留工具栏外观和提交信息编辑。回归比较浅/深色及空/非空草稿的整个顶部像素；仅断言文件行位置无法识别此类闪动。`TestGitIndexActionsKeepHeaderPixelsSteady` 在修复前四组均失败，修复后通过。

已有 Git 快照时，每 5 秒的后台检查仍插入、移除加载行，并切换刷新按钮禁用态，导致列表跳动和按钮闪动。当面板已有可用内容时，后台刷新保留其几何位置和操作外观，仅首次读取展示加载提示；滚动容器赋予稳定身份，错误单独呈现。回归覆盖首次读取及六次加载状态切换后的行位置、滚动与分组折叠，见 `TestP4GitBackgroundRefreshKeepsListSteady` 和 [P4 验证](docs/verification/2026-10-24-p4.md)。

## 2026-10-24：删除目录后的展开状态与空目录重试

验收夹具目录已删除，但其展开记录跨重启保留，恢复文件树时将 ENOENT 显示为常驻错误。读取发现子目录消失，或父目录快照已不含该目录时，移除对应导航状态及过时请求，并在后续快照保存清理结果；工作区根目录和权限错误继续提示，打开文件与草稿保留。空目录成功返回的切片可能为 nil，应使用缓存键是否存在区分“空目录”与“未加载”，避免逐帧重读。回归见 `TestP4RemovedExpandedDirectoryIsPruned`、`TestP4EmptyExpandedDirectoryDoesNotReloadEveryFrame`、`TestP4MissingRootAndDirectoryPermissionsRemainErrors`。
