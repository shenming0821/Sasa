# 保持 Mac 唤醒：pmset 系统配置验证

日期：2026-11-04。范围：按用户要求将三档唤醒策略从 Fuyu 私有断言改为直接读写 macOS `pmset` 系统电源配置，与其他软件（系统设置类）保持同一状态并可同步显示。

## 实现与行为

- 读取：`pmset -g custom` 解析 AC/电池 profile 的 `sleep` 分钟值，映射为最近档位；应用启动与每次打开设置页执行同步，显示值始终反映系统实际状态。
- 写入：`osascript -e 'do shell script "pmset -c sleep X && pmset -b sleep Y" with administrator privileges'`，后台等待系统密码授权框；目标与现状一致时不写、不弹窗。读取限时 5 秒，授权写入限时 2 分钟，写入后读取核对目标。
- 档位目标：始终 → AC=0/电池=0；仅使用电源适配器时 → AC=0、电池=正数基线；永不 → 两档还原为正数基线。基线为 0 或无有效值时，对需要睡眠的电源档使用 10 分钟。
- 基线：Fuyu 首次写入前记录当时系统值，存入 `Preferences.KeepAwakeBaseline` 持久化；重启后经 `RestoreBaseline` 恢复，只记录一次。
- 备注行说明写入系统设置、需要授权、退出后仍生效；授权取消或失败保留原选择并显示错误。
- 授权及回滚在后台运行，主线程保持可交互；等待时拦截重复电源选择，保留其他偏好修改；保存成功后确认基线已保存，失败及重试继续提供原始基线。

## 审核问题复现与修复验证

- 修复前 `TestModesWithZeroSleepBaseline` 在基线 AC/电池为 0/5、0/0、12/0 三组均失败：选择“永不”后仍禁止至少一种电源的睡眠。
- `TestModesWithZeroSleepBaseline` 覆盖上述基线的“始终→永不→仅接电→永不”往返，并核对保留已有正数计时。
- `TestAuthorizationDoesNotBlockStatusAndCloseCancels` 用受控授权等待核对状态读取不阻塞及关闭取消；`TestKeepAwakeAuthorizationKeepsUIResponsive` 通过内存渲染器点击真实设置入口，核对等待时绘制、重新打开设置、其他偏好保存、重复操作拦截与授权取消。
- `TestBaselineRemainsPendingUntilPersistenceConfirmed` 覆盖读取基线、回滚、重试及确认；`TestKeepAwakeSaveFailureRetryPersistsBaseline` 使用真实临时 SQLite，关闭存储复现保存失败，重新打开后重试并读取基线。
- 保存失败回滚使用修改前的实际 AC/电池值，保留原有 0 值；`TestRollbackRestoresExactZeroTimers` 覆盖该路径。
- 本轮 `./scripts/go test ./...`、`./scripts/go vet ./...`、power/UI 相关定向 `-race`、`./scripts/build`、严格签名核验及 `git diff --check` 全部通过。
- 旧版通过 Cmd+Q 正常退出；系统 `open` 首次启动的 PID 91590 主线程卡在 `__open`，窗口读取超时，采样保存在 `/tmp/fuyu-power-restart-sample.txt`。该新进程对 TERM 无响应，清理后直接启动已构建程序，PID 92249 原生主窗口及既有会话恢复。设置页实读显示“仅使用电源适配器时”，与本机 AC=0/电池=5 一致；未执行真实授权写入。系统启动阻塞未在本轮定位或修复。

## 已通过

- `./scripts/go build ./...`、`./scripts/go vet`（power/storage/ui）。
- `./scripts/go test ./internal/power ./internal/storage`：解析、档位映射（含不归三档组合与无电池台式机）、基线记录与单次消费、还原驱动、无变化跳过写入、写入/读取失败保留、并发与关闭、存储默认值与序列化。
- `./scripts/go test -race ./internal/power -count=1`。
- `./scripts/go test ./internal/ui -run 'TestGeneralKeepAwakeSettings|TestSettings' -count=1`：三档选择、持久化恢复、保存失败回滚与既有设置回归。
- `FUYU_LIVE_POWER=1 ./scripts/go test ./internal/power -run TestLiveRead -v -count=1`：本机真实 `pmset` 读取 AC=0、电池=5，映射为“仅使用电源适配器时”，与用户在别的软件中的设置一致，同步读取路径核验通过。

## 验证边界

真实授权写入（弹密码框改 `pmset`）、授权取消路径及跨重启持久性尚未在本机执行，由受控后端的写入目标与失败分支覆盖；原生窗口中的同步显示与授权弹窗交由用户验收。系统级 `SleepDisabled 1` 与 `displaysleep` 等其他键不在本功能范围内，未读取也未写入；本机当前 `SleepDisabled=1` 可能来自其他软件，会单独使整体睡眠失效，与本设置的 `sleep` 值语义不同。早期 IOKit 断言版本及验证见[保持唤醒验证](2026-10-30-keep-awake.md)。
