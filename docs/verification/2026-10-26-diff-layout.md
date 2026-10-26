# Diff 宽度与横向滚动修复

日期：2026-10-26。

## 问题与修复

用户反馈 Diff 正文没有横向撑满窗口，也没有横向滚动条。修复前原生历史 Diff 截图可见正文在约 600 DIP 处截断，右侧仍有大块空白。

- 行内正文取实际可用区与内容宽度的较大值；并排视图按扣除分隔线后的半宽计算各侧占位。
- 文本区域设置 `Shrink(0)`，长行内容保留横向溢出，交由外层水平滚动容器处理。并排两侧各自维护水平滚动状态，原有垂直联动保留。
- 新 TextBuffer 初始版本为 0，不能仅用默认高亮版本判断是否需要计算宽度；尚无内容宽度时也启动计算，刷新 Diff 后清除旧宽度和装饰。

## 自动化验证

`TestDiffContentFillsViewportAndScrollsHorizontally` 使用临时目录和受控 patch，通过实际异步宽度计算验证 480/1000/1800 DIP、行内/并排、长短行共 12 个场景，包含窄窗口行内回退。

短行断言填满视口且无横向溢出；长行断言横向滚轮改变偏移、底部滚动条可拖至后半区。长行刷新为短行后，断言内容重新计算、滚动范围和偏移归零。修复前首次执行复现宽窗口正文只有 600 DIP、长行无法横向滚动；修复后通过。

通过：

```sh
./scripts/go test ./...
./scripts/go vet ./...
./scripts/go test -race ./internal/ui -run 'TestDiffContentFillsViewportAndScrollsHorizontally|TestGitSplitScrollAndSelectionFollowEitherPane' -count=1
./scripts/build
git diff --check
```

原有修改块动作、并排垂直滚动联动和点击定位回归通过。未重复执行全量 race 或真实 Codex 调用。

## 原生窗口复核

正常退出并重开 `bin/Fuyu.app`，打开本次 `docs/subsystems/editor.md` 的真实工作区 Diff：

- 行内修改背景延伸至右侧 Git 面板边缘，长行区域底部可见横向滚动条。
- 切换并排后，左右列均占满各自半区，修改背景延伸到各自边缘；右侧底部可见横向滚动条。
- GUI 拖动复核期间，工具多次报告应用已被用户改变，要求重新读取状态；刷新后仍重复拦截。未将 GUI 拖动动作记为通过；滚轮位移和滚动条拖动由上述组件回归确认。

原应用保留打开状态与当前 Diff。未执行暂存、提交或推送。

## 后续：最右端仍截断

用户继续反馈滚动条到头仍不能显示完整内容。原因是后台按“字符数 × 8 + 90”估算宽度，中文和字体回退后的字符实际宽度更大；原回归只覆盖英文长行，未检查最大滚动位置与实际文本末端的关系。

改用 MyGo `ShapeText` 的真实排版结果逐行测宽，保留 Tab 与行尾空格，并在后台任务中保持取消及缓冲版本校验。行内采用 monospace 13 DIP，宽度额外计入 90 DIP（82 DIP 左右内边距及 8 DIP 留白）；并排两侧分别按其实际文本、monospace 12 DIP 测量，各加 28 DIP（20 DIP 内边距及 8 DIP 留白）。两侧不再共用原始 patch 的估算宽度。外部更改比较也从首次载入开始计算实际宽度。

新增 `TestDiffHorizontalEndRevealsWholeLine`，覆盖中文、Tab/中英混排/行尾空格、Emoji/全角字符及行内/并排两侧。测试将偏移设置到最大值，核对真实排版行尾落在视口右内边距之内；中文和 Emoji 场景修复前失败、修复后通过。此前 12 个布局场景仍通过。

完整 test、vet、构建和以下 race 检查通过：

```sh
./scripts/go test -race ./internal/ui -run 'TestDiffHorizontalEndRevealsWholeLine|TestDiffContentFillsViewportAndScrollsHorizontally|TestP4CanceledHighlightLeavesQueueBeforeWorkerIsFree' -count=1
```

原应用已正常退出并启动最新构建；读取原生窗口连续两次返回 `timeoutReached`，因此本次“实际窗口滚到最右后行尾完整可见”尚未完成 GUI 验收。保留运行实例，没有因读取超时重复重启。
