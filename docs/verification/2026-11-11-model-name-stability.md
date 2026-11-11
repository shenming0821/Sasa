# 模型名称在启动与刷新时保持一致

## 行为

已选模型 `devin/swe-2` 的目录显示名称为 `SWE-2 · Devin`，工具栏缩写为 `SWE-2`。原实现重启后仅恢复模型 ID，在模型目录返回前显示 `swe-2`，随后变成 `SWE-2`。

本次保存已选模型的目录显示名称，重启、切换会话、后台刷新时优先显示当前目录名称或相同 Agent、Runtime、cwd、模型 ID 下保存的名称。首次没有名称缓存时显示选择或读取状态，不提前展示作为临时名称的模型 ID。已读取目录但没有该名称的未知模型仍保留原始选择。目录中的完整提供方名称继续显示在菜单中，协议提交仍使用原模型 ID。

## 自动化验证

- `TestModelPickerKeepsDisplayNameAcrossRestart`：Codex、Claude 分别通过真实临时数据库保存并恢复；在目录未加载与目录已加载时均显示 `SWE-2 高`，菜单显示 `SWE-2 · Devin`，模型 ID 保持 `devin/swe-2`。
- `TestModelPickerWaitsForUncachedName`：首次无名称缓存显示读取状态，目录取得后显示正式名称，不闪出小写 ID。
- `TestModelDisplayNameCacheIsScoped`：Agent、Runtime 和 cwd 之间名称缓存隔离。

`./scripts/go test ./internal/ui ./internal/storage -count=1 -timeout=120s` 与 `./scripts/go vet ./internal/ui ./internal/storage` 通过。

## 原生窗口验证

Preview `20261111T092128Z-72b5815fa9-cdf890` 已构建并启动，工具栏显示 `SWE-2 高`。只读核查 Preview 数据库，名称缓存已保存为 `SWE-2 · Devin`，对应 Codex、默认 Runtime、Fuyu 项目目录和 `devin/swe-2`。再次通过 `run-preview` 重启同一候选，原生窗口仍显示 `SWE-2 高`。目录返回前的逐帧显示由自动化回归验证，原生截图核验重启后的实际显示；本次未发送额外真实模型任务。

## 交付范围

修改保留工作区，沿用原有会话持久化 JSON，无 schema 迁移。未暂存、提交或安装正式版。
