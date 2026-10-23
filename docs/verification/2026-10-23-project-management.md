# 仓库导入与 worktree 会话导航验证

日期：2026-10-23。

## 本轮行为

参考用户提供的 Orca 截图与本地源码，Fuyu 左侧新增仓库导入，以及仓库、worktree / 分支、所属会话三级列表。导入主目录或任一 worktree 按 common dir 合并；点击工作区打开或新建 cwd 对应会话，独立按钮可创建更多会话。项目列表与工作区元数据写入原 settings JSON，保持 schema 2。普通目录保留原有分组。

启动、每 30 秒和手动操作刷新 Git 登记的 worktree。导入超时 10 秒，单次刷新总超时 20 秒；扫描在后台进行。主路径不可读时尝试关联 worktree；失败保留旧列表并提示错误。缺失目录、bare 与 prunable 禁止从工作区入口新建会话；存在的 locked 目录仍可使用。

本轮不执行 Git checkout、创建或删除分支与 worktree、不改变用户暂存状态，不包含已移除的代码审查功能。页面上的分支对应 Git 已登记工作区当前检出的分支；未创建工作区的其他分支不作为独立目录列出。

## 源码参考

只读检查 `/Users/shenming/Documents/AI/orca/` 下的 `src/main/git/worktree-list-reader.ts`、`worktree-listing.ts`、`src/shared/git-worktree-porcelain-parser.ts` 与 `src/main/ipc/repos.ts`。采用 common dir 身份、NUL 列表、主工作区标记和失败保留策略；使用 Go 独立实现。Orca 原仓库未修改，参考项目测试不作为 Fuyu 验收证据。

## 自动化

`internal/project/repository_test.go` 使用临时真实 Git 仓库，不访问远端或模型：

- 从关联 worktree 发现主目录与完整列表；主目录和符号链接导入得到同一 common dir。
- 中文、空格和换行目录，分支 refs 前缀处理、主工作区、detached、locked 与 prunable / missing 状态。
- locked 不误判为不能对话；实际缺失目录不误判可用。
- bare 仓库没有可用工作目录；非 Git 导入、已取消上下文及空列表返回明确失败。
- 扫描前后原仓库的已暂存与未暂存修改保持一致。

`internal/ui/projects_test.go` 使用临时 Git、SQLite 与 MyGo 组件：

- 导入关联 worktree，重复导入主目录不重复仓库，不为所有分支批量创建会话。
- 分层显示工作区、分支和主要标识；切换会话绑定正确 cwd，草稿隔离，同一工作区可创建多个会话。
- 分支搜索保留父层级，项目折叠隐藏工作区；已归入仓库的会话不再重复显示为普通目录组。
- 已有目录会话后台发现 Git 仓库，保存后重开恢复项目列表。
- 外部删除与新增 worktree 后刷新列表；全部扫描路径失效时保留上次数据及具体错误；导入失败不影响已有仓库。
- 嵌套仓库按最深路径归属，路径相似的其他目录不误归类；不可用 worktree 不创建会话。
- 640 / 1000 / 1400 DIP 下工作区按钮与新建按钮不重叠，窄窗口的临时侧栏仍可操作。

## 验证结果与边界

已通过完整 `./scripts/go test ./...`、`./scripts/go vet ./...`、`./scripts/go test -race ./...`、`./scripts/build`。新增与调整后的定向测试另行复核，差异执行 `git diff --check`。

使用 `FUYU_PROJECT_PREVIEW=/tmp/fuyu-project-management.png ./scripts/go test ./internal/ui -run '^TestRepositoryNavigationLayoutAndCollapse$' -count=1` 生成并检查受控渲染截图，显示仓库、两个 worktree、分支、主要标记、独立会话和新建入口。截图为组件样例，不是用户真实目录或原生窗口验收。

尚未验证最新版真实原生窗口的目录选择器、导入与搜索点击、周期 30 秒后外部 worktree 自动出现，以及真实 Codex 在不同目录执行任务。源码和组件验证确认新建会话的 cwd 绑定，不能据此宣称真实模型目录隔离已实测。构建位置为 `bin/Fuyu.app`。

## 补充：保留真实 worktree 测试空间

用户要求建立多个 Git worktree 进行测试，创建独立临时仓库，并保留以下空间供原生界面检查：

- 根目录：`/private/tmp/fuyu-worktree-test-4pa20moz`
- 主仓库：`/private/tmp/fuyu-worktree-test-4pa20moz/demo-repository`，分支 `main`。
- 关联工作区：`feature-chat` → `feature/chat`；`中文工作区` → `feature/chinese`（Git locked）；`workspace with spaces` → `fix/input-layout`；`review-detached` → detached HEAD；后加 `external-discovery` → `feature/external-discovery`。
- Manifest：根目录的 `manifest.json`，另存定位文件 `/tmp/fuyu-worktree-test-location.json`。
- 独立应用数据：根目录的 `app-data`，与日常 Fuyu 索引隔离。

先用 5 个工作区验证导入、身份去重、逐工作区点击绑定 cwd、独立草稿、刷新、重启项目与标签恢复。随后在外部新增第 6 个 worktree，再次验证自动扫描可识别新增项。入口为 opt-in `TestLiveProjectWorktreesReadOnly`，使用真实 Git 目录和 MyGo 组件，Git 只读；持久化到测试数据目录的 6 个会话均没有原生 thread，没有发送模型任务。

实际执行：

```sh
FUYU_PROJECT_TEST_REPO=/private/tmp/fuyu-worktree-test-4pa20moz/demo-repository \
FUYU_PROJECT_TEST_DATA=/private/tmp/fuyu-worktree-test-4pa20moz/app-data \
FUYU_PROJECT_PREVIEW=/private/tmp/fuyu-worktree-test-4pa20moz/worktree-preview.png \
./scripts/go test ./internal/ui -run '^TestLiveProjectWorktreesReadOnly$' -count=1 -v
```

5 项与 6 项验证均通过。完整输出在根目录的 `verification.log`、`external-discovery.log`、`verification-six-worktrees.log`，组件截图为 `worktree-preview.png`。重新执行带 `FUYU_PROJECT_TEST_DATA` 的验证前，应先退出占用该测试数据目录的应用；省略该变量时使用自动清理的独立临时索引。

已启动隔离原生实例：

```sh
open -n /Users/shenming/Documents/Sync/AI/Fuyu/bin/Fuyu.app --args \
  -cwd /private/tmp/fuyu-worktree-test-4pa20moz/demo-repository \
  -data-dir /private/tmp/fuyu-worktree-test-4pa20moz/app-data
```

核对时 Fuyu PID 为 `1812`（后续以实时进程为准）。只读查询其数据库得到 1 个仓库、6 个 worktree、6 个已打开标签，选中会话为 1，6 个会话的原生 thread 均为空。确认了启动进程和持久数据，不等同于窗口交互验收。

Computer Use 返回 `Computer Use was not approved to use Fuyu`，因此未核验原生窗口中的真实点击、目录选择器或 30 秒计时刷新，也没有执行真实模型任务。保留原生测试实例、仓库、worktree、数据和证据，未修改用户其他仓库或日常应用数据。


## 补充：工作区不展示空白会话占位

按用户补充的 Orca 截图调整：工作区下仅列出已有对话、未加载原生历史或待核验任务；空白及仅草稿会话隐藏子行。草稿和标签仍保留，工作区点击可进入原空白会话，新建按钮可创建独立会话；发送任务后显示实际会话标题。

`TestWorktreeHidesBlankSessionRowsUntilConversationStarts` 已通过，覆盖占位隐藏、工作区和标签入口保留、草稿切换不丢失、发送后出现、新建空白不重复增加子行、运行任务切回以及未加载原生历史继续可达。

六工作区受控渲染输出至 `/private/tmp/fuyu-worktree-test-4pa20moz/worktree-preview-no-placeholders.png`。截图来自真实 Git 列表与 MyGo 组件，不是原生窗口交互验收。原测试空间中的空白会话不删除，显示时按新规则隐藏。

### 工作区卡片完整悬停区域

`TestWorktreeCardHoverCoversActionArea` 已通过：文字区域悬停时背景覆盖操作区，移动至新建图标后外层背景保持一致，点击新建只创建一个对应目录的会话。组件预览 `/tmp/fuyu-card-hover.png` 已检查；原生视觉效果留待重开应用后确认。

### 提交前最终核对

- 已按 Orca 层级调整仓库、工作区与会话文字；卡片隐藏外部应用占用提示。
- 导入项目、打开项目、刷新统一到项目标题右侧。
- 全量 `./scripts/go test ./...`、`./scripts/go vet ./...`、`./scripts/go test -race ./...` 通过；最新侧栏构建成功并已重开日常应用。组件预览 `/tmp/fuyu-project-actions.png` 已检查，原生交互验收边界保持如上。
