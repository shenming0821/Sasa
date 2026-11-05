# 编辑器全文搜索与文件夹范围

## 行为与原因

按用户要求实现编辑器全文搜索与目录右键“在文件夹中搜索”。文件面板查找输入框新增“全文”开关（`workspacePage.searchContent`）：内容模式调用新增的 `workspace.SearchContent`，逐行不区分大小写匹配；结果行显示文件名、`:行号`、目录和以匹配为中心的裁剪预览，点击、Enter 或方向键打开文件并按匹配行列定位光标（复用消息链接的 `fileLocation`/`pendingLocation`）。

目录行右键菜单（`fileMenu` 新增 `dir` 参数，仅 `row.Dir && !row.Link` 的行提供）选择“在文件夹中搜索”后切到内容模式、范围设为该目录并聚焦输入框；范围目录同时作用于文件名搜索，输入框下方范围条显示相对路径，清除按钮恢复整个工作区。范围文件夹失效（重命名后由 `refreshEditorPath` 跟随新路径，删除时清除）或已不存在时静默回退到工作区根。

`SearchContent` 与原 `Search` 共用新的 `walkEntries` 遍历（忽略标记、符号链接、常见依赖目录、10 万项与 16 MiB 路径上限），另跳过二进制与非 UTF-8 文件；单文件上限 4 MiB、每文件 50 行、合计 200 行封顶，超限或文件读取失败标记 `Truncated`，面板显示“结果不完整，请缩小搜索范围”。搜索发起以模式+范围+查询为键（`searchKey` 存入原 `searchQuery` 字段），沿用既有取消与代次丢弃。

## 自动化

- `TestContentSearchScopeSkipsAndTruncates`（internal/workspace）：内容搜索命中行号/列号与预览、每文件 50 行封顶标记 Truncated、跳过 `node_modules` 与含 NUL 二进制、中文列号按 rune 对齐（`needle 你好` → 列 7）、子目录范围只命中范围内文件、越出工作区报错、范围目录的文件名搜索可用。
- `TestP4FolderContentSearchMenuScopeAndOpen`（internal/ui）：目录右键出现“在文件夹中搜索”，选择后内容模式+范围+范围条生效；输入 `fuyu` 命中 `internal/main.go:4:14` 并显示预览，Enter 打开文件且光标落在匹配行；清除范围后命中增至 2 条（含 README.md）；切回文件名模式清空内容结果；文件行菜单不含目录项。
- `TestP4ContentSearchKeyboardAndModeToggle`：文件名模式无结果时切“全文”得到 2 条命中，Down/Enter 打开第二条。
- 受影响既有测试：`TestP4SearchPreservesTreeScrollAndRejectsOldRows` 改为用 `w.searchKey()` 写入已发起键；其余文件面板/菜单/重命名删除测试保持。
- 已跑：`./scripts/go test ./internal/workspace -count=1`、`./scripts/go test ./internal/ui -count=1`、`./scripts/go test -race ./internal/workspace -count=1`、`./scripts/go test -race ./internal/ui -run 'ContentSearch|FolderContentSearch|SearchPreserves|WorkspaceSwitch|EditorFileMenus|EditorRename|EditorPath|EditorDelete' -count=1`、`./scripts/go vet ./internal/...`、`./scripts/go build ./...`、`gofmt`、`git diff --check` 全部通过。

## 原生窗口

`./scripts/build` 签名构建通过。旧版正常退出（PID 25310），`open bin/Fuyu.app` 复现既有启动阻塞（PID 42962 进程存活但无窗口，SIGTERM 不响应），结束后直接启动二进制，新版 PID 43370 主窗口可见（1920×998）。待人工核对：目录右键出现并执行“在文件夹中搜索”，范围条显示与清除，全文结果行点击/键盘打开定位，与文件名搜索切换的滚动和选中状态。
