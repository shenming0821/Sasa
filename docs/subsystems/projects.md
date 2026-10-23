# 项目与工作区管理

## 范围与入口

左侧按“仓库 → Git worktree / 分支 → 会话”显示。“项目”标题右侧按顺序放置导入项目、打开项目、刷新三个图标按钮，通过悬停提示说明动作；搜索框独立保留。导入项目选择本地目录，既可选择主工作目录，也可选择已关联的 worktree；不会克隆仓库或创建、删除 Git 分支与工作区。“打开项目”继续提供普通工作目录入口，非 Git 会话保留原有目录分组。

`internal/project/repository.go` 只读执行 `git -C <path> rev-parse --git-common-dir` 和 `git -C <path> worktree list --porcelain -z`，规范化公共 Git 目录作为仓库身份。列表使用 NUL 分隔，保留中文、空格、换行路径；主工作区按 Git 列表首项标识。branch、HEAD、detached、bare、locked、prunable 各自解析，目录存在性在后台扫描时核验。扫描使用当前机器可执行的 Git；调用错误原样保留上下文，不将失败当作成功的空列表。

## 导航与会话

`internal/ui/projects.go` 负责异步导入、扫描与仓库行。仓库名加粗，工作区显示目录短名与分支；主要工作区带“主要”标记。点击工作区优先打开最新已有对话；没有对话时复用 cwd 精确匹配的空白会话，再没有时创建空白会话。右侧新建按钮始终创建该工作区的独立会话；一个工作区可有多个会话。左侧仅在工作区下显示已有对话、原生历史或未确认任务，空白与仅有未发送草稿的会话不额外显示“新会话”占位行。隐藏的空白会话保留标签和草稿，点击工作区仍可进入；首次发送任务后显示对应会话行。当前选中的是隐藏空白会话时，工作区行承担选中态。工作区名称、分支和最新已有对话合成一个圆角列表项，由外层卡片统一绘制选中与悬停背景，覆盖文字、间隙及右侧操作区。主按钮透明，新建按钮仅在 28 DIP 图标热区内提供反馈，键盘聚焦保留可见反馈；最新会话与工作区文字左侧对齐。文字层级按 Orca 参考：仓库标题 14 DIP 加粗深色，工作区名 13 DIP 深色常规字重，会话摘要 11 DIP 次要灰色常规字重，分支与状态 10 DIP 次要灰色，“主要”为 9 DIP 细边标签。空白会话不替换已有对话摘要，其他会话继续通过标签和历史入口进入；搜索时显示最新匹配的对话。卡片不展示其他应用占用会话的提示，该状态在打开对话后显示；运行、失败及待处理状态继续保留。

会话依旧以绝对 cwd 绑定，Git 分支只是可刷新的显示元数据；外部切换分支不重写已有 thread 的目录或创建新 thread。子目录会话归到包含它的最深已导入 worktree，避免嵌套仓库归属冲突。关闭标签、草稿、活动任务、历史和重启标签恢复沿用原会话机制。导入和扫描不创建 Codex thread 或发送模型任务。

搜索支持仓库名、路径、分支与会话标题；匹配会话会显示其工作区和仓库层级。折叠使用现有 `CollapsedProjects`，仓库以 common dir 为 key，普通目录继续使用路径。

## 刷新、持久化与异常

启动、手动点击“刷新项目”以及每 30 秒触发一次扫描。整个刷新最多 20 秒，导入最多 10 秒；同一时刻只有一个项目扫描。耗时 Git 与文件状态读取在后台执行，结果通过主线程更新并持久化；应用退出会取消扫描上下文。

先刷新已保存仓库；主目录无法扫描时尝试关联 worktree。扫描成功且 common dir 相同才替换缓存，失败保留上次列表并显示“刷新失败，保留上次列表”，提示中保留具体错误。现有会话的尚未识别目录也在后台尝试发现，普通目录不报 Git 错误。外部删除 worktree 后，相关会话和草稿仍保留，必要时回到普通目录分组。

bare 仓库本身、prunable 登记或实际缺失目录不能新建工作区会话；locked 只是 Git 管理锁，仍可在存在的目录中对话。工作区导入通过 common dir 去重，符号链接导入也归一化身份。暂不提供删除项目、创建 worktree、checkout 或分支管理入口。

`storage.Snapshot.Repositories` 扩展现有 settings JSON，保持数据库 schema 2。缓存包含工作区状态，不包含消息正文或凭据。重启先显示缓存，后台刷新元数据；只有选中会话按现有机制加载原生历史。

## 参考与验证

只读参考用户提供的 Orca 源码：

- `/Users/shenming/Documents/AI/orca/src/main/git/worktree-list-reader.ts`
- `/Users/shenming/Documents/AI/orca/src/main/git/worktree-listing.ts`
- `/Users/shenming/Documents/AI/orca/src/shared/git-worktree-porcelain-parser.ts`
- `/Users/shenming/Documents/AI/orca/src/main/ipc/repos.ts`

Fuyu 独立使用 Go 实现，Orca 的测试结果不代表 Fuyu 通过。临时真实 Git 仓库、受控 UI 与存储测试见[验证记录](../verification/2026-10-23-project-management.md)。
