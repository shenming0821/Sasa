# Fuyu 局域网请求阻断核验

日期：2026-11-02；时间均为北京时间。环境：macOS 27.0.1（26A434）、arm64；Codex CLI 0.165.0、Claude Code 2.1.307。

## 故障与证据

用户在 Fuyu 中分别向 Codex、Claude 发送 hi，数分钟没有回应；直接运行 CLI 平时没有遇到同类问题。

Fuyu PID 58457 的 Codex 子进程 PID 58548 在 18:36:09.925674 至 18:38:41.457210 之间，对网关端口 3425 发生 25 次 `tcp drop outgoing`，内核原因为 `NECP`，连接错误为 65，`SYN in/out: 0/0`，发送字节和包数均为 0。这段等待发生在本机网络策略层，请求尚未抵达网关。同时其他 Codex 进程成功连接该端口；这些进程不能全部认定为用户直接终端 CLI。

Codex 故障轮次 thread `01a19bd5-6fdf-7793-937a-a3089ceae090`、turn `01a19bd5-700b-76a1-87a2-ce0f2f47aaba`：18:36:58.805 开始，18:37:01.084 原生记录 HI；网关于 18:40:29.887781303 收到请求，路由记录 `1793582728347`、HTTP 200、处理 7485 ms、首输出 6805 ms；18:40:37.420 原生完成。约 219 秒总耗时主要发生在网关收到请求之前。

Claude 两条故障会话 `e5c2e420-d987-4745-af83-516922d12f01`、`5c1af64c-28b7-465a-8a71-d964e6c10aea` 的原生日志记录 `EHOSTUNREACH`，最多重试 10 次；网关分别于 18:39:23.176757875、18:39:51.773231828 才收到成功请求，处理耗时 1834 ms、1732 ms。初次长等待不能归因于模型调用工具。

`nehelper` 同期记录 `Local network allowed by preference for Fuyu (com.shenming.fuyu)`，因此没有证据说明用户关闭了 Fuyu 的局域网权限。18:39:19.022 系统重新缓存 `a.out` UUID，之后连接恢复；这是时间关联，尚未证明缓存刷新是恢复原因。系统启用 Tailscale 与 AntifyCore 网络扩展，未取得将本次拦截归因于其中某一个的证据。

## 当前配置与平台差异

- Codex 网关 `http://192.168.76.140:3425/v1`，`request_max_retries=100`、`stream_max_retries=100`、`supports_websockets=false`，模型 `codex/gpt-6.1-sol`。
- Claude 网关 `http://192.168.76.140:3425`，opus 映射 `winning/deepseek-v4-1-flash-261017`；故障日志 `maxRetries=10`。
- Fuyu 与其 Codex 子进程未设置 HTTP_PROXY、HTTPS_PROXY、ALL_PROXY、NO_PROXY 及对应小写变量；系统 HTTP、HTTPS、SOCKS、PAC 均为 0。未据此推断所有用户终端会话环境完全一致。
- 原 bundle ID 为 `com.shenming.fuyu`，实际代码签名标识却为 `a.out`；Go 链接器 ad hoc 签名未绑定 Info.plist，也未封装资源签名。
- 本机 `security find-identity -v -p codesigning` 返回 0 个有效身份。此次构建实际使用 `FUYU_CODESIGN_IDENTITY=-`，TeamIdentifier 未设置。

Apple TN3179 说明：从 Terminal/SSH 运行的 CLI 及其子进程自动获准局域网访问；GUI helper 按负责的 App 判断。macOS 通过签名追踪身份，Apple 建议使用其签发的证书可靠追踪，ad hoc 身份仍存在限制。文档还要求主程序 UUID 存在且唯一。当前主程序 UUID 为 `CEC7ED67-8288-34FA-808F-DF5B37C28FDF`，此次签名未修改 UUID；未完成跨所有历史测试产物的 UUID 唯一性审计。

官方依据：https://developer.apple.com/documentation/technotes/tn3179-understanding-local-network-privacy 。这些平台规则解释了直接 CLI 与 Fuyu 结果可能不同，但不能单凭规则认定具体是哪项 NECP 策略拦截。

## 修改

`scripts/build` 在生成全部资源与 Info.plist 后签名 libghostty，再显式以 `com.shenming.fuyu` 签名整个 App，并执行 `codesign --verify --deep --strict`。增加 `NSLocalNetworkUsageDescription`，实际值为“Fuyu 需要连接你配置的局域网模型服务，以发送任务并接收回复。”支持通过 `FUYU_CODESIGN_IDENTITY` 指定已有签名身份，默认值为 `-`。

本次修正构建产物身份不一致及缺失的完整包签名；没有改动系统网络权限、代理、VPN、模型重试设置或登录配置。没有绕过 macOS 权限检查。

## 验证

| 检查 | 实际结果 |
|---|---|
| `sh -n scripts/build`、`./scripts/build` | 通过 |
| 严格验证主包与内嵌库签名 | 通过；Identifier=`com.shenming.fuyu`，Info.plist 已绑定，资源封装 477 个文件 |
| 修复前仍运行的旧实例：Fuyu 新建原生终端执行无认证 curl | 18:59:18，TCP 4.745 ms，总计 9.544 ms，HTTP 401；证明旧实例在修改前已自行恢复连通 |
| 正常退出、重新启动 | 旧 PID 58457/58548 已退出；新 Fuyu PID 70300、Codex PID 70340；窗口及原生终端可用 |
| 新实例无认证 curl | 19:02:40，TCP 5.352 ms，总计 10.892 ms，HTTP 401；401 为网关认证拒绝，仅证明连接成功 |
| Fuyu 内新建 Codex 会话 126，输入 `hi hi` | thread `01a19bed-73f8-7e00-865a-5855518c8041`；19:03:12.747 原生开始，19:03:15.707990903 网关收到，路由 `1793582728421`；网关处理 3807 ms、首输出 3396 ms，19:03:19.568 完成，窗口显示回复与用时 6 秒 |
| Fuyu 内新建 Claude 会话 127，输入 `hi. Reply hi only. Do not use tools.` | session `dc56d133-60b3-49b4-b50d-30c7efc8e30a`；19:03:32.489 原生用户消息，19:03:32.538704850 网关收到，路由 `1793582728423`；HTTP 200、处理 1120 ms，19:03:33.687 原生回复 hi，窗口显示用时 1 秒 |
| 新实例内核日志 19:00:52–19:04:00 | Codex、Claude 连接成功；该时间窗目标端口未检出 NECP 拦截 |

证据来源：macOS unified log（kernel、nehelper）、原生 Codex rollout 与 Claude transcript、Fuyu SQLite 会话归属、网关容器 `magpie-magpie-1` 的 `routing/2026-11-02.jsonl`、真实 Fuyu 窗口及签名工具输出。模型验证为真实服务调用；curl 未发送认证信息。

## 限制与接续

当前双 Agent 已正常回复，构建身份缺陷已修正。故障在修复前已自行恢复，不能用本次成功验证证明签名缺陷就是唯一根因，也不能宣称间歇性 NECP 拦截已根治。Codex 此次从原生开始到网关收到仍约 2.96 秒，属于尚未进一步拆分的 Runtime 准备时间，不应计作 TCP 连接耗时。

如果再次出现长等待，按新实例子进程 PID 对齐同一轮的内核 NECP、nehelper 与网关到达时间。需要验证跨构建身份可靠性时，使用用户已有或新取得的 Apple 签发证书，再进行实际重建/重启对照；本机目前没有可用证书。保留此次两个独立验收会话供用户查看。

## 间歇性复发取证

在出现长等待时先保留 Fuyu 与故障会话，在重启或切换网络前执行 `python3 scripts/diagnose-network.py`，并记录消息发送时间。脚本默认回溯 15 分钟、目标端口 3425；保存内核连接与 NECP 日志、nehelper 策略记录、Fuyu 进程树和应用签名、系统版本、代理与路由状态。产物仅保存在本机 `~/Library/Logs/Fuyu/network/`，目录权限 700、文件权限 600；不读取凭据、聊天正文或进程环境，不启动测试连接、不改变应用与系统状态。日志仍可能包含本机路径和网络地址，按本地诊断资料保存。

2026-11-02 19:10:09–19:10:12 实跑完成，识别 Fuyu PID 70300 及共 8 个关联进程，8 项采集命令退出码均为 0，文件权限 600。受控不存在命令检查确认错误写入对应文件并保留错误元数据。`git diff --check` 通过。该验证证明取证入口可用，未复现间歇性阻断，也未证明所有历史内核日志均会被系统保留。

拿到复发记录后，优先核对同一 Fuyu 子进程是否再次出现 NECP；再按故障会话对齐 Runtime 与网关时间。若取证后需要临时恢复，使用正常退出/重启；有活动任务时保留退出提示，先明确停止状态，避免重复发送造成任务重复执行。
