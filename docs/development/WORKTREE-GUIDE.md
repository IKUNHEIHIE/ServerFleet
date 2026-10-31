# Worktree 操作指南

主目录保留给集成负责人。辅助脚本只操作当前仓库 `.worktrees/` 下的任务目录，不触碰其他项目；不自动启动 Agent、不推远程。无需 GitHub 账号。

在主目录执行（PowerShell 与 shell 通用）：

```sh
node scripts/worktree.mjs list
node scripts/worktree.mjs create feature/p2-nodes-ds01 main
```

脚本打印完整路径，例如 `<项目>/.worktrees/feature--p2-nodes-ds01`。将这个目录交给对应会话，不要把主目录交给多个写代码的会话。新会话必须核对 `git branch --show-current` 和 `git rev-parse --show-toplevel`，再读任务单。Worktree 不会自动复制未提交文件、本地依赖或 `.tools`；先安装工具，或在任务单中明确由负责人提供受控只读工具路径。

任务内独立提交：

```sh
git status --short
npm run check
git add internal/nodes
git commit -m "feat(nodes): add reviewed inventory rules"
```

只添加明确属于任务的文件，勿把其他会话产物加入。提交身份缺失时由执行人提供自己的身份；P1 的本地构建提交用明确的占位本地构建身份，不冒充项目所有者。

集成负责人确认目标目录干净，在专用 integration Worktree 审查和合并：

```sh
node scripts/worktree.mjs create integration/p2-batch01 main
git -C .worktrees/integration--p2-batch01 merge --no-ff feature/p2-nodes-ds01
```

在 integration 目录运行全量检查，检查合同/生成代码并完成 INTEGRATION-CHECKLIST；主目录 main 干净时再由集成负责人 `git merge --ff-only integration/p2-batch01`。不得在未完成审查时执行示例合并。

发生冲突时，用 `git status` 查看文件；由各 owner 决定保留的语义，手工合并后重新测试。不可用 `--ours/--theirs` 整体丢弃对方改动、强推、硬重置。无法继续时保留现场与冲突说明；`git merge --abort` 仅在确认没有需保留的新工作后使用。

清理：

```sh
node scripts/worktree.mjs remove feature/p2-nodes-ds01 main
node scripts/worktree.mjs list
```

脚本会拒绝未合并分支，以及任何已修改、未跟踪或忽略文件（包括 node_modules、报告、数据库）。先保存有价值内容；若需清理任务生成依赖，只在该任务绝对目录经过核对后操作，不能删主目录或其他会话。脚本不强制删除、不自动删分支。确认不再需要时可用 `git branch -d <已合并分支>`，不用 `-D`。

本地无远程也能完成上述流程。推送、建立远程仓库和 GitHub 设置必须由负责人明确授权。
