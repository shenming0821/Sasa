# 存储与恢复

## 数据与归属

主窗口通过 MyGo 的 `WindowOptions.StateKey = "main"` 保存窗口状态。状态单独写入应用数据目录的 `window-state.json`，随 `-data-dir` 隔离；正常退出时保存，下次创建窗口时恢复。首次启动或无可读记录时采用 1000×760 DIP，最小尺寸为 640×480 DIP。MyGo 同时记录位置、最大化与全屏状态，并提供显示器可见范围处理；当前原生验收确认尺寸恢复，位置和其他状态的验证边界见[窗口恢复验证](../verification/2026-11-03-window-state.md)。隐藏窗口继续保留当前原生窗口。此记录不进入 SQLite，也不改变数据库版本。

`internal/storage/store.go` 使用 `modernc.org/sqlite v1.60.1` 保存应用索引。macOS 默认路径为 `~/Library/Application Support/Fuyu/fuyu.db`，可通过 `-data-dir` 指定独立目录。

数据库保存会话 ID、原生 thread ID、cwd、标题、草稿、选中会话、已打开标签、侧栏开关与宽度、项目折叠、历史面板开关、阅读锚点和条目内偏移、工具展开状态，以及未确认任务标记。消息、工具正文由 Codex 原生历史提供；凭据、待处理协议请求和秘密表单答案不写入应用数据库。用户主动输入的草稿属于本地持久数据。

`fuyu.lock` 通过非阻塞独占 flock 防止两个进程同时使用同一数据目录；进程退出后操作系统释放锁。新目录权限 0700，数据库权限 0600。按 Agent 分别绑定规范化后的历史根目录；Codex 保留 `CODEX_HOME`（默认 `~/.codex`）校验，同一 Agent 切换原生历史目录时要求独立应用数据目录。Claude 配置根的实际接线待 P6.3。

## Schema 与事务

`Preferences.Terminal` 在 settings JSON 中保存原生终端默认实现，目前支持 `ghostty`。旧记录缺失、网页原型的 `xterm` 或无效值由 UI 归一为 `ghostty`，无须迁移数据库。`WorkspaceTabs` 中 `Kind=terminal` 的记录保存 ID、Workspace 及 `TerminalEngine`，重启恢复标签并提示用户新建 shell；原生 surface、PTY 进程及输出不写入索引。Ghostty 自身的配置从本机文件读取，不复制到 Fuyu 索引。

删除会话使用 `Store.DeleteSession(snapshot, id)` 在同一 SQLite 事务内删除指定 `sessions` 行并保存剩余会话及设置；验证或写入失败时整个事务回滚。设置 payload 的 `DeletedThreads` 保存 `Agent`、`Runtime`、`ThreadID`，由 UI 在原生历史呈现时过滤。旧设置缺少此字段等同于空列表，无需变更 schema 版本。原生历史和共享图片不在此事务中删除。行为见[会话管理](sessions.md#删除会话)。

通过 `PRAGMA user_version` 管理版本：

| 版本 | 内容 |
|---|---|
| 1 | sessions、cwd 索引、settings；原生非空 thread ID 唯一 |
| 2 | runtime_identity，绑定 Codex 历史目录 |
| 3 | 有类型的 WorkspaceTabs、ActiveTabID、文件位置和工作区面板状态 |
| 4 | sessions.agent、`(agent, thread_id)` 联合唯一约束，runtime_identity 按 Agent 保存 home |

拒绝高于当前支持版本 4 的数据库；旧版程序也会拒绝打开 v4，避免误把 Claude 会话作为 Codex。v1/v2 升级至 v3 前先读取并校验旧 JSON；已有数据库以 `VACUUM INTO` 创建一致性备份，权限 0600，并校验备份版本 2 与 `quick_check`。同名备份损坏或不是普通文件时保留原数据库并报错；已有有效备份时保留旧备份，另建唯一名称的新快照，避免把过时备份当作本次迁移来源。版本提升在事务内完成。

v4 迁移先校验现有 JSON，再通过 `VACUUM INTO` 创建本次唯一命名的 `fuyu-before-v4-*.db` 一致性备份，校验版本 3 与 `quick_check`、权限 0600；在同一事务重建会话和 Runtime 身份表并提升版本，失败回滚。旧列归属填为 codex，空原生 ID 转为 NULL，不占唯一约束；旧 home 和 payload 字节保持不变。会话 payload 新增 `Agent`（空值经 `Normalized()` 默认为 Codex）、`Runtime` 与 `ModelAgent`；模型归属不匹配或未知 Agent 的保存被拒绝。相同原生 ID 可分别属于 Codex 和 Claude，同一 Agent 内仍唯一。Linux 编写，Mac 待验证，新增回归见 `internal/storage/agent_test.go`。

版本提升保留旧 settings JSON，UI 兼容读取旧 `OpenTabs` / `Selected` 后，在首次成功保存时将有序混合标签与会话索引一同写入事务。`WorkspaceTabs` 是新记录的打开标签来源，明确空数组保持空白；旧记录缺少 `OpenTabs` 时仅打开选中会话，已有数组保留顺序并去重。文件正文按需读取，不因恢复标签批量加载。

`Repositories` 保存 common dir、主路径、名称与 worktree 元数据缓存；旧记录缺省为空，启动后台扫描现有会话目录以发现仓库。旧记录缺少侧栏宽度时使用 240 DIP；搜索内容只保留在当前进程，窗口自动收起侧栏不会覆盖用户的展开偏好。

`Snapshot.RightPanelWidth` 在现有 settings JSON 中保存右侧宽度偏好，读取时接受 240–520 DIP，缺失或无效值使用默认 320 DIP，无须迁移数据库。窗口变窄时仅调整显示宽度，不覆盖保存的偏好；文件、会话历史和 Git 共用该值。

会话 JSON 中的 `DraftSkills` 保存草稿里已选择技能的名称与原生路径；仅保留仍被正文引用的项，与草稿一起恢复，不保存技能正文或整个候选缓存。文件未保存正文使用独立、自包含且带摘要校验的恢复副本，提交顺序、容量和清理失败保护见[编辑器子系统](editor.md)与 [P4 设计](../p4-editor-design.md)。

`Snapshot.GitDrafts` 在现有 settings JSON 中按工作区根路径保存提交信息草稿，缺省为空，沿用 schema 3 的事务而无需迁移。暂存状态、当前分支、远程和进行中的 Git 操作始终从实际仓库重新读取；列表选择和确认卡仅在当前进程有效，恢复不重复执行 Git 命令。

数据库单连接，`busy_timeout=250` 毫秒，WAL，`synchronous=FULL`。保存将全部会话元数据与设置置于同一事务，失败回滚。当前没有删除会话功能，保存采用 upsert。

## 保存时机与恢复门控

settings JSON 的 `FolderSelectedTabs` 按文件夹绝对路径保存上次选中页签 ID，未绑定目录的分组使用空路径；`ActiveWorkspace` 和 `ActiveTabID` 保存当前文件夹及当前页签。关闭同组最后一页时允许保留非空 `ActiveWorkspace` 与空 `ActiveTabID`，其他文件夹的页签继续保存在 `WorkspaceTabs` 中。此扩展保持数据库 schema 4。读取旧设置时从各组有效页签建立默认选择；保存和恢复均排除已关闭、归属不符或不存在的选择，当前有效页签优先。文件重命名同步迁移选择 ID。

分组恢复只恢复导航信息，后台文件夹的会话历史继续延迟加载；当前文件夹为空时不请求后台会话历史，也不自动跳到其他文件夹。终端恢复继续要求用户显式启动新 shell。分组行为见[项目管理](projects.md#按文件夹展示页签)。

`internal/ui/persistence.go` 生成快照，比较上次成功保存内容后写入。普通界面与异步更新合并为首次变化后 1 秒保存，持续变化期间不延后截止点；发送前、安装原生会话 ID 后、任务终态、断线、项目目录元数据变化、关闭窗口和退出前立即保存。空闲维护不主动生成快照，文件检查无变化时不安排保存。数据库失败显示具体错误，保持重试；发送前的关键状态无法保存时停止提交。调度与缓存边界见[使用性能与后台维护](performance.md)。

应用重启后恢复已打开标签和选中会话，仅请求选中项的原生历史；其余已打开标签在首次切换时加载，未打开的索引在点击时打开。未加载记录的 Active、TurnID、阅读与折叠数据在保存时原样保留。关闭标签从下次恢复列表中移除该项，保留数据库中的会话记录。原生历史核验前禁止发送。正常完成的原生任务允许同 thread 新 turn；未结束 turn、原生失联或未知提交继续保留 Active/TurnID。即使多次异常重开，也不能用空闲 UI 覆盖原有不确定标记。没有原生 thread/turn ID 的提交需要显式确认，不能自动重发。

跨进程恢复流程和事件边界详见[会话管理](sessions.md)。数据库只负责应用自身数据，不能据索引内容宣称原生任务成功或原进程已重新接入。

## 验证

`internal/storage/store_test.go` 覆盖重开、唯一归属、事务回滚、损坏保留、高版本拒绝、v1/v2 升级、备份完整性与新鲜度、旧 JSON 预检和 Codex 历史目录不匹配。UI 恢复及真实运行见 [P3 验证](../verification/2026-10-22-p3.md)与 [P4 验证](../verification/2026-10-24-p4.md)。

## 图片资源

会话 JSON 的 `DraftImages` 保存附件路径、名称及可选 URL，与文字草稿同一快照恢复，无须新增数据库版本。应用导入副本存于数据目录的 `attachments/`，目录权限 0700，文件权限 0600，按内容摘要命名、临时文件原子替换及去重。图片正文不存于 SQLite。移除草稿附件只移除引用，副本继续保留，避免破坏其他草稿和原生历史的共享引用；本轮未提供自动垃圾回收。详见[图片附件](attachments.md)。

会话 JSON 的 `SelectedModel` 保存具体的模型选择；空字符串表示尚未初始化。旧记录缺少字段时，从当前目录的有效配置解析初始模型并保存，无需数据库迁移。选择与原生上次使用的模型分开，后者由 Runtime 恢复确认。

应用级设置通过可选的 `Snapshot.Preferences` 随 settings JSON 保存；缺少字段的旧记录使用默认主题、字体和通知偏好，详见[设置与完成通知](settings.md)。
