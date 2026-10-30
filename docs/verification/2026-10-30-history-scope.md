# 历史筛选选中底色

日期：2026-10-30。

“当前项目 / 全部项目”在独立历史面板与工作区右侧历史页中共用 `historyScopeControl`。保留 MyGo SegmentedBase 的单选、焦点与方向键行为，选中底色使用主题 SurfacePressed，复用应用按钮的悬停、按下及减少动态效果支持，取消默认分段控件的白色凸起和阴影。

`./scripts/go test ./internal/ui -run 'TestHistory|TestThreadHistory|TestPanelTabs|Test.*RightPanel|Test.*Navigation' -count=1`、UI vet、构建与差异检查通过。历史回归验证点击全部项目后的实际筛选结果，既有按钮回归覆盖浅深色主题反馈。

正常退出旧实例后启动新版 PID 13999，CoreGraphics 确认 1000×760 主窗口可见。原生切换“全部项目”和“当前项目”后辅助功能均报告所点项为选中；最终保留当前项目。浅色窗口截图 `/tmp/fuyu-history-scope-20261030/native-light.png` 确认选中项为灰色并与面板协调。原生深色窗口及方向键操作本次未单独验收。
