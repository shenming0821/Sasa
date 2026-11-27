# Agent 工作指南

## 项目与开发

Sasa 使用 Go + MyGo Native UI，macOS 优先，接入 Codex 与 Claude Code 原生 Runtime。任务范围以用户当前要求为准，当前行为以源码、配置和运行结果核验。

按任务需要查阅[代码标准](docs/code-standards.md)、[经验记录](LESSONS.md)和[子系统索引](docs/subsystems/README.md)；环境与验证命令见[开发指南](docs/development.md)。

文档使用具体功能名称描述范围与状态。

主窗口通用能力默认同时覆盖 Codex 与 Claude。

## 开发、集成验收与发布

- **开发、修复**：独立任务先在主目录执行 `./scripts/task-worktree create <任务名>`，所有代码修改和相关测试都在任务 worktree 完成。同一需求的反馈继续用原目录，回收后再建。修改默认留在工作区，完成后报告“开发完成，待集成验收”。
- **集成验收**：在主目录执行 `./scripts/preview-task <任务名>`，用公共 `bin/Sasa Preview.app` 验证用户可观察到的行为，报告构建 ID、结果和未验证项。任务分支保持独立，主目录原有修改和暂存状态保留。自动化和真实 Runtime 需要原生窗口时共用这一个 Preview。
- **发布**：用户明确说“发布”时，核对并提交主目录全部待交付改动（含已有暂存、未暂存和未忽略的新增文件）及本次明确交接的任务。任务分支 rebase 到主分支并快进合入，用 `./scripts/task-worktree integrated <任务名>` 登记。核对最终源码，验收对应的公共 Preview 后，按构建 ID 晋升并推送当前分支，核验远端结果。正式版下次启动生效。成功安装后回收已交付任务目录，并在发布结束前清理本次开发和验收产生的全部临时残留。用户当次指定的范围或限制优先。

操作细节见[并行开发](docs/parallel-development.md)和[发布说明](docs/subsystems/releases.md)。

## 修改与验证

- 用 `apply_patch` 编辑文件，`./scripts/go fmt` 只传本次改过的文件路径。用户单独授权暂存或提交时，只处理核对过的任务文件并保留其他暂存内容；提交格式见[贡献指南](CONTRIBUTING.md)。
- 涉及多个组件时，沿输入、协议、状态到最终结果核对调用链。
- 按风险选择验证，自动化测试隔离配置与数据；分别报告自动化、真实 Runtime 和原生 UI 的结果及未验证项。
- 发现来源不明的并行修改时，暂停写入相关文件，向用户确认归属。

## 沟通与规则维护

- 回答判断、原因或范围问题时，先给结论，再补直接依据与动作。
- 输出系统参数时，同时提供名称与实际生效值；未取得的值标为未核验。

## 浏览器任务

使用 ego-lite 前读取 ego-browser 技能。接管指定 TaskSpace 时，先用 `listTaskSpaces()` 定位，再用 `takeOverTaskSpace` 或 `claimTaskSpace` 接管；用户要求新建或确认目标不存在时再创建。

操作完成后保留页面和空间，用 `handOffTaskSpace` 交还用户；用户明确要求关闭或确认不再需要时再清理。
