# 编辑器搜索面板重设计

## 行为与原因

按用户要求参考 VS Code 搜索面板重新设计文件面板的内容搜索；用户明确不做替换。

- 查询框由 `searchField` 容器复刻输入框外观（Surface 底、主题圆角、聚焦描边），“区分大小写”（Aa）、“全字匹配”（ab）、“使用正则表达式”（.*）三个开关内嵌在输入框右缘；内容模式下“包含的文件 / 排除的文件”两个输入框常显在查询框下方，“使用排除设置和忽略文件”开关位于排除框内右缘。
- 搜索期间不再显示“正在查找…”：发起新搜索保留旧结果列表，响应返回时与折叠状态、选中项一并替换；查询清空时立即恢复文件树。
- 后端 `workspace.SearchContent(ctx, root, ContentQuery)` 取代原 `(root, dir, query)` 签名，内容搜索迁到 `internal/workspace/search.go`；`walkEntries` 增加 `respectIgnore` 参数，文件名搜索仍传 `true`，行为不变。
- glob 以逗号分隔（`{a,b}` 内的逗号保留），`./` 锚定工作区根，其余任意层级匹配，命中目录即覆盖其下文件；无效正则或 glob 返回错误并在面板显示。
- `ContentMatch.Ranges` 记录预览内的 rune 半开区间用于高亮，`Count()` 为该行命中数；上限调整为单文件 4 MiB、每文件 100 行、合计 1000 行、单行 100 个区间。
- 结果显示“N 个结果，位于 M 个文件”，按文件分组、可单个或全部折叠，文件头带命中数徽标；方向键选中已折叠文件内的命中时自动展开。
- “在文件夹中搜索”改为把 `./相对路径` 写入“包含的文件”，取代原范围条与 `searchScope`；目录重命名时该包含项跟随，删除时清空（`renamedSearchInclude`）。

## 自动化

- `TestContentSearchScopeSkipsAndTruncates`（internal/workspace）：改用 `ContentQuery`，每文件 100 行封顶标记 Truncated，校验匹配区间。
- `TestContentSearchOptionsAndGlobs`：默认不区分大小写、区分大小写、全字、正则、包含/排除、`**` 与 `{a,b}` glob、默认跳过依赖目录、`SearchIgnored`、锚定文件、无效正则与无效 glob。
- `TestMatchPreviewRangesOnLongLines`：长行裁剪后高亮区间仍对准匹配文本。
- `TestP4FolderContentSearchMenuScopeAndOpen`（internal/ui）：目录右键后包含项为 `./internal`，摘要“1 个结果，位于 1 个文件”，Enter 定位；清空包含项后命中 2 条。
- `TestP4ContentSearchOptionsGroupsAndCollapse`：分组行数、文件头折叠、方向键自动展开、全部折叠、区分大小写重新搜索并清空折叠状态、正则开关、排除 `*.md`、无效正则提示。
- `TestRenamedSearchIncludeFollowsFolder`：包含项随重命名迁移、删除清空、glob 或无关路径保持不变。
- 已跑：`./scripts/go test ./... -count=1`、`./scripts/go test -race ./internal/workspace ./internal/ui -run 'Search|P4|Content|Renamed' -count=1`、`./scripts/go vet ./internal/...`、`./scripts/go build ./...`、`gofmt -l internal/`、`git diff --check` 全部通过。

## 原生窗口

`./scripts/build preview` + `./scripts/run-preview` 启动候选 `20261105T122754Z-f82f6ca939-5ed2dc` 后，在 Preview 中以 CGEvent 模拟点击和输入实测：切到“全文”后出现 Aa / ab / .* 开关和“⋯”；搜索 `searchKey` 显示“11 个结果，位于 5 个文件”，按文件分组并带命中数徽标，匹配片段高亮；单击文件头可折叠；“⋯”展开“包含 / 排除”输入框和忽略文件开关。

实测发现：长行或窄面板下预览从命中前 60 个字符开始，命中常被截在面板外。改为预览始终从首个命中前约 20 个字符开始（前面加“…”），`TestMatchPreviewRangesOnLongLines` 补充了窄面板和短行用例。重建候选 `20261105T123407Z-c2acf02547-1e4708` 后复测，每行命中均可见。

第二轮按截图调整为开关内嵌输入框、包含/排除常显并移除加载提示：候选 `20261105T124711Z-28c9d7858c-6f0e38` 已在 Preview 中启动，人工核验布局与搜索行为。
