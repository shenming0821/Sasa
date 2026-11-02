# 助手回复红色强调

2026-11-02 按用户截图，在 `App.messageMarkdown` 中将 Markdown 的 `Strong` 配色设为当前窗口主题的 `Danger`：浅色为 `#dc2626`、深色为 `#ef4444`，保留原有 600 字重。强调范围由助手输出的 Markdown 加粗标记决定，沿用解析器与渲染器；正文、行内代码、链接和标题继续使用各自配色。Codex、Claude 的助手消息共用该入口，流式、完成态和历史恢复一致，切换主题时渲染缓存随主题更新。

验证通过：

- `./scripts/go test ./internal/ui -run 'Test(MessageStrongText|MessageFileLink|StreamList|UnorderedList)' -count=1`：像素回归覆盖流式与完成态、浅色→深色→浅色切换、段落、列表及表格强调文字，并核对普通正文、行内代码与链接未变红。
- `./scripts/go test ./... -timeout=180s`：全量通过，UI 包耗时 21.811 秒。
- `./scripts/go vet ./internal/ui`、`./scripts/build`、`git diff --check` 通过。

构建后通过 Cmd+Q 正常退出旧版，重新打开应用，新 PID 为 31102，原会话与窗口恢复。切换到既有 Windows 网络问题会话，原生截图确认“免驱 USB 网卡”“离线拷驱动”“网络重置”及正文中的加粗重点呈红色，普通文字和行内代码保留原样。验证直接使用已有历史，未发送新任务。

原生窗口验收覆盖浅色历史回复；深色、实时流式和表格的颜色来自内存渲染器验证，未单独进行原生验收。本轮未运行 race；变更只涉及主线程渲染配色。
