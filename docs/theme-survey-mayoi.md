# Sasa 主题系统调研：Mayoi

调研基线为 Sasa `3706a43`、Mayoi `b1f8126`。下文的 `MyGo/` 指 Sasa 锁定的 MyGo 依赖版本，见 `go.mod:9`。

**结论**　Sasa 已有跟随系统、强制浅色和强制深色三种外观，以及少量灰阶和状态色覆盖。要达到 Mayoi 的配色覆盖范围，建议把主题 ID 与原生外观拆开，由同一个应用级 palette 驱动控件、编辑器、diff、Markdown 和终端。同明暗主题之间的刷新、Markdown 的两条代码渲染路径，以及 Ghostty 已打开终端的原地更新，是三个需要单独验证的边界。依据：Sasa `internal/ui/settings.go:45–56,77–104,298–314`、`internal/ui/conversation_find.go:493–559`、`internal/ui/terminal.go:133–182`；Mayoi `cmd/mayoi/theme.go:1418–1478,1601–1630`、`cmd/mayoi/main.go:237–245`。

## 1. Mayoi 架构

### Palette 字段

| 范围 | 字段与用途 | Mayoi 依据 |
| --- | --- | --- |
| 外观与基础色 | `dark`、`mono`（单色文件图标和提示）；`editor`、`chrome`、`panel`、`border`、`hover`、`active`；`text`、`muted`、`faint`、`accent`、`accentDim` | `cmd/mayoi/theme.go:23–39` |
| 控件 | `fill`、`fillHover`、`fillPressed`、`fillText`；`dangerFill`、`dangerHover`、`dangerPressed`、`dangerText`；`surface`、`surfaceHover`、`surfacePressed`、`scrollbar`；`inverse`、`inverseText`（提示和 toast） | `cmd/mayoi/theme.go:40–53` |
| 编辑交互 | `line`、`selection`、`selected`、`selectedDim`、`gutter`、`gutterNow`；`match`、`matchNow`、`matchRing`、`guide`、`guideNow`、`bracket`、`word`、`cursor` | `cmd/mayoi/theme.go:54–60,65–72` |
| 状态、Git 与外壳 | `status`、`statusFg`、`modified`、`danger`、`shadow`、`added`、`changed`、`removed`；文字用 `modifiedFg`、`addedFg`、`removedFg`、`dangerFg`；装饰与布局用 `rule`、`gilt`、`card`、`grain`、`strip`、`sentence` | `cmd/mayoi/theme.go:61–64,73–97` |
| 语法与终端 | `syntax [highlight.Kinds]ui.Color`，含 Plain、Keyword、Type、Function、String、Number、Comment、Operator、Punctuation、Constant、Builtin、Tag、Attribute、Variable、Heading、Deleted、Inserted、Strong、Emph、Mark。`ansi [16]ui.Color` 依次为黑、红、绿、黄、蓝、品红、青、白，再按同顺序排列高亮色 | `cmd/mayoi/theme.go:99–102`、`internal/highlight/highlight.go:24–51` |
| 吉祥物 | 独立的 `mascot` 结构有 `name`、`size`、`draw`。`themeMascotOf(id)` 按主题 ID 查找，绘制接收 palette。欢迎页、状态栏和主题色卡消费该映射 | `cmd/mayoi/mascot.go:9–73`、`cmd/mayoi/themepicker.go:298–309` |

- **派生**　`derive()` 生成危险按钮悬停和按下色，针对 `chrome`、`editor`、`panel` 把四种语义文字色朝黑或白调整至目标 4.5:1，若离散搜索走到末步则返回端点。它把选区叠到编辑器底色上得到不透明的聚焦和失焦选区，用半透明强调色生成块光标，令 Markdown 粗体和斜体用关键字色、标记用正文色，并给透明的 `strip` 补上 `chrome`。依据：`cmd/mayoi/theme.go:1482–1502,1558–1576`。
- **分发**　设置分别保存 `theme` 与 `appearance`，空值分别表示 Mayoi 默认主题和跟随系统。11 个内置选项共 16 个 palette。默认、Claude、Qoder、Grok Bot、Cursor 各有浅深变体，Matrix、Purple Gold、Jack-o’-Lantern、Witch’s Brew、Blood Moon、GTA VI 固定为深色。`alwaysDark()` 同时检查插件。`themeSource()` 给固定深色主题返回原生 `ThemeDark`，其他主题交由外观设置。`paletteFor()` 优先找插件，然后匹配内置 ID 和明暗，最后回退默认浅深色。`applyTheme()` 在帧首解析 palette，把通用控件字段投影到 `ui.Theme` 并调用 `c.SetTheme()`，业务视图继续用 palette，例如窗口外壳的 `chrome` 和编辑器的 `syntax`。依据：`internal/settings/schema.go:62–64`、`cmd/mayoi/themepicker.go:22–38`、`cmd/mayoi/theme.go:1372–1478,1601–1630`、`cmd/mayoi/view.go:66–71,111`、`cmd/mayoi/editor.go:1151`。
- **预览、提交与重绘**　主题弹层打开时记住原值，搜索和上下键移动候选时 `previewChoice()` 调 `previewTheme()`，更新共享内存设置、当前窗口跳过渐变，并直接调 `host.setTheme()`。确认调用与命令 `app.setTheme()` 共用的 `themeChanged()`，经 `settings.Changed()` 延迟写盘，取消则用 `previewTheme()` 恢复。`desktop.setTheme()` 设置原生外观，逐个使工作窗口失效并刷新菜单，覆盖原生明暗相同的切换。各窗口下一帧重新取 palette，普通切换可逐帧插值并遵守减少动态效果偏好。独立设置窗口在帧首也解析 palette，提交时由偏好变更回调显式失效，实时预览走的是工作窗口广播路径。终端在主题 ID 变化时给已有会话更新浅深两套方案，包含前景、底色、光标、选区、搜索色和 16 ANSI 色，再由终端跟随窗口明暗。依据：`cmd/mayoi/themepicker.go:55–109,119–145,177–209,216–232`、`cmd/mayoi/commands.go:435–452`、`cmd/mayoi/main.go:237–245,484–499`、`cmd/mayoi/app.go:516–523`、`cmd/mayoi/view.go:66–70`、`cmd/mayoi/theme.go:1639–1703`、`cmd/mayoi/prefswindow.go:147–166,259–265`、`cmd/mayoi/panel.go:154–182`、`internal/settings/settings.go:899–920`。
- **插件**　启用插件通过 `contributes.themes` 提供 ID、标签、路径和可选 `dark`。运行时将贡献追加进选择列表，同时注册命令。主题文件从插件文件系统读为 JSONC，可继承默认浅深色或指定主题，并覆盖 `colors`、`syntax`、`ansi`。也支持将部分 VS Code `colors`、`tokenColors` 的 scope 与 ANSI 键映射至自身字段。合并后重新派生，无法读取的主题经 `paletteFor()` 回退。palette 缓存以 ID 与明暗为键，插件 generation 变化后清空。依据：`internal/plugin/manifest.go:54,164–172,411–415`、`cmd/mayoi/plugins.go:474–476`、`cmd/mayoi/plugintheme.go:24–228,350–483`、`cmd/mayoi/theme.go:1431–1467`。

## 2. Sasa 现状

| 子系统 | 行为与依据 |
| --- | --- |
| 外观、控件 | `Preferences.Theme` 保存 `system`／`light`／`dark`，默认 `system`。启动和保存成功后用 `mygo.Theme.SetSource()` 应用。主窗口和 Quick Open 浮窗在入口执行 `applyAppearance()`，复制帧主题，浅色覆盖五个灰阶，再设置字体和 `Accent`、`Warning`、`Danger`、`Success`。状态色朝 4.5:1 目标调整，Accent 的 hover、pressed 等字段继续来自 MyGo 帧主题。按 `git grep -l -F 'c.Theme()' -- '*.go'` 复核为 **63 个文件，48 个实现、15 个测试**。代表入口见 `internal/ui/view.go:45–49,127,141,227,353,483,535` 和 `internal/ui/quick_open.go:334–356`。其余依据：`internal/storage/store.go:132–146`、`internal/ui/persistence.go:84–85`、`internal/ui/app.go:160–168`、`internal/ui/settings.go:45–56,77–125,154–171,298–314`、`internal/ui/status_palette.go:48–75`、`MyGo/ui/theme.go:22–32`。 |
| 源码语法高亮 | 文件编辑器用 `t.Dark` 选 `githubEditorPalette()`。底色、选区、行号和强调色独立定义。Chroma 分词保存类型和偏移，绘制时取 `styles.Get("github-dark")` 或 `styles.Get("github")`，按 token 类型转成 `TextRange`。依据：`internal/ui/editor_syntax.go:14–17,80–125`、`internal/ui/editor_highlight.go:118–150`、`internal/ui/editor_view.go:845–847,901–906`。 |
| Diff / Git | 行内 diff 沿用文件编辑器 palette，新增和删除行分别涂底，边缘定位条用 `accent`。并排 diff 使用同一函数，两侧分别用删、增底色，行号的 `+`／`-` 由 `lineTheme()` 映射到增删前景。工具栏和聊天变更卡统计读控件主题。提交图与作者头像共用六色循环，其中四个槽位固定。依据：`internal/ui/editor_view.go:822,845–847,957–1002`、`internal/ui/git_diff.go:76–85,208–255,271–296`、`internal/ui/editor_syntax.go:110–112`、`internal/ui/diff_rows.go:21–33`、`internal/ui/change_card.go:68–71`、`internal/ui/git_commits.go:471–475`、`internal/ui/git_commit_avatar.go:70–80`。 |
| Markdown | 聊天普通和流式渲染均用 MyGo Markdown。Sasa 配字体和排版，粗体用 `Danger`，搜索标记用 `Accent` 的透明变体。MyGo 默认从帧主题取正文、链接、边框，代码背景用文字色透明层，代码块 token 用 MyGo 自有 GitHub 式明暗表。搜索命中的代码块走 Sasa 自绘路径，并重新取 `markdown.DefaultTheme(t)`。计划页也使用聊天 Markdown。依据：`internal/ui/app.go:119–120`、`internal/ui/settings.go:102–103`、`internal/ui/message_links.go:26–28,63–105`、`internal/ui/conversation_find.go:361–363,493–559`、`internal/ui/plan.go:133`、`MyGo/ui/markdown/theme.go:100–126,147–208`、`MyGo/ui/editor/theme.go:55–131`。 |
| 终端 | 工作区终端是 macOS 下的 Ghostty 原生 surface。容器背景取 MyGo 帧主题，`Present()` 跨桥传 `dark` 和 `focus`。Ghostty 载入默认和递归配置，创建时设 app color scheme，显示和刷新时设 surface color scheme。有效 ANSI、底色和光标色由 Ghostty 配置及默认值解析，Sasa 当前只传明暗布尔值。依据：`internal/ui/terminal.go:96–109,133–182`、`internal/terminal/session_darwin.go:22–57`、`internal/terminal/bridge_darwin.m:167–180,216–244,258–264`。 |

### 全部 `native.Hex` 调用点

扫描当前 Git 跟踪的 Go 文件，共 **45 次调用、30 个源码行、8 个文件**。表内路径均相对于 Sasa 仓库根目录。同一行的多个调用按源码顺序列出。

| 文件:行 | 字面量及用途 |
| --- | --- |
| `internal/ui/settings.go:91` | `#fefefe` 浅色窗口背景 |
| `internal/ui/settings.go:92` | `#fafafa` 浅色控件表面 |
| `internal/ui/settings.go:93` | `#efefef` 浅色悬停表面 |
| `internal/ui/settings.go:94` | `#e4e4e4` 浅色按下表面 |
| `internal/ui/settings.go:95` | `#e4e4e4` 浅色边框 |
| `internal/ui/settings.go:112` | `#f0f0f1` 浅色用户消息气泡 |
| `internal/ui/settings.go:121` | `#ffffff` 浅色聊天输入卡 |
| `internal/ui/status_palette.go:29` | `#8b7cf6` 深色 accent／运行 |
| `internal/ui/status_palette.go:30` | `#e8b86d` 深色 modified／等待 |
| `internal/ui/status_palette.go:31` | `#f0718b` 深色 danger／失败 |
| `internal/ui/status_palette.go:32` | `#4fa86d` 深色 added／成功 |
| `internal/ui/status_palette.go:36` | `#6d5bd0` 浅色 accent／运行 |
| `internal/ui/status_palette.go:37` | `#c4862a` 浅色 modified／等待 |
| `internal/ui/status_palette.go:38` | `#d0365a` 浅色 danger／失败 |
| `internal/ui/status_palette.go:39` | `#3fa363` 浅色 added／成功 |
| `internal/ui/editor_syntax.go:90` | `#0d1117`、`#e6edf3`、`#8b949e`，深色编辑底色、正文、次级文字／行号 |
| `internal/ui/editor_syntax.go:91` | `#264f78`、`#30363d`、`#58a6ff`，深色编辑选区、边框、强调 |
| `internal/ui/editor_syntax.go:92` | `#3fb950`、`#f85149`，深色 diff 增删前景 |
| `internal/ui/editor_syntax.go:93` | `#12261e`、`#2c171c`，深色 diff 增删行底 |
| `internal/ui/editor_syntax.go:98` | `#ffffff`、`#1f2328`、`#57606a`，浅色编辑底色、正文、次级文字／行号 |
| `internal/ui/editor_syntax.go:99` | `#b6d7fb`、`#d0d7de`、`#0969da`，浅色编辑选区、边框、强调 |
| `internal/ui/editor_syntax.go:100` | `#1a7f37`、`#cf222e`，浅色 diff 增删前景 |
| `internal/ui/editor_syntax.go:101` | `#dafbe1`、`#ffebe9`，浅色 diff 增删行底 |
| `internal/ui/agent_status_badge.go:21` | `#D97757` Claude 状态徽标 |
| `internal/ui/agent_picker.go:75` | `#D97757` Claude 选择项图标 |
| `internal/ui/panes_view.go:183` | `#D97757` Claude 会话标签图标 |
| `internal/ui/git_commit_avatar.go:85` | `#D97757` Claude 共著者标记底色 |
| `internal/ui/git_commit_avatar.go:87` | `#13985a` Qoder 共著者标记底色 |
| `internal/ui/git_commit_avatar.go:89` | `#6d5ce0` Pi 共著者标记底色 |
| `internal/ui/git_commits.go:472` | `#b98a24`、`#2b9c69`、`#b557c7`、`#269eb8`，提交图和作者头像循环第 2、3、5、6 槽 |

同次扫描还核对了非 `Hex` 字面量。固定黑色阴影与遮罩见 `internal/ui/view.go:97,339,356`、`internal/ui/completion.go:296`、`internal/ui/editor_view.go:1009`、`internal/ui/conversation_find.go:788`、`internal/ui/conversation_navigation.go:207`、`internal/ui/quick_open.go:355,357`、`internal/ui/settings.go:361`、`internal/ui/settings_general.go:36,79`、`internal/ui/settings_auxiliary.go:119`、`internal/ui/models.go:585`。搜索叠层用近透明黑色 `ghost`（`internal/ui/conversation_find.go:439`），缩略图删除按钮用固定白色图标和三态深灰色（`internal/ui/images.go:413,417`），SVG 含运行时品牌色（`internal/ui/agent_icons.go:7,19`），对比度算法使用黑白端点（`internal/ui/status_palette.go:61,63`）。

## 3. 改造优先级与工作量

| 顺序 | 建议 | 粗估与依据 |
| --- | --- | --- |
| P0 | **应用级 palette 与字段**。集中定义主题 ID、明暗变体、默认回退和派生。扩展 Sasa 自己的 palette，持有能投影到 `native.Theme` 的背景、表面三态、前景、边框、强调三态及前景、焦点、选区和状态色，再补聊天气泡、输入卡、编辑器、diff、Markdown、终端、阴影和提交图字段。把状态点原色、语义文字色、增删行底色分开。沿用 MyGo 结构与 `c.Theme()` 消费方式。 | **中**。Sasa `internal/ui/settings.go:77–122`、`internal/ui/status_palette.go:26–75`；`MyGo/ui/theme.go:7–57`；Mayoi `cmd/mayoi/theme.go:1482–1502,1601–1630`。 |
| P0 | **设置与热切换**。保留旧 `Preferences.Theme` 三个取值的外观语义，新增 `ThemeID`，缺省和未知 ID 都解析为默认主题。现有设置内提供独立的「配色主题」和「外观」选项，复用保存失败回滚。保存成功后从统一入口更新外观并显式失效主窗口与打开的浮窗，覆盖同明暗切换；系统外观变化继续交由 MyGo 通知。主题资源以 ID 和解析后的明暗建立缓存，刷新保留现有视图键、文档和会话。 | **中**。Sasa `internal/storage/store.go:132–146`、`internal/ui/settings.go:154–180,298–314`、`internal/ui/app.go:160–168`、`internal/ui/quick_open.go:292–335`；`MyGo/modules.go:479–497`；Mayoi `cmd/mayoi/main.go:237–245`。 |
| P1 | **组件接入**。通用侧栏、会话标签、设置和弹窗继续读 `c.Theme()`；在主视图和 Quick Open 共用主题解析。迁移聊天气泡、输入卡、浮层阴影与遮罩、状态点和 Git 提交图的专项颜色。针对 Codex 与 Claude 都要覆盖的侧栏状态、会话标签和聊天页检查对比度。 | **中**。`internal/ui/view.go:45–49,97,353–356`、`internal/ui/message_edit.go:362`、`internal/ui/quick_open.go:334–357`、`internal/ui/agent_status_badge.go:79–81`、`internal/ui/panes_view.go:183`、`internal/ui/git_commits.go:471–475`。 |
| P1 | **编辑器、语法高亮、diff 与 Markdown**。用有效 palette 替换 `githubEditorPalette(t.Dark)` 的调用，默认主题可沿用现有 Chroma 样式。主题提供 Chroma 样式或语义 token 映射，切换时重映射已有 token，保留 buffer、选区、滚动和未保存编辑。同步适配 MyGo `editor.Theme` 与 `markdown.Theme`，让普通、流式和搜索命中代码块共用同一配色；主题变更时更换 `CodeColors` 快照以使已缓存的 spans 重建。diff 的行内与并排共用增删底色、前景和选区。 | **大**。`internal/ui/editor_syntax.go:80–125`、`internal/ui/editor_view.go:845–847,901–906,957–1002`、`internal/ui/editor_syntax_test.go:237–273`、`internal/ui/git_diff.go:208–255`、`internal/ui/message_links.go:63–105`、`internal/ui/conversation_find.go:493–559`、`MyGo/ui/markdown/view.go:195–209`。 |
| P2 | **Ghostty 终端联动**。通过现有 bridge 传递应用的前景、底色、光标、选区及 ANSI 16 色，先验证 libghostty 的配置注入与现有 surface 原地更新，再确定应用主题与 Ghostty 用户配色的优先级。切换验证应覆盖已打开与隐藏后恢复的终端，保留 shell、滚屏和焦点，并验证同明暗的换色。头文件声明了配置加载和 surface 更新接口，当前 bridge 的动态加载表已有 app 更新接口。 | **大**。`internal/ui/terminal.go:156–182`、`internal/terminal/bridge_darwin.m:12–24,167–180,216–264`、`internal/terminal/native/ghostty.h:1041–1048,1067–1080`。 |

## 4. 建议后置

- **吉祥物、专属装饰和主题动画**。吉祥物、金边、颗粒、卡片布局和主题淡入属于独立的绘制或布局改动。首版用默认主题和少量有区分度的内置主题验证全链路即可。模糊搜索、微缩色卡和键盘移动时的全窗预览也可后置。依据：Mayoi `cmd/mayoi/mascot.go:9–73`、`cmd/mayoi/theme.go:82–97,1505–1553,1639–1703`、`cmd/mayoi/themepicker.go:119–145,298–317`。
- **插件主题、VS Code 导入和外部文件热重载**。后续可沿用主题 ID 和 palette 查找接口，再引入外部格式的字段映射、继承与校验、启停和缓存失效。运行时与共著者品牌标记可继续使用原色，首版检查它们在各主题背景上的可读性。依据：Mayoi `cmd/mayoi/plugintheme.go:49–109,159–228,350–483`、`cmd/mayoi/main.go:786–807`；Sasa `internal/ui/agent_icons.go:7,19`、`internal/ui/git_commit_avatar.go:81–95`。

## 验证

- 自动化使用 Go 1.27.3（Linux/amd64）。`go vet ./...` 和仓库包装入口 `./scripts/go vet ./...` 均在既有的 `internal/power/power_test.go:42` 报 `undefined: parseCustom`，该函数定义在 Darwin 专用的 `internal/power/system_darwin.go:25`。排除 `internal/power` 后，其余包的 `go vet` 通过。该包单独以 `GOOS=darwin CGO_ENABLED=1 go vet ./internal/power` 交叉检查通过。包装入口依据 `docs/development.md:11,76`。
- 本次仅新增调研文档。Codex、Claude 真实 Runtime 及 macOS 原生 UI 未运行，Ghostty 配色接口只做了源码核对。
