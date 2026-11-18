# task-worktree integrated 的 rebase 线性合入核验

日期：2026-11-19

## 范围

- `scripts/task_worktree.py`：保留任务 HEAD 到集成 HEAD 的祖先检测，错误提示改为先 rebase 再快进合入；新增 `head..commit` 范围内的 merge 提交检测，拒绝非线性集成登记。
- `scripts/test_task_worktree.py`：新增真实临时 Git 仓库中的 `merge --no-ff` 回归用例，确认祖先关系成立时仍拒绝登记，并保持任务为 `developing`。
- `docs/parallel-development.md`：对齐 rebase、快进合入及登记约束。
- 登记字段、发布回收祖先核验和 `AGENTS.md` 未改动。`docs/implementation-plan.md` 不存在，跳过接续点更新。

## 自动化结果

在任务 worktree 根目录运行：

```sh
python3 -B -m unittest discover -s scripts -p 'test_task_worktree.py'
```

首次执行时进程 `umask` 实际生效值为 `0002`，输出摘要：

```text
FAIL: test_commit_digest_matches_release_source_identity_with_links_and_mode (test_task_worktree.TaskWorktreeTests.test_commit_digest_matches_release_source_identity_with_links_and_mode)
Ran 18 tests in 2.849s
FAILED (failures=1)
```

新增用例通过。失败来自既有源码摘要测试：普通夹具文件在该环境下以 `0664` 创建，`release.source_identity()` 使用实际文件权限，而 `Tasks.commit_digest()` 按 Git 模式将普通文件权限归一为 `0644`，导致摘要不一致。本次未修改该测试或摘要逻辑。

将当前 shell 的 `umask` 设为 `0022` 后，重新执行同一验证命令：

```sh
umask 0022
python3 -B -m unittest discover -s scripts -p 'test_task_worktree.py'
```

输出摘要：

```text
Ran 18 tests in 2.535s

OK
```

18 个测试全部通过，涵盖既有快进集成、发布回收和新增非线性集成拒绝场景。`git diff --check` 通过。

## 边界与未验证项

- 线性检测范围严格为 `任务 HEAD..集成 HEAD`，不检测任务 HEAD 自身或其之前的 merge 提交，也不证明执行过 rebase；已满足祖先关系且区间无 merge 提交即可登记。
- 真实 Runtime：未运行 Codex 或 Claude Code，本次仅修改 Git 任务管理脚本及文档。
- 原生 UI / 公共 Preview：未验证，本次未改 UI，也未执行集成。
- 真实 Mac 正式版发布、安装与 worktree 回收全流程未验证；相关实现未改动，回收行为仅由隔离临时仓库和发布夹具测试覆盖。
- 修改保留在指定任务 worktree 工作区，未暂存、提交、合入或发布。
