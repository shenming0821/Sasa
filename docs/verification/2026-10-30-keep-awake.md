# 保持 Mac 唤醒验证

日期：2026-10-30。范围：设置新增通用分类、三档唤醒策略、持久化与应用生命周期。

## 实现与默认值

- 入口：设置 → 通用 → 让这台 Mac 保持唤醒状态。
- 选项：永不 `never`（默认）、始终 `always`、仅使用电源适配器时 `ac-only`。
- 生效范围：Fuyu 运行期间；仅接电模式每 5 秒重新核验实际供电来源。
- 原生后端：IOKit `PreventUserIdleSystemSleep`；允许屏幕熄灭，合盖与手动睡眠遵循系统规则。

## 已通过

- `./scripts/go test ./...`。
- `./scripts/go vet ./...`。
- `./scripts/go test -race ./internal/power ./internal/ui -run 'Test(Modes|ACRefresh|ConcurrentUpdates|GeneralKeep|Settings)' -count=1`。
- `./scripts/build`。
- `git diff --check`。
- `FUYU_LIVE_POWER=1 ./scripts/go test ./internal/power -run TestLiveMacAssertion -v -count=1`：本机实际供电为电源适配器（AC connected=true），通过 `pmset -g assertions` 确认始终和仅接电模式创建 Fuyu 电源断言，永不及 Controller 关闭后释放。
- 组件测试：浅色/深色、640×480/1000×760 下三项下拉可见可选、当前选择勾选和关闭浮层、SQLite 恢复、保存失败恢复原值，既有字体及通知设置回归。
- 受控后端：电源适配器→电池→电源适配器、重复设置无重复断言、未知供电、断言创建/释放失败、恢复旧策略、后续重试、并发设置与退出。

## 验证边界

真实插拔电源与实际等待系统睡眠尚未执行；插拔分支由受控后端验证。真实测试核对当前接电状态下的系统断言，不等价于已验证合盖运行。未发送模型任务。

组件渲染图 `/tmp/fuyu-general-settings.png` 已检查：通用分类、标题、说明及下拉框显示正常。图片来自内存渲染器，独立于原生窗口验收。

首版正常退出旧实例 PID 43986 并启动构建 PID 65185；当时系统 `CGSSessionScreenIsLocked=1`，窗口 bounds 为 0×0，System Events 无法取得可交互窗口，因此未记为原生窗口验收通过。

## 单行布局调整

按最新要求，标题与宽 190 DIP 的下拉框同行、垂直居中，移除上下两段灰色备注，异常时继续显示具体错误。

重新通过设置回归（含浅深色及两种窗口尺寸）、UI vet、构建及差异检查。组件渲染图 `/tmp/fuyu-general-settings-inline.png` 已检查：标题在左、下拉在右，两段备注已移除。

正常退出 PID 65185 并启动最新构建 PID 82806。当前系统已解锁，CoreGraphics 确认 Fuyu 主窗口 1000×760、`kCGWindowIsOnscreen=1`，System Events 可读取窗口名。首版窗口可见性阻塞已解除；原生下拉点击与实际观感交由用户复核。
