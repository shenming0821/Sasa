# 模型推理强度与速度选项

2026-11-04：模型菜单底部按所选模型渲染“推理强度”和“速度”选项。候选项来自 `model/list` 返回的 `supportedReasoningEfforts` 与 `serviceTiers`，未选择的默认高亮取 `defaultReasoningEffort`/`defaultServiceTier`；选择按会话持久化（`SelectedEffort`/`SelectedTier`），切换模型时重置，新会话不继承其他会话的选择。`turn/start` 通过 `effort` 与 `serviceTierForTurn`（`"default"` 为标准速度）透传，未选择时省略参数沿用原生配置；不改动 thread 持久档位。模型按钮以 `模型·档位` 显示当前强度，取已选值、缺省回落目录默认档，两者皆无时省略后缀。无目录能力项的模型不显示两行选项；已加载目录确认所选模型不再提供已存档位时，发送省略参数回退原生默认，目录未加载或缺少该模型条目时按原值透传。

参数依据：codex-cli 0.166.0 `app-server generate-ts` schema（`TurnStartParams.effort`、`serviceTierForTurn`，其中 `"default"` 注明为标准速度）与本机真实 `model/list`（Codex 模型 effort 为 low/medium/high/xhigh/max/ultra，档位 `priority` 显示名 Fast）。

通过：

- `TestSendOptionsPassEffortAndServiceTier`：协议夹具核验 `turn/start` 携带 `effort="high"` 与 `serviceTierForTurn="priority"`。
- `TestLiveEffortReachesTurnContext`（`FUYU_LIVE=1`）：真实 Runtime 两轮 `turn/start`（effort=low/max）完成后，rollout `turn_context` 按序记录 `effort=[low max]`，确认逐轮实际生效；同日无显式档位的历史轮次记录 `medium`，未选择回落原生默认。
- `TestModelMenuEffortAndSpeedOptionsSendAndPersist`：选项行呈现与中文标签、选择持久化与重启恢复、切换模型重置、无目录项模型不显示、发送时携带 `TurnOptions`。
- `TestStaleTurnOptionsFallBackToModelCapabilities`：目录确认不支持已存档位/强度时发送回退省略参数，目录未加载或模型条目缺失时原值透传。
- `./scripts/go test ./...`、`./scripts/go vet ./...`。
- `./scripts/go test -race ./internal/codex/ -count=1`、`./scripts/go test -race ./internal/ui/ -count=1 -run 'Model|Effort|Speed|Option|Send'`。
- `./scripts/build`、`git diff --check` 通过。
- 内存渲染截图核对菜单外观：选中项为灰底圆角加深文字，未选项为次要色；与参考布局一致。

`serviceTierForTurn` 的实际生效与档位名称待用户验收（effort 已实测生效）；`model/list` 中未使用的目录字段未做映射。当时 Claude 会话不显示两组选项，同日已接入，见下节。

## Claude 推理强度

2026-11-04：Claude 会话复用同一模型菜单。initialize 响应的逐模型 `supportedEffortLevels` 映射为“推理强度”选项，未选择时不高亮、省略参数沿用 CLI 原生默认；无“速度”行。选择按会话持久化（`SelectedEffort`）、切换模型时重置；`SendWithOptions` 实现 `OptionSender`，以 `--effort` 启动参数透传并将档位并入空闲进程复用键，变化时先回收再重新 initialize/resume；`TurnOptions.ServiceTier` 对 Claude 忽略。

参数依据：本机 Claude Code 2.1.307 `claude --help`（`--effort <level>` 接受 low/medium/high/xhigh/max）与真实 initialize 录制 `mac-control-interrupt.jsonl`（逐模型 `supportsEffort`/`supportedEffortLevels`，未报告默认档位）。

真实 CLI 生效核验（协议夹具同源 spawn 参数 + transcript `api-request` 请求参数）：

- 隔离配置根（无 effort 配置）：`--effort low`/`--effort max` → `output_config.effort` 实际为 low/max，flag 生效。
- 仅 `effortLevel: high` 设置项 + `--effort low` → effort 实际为 low，flag 覆盖设置项。
- `~/.claude` 真实配置（`env.CLAUDE_CODE_EFFORT_LEVEL=high` 注入）：`--effort low`、`--effort max`、启动 env 均为 high，flag 不生效；非法档位被 CLI 警告并回退默认。
- 协议侧无替代通道：`set_effort`/`get_effort` 不支持，`set_model` 忽略 effort，user 帧 effort 字段无效。

因此增加锁定检测：`PinnedEffort` 读取配置根与项目 `.claude` 的 settings(.local).json `env.CLAUDE_CODE_EFFORT_LEVEL` 及启动环境变量；打开模型菜单时刷新，存在锁定时在“推理强度”行下显示“已被 CLAUDE_CODE_EFFORT_LEVEL 固定为 X，选择不会生效”。`effortLevel` 设置项不阻止 flag，不作锁定处理。本机 env 注入项已于本次会话删除（`effortLevel: high` 保留为默认档），此后逐会话选择真实生效。

通过：

- `TestSendWithOptionsPassesEffortAndKeysProcessReuse`：夹具进程记录 argv，核验带档位的二次发送生成新进程并携带 `--effort high`，同档位复用空闲进程。
- `TestPinnedEffortReadsConfigAndEnvironment`：配置根、项目 `.claude`、启动环境三类锁定源与覆盖顺序。
- `TestCatalogSharesProbeAndPublishesModelsBeforeSkills`：目录模型解析五档 effort。
- `TestRecordedControlResponsesMatchAdapter`：真实录制 initialize 的 effort 字段不丢失。
- `TestClaudeModelMenuShowsEffortOptionsAndSendsThem`：Claude 会话菜单渲染五档中文标签、无“速度”行、锁定警告、选择持久化、发送携带 `TurnOptions`。
- `./scripts/go test ./internal/claude ./internal/ui -count=1`、`./scripts/go vet` 通过。

真实模型下档位对回答深度的实际影响未量化验证（zhipu provider 两轮 `thinkingTokens` 均为 0）；原生窗口观感待用户验收；initialize 未报告默认档位，未选择时菜单无高亮、按钮不显示后缀为有意行为。
