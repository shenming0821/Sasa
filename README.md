# Fuyu

Fuyu 是面向编程 Agent 的原生桌面工作台，采用 Go + MyGo Native UI，优先支持 macOS。

当前聚焦 Codex：在桌面窗口中选择工作目录、连续对话、查看执行过程、处理交互并审查代码修改，同时保留 Codex 原生 Runtime 的执行机制。

## 项目状态

项目处于设计与工程准备阶段，尚无可运行应用或发布版本。当前计划从一个真实的 Codex 任务闭环开始，再逐步加入工具展示、审批、会话恢复与代码审查。

## 文档

- [文档入口](docs/README.md)：设计与实施资料索引。
- [实施计划](docs/implementation-plan.md)：当前进度、阶段验收与接续点。
- [开发指南](docs/development.md)：环境准备、验证要求和命令状态。
- [贡献指南](CONTRIBUTING.md)：变更范围、审核和提交约定。
- [Agent 工作指南](AGENTS.md)：编码 Agent 的仓库入口。

构建和启动方式在工程初始化并实际验证后写入开发指南。
