# Codex 桌面启动路径验收

日期：2026-10-23。

## 问题与修复

用户从桌面打开 Fuyu 时报告 `exec: "codex": executable file not found in $PATH`。原实现默认通过 `exec.Command("codex", ...)` 查找，依赖启动环境继承的 PATH。本机终端安装入口为 `/Users/shenming/.bun/bin/codex`，实际为 `#!/usr/bin/env node` 包装脚本；终端当前 Node.js 位于 `/Users/shenming/.local/share/nvm/v24.18.0/bin/node`。

新增 `internal/runtime/codex.go`，按当前用户目录与环境定位，不将上述本机绝对路径写入实现。恢复交互式登录 shell PATH，并补充常见安装目录与 nvm 版本目录。桌面入口统一设置 Fuyu 进程 PATH，使用绝对入口执行版本探测与 App Server，子进程继承相同路径。保留所有其他环境变量和 Codex 原生配置。

显式路径优先且失效时直接报错；自动搜索不查找项目当前目录。Node.js 包装脚本在启动前检查 node 是否可用。shell 环境探测限时 2 秒、输出上限 64 KiB；失败后使用常见目录。版本探测限时 5 秒，真实初始化单独允许 60 秒。

## 自动化检查

- `TestCodexDiscoveryAndOverrides`：继承环境、登录环境、常见目录的优先级；绝对路径覆盖、无效覆盖和未安装提示；用户目录含空格。
- `TestFinderPathStartsNodeWrapper`：继承 PATH 仅含系统目录，由模拟用户安装和登录路径找到 Codex 与 Node.js，并真实执行受控包装脚本。
- `TestDiscoveryRejectsNonExecutableAndRelativeSearch`：拒绝非可执行文件；去重并排除空/相对 PATH 项。
- `TestNodeRequirementAndVersionOrder`：缺少 Node.js 的明确信息；nvm 数字版本排序。
- `TestLoginShellPathIsBoundedAndParsesOnlyMarkedOutput`：受控 shell 的启动噪声、缺失标记、非零退出、超时与输出上限。

完整 `./scripts/go test ./...`、`./scripts/go vet ./...`、`./scripts/go test -race ./...` 和 `./scripts/build` 通过。普通测试使用临时安装目录和受控脚本；真实安装验证单独显式开启。

## 真实 Codex 验证

执行 `FUYU_LIVE=1 ./scripts/go test ./internal/codex -run '^TestLiveDesktopRuntimeDiscovery$' -v -count=1`。测试将 PATH 设置为 `/usr/bin:/bin:/usr/sbin:/sbin`，再调用与桌面相同的发现逻辑和启动接口。

最终结果：定位 `/Users/shenming/.bun/bin/codex`，读取 `codex-cli 0.163.0`，Node.js 定位 `/Users/shenming/.local/share/nvm/v24.18.0/bin/node`；完成真实 `initialize` / `initialized` 并确认自有 App Server 退出。没有创建 thread 或 turn，也没有发起模型请求。

首次使用非交互登录 shell 与 20 秒验证截止时间时，版本探测成功，但真实初始化超时；修改为交互式登录 shell，并使用独立 60 秒启动截止时间后复核通过。真实启动耗时有波动，两次通过分别约 2.4 秒与 31.6 秒，因此桌面初始化不复用普通 RPC 的 30 秒上下文。

## 验收边界

本机精简 PATH 的真实初始化已验证；其他用户目录与安装优先级通过隔离夹具覆盖，没有在其他实体电脑上执行。当前支持范围仍为项目已有的 macOS 优先环境。使用者需要已有可运行的 Codex；自定义安装可通过登录 shell PATH 或 `-codex` 指定，未安装时显示提示。

尝试通过原生工具打开更新后的 Fuyu 时，工具返回未获准访问 Fuyu。最终 `.app` 已构建，但 Finder / Dock 直接打开后的 GUI 状态尚未核验。
