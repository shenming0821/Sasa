# Fuyu

Fuyu 是面向编程 Agent 的原生桌面工作台，采用 Go + MyGo Native UI，优先支持 macOS。

当前聚焦 Codex：在桌面窗口中选择工作目录、连续对话、查看执行过程、处理交互并审查代码修改，同时保留 Codex 原生 Runtime 的执行机制。

## 项目状态

当前已有可运行的 macOS 开发版本：原生目录选择、多行输入、真实 Codex 流式对话、连续追问、结构化工具卡片、命令输出、文件摘要、审批与问题表单及原生中断。P1、P2、P3 已完成当前环境验收，真实模型与夹具的覆盖边界见[P2 验证记录](docs/verification/2026-10-22-p2.md)。P3 已实现多会话、SQLite 持久化、原生历史恢复与窗口驻留，当前验收与支持边界见 [P3 记录](docs/verification/2026-10-22-p3.md)。左侧已加入统一添加本地项目（普通文件夹或 Git 仓库）、关联 worktree 自动发现与按工作区会话导航，实际覆盖见[项目管理验证](docs/verification/2026-10-23-project-management.md)。其他后续开发范围按新计划确认。

## 启动开发版本

需要 macOS、Command Line Tools、Go 1.27.3、Python 3（构建样式补丁使用标准库）和已配置可用的 Codex CLI。

```sh
./scripts/build
open bin/Fuyu.app
```

也可在终端指定初始目录或 Codex 可执行文件：

```sh
bin/Fuyu.app/Contents/MacOS/Fuyu -cwd /path/to/project -codex codex
```

先选择目录，再输入任务。`Enter` 发送，`⌘ Enter` 换行。停止以 Codex 返回的 turn 终态为准；已转入后台的命令可能继续运行。草稿、阅读位置与工具折叠保存在应用索引，消息历史从 Codex 原生记录恢复。关闭窗口后应用继续驻留，可从 Dock 重新打开；退出时有活动任务会列出受影响会话。异常退出后先核验原生状态，旧审批不会恢复，也不会自动重发。

## 文档

- [文档入口](docs/README.md)：设计与实施资料索引。
- [实施计划](docs/implementation-plan.md)：当前进度、阶段验收与接续点。
- [开发指南](docs/development.md)：环境准备、验证要求和命令状态。
- [贡献指南](CONTRIBUTING.md)：变更范围、审核和提交约定。
- [Agent 工作指南](AGENTS.md)：编码 Agent 的仓库入口。

工具链选择、测试入口与已知边界见[开发指南](docs/development.md)。
