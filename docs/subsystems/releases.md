# 双渠道与发布

## 渠道与入口

本机日常使用正式版，同时构建、运行 Preview。公共入口和原生 UI 对 Codex、Claude 同时生效。

| 项目 | 正式版 | Preview |
|---|---|---|
| 应用 | `/Applications/Sasa.app`；入口 `bin/Sasa.app`、`bin/sasa` | `bin/Sasa Preview.app` |
| Bundle ID 与签名标识 | `com.shenming.sasa` | `com.shenming.sasa.preview` |
| 默认数据目录 | `~/Library/Application Support/Sasa` | `~/Library/Application Support/Sasa Preview` |
| Codex 原生目录 | `CODEX_HOME`，未设置时 `~/.codex` | `<Preview 数据目录>/runtimes/codex` |
| Claude 原生目录 | `CLAUDE_CONFIG_DIR`，未设置时 `~/.claude` | `<Preview 数据目录>/runtimes/claude` |
| 视觉识别 | 红橙 S/a 图标与 Sasa 标题 | 紫色 P 图标标记，标题含 Preview 与构建 ID |

应用显示名称与主程序为 Sasa / Sasa Preview；Bundle ID、数据目录及发布备份目录继续沿用原有名称。首次默认安装时，旧 `/Applications/Sasa.app` 移入本次恢复目录并移除旧名称入口，不请求正在运行的正式版退出。旧构建候选保留原文件，需重新构建 Sasa 候选后验收晋升。

`cmd/desktop/main.go` 在读写数据库前通过 `internal/release` 读取包内渠道信息，校验与链接到程序中的构建 ID、源码版本和源码摘要一致。直接 `go run` 的开发程序默认使用 Preview。`-version` 以 JSON 输出渠道、构建 ID、源码版本、源码摘要、应用版本和支持的数据版本，随后退出，不打开数据库。

应用数据目录带 `release-channel` 标记，同一目录仍由原有 `sasa.lock` 排他持有。Preview 拒绝正式版默认目录及其子目录、正式版标记和未标记的既有数据库。正式版可接续未标记的旧正式版数据，拒绝 Preview 标记。

Preview 首次启动从现有环境配置根复制 Codex 的 `config.toml`、`auth.json` 和 Claude 的 `settings.json`、`.credentials.json`；已有 Preview 原生目录不再覆盖。仅复制存在的文件，目标目录 0700、凭据副本 0600，不复制原生会话、历史数据库、项目历史或应用草稿。缺少文件凭据、钥匙串登录不适用或登录过期时，需要在 Preview 的配置根完成原生 CLI 登录。配置中的外部 MCP、绝对路径和模型服务仍指向原服务；配置文件副本不能视为外部资源隔离。

项目文件、macOS 钥匙串、系统电源设置仍属于同一台机器。会修改项目文件的验收使用单独 worktree。当前未提供正式版数据导入 Preview 的入口。

## 构建与验收

```sh
./scripts/build preview
./scripts/run-preview <构建ID>
"bin/Sasa Preview.app/Contents/MacOS/Sasa" -version
```

`scripts/build` 默认等同于 `preview`，不接受直接构建正式版。构建先写临时目录，再签名、核对构建前后源码摘要，完成后保存到 `bin/candidates/<构建ID>/`。摘要包含 Git 已跟踪、未提交、删除和未忽略的未跟踪文件；源码在构建期间变化时拒绝生成候选。

`candidate.json` 保存身份、每个产物文件的 SHA-256 与权限，以及去除签名、归一化主程序 UUID 后的程序摘要。主程序 UUID 按 Bundle ID 和构建 ID 确定，同一候选的 Preview 与正式版使用不同 UUID，重复配置同一渠道保持一致。构建成功后保留当前公共 Preview 和本次待启动候选。新版通过窗口启动确认后，日常验收只保留 `bin/Sasa Preview.app` 一份实体；对应构建目录保存校验清单和指向公共 Preview 的链接，旧候选自动清理。按构建 ID 重启或晋升时仍校验文件清单、签名和包内身份。正式版回退使用安装时的独立备份。

Preview 启动和回退重开通过 macOS Launch Services（`/usr/bin/open -n -a`）启动应用，使应用建立自己的启动域，避免直接创建进程时继承调用方已退出的 coalition。启动继续传递数据目录、一次性 token 和 ready 文件；以 ready 中的 PID 对照实际可执行路径，并核对构建、渠道和数据目录，再确认窗口初始化后进程仍存活。启动日志由 `open --stdout/--stderr` 保存；保留调用环境中的 PATH、CODEX_HOME、CLAUDE_CONFIG_DIR 路径配置。正式版晋升不启动应用，只交换安装目录，见下文。

窗口中的“重启 Sasa”同样通过 Launch Services 启动，沿用正常退出时的活动任务、文件保存与 Git 写入门控。取消退出或保存失败时清除重启请求；确认退出后完成窗口资源与自有 Agent 清理，再启动等待旧 PID 退出的短期辅助进程。辅助进程最多等待 30 秒，随后通过参数数组执行 `open -n -a`，保留当前工作区、数据目录和渠道运行时环境，避免新实例抢占尚未释放的数据锁。此操作独立于发布，晋升仍保留运行中的正式版。

`run-preview` 接受明确构建 ID；省略时选择最近候选。复制到启动位置后登记应用，并通过 Launch Services 启动。替换前正常请求退出已有 Preview；因未保存内容或活动任务提示未退出时，强制结束目标 Preview 进程后重试。正式版遇到同类提示时保留提示并告知用户。

并行开发的公共构建、Preview 启动和晋升入口限在主目录执行。任务 worktree 完成集成登记后，保留至默认正式版安装成功；`scripts/promote` 随后根据发布回执和提交关系自动回收符合条件的任务目录。预检和隔离晋升不回收，失败保留目录。具体登记、核验与重试见[并行开发](../parallel-development.md)。

## 晋升正式版

```sh
./scripts/promote <构建ID> --check
./scripts/promote <构建ID>
```

预检输出候选身份、安装位置和数据目录，不替换应用。发布接受明确构建 ID，代表调用者已完成该候选的验收。可用 `--app /absolute/Sasa.app --data-dir /absolute/data` 指定隔离环境。

晋升复制该候选，切换渠道信息、应用名、图标、Bundle ID 和主程序 UUID，再重新签名；不重新编译 Go、不读取新工作区功能代码。比较程序摘要时移除签名并将 UUID 的 16 字节归零，确认其余功能程序与候选相同。候选与安装文件清单仍校验完整字节，UUID 归一化仅用于跨渠道功能程序比较。

在正式版安装目录旁的 `.Sasa-releases/` 保存每次事务。晋升固定采用延迟生效流程，不请求运行中的应用退出：

1. 验证候选，准备并签名完整正式版应用包。
2. 备份应用数据与上一版应用。SQLite 通过 backup API 创建一致性备份并执行 `quick_check`，草稿、附件和其他应用数据随备份保存；不取得数据锁，运行中的正式版可继续持有。
3. 在同一文件系统中移动完整应用目录。正在运行的实例持有旧应用 inode 不受影响，下次启动自然进入新版；首次从旧 `bin/Sasa.app` 迁移时旧应用复制为上一版恢复来源。
4. 回执记录 `installed` 与安装文件清单，`latest.json` 指向本次事务。登记应用后，默认位置发布将旧 `bin/Sasa.app` 实体移到 `bin/.stable-backups/<事务ID>/Sasa.app`，移除 `bin/Sasa.app`、`bin/sasa` 和 `/Applications/Sasa.app` 的旧符号链接，仅保留 `bin/Sasa.app` 与 `bin/sasa` 启动入口。入口整理仅随真实默认位置发布执行，普通构建与隔离验收保持原入口。

正常发布沿用正式版自己的数据，不合并 Preview 会话或设置。晋升不验证新版可启动（同一程序已在 Preview 验收中运行过），安装后如需撤回经 `./scripts/rollback` 回退；目录交换失败时自动还原上一版应用。异常断电或进程强制终止时，依据事务目录的 `receipt.json`、`previous.app`、`data-before/` 接续人工恢复；当前不提供跨进程崩溃后的自动事务重放。

## 回退

```sh
./scripts/rollback
./scripts/rollback --restore-data
```

默认回退到上一次成功发布前的应用，数据库版本一致时保留升级后的数据。若数据格式变化，要求明确 `--restore-data`，恢复发布前的应用数据；恢复前另存回退时的数据快照，供提取升级后的新增内容。回退核对安装位置、数据目录和当前应用文件清单，避免套用其他位置的恢复记录。外部 Agent 历史和项目文件不属于应用数据备份，应用回退不撤销这些外部效果。

## 验证入口

备份与回退核验数据库时，在应用数据锁内以 `mode=rw` 打开已存在的数据库，并设置 `PRAGMA query_only=ON` 限制 SQL 写入；允许 SQLite 为已退出应用的 WAL 数据库重建所需辅助文件。备份源、目标及版本检查连接在操作结束时明确关闭，避免连接跨恢复步骤持有旧文件。

```sh
python3 -B -m unittest discover -s scripts -p 'test_release.py'
./scripts/go test ./internal/release ./internal/ui
./scripts/go test -race ./internal/release ./internal/ui
./scripts/go vet ./...
```

受控发布测试使用临时 `.app`、临时真实 SQLite 和替代进程控制器，覆盖候选篡改、数据锁、退出取消、健康确认失败恢复和数据版本回退，不等同于 macOS 原生窗口验收。
