# 展开与收起动画优化验证

## 范围

执行过程、命令组、工具详情、错误详情、项目分组、本轮文件列表、文件目录、搜索筛选与结果分组，以及 Git 分组、分支列表和提交文件列表。

按钮先处理输入，再更新动画目标。共用无回弹弹簧，响应时长为 180 ms、精度为 0.002；中途改变方向保留当前速度。普通内容通过正常布局中的外层裁切区域调整高度，内层保留自然高度；Git 历史维持原有受限滚动区域。系统减少动态效果开启时直接切换。

工具标题、输入解析、输出格式化及错误摘要按原始内容缓存。输入、输出或工具类型变化后重新计算；缓存不写入持久化数据。成功且收起的工具只解析状态，在展开详情或展示错误摘要时才创建格式化结果。

## 自动化

`internal/ui/disclosure_motion_test.go` 覆盖点击后的首次刷新、后续内容单调移动、收起最终移除内容、Git 分组过渡、执行过程首帧响应、工具缓存更新和稳定缓存零分配。既有项目分组与执行过程测试继续覆盖动画反向切换、减少动态效果和阅读位置保持。

桌面启动与新建入口只提供 Codex、Claude。旧 Agent 专用权限模式、模型提交和补全测试已删除，空分屏入口预期同步更新。使用正常命令验证，无 Go overlay、无额外跳过测试：

race 检查发现既有图片草稿测试在缩略图后台解码期间改写 `dispatch`。测试改为始终使用同一回调队列，按导入状态等待文件拖放完成，并在清理时等待缩略图与导入回调结束。

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

执行 `./scripts/build preview` 构建候选，并通过 `./scripts/run-preview <构建ID>` 启动。

早期隔离数据启动已取得含正确渠道、构建 ID、数据目录与 PID 的窗口初始化确认。电脑控制工具定位该应用返回 `cgWindowNotFound`，因此原生点击、视觉流畅度、实际显示帧率及真实 Runtime 输出期间的动效尚未验收。自动化验证为 MyGo 无窗口渲染结果。正式版晋升采用延迟生效，保留正在运行的正式版实例。

## 左侧项目长列表的后续优化

用户反馈编辑器目录动效流畅，但左侧 `order` 项目展开与收起仍卡顿。只读核对当前正式版索引：7 个仓库、258 个历史会话，`order` 包含 10 个工作区。左侧原实现为每个工作区重新遍历全部会话，并为每个会话遍历全部工作区、通过 `filepath.Rel` 计算归属；路径计算随工作区与会话数量反复增加。合成侧栏基准的 CPU 采样中，`worktreeFor` 累计约占 86%。

左侧每次构建先建立仅在本帧有效的工作区路径索引，以逐级父目录查询最近的工作区；会话按工作区一次分组并保持原有倒序。搜索、工作区摘要与目录分组共享该索引，帧结束即释放，下帧重新读取最新项目与会话。编辑器目录沿用既有实现。

`sidebar_performance_test.go` 覆盖索引与旧路径归属的一致性、嵌套工作区、相邻路径边界、未归属路径、会话顺序、会话移动与新增、项目刷新后重新取值，以及索引作用范围。

相同合成数据（7 个仓库，每仓库 10 个工作区，共 258 个历史会话；260 × 760 内存窗口）连续 96 次模拟刷新，包含展开与收起，每 24 次反转状态：

| 实现 | 每次模拟刷新耗时 | 每次分配字节 | 分配次数 |
|---|---:|---:|---:|
| 修改前 | 55.70 ms | 8,735,436 | 282,019 |
| 本帧共享路径和会话索引 | 0.328 ms | 50,296 | 492 |

性能对照使用 Go overlay 仅替换基准中的 `navigation.go`、`projects.go` 为修改前版本；常规测试使用工作区正常代码，无 overlay 或额外跳过。此处是内存窗口的合成动效基准，包含帧构建与软件渲染，不代表原生显示帧率。

```sh
./scripts/go test ./...
./scripts/go vet ./...
./scripts/go test -race ./internal/ui \
  -run 'TestProjectFrame|TestProjectDisclosure|TestRepository|TestWorktreeRename|TestProjectHeading|TestProjectMenu' -count=1
./scripts/go test ./internal/ui -run '^$' \
  -bench '^BenchmarkSidebarDisclosure/Worktrees10$' -benchmem -benchtime=96x
```

上述全量测试、vet 与相关 race 检查通过。Preview 已通过正常构建、启动及窗口初始化确认，并在原生窗口实际操作 `order` 的展开、收起、再次展开，无障碍树确认 10 个工作区条目依次出现、移除、重新出现。保留 Preview 窗口与展开后的列表供用户验收；未采集原生逐帧时间或实际显示帧率。
