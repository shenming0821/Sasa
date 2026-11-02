# 聊天二级标题缩小

2026-11-02 按用户截图调整 `App.New` 的 Markdown 主题，将二级标题（`##`）字号从正文的 1.35 倍改为 1.2 倍。当前正文为 15 DIP，标题从 20.25 DIP 调为 18 DIP，缩小约 11%；字重、颜色、间距与其他标题级别沿用原设置。流式、完成态和历史恢复共用该主题。

验证通过：

- `./scripts/go test ./internal/ui -run 'Test(MessageStrongText|MessageFileLink|StreamList|UnorderedList)' -count=1`。
- `./scripts/go vet ./internal/ui`。
- `./scripts/build`。
- `git diff --check`。

旧版通过 Cmd+Q 正常退出，`pgrep` 确认进程退出。首次重新打开的 PID 43272 窗口读取连续超时，采样显示主线程停在系统 `open`，原因未定位，采样保留于 `/tmp/fuyu-heading-restart.sample.txt`。向本轮新启动的阻塞进程发送 SIGTERM 后直接运行已构建可执行文件，PID 44326 窗口恢复可用。

切换到已有 Windows 网络问题会话，原生截图确认“关于‘用手机共享’为什么可能失败”二级标题采用缩小后的尺寸，普通正文和红色强调正常。未发送新任务。本轮为单项字号配置调整，未新增测试、未重复全量测试或 race；原生深色与流式标题未单独验收。
