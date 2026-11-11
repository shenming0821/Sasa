# 文件内容搜索 ripgrep 后端

## 改动

- `internal/workspace/search.go`：`SearchContent` 优先调用 `rg --json`（单进程、C 正则、并行遍历），通过 `rgPatterns` 把 Fuyu 的 `./` 锚定、目录名、`{a,b}` glob 翻译成 `--iglob`；解析 JSON 后按 Fuyu 树序（目录优先 + naturalLess）排序，复用 `matchPreview`/`IsWholeWord` 保持预览高亮与 Unicode 全字语义。rg 缺失或失败回退 Go walker。
- `searchFileContent` 改 128KB 分块流式读取，首块含 `\0` 即按二进制跳过；不再整文件 `ReadFile`+`strings.Split`。
- `internal/workspace/tree.go`：fallback 路径新增 `snapshotIgnored`，一次 `git ls-files --ignored --directory` 收集忽略文件/折叠目录；仅快照含深层忽略路径时退回逐目录 `git check-ignore`。
- per-file 100 行 / 总 1000 行 cap 与 `Truncated` 语义保留（`--max-count 101` 探测截断）。

## 性能（order 仓库，~1800 个非忽略文件，2.5G node_modules）

| 查询 | 改前 | 改后 | 匹配 |
|---|---|---|---|
| handleExpressChange | 413ms | 21ms | 20 |
| encounterId | 314ms | 30ms | 1564（cap 1000，Truncated） |
| nationCode | — | 21ms | 33 |

提速约 12-14 倍；旧实现主要耗时在逐文件 `ReadFile`+逐目录 fork `git check-ignore`。

## 自动化

`./scripts/go test ./...`、`./scripts/go vet ./internal/...`、`git diff --check` 全部通过；`TestContentSearchOptionsAndGlobs`、`TestP4ContentSearchOptionsGroupsAndCollapse`、`TestP4SearchFiltersCollapseAndFolderSearchReveal` 等搜索用例覆盖语义一致性。

## 原生验证（Preview 20261111T151248Z-c0ec73162d-8a8095）

- Fuyu 仓库搜 `searchContentRipgrep` → "3 个结果，位于 1 个文件"。
- 添加 order 项目并打开工作区，搜 `handleExpressChange` → "20 个结果，位于 5 个文件"，与 `rg` 直接执行结果一致，响应瞬时。
- 搜 `encounterId` → "1564 个结果，位于 145 个文件"并显示"结果不完整，请缩小搜索范围"提示，截断行为正确。
- 匹配片段按文件分组、行号可点击跳转、折叠按钮正常。
