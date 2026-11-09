# 应用启动域与 Codex 网络阻断

日期：2026-11-09，Asia/Shanghai。

## 正式版现场

正式版 PID 73482、自有 Codex PID 73600 保持运行。会话 195 的图文消息和 hi 均已写入原生 rollout，但没有助手输出。内核多次记录 codex:73600 连接网关 192.168.76.140:3425 被 NECP 丢弃，error=65、SYN in/out=0/0。

系统设置中 Fuyu 的本地网络权限为开启；用户授权关闭再开启正式版这一项后，Fuyu 内新建终端的无凭证 curl 返回 HTTP 401，连接耗时 0.007922 秒。同一 Codex 进程中的独立会话 196 仍被 NECP 拦截，因此不能用终端成功证明 Codex 已恢复。

`launchctl print pid/73482` 和 `pid/73600` 显示 resource/jetsam coalition 分别为 2075/2076，名称 application.com.exafunction.windsurf.274888093.274888099，状态 terminated；Preview 的 Codex PID 78083 也归属相同启动域。Codex 被拦截的时间点对应 UserEventAgent 报告已退出 PID 3187，以及 nehelper 对 Devin（com.exafunction.windsurf）的权限检查。该归属异常有实时证据；尚未通过正式版重启前后对照证明它是唯一根因。

原始记录保存在本机 `~/Library/Logs/Fuyu/network/20261109-143420-1wsu70vc/`。故障会话原生 thread 为 01a1be41-3b18-7362-b21c-3d5fdfbf8414。正式版与 Preview 主程序 UUID 相同，此轮未改变其签名或可执行文件。

## 启动修正

`scripts/release.py` 的启动、回退重开改用 Launch Services；启动确认从核对 Python 子进程 PID 改为核对 ready 中的应用 PID 及实际程序路径，保留 token、构建、渠道、数据目录和窗口初始化后的存活检查。

`python3 -m unittest discover -s scripts -p 'test_release.py'`：15 项通过，覆盖系统启动参数与 Runtime 路径配置、正确 PID、错误 PID、窗口初始化后退出及既有晋升/回退测试。`git diff --check` 通过。

## Preview 原生与真实 Runtime 验证

`./scripts/build preview`、`./scripts/run-preview 20261109T064827Z-68520ec265-fce58a` 完成，主窗口可用；Preview PID 46950、Codex PID 47105。两者 resource/jetsam coalition 为 4192/4193，状态 active，bundle ID 为 com.shenming.fuyu.preview，已脱离旧 Devin 启动域。

Preview 会话 20，thread 01a1bf11-fa47-7451-a17a-b1c400a4f279，输入 `Reply exactly OK. Do not use tools or modify files.`，14:49:48 发送后窗口显示真实回复 OK、用时 6 秒，持久化 Active=false；Codex PID 47105 与 192.168.76.140:3425 存在 ESTABLISHED 连接。正式版同一消息的验证会话 196 仍未完成。

正式版进程及故障会话继续保留。正式版恢复需要用户授权正常退出后通过系统启动同一正式版；此轮尚未执行退出、重启、晋升、提交或推送。启动域修正与 Preview 的成功对照支持本次定位，跨发布长期复发情况尚未验证。
