# Fuyu

Fuyu 是面向编程 Agent 的原生桌面工作台，采用 Go + MyGo Native UI，优先支持 macOS。

当前聚焦 Codex：在桌面窗口中选择工作目录、连续对话、查看执行过程、处理交互并审查代码修改，同时保留 Codex 原生 Runtime 的执行机制。

## 项目状态

当前已有可运行的 macOS 开发版本：原生目录选择、多行输入、真实 Codex 流式对话、连续追问、简单工具摘要和原生中断。真实文件操作与两轮上下文已验证；P1 的输入法与持续输出阅读验收仍待补齐。审批表单、会话持久化和代码审查按后续阶段推进。

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

先选择目录，再输入任务。`⌘ Enter` 发送，`Enter` 换行。停止以 Codex 返回的 turn 终态为准；已转入后台的命令可能继续运行。当前会话仅保留在内存，退出前自行保留需要的内容。

## 文档

- [文档入口](docs/README.md)：设计与实施资料索引。
- [实施计划](docs/implementation-plan.md)：当前进度、阶段验收与接续点。
- [开发指南](docs/development.md)：环境准备、验证要求和命令状态。
- [贡献指南](CONTRIBUTING.md)：变更范围、审核和提交约定。
- [Agent 工作指南](AGENTS.md)：编码 Agent 的仓库入口。

工具链选择、测试入口与已知边界见[开发指南](docs/development.md)。
