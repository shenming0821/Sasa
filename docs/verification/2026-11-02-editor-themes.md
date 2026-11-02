# 编辑器 GitHub Light / Dark 主题验证

## 实现与来源

文件编辑器、行内及并排 Diff 跟随应用外观使用 GitHub 配色。后台保留 Chroma token 类型与 rune 区间，当前帧映射颜色；移除原来的固定五类颜色和 Diff 深色混色。浅色底色/正文 `#ffffff` / `#1f2328`；深色底色/正文 `#0d1117` / `#e6edf3`。选区浅色 `#b6d7fb`、深色 `#264f78`；行号浅色 `#57606a`、深色 `#8b949e`。增行底色浅色 `#dafbe1`、深色 `#12261e`；删行底色浅色 `#ffebe9`、深色 `#2c171c`。工具栏与其他应用界面使用原有主题。

只读参考本机 Mayoi 固定提交 `cd08688ade5ece3411d9bb812228c7d30492526a` 的高亮职责分离、文件名识别与 Vue 指令边界；没有运行 Mayoi。实际语法色使用 Fuyu 既有 Chroma v2.27.0 的 `github` / `github-dark` token 继承规则。Vue 规则副本按关闭引号结束指令值，并委派 script/style 的嵌入语言。MyGo 隔离补丁使光标跟随控件解析后的文字颜色。

## 自动化与组件验证

- `TestEditorLanguageTokens`：Go、JS/TS、JSX/TSX、Vue、HTML/CSS、JSON/YAML、Python、Shell、SQL、Markdown、Dockerfile、Makefile、Brewfile 和 `.npmrc` 的实际 token 类型，以及两套配色均有映射。
- `TestEditorVueDirectiveStopsAtClosingQuote`：先确认上游 Vue lexer 将多行指令后的 `v-show` 吞为字符串，再核对修正后的指令、属性、脚本关键字和 CSS 属性；`TestEditorVueEmbeddedLanguages` 覆盖 TS/TSX/JSX 委派。
- `TestEditorTokenOffsetsAndThemeInheritance`：中文、多行注释和字符串的 rune 区间，以及 token 子类继承。
- `TestEditorGitHubThemesAcrossViews`：实际文件/行内/并排组件浅深色反复切换，核对底色、关键字、行号和删行背景像素；切换不改变文档或重新分词。可用 `FUYU_EDITOR_THEME_PREVIEW` 导出六张组件图。
- `TestEditorThemeKeepsDirtySelectionAndScroll`：两个不同语言的未保存文档保持选区、非零滚动和修改状态。
- `TestEditorThemeSelectionAndCaretPixels`：原生选区及光标实际像素；继续运行原有点击重置闪烁、取消高亮、可见区优先、大文件只读及 Diff 展示回归。

验证结果：

- 语言、Vue、字符区间、三种视图配色、未保存选区/滚动、光标和原有高亮回归通过。六张组件图导出至 `/tmp/fuyu-editor-theme-20261102`，人工查看浅色文件与深色并排图；来源为 MyGo 内存渲染器。
- `./scripts/go test ./... -timeout=180s` 全量通过，UI 包 21.355 秒。首轮全量发现独立并排视图夹具没有主缓冲，补上主缓冲存在性校验后，该回归与全量复跑通过。
- `./scripts/go vet ./...`、相关 `-race`、`./scripts/build`、`git diff --check` 通过。光标像素回归曾复现全局正文色覆盖编辑区颜色，修改 MyGo 本地补丁后验证通过。

## 原生窗口验收与启动恢复

旧版 PID 78591 通过 Cmd+Q 正常退出，进程查询确认退出。Launch Services 首次启动新版 PID 83293 后，窗口读取连续超时；主线程采样停在系统 `open`，与已有[标题修改验证](2026-11-02-message-headings.md)记录一致，原因仍未定位。本次采样为 `/tmp/fuyu-editor-theme-startup-20261102.sample`。向本轮新启动、尚无可操作窗口的进程发送 SIGTERM 后仍持有数据目录锁，直接启动返回“数据目录正在使用”；随后结束该阻塞进程，直接运行已构建可执行文件，PID 84683 窗口恢复可用。没有将这次启动恢复记为启动问题已修复。

在真实 1000×760 窗口中打开本仓库的 Go 与 Python 文件：确认关键字、函数、字符串、行号、正文与底色；在设置中从“跟随系统”切至“深色”，确认 Go 编辑器及行内/并排 Diff 正确切换，删行背景与修改块标记可辨。随后恢复“跟随系统”（当前系统为浅色），确认并排 Diff 恢复浅色并保留位置。Python 编辑区另核对浅色函数、关键字、字符串及光标，未编辑或保存源文件。保留新版窗口供用户使用。

接续验收在仓库外 `/tmp/fuyu-editor-theme-native-20261102` 创建 12 个受控样例，通过真实文件树逐一打开 Vue、TSX、TypeScript、JavaScript、JSON、YAML、CSS、HTML、Python、SQL、Shell 和 Markdown，并分别核对浅色与深色窗口截图。Vue 多行 `v-if` 关闭引号后的 `v-show`、`class` 和后续 template/script/style 均恢复正确 token 配色；另核对 TSX 标签、JSON 键/布尔值、CSS 属性、Python 类型/函数/字符串、跨行注释结束位置与中文正文。所有样例的保存按钮保持禁用，没有编辑或保存业务源文件。

多文件打开后切换深色，再恢复“跟随系统”，未见旧主题颜色残留；返回 Fuyu 原工作区后，浅色 Diff 与原阅读位置可用。样例工作区及文件保留用于复查，没有发送模型任务。本次来源为真实原生编辑器中的受控样例；JSX 的独立文件、特殊文件名和未保存选区/滚动仍以自动化回归为证据。真实窗口截图见本轮工具记录，组件图不作为原生验收证据。

## 覆盖边界

未新增独立主题设置；未知文件、`go.mod` / `go.work` / `go.sum` 按正文显示。继承现有大文件限额和可见区预览后的全文校正机制。Chroma 未提供语义信息的普通标识符使用正文色，不额外猜测符号类型。聊天代码块与终端主题不在本轮范围。
