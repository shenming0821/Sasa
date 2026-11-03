# 新增 Worktree 验证

日期：2026-11-03。

## 行为与边界

- Git 仓库总标题右键增加“新建 Worktree”；普通目录和工作区卡片菜单沿用原有入口。
- 基础分支支持本地及已获取的远程引用搜索，默认当前所属工作区，否则主工作区；detached HEAD 使用当前提交，尚无提交显示错误。
- 文件夹名称默认跟随分支名，`/` 转为 `-`；自定义后保持，清空恢复跟随。路径在当前应用数据目录 `worktrees/<仓库标识>/<名称>` 下，实时预览并支持复制。
- 后台重新校验并解析来源提交，再创建分支和 worktree；原目录文件、index 和分支保持原状。失败后核对目录、分支与登记状态，保留实际内容，不将已有登记误判为完整检出成功。
- 创建成功后刷新所属仓库并进入空白会话，不发送模型任务；刷新失败时只重试发现。创建期间关闭弹窗和退出保护复用 Git 写入门禁；终端与消息编辑快捷键受模态框遮挡。
- 首版不包含复制未跟踪配置、安装依赖、已有分支创建、删除、归档与自动清理。

## 自动化验证

- `./scripts/go test ./...` 通过，UI 整包 20.376 秒、Git 整包 9.982 秒；补充来源搜索交互后，`./scripts/go test ./internal/ui -run '^TestWorktreeCreation' -count=1` 再次通过。
- `./scripts/go vet ./...`、`./scripts/build`、`git diff --check` 通过。
- `./scripts/go test -race ./internal/git ./internal/ui`：Git 整包通过；UI 的既有 `TestGitHunkStageConfirmsUnsavedEditorBeforeUsingDisk` 在繁忙验证期间等待状态超时，因此该次整包 race 未通过。随后 `./scripts/go test -race ./internal/ui -run 'TestWorktreeCreation|TestGitHunkStageConfirmsUnsavedEditorBeforeUsingDisk' -count=1` 通过，10.595 秒。
- 临时仓库覆盖远程来源精确提交、detached 来源、无首次提交、非法及已有分支、目标路径冲突、来源失效、中文空格目录、仓库隔离、继承 Git 环境覆盖变量、原工作区文件与 index 不变及不复制 `.env`。
- 用受控 post-checkout hook 和 300 毫秒超时制造中断，核验已存在的目录、分支及 Git 登记保留，并明确报告未完成。
- 受控 UI 覆盖右键菜单、来源搜索、当前关联工作区默认来源、文件夹跟随及清空恢复、自定义保留、路径复制、浅深色布局、失败保留输入、忙碌关闭门禁、成功导航、重新发现及已创建后的刷新失败重试。

## 原生验收

使用 `/tmp/fuyu-worktree-native.1kVO6t/` 内的独立仓库及独立应用数据目录，不修改用户业务仓库或默认项目列表，不发送模型任务。

1. Cmd+Q 正常退出旧日常实例，进程检查确认退出，直接启动已构建的新版本隔离实例。
2. 仓库总标题右键出现“新建 Worktree”，打开后基础分支为 `main`。
3. 输入 `feature/native-check` 后，文件夹自动显示 `feature-native-check`；通过粘贴改为 `自定义 验收`，原生截图确认表单及实时完整路径可读。
4. 点击“创建”后弹窗关闭，新目录卡片被选中并进入空白会话 2；Git porcelain 核验主目录仍为 `main`，新增目录分支为 `feature/native-check`。
5. 正常退出并使用同一隔离数据目录重启，新增 worktree 卡片和空白会话 2 恢复。
6. 正常退出隔离实例，启动默认数据目录的新版日常实例，原项目列表及会话 167 恢复，窗口可操作。

原生验收目录保留在临时目录中。原生深色、中文输入法候选确认、真实失败后的表单保留尚未逐项操作；其中深色布局与失败输入保护已有受控 UI 覆盖。中文内容通过粘贴输入，不能作为中文输入法候选验收。

## 分支选择卡顿修复

同日用户反馈展开基础分支卡顿。只读计时：order 仓库共 9,646 条本地和远程引用，`rev-parse` 12.8 毫秒、`symbolic-ref` 12.5 毫秒、`for-each-ref` 22.0 毫秒；banner 为 3,200 条，Fuyu 为 7 条。这些计时来自本地 Git 子进程，不包含 UI，也未 fetch。

原选择器虽然放在 130 DIP 的 Scroll 中，仍逐条构建所有按钮。改为原生 List 虚拟化，缓存当前查询的过滤结果，在查询变化和重新打开时滚动回首项；完整候选可搜索和滚动访问。

- `BenchmarkWorktreeSourcePicker`，同机 `-benchtime=1x` 的受控单帧结果：10,000 条候选从 19,812,000 ns、1,288,648 字节、30,018 次分配降至 286,291 ns、12,840 字节、58 次分配。该基准为内存 UI 帧，不代表原生点击端到端延迟。
- `TestWorktreeSourcePickerLargeRepositoryScrollSearchAndSelection` 覆盖 10,000 条候选只构建可见行、尾项滚动、搜索远处项及选择后精确引用。
- Worktree 定向测试及 `./scripts/go test ./internal/ui` 通过，UI 整包 21.155 秒；`./scripts/go test -race ./internal/ui -run '^TestWorktree' -count=1` 通过，7.352 秒；UI vet、构建及差异检查通过。
- 正常退出旧实例并重启新构建，原会话 168 恢复。在真实 order 仓库打开表单，默认基础分支为本地 `sr-next`；展开后原生树仅含 7 个候选按钮，输入 `origin/sr-next` 后显示对应远程候选并能选择，收起后准确显示 `origin/sr-next（远程）`。取消弹窗后回到原会话，没有创建分支、worktree 或发送模型任务。本轮原生操作未记录精确端到端耗时。

## 与 Git 面板共享分支数据

同日按用户确认，以 Git 面板的分支加载层为统一入口，创建弹窗复用仓库级缓存。

- `RepositoryBranches` 一次读取完整本地/远程引用及提交、上游、worktree 占用信息，过滤符号引用别名；Git 面板取本地分支，创建弹窗取全部引用。当前分支标记由各自工作区状态计算，文件状态和暂存保持工作区隔离。
- `internal/ui/git_branches.go` 按 common dir 缓存并合并正在进行的请求；首次仓库发现前的工作区缓存会接续到仓库身份。有效缓存立即显示，30 秒后后台更新；初次失败和刷新失败均可重试，已有列表及用户输入保留。
- 普通文件/index 事件与正常状态刷新复用分支缓存；手动“刷新 Git”、非暂存 Git 操作、新建 worktree、refs/packed-refs/HEAD/相关配置变化使缓存失效。读取途中再次失效的旧结果丢弃并统一补读。
- 相同候选数据继续复用过滤缓存和虚拟列表位置；实际创建、切换仍重新校验 Git 状态。缓存仅存于当前进程内存。

验证结果：

- `./scripts/go test ./...` 通过，UI 整包 21.773 秒。
- `./scripts/go vet ./...`、`./scripts/build`、`git diff --check` 通过。
- `./scripts/go test -race ./internal/git ./internal/ui -run 'TestGitBranch|TestWorktree|TestWorkspace|TestGit.*Branch|TestGitWorkflow|TestRemoteActions' -count=1` 通过，Git 3.670 秒、UI 19.203 秒。
- `TestGitBranchCatalog*` 覆盖 Git 面板/弹窗/关联工作区同时请求只读取一次、暖缓存立即展示、工作区当前分支独立、普通文件和 index 事件不失效、引用事件失效、进行中失效结果淘汰、首次与后台读取失败、重试、发现前缓存接续、缓存到期更新及候选未变时保留搜索、Git 面板真实新建分支后弹窗立即复用更新后的快照。
- 原有状态刷新队列回归预先准备分支缓存，继续独立核验同一文件事件批次只补读一次工作区状态；分支缓存并发路径由新回归单独覆盖。
- 正常退出旧 Fuyu 后启动新构建，原会话 168 和 order Git 面板恢复。原生打开 Git 分支列表，本地 `sr-next` 排首；收起后从仓库右键打开 Worktree 弹窗，默认立即显示同一 `sr-next`，展开并搜索得到 `origin/sr-next（远程）`。取消后回到原会话及 Git 面板。没有创建业务分支或 worktree，也未发送模型任务。

请求合并次数由受控读函数计数证明；原生验收确认实际界面与两处数据投影，未对进程中的 Git 调用另行抓取计数。
