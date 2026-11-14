# 分支与工作目录

当前远程为公有 IKUNHEIHIE/ServerFleet；协作入口见 [HANDOFF](HANDOFF.md)。同一个 GitHub 账号可用于多个工作目录，各目录仍须独立分支/任务；同账号不构成独立审核。

开始先读取 git status、已有 Worktree 和远程 main。不要在他人目录切分支、自动 stash、删除数据、reset --hard 或 clean -fd。创建任务分支时使用当前 main，不使用历史文档中的固定 p1/p2 分支作为必选基线。

```sh
git fetch origin
git worktree list
git worktree add ../ServerFleet-docs-task -b docs/task-name origin/main
```

示例路径/分支须换成当前任务的唯一名称，不覆盖已有目录。首次 clone 的普通独立目录也可承载单任务；并行任务分开目录。

在任务目录检查与提交，通过 GitHub PR 审查；集成负责人以 expected head 合并。不要直接在主工作目录合并并推 main 来代替 PR。出现远程变化时先读差异并正常解决冲突，不强推覆盖。

清理只针对自己已合并且确认无未提交/忽略数据的 Worktree；不能因分支已合并就删除运行数据库、私钥、下载材料或其他任务目录。历史平台的 Windows 路径/代理/SSH 别名不复制为当前环境配置。
