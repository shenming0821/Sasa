# 展开与收起动画优化验证

## 范围

执行过程、命令组、工具详情、错误详情、项目分组、本轮文件列表、文件目录、搜索筛选与结果分组，以及 Git 分组、分支列表和提交文件列表。

按钮先处理输入，再更新动画目标。共用无回弹弹簧，响应时长为 180 ms、精度为 0.002；中途改变方向保留当前速度。普通内容通过正常布局中的外层裁切区域调整高度，内层保留自然高度；Git 历史维持原有受限滚动区域。系统减少动态效果开启时直接切换。

工具标题、输入解析、输出格式化及错误摘要按原始内容缓存。输入、输出或工具类型变化后重新计算；缓存不写入持久化数据。成功且收起的工具只解析状态，在展开详情或展示错误摘要时才创建格式化结果。

## 自动化

`internal/ui/disclosure_motion_test.go` 覆盖点击后的首次刷新、后续内容单调移动、收起最终移除内容、Git 分组过渡、执行过程首帧响应、工具缓存更新和稳定缓存零分配。既有项目分组与执行过程测试继续覆盖动画反向切换、减少动态效果和阅读位置保持。

发布收尾保留旧 Devin 会话身份、存储校验和遗留适配器所需的兼容接口；桌面启动与新建入口只提供 Codex、Claude。旧默认 Agent 为 Devin 时，新会话默认使用 Codex，旧会话原生 ID、草稿和模型归属仍保持 Devin。`TestLegacyAgentHistorySurvivesWithoutDesktopRuntime` 覆盖加载、缺少运行时、入口、新会话及再次保存后的数据保留。

删除三个只针对已移除桌面入口的旧权限模式、Devin 补全测试，并更新空分屏入口预期。使用正常命令验证，无 Go overlay、无额外跳过测试：

```sh
./scripts/go test ./...
./scripts/go vet ./...
./scripts/go test -race ./internal/agent ./internal/app ./internal/storage ./internal/ui
python3 -B -m unittest discover -s scripts -p 'test_release.py'
./scripts/go test ./internal/ui -run '^$' \
  -bench '^BenchmarkToolDetailsFormatting$' -benchmem -benchtime=300ms
```

Apple M1 Max 上，约 141 KiB 输入和 147 KiB 输出的解析与格式化基准：

| 路径 | 每次耗时 | 每次分配字节 | 每次分配次数 |
|---|---:|---:|---:|
| 重复解析与格式化 | 2.069 ms | 3,215,716 | 118 |
| 已缓存、原始内容未变 | 5.827 ns | 0 | 0 |

此基准仅测内容处理，不代表整个窗口的帧耗时或实际显示帧率。

## Preview 与原生验证边界

执行 `./scripts/build preview` 构建候选，并通过 `./scripts/run-preview <构建ID>` 启动。初始工作区曾因移除 Devin 校验导致已有 Preview 历史无法加载；发布前已保留旧数据兼容接口并添加回归，原历史数据保持原样。

早期隔离数据启动已取得含正确渠道、构建 ID、数据目录与 PID 的窗口初始化确认。电脑控制工具定位该应用返回 `cgWindowNotFound`，因此原生点击、视觉流畅度、实际显示帧率及真实 Runtime 输出期间的动效尚未验收。自动化验证为 MyGo 无窗口渲染结果。正式版晋升采用延迟生效，保留正在运行的正式版实例。
