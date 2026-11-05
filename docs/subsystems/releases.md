# 双渠道与发布

## 渠道与入口

本机日常使用正式版，同时构建、运行 Preview。公共入口和原生 UI 对 Codex、Claude 同时生效。

| 项目 | 正式版 | Preview |
|---|---|---|
| 应用 | `/Applications/Fuyu.app`；迁移前保留既有 `bin/Fuyu.app` | `bin/Fuyu Preview.app` |
| Bundle ID 与签名标识 | `com.shenming.fuyu` | `com.shenming.fuyu.preview` |
| 默认数据目录 | `~/Library/Application Support/Fuyu` | `~/Library/Application Support/Fuyu Preview` |
| Codex 原生目录 | `CODEX_HOME`，未设置时 `~/.codex` | `<Preview 数据目录>/runtimes/codex` |
| Claude 原生目录 | `CLAUDE_CONFIG_DIR`，未设置时 `~/.claude` | `<Preview 数据目录>/runtimes/claude` |
| 视觉识别 | 原图标与 Fuyu 标题 | 紫色 P 图标标记，标题含 Preview 与构建 ID |

`cmd/desktop/main.go` 在读写数据库前通过 `internal/release` 读取包内渠道信息，校验与链接到程序中的构建 ID、源码版本和源码摘要一致。直接 `go run` 的开发程序默认使用 Preview。`-version` 以 JSON 输出渠道、构建 ID、源码版本、源码摘要、应用版本和支持的数据版本，随后退出，不打开数据库。

应用数据目录带 `release-channel` 标记，同一目录仍由原有 `fuyu.lock` 排他持有。Preview 拒绝正式版默认目录及其子目录、正式版标记和未标记的既有数据库。正式版可接续未标记的旧正式版数据，拒绝 Preview 标记。

Preview 首次启动从现有环境配置根复制 Codex 的 `config.toml`、`auth.json` 和 Claude 的 `settings.json`、`.credentials.json`；已有 Preview 原生目录不再覆盖。仅复制存在的文件，目标目录 0700、凭据副本 0600，不复制原生会话、历史数据库、项目历史或应用草稿。缺少文件凭据、钥匙串登录不适用或登录过期时，需要在 Preview 的配置根完成原生 CLI 登录。配置中的外部 MCP、绝对路径和模型服务仍指向原服务；配置文件副本不能视为外部资源隔离。

项目文件、macOS 钥匙串、系统电源设置仍属于同一台机器。会修改项目文件的验收使用单独 worktree。当前未提供正式版数据导入 Preview 的入口。

## 构建与验收

```sh
./scripts/build preview
./scripts/run-preview <构建ID>
"bin/Fuyu Preview.app/Contents/MacOS/Fuyu" -version
```

`scripts/build` 默认等同于 `preview`，不接受直接构建正式版。构建先写临时目录，再签名、核对构建前后源码摘要，完成后保存到 `bin/candidates/<构建ID>/`。摘要包含 Git 已跟踪、未提交、删除和未忽略的未跟踪文件；源码在构建期间变化时拒绝生成候选。

`candidate.json` 保存身份、每个产物文件的 SHA-256 与权限，以及去除签名后的程序摘要。候选是验收与发布的固定来源，启动和晋升前都核对文件清单与签名；不在候选内写运行数据。再次开发产生新候选，不覆盖旧候选。

`run-preview` 接受明确构建 ID；省略时选择最近候选。复制到启动位置后登记应用并直接执行包内程序，绕开本机曾出现的 `open` 启动阻塞。替换前正常请求退出已有 Preview；等待超过默认 30 秒则保留用户提示并报错，处理后重试。

## 晋升正式版

```sh
./scripts/promote <构建ID> --check
./scripts/promote <构建ID>
```

预检输出候选身份、安装位置和数据目录，不退出或替换应用。发布接受明确构建 ID，代表调用者已完成该候选的验收。可用 `--app /absolute/Fuyu.app --data-dir /absolute/data` 指定隔离环境，`--timeout` 设置退出和启动等待秒数，默认 30。

晋升复制该候选，切换渠道信息、应用名、图标和 Bundle ID，再重新签名；不重新编译 Go、不读取新工作区功能代码。macOS 签名会改变可执行文件中的签名区域，因此去除签名后比较程序摘要，确认功能程序与候选相同。

在正式版安装目录旁的 `.Fuyu-releases/` 保存每次事务。流程为：

1. 验证候选，准备并签名完整正式版应用包。
2. 根据实际可执行文件路径查找进程，连接 `~/Library/Caches/Fuyu/control/<PID>.sock` 请求退出；通道校验目标程序路径，将退出派发到 UI 主线程，沿用原有任务、未保存内容与保存失败门控，正常退出时清理 socket。旧程序没有通道时尝试 `NSRunningApplication.terminate`；本机原生验收证实旧版可能不响应该请求，此时需用户在旧版按 ⌘Q。默认数据目录的首次晋升也检查旧 `bin/Fuyu.app`；用户取消退出或保留任务提示时停止发布，不强制结束进程。
3. 取得正式版数据锁，备份应用数据与上一版应用。SQLite 通过 backup API 创建一致性备份并执行 `quick_check`，草稿、附件和其他应用数据随备份保存。保留数据锁文件的 inode，恢复期间继续排他。
4. 在同一文件系统中移动完整应用目录，启动新版并指定随机 token 的确认文件。
5. 主窗口创建、应用索引恢复后写入确认。发布工具核对 token、PID、渠道、构建 ID、数据目录与存活进程后确认成功。Agent 后台连接与真实模型请求另行验收。

正常发布沿用正式版自己的数据，不合并 Preview 会话或设置。首次从旧 `bin/Fuyu.app` 迁移到 `/Applications/Fuyu.app` 时，旧应用复制为上一版恢复来源。默认位置发布成功后，将旧 `bin/Fuyu.app` 移到 `bin/.stable-backups/<事务ID>/Fuyu.app`，原路径改为指向已安装正式版的符号链接；原有 `bin/fuyu` 因而继续启动当前正式版，旧入口和 Dock 路径不会一直打开旧程序。此兼容入口同步仅随真实默认位置发布执行，普通构建与隔离验收保持原入口。

启动失败时先请求失败新版正常退出，再取得数据锁，保留失败应用，恢复上一版应用和本次发布前的数据备份。若应用未退出或恢复失败，停止恢复并报告事务目录，保留所有备份供处理。异常断电或进程强制终止时，依据该目录的 `receipt.json`、`previous.app`、`data-before/` 接续人工恢复；当前不提供跨进程崩溃后的自动事务重放。

## 回退

```sh
./scripts/rollback
./scripts/rollback --restore-data
```

默认回退到上一次成功发布前的应用，数据库版本一致时保留升级后的数据。若数据格式变化，要求明确 `--restore-data`，恢复发布前的应用数据；恢复前另存回退时的数据快照，供提取升级后的新增内容。回退核对安装位置、数据目录和当前应用文件清单，避免套用其他位置的恢复记录。外部 Agent 历史和项目文件不属于应用数据备份，应用回退不撤销这些外部效果。

## 验证入口

```sh
python3 -B -m unittest discover -s scripts -p 'test_release.py'
./scripts/go test ./internal/release ./internal/ui
./scripts/go test -race ./internal/release ./internal/ui
./scripts/go vet ./...
```

受控发布测试使用临时 `.app`、临时真实 SQLite 和替代进程控制器，覆盖候选篡改、数据锁、退出取消、健康确认失败恢复和数据版本回退，不等同于 macOS 原生窗口验收。本次实际结果见[双渠道验证](../verification/2026-11-05-preview-release.md)。
