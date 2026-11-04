# 模型推理强度与速度选项

2026-11-04：模型菜单底部按所选模型渲染“推理强度”和“速度”选项。候选项来自 `model/list` 返回的 `supportedReasoningEfforts` 与 `serviceTiers`，未选择的默认高亮取 `defaultReasoningEffort`/`defaultServiceTier`；选择按会话持久化（`SelectedEffort`/`SelectedTier`），切换模型时重置，新会话不继承其他会话的选择。`turn/start` 通过 `effort` 与 `serviceTierForTurn`（`"default"` 为标准速度）透传，未选择时省略参数沿用原生配置；不改动 thread 持久档位。无目录能力项的模型（含 Claude 目录）不显示两行选项；已加载目录确认所选模型不再提供已存档位时，发送省略参数回退原生默认，目录未加载或缺少该模型条目时按原值透传。

参数依据：codex-cli 0.166.0 `app-server generate-ts` schema（`TurnStartParams.effort`、`serviceTierForTurn`，其中 `"default"` 注明为标准速度）与本机真实 `model/list`（Codex 模型 effort 为 low/medium/high/xhigh/max/ultra，档位 `priority` 显示名 Fast）。

通过：

- `TestSendOptionsPassEffortAndServiceTier`：协议夹具核验 `turn/start` 携带 `effort="high"` 与 `serviceTierForTurn="priority"`。
- `TestModelMenuEffortAndSpeedOptionsSendAndPersist`：选项行呈现与中文标签、选择持久化与重启恢复、切换模型重置、无目录项模型不显示、发送时携带 `TurnOptions`。
- `TestStaleTurnOptionsFallBackToModelCapabilities`：目录确认不支持已存档位/强度时发送回退省略参数，目录未加载或模型条目缺失时原值透传。
- `./scripts/go test ./...`、`./scripts/go vet ./...`。
- `./scripts/go test -race ./internal/codex/ -count=1`、`./scripts/go test -race ./internal/ui/ -count=1 -run 'Model|Effort|Speed|Option|Send'`。
- `./scripts/build`、`git diff --check` 通过。
- 内存渲染截图核对菜单外观：选中项为灰底圆角加深文字，未选项为次要色；与参考布局一致。

真实模型下 `effort`/`serviceTierForTurn` 的实际生效与档位名称待用户验收；`model/list` 中未使用的目录字段未做映射。Claude 会话不显示两组选项。
