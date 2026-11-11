# Codex 模型目录空响应与网络超时

## 正式版现场

2026-11-11 21:15 左右，正式版的 Codex 模型菜单仅保留当前选择，并显示“当前运行时未返回可用模型”。Claude 和 Devin 未报告同样故障。正式版 PID 30274，自有 Codex PID 30375，CLI 版本为 0.166.0。

相同可执行文件、参数和 HOME 工作目录的独立 CLI 读取取得 8 项。原生共享缓存最初由 0.167.0 写入；0.166.0 重新读取并写入缓存后，未重启的正式版也显示了 8 项。因此终端成功和当前菜单恢复不能证明 GUI 的冷读取链路已经稳定。

21:22 从正式版内新建的终端启动隔离 Codex：临时 CODEX_HOME 只复制原生配置和认证文件，不复制模型缓存，不创建任务；临时配置在探测结束后删除。原生 `model/list` 在约 10.01 秒后返回成功的空目录，同时 stderr 包含 `failed to refresh available models` 和超时信息。相同探测从外部终端运行返回 8 项，约 0.06 秒。21:28 左右再次从应用内先做无凭证 HTTP 可达性检查得到 401，随后隔离 Codex 读取 8 项、约 0.10 秒。401 在此只表示探测没有提供凭证且服务器可达，并非原生认证失败。

网关目录地址实际为 `http://192.168.76.140:3425/v1/codex/models`，本机 IPv4 为 `192.168.76.151`；系统 HTTP、HTTPS、SOCKS 和自动代理均为关闭。故障采集路径为 `~/Library/Logs/Fuyu/network/20261111-212350-bkao4cp4`。这次没有取得与 Codex 请求对应的内核 NECP 拒绝记录，不能沿用此前网络故障的根因结论。

正式版与 Preview 的主程序 UUID 相同，系统网络日志曾把请求归到 Preview。临时复制候选、更换主程序 UUID、改成 stable 渠道并重新签名后，隔离读取仍超时返回空目录；该实验不能证明 UUID 调整能修复问题，未纳入代码。临时应用已正常退出，原正式版保持运行。

## 原生契约与修改

对照官方 `openai/codex` 的 `rust-v0.166.0` 源码：`app-server/src/request_processors/catalog_processor.rs` 使用 `OnlineIfUncached`；`models-manager/src/manager.rs` 的 `model_catalog_with_origins` 记录刷新错误后继续返回当前目录，缓存加载要求客户端版本匹配。安装版本生成的 `ModelListParams` 只有 cursor、limit、includeHidden，没有强制在线刷新的参数。

Fuyu 针对空目录追加一次 250 ms 后的重读，沿用原请求取消和截止时间。持续为空时，从当前请求期间的原生模型管理器错误中返回固定分类，避免只报告空目录。原始 stderr 不持久化，不回显 URL 或凭据。已有 UI 继续保留最近成功列表与选择。

该修改改善短暂失败的恢复和错误可见性；GUI 冷读取的间歇网络超时仍未确定到具体系统组件，不能称为已经根治。

## 验证

自动化覆盖空目录后恢复、最多一次重试、取消等待、原生超时转为可见错误、分块 stderr、超大 stderr 有界和敏感原文不回显。`./scripts/go test ./internal/codex ./internal/ui -count=1 -timeout=150s`、`./scripts/go test -race ./internal/codex -count=1 -timeout=150s` 和 `./scripts/go vet ./internal/codex ./internal/ui` 通过。

真实只读 Runtime 测试 `FUYU_LIVE_MODELS=1 ./scripts/go test ./internal/codex -run '^TestLiveModelsReadOnly$' -count=1 -v` 返回 8 项，有效默认模型为 `codex/gpt-6.1-sol`。Preview 候选 `20261111T133121Z-32f325e53e-ecfd23` 已通过构建、启动回执和原生窗口验收：新 Codex 会话 43 的模型菜单显示完整 8 项，主动刷新后仍正常。没有发送模型任务；持续网络故障的原生错误 UI 由协议夹具和组件现有错误展示路径覆盖，没有在真实窗口强制制造网关故障。

上述排查与 Preview 验收时，修改保留在工作区，尚未暂存、提交或发布；正式版保持运行，菜单已恢复完整 8 项。该现场恢复先于代码修改，不能归因于当时尚未安装的修复。后续用户要求将模型刷新入口改为标题右侧的纯图标按钮，并授权完成后直接发布；交付身份以对应候选和正式版发布回执为准。
