# Agent 常驻与文件能力交接

仅总控操作 awskr；以下是普通用户私有目录候选，不安装系统服务、不修改旧 Agent/站点。先由总控给新临时节点完成 enrollment（既有命令），固定新 Controller 的 root PEM / pin；根私钥从不送给 Agent。

## 手动启动/停止

在普通用户专用测试目录内保存固定 SHA 编译的 Linux Agent、已签发身份目录与公开 root.pem，目录 0700、文件 0600；绝对路径。前台启动（占位路径仅替换为本次私有目录）：

```sh
./agent -serve -state-dir "$PWD/identity" -root-cert "$PWD/root.pem" -controller-url https://localhost:本次空闲端口
```

SIGTERM / Ctrl-C 停止；停止等待最多 10 秒。最多连续 8 次失败，延迟 1/2/4/8/16/30/30 秒 + 0..255ms 抖动；稳定连接至少 60 秒重置失败预算。不得配置无限外部重启。不会重入网、替换身份或自动重跑持久 UNKNOWN。执行 worker 未能停止会导致失败退出，不建立下一连接。

可选 `systemd --user` unit **候选**（仅总控评审后在私有测试环境使用；本任务没有执行安装）：ExecStart 使用上述绝对路径；Restart=no；UMask=0077；NoNewPrivileges=yes；ProtectSystem=strict；ReadWritePaths 限定该 identity 和数据目录；不声明 root 用户，不覆盖已有 serverfleet-* unit。正式安装/双架构/系统隔离准入仍缺实测。

## 文件帧待接收

`agent-file-proto.patch` 仅是具体 Proto 差异，未触碰共享 proto/gen。总控与 DS 接收后需用固定生成工具产物、更新校验/兼容矩阵，再组合 ControllerLinks 与 runAgentSession；当前 CLI 不宣传/启用 files.chunk.v1。

- 新 envelope oneof 33/34；仅 protocol minor >=1 且双方授予 `files.chunk.v1` 时使用。旧 peer 不授予，不能给旧 peer 发新 oneof。
- `files.read`、`files.write` 两个正常权限动作各有 TaskOffer（schema1），参数冻结 task/execution/transfer/node/relativePath/direction/size/sha256。源读、目标写分别经 Controller 当前授权、任务、审批、审计，不能用源权限授权目的端。
- 单帧 data <=32768，完整帧 <=65536，单文件 <=64MiB；流水窗口 1、严格偏移。读完成还需 DS 流层验证整文件 SHA；写最终硬链接发布，绝不覆盖已有目的。
- 注册的 gate 对每次读/写/提交及提交后复核；它必须验证当前 TaskOffer 绑定、执行未取消及证书当前身份。字节不得写入 task logs、审计或错误信息。
- 私有 partial/binding 保存断点，同绑定和当前授权可续写；不同绑定/偏移拒绝。最终回执丢失为 UNKNOWN，由 Controller/DS 对账目的端哈希和执行状态，不自动重跑提交。
- 所有路径限于总控预注册私有数据根，拒绝路径逃逸、符号链接及 root 进程；无任意 shell、SSH、CA 或恢复秘密能力。

本地测试可直接调用未导出的文件能力；它不能代替两端 Agent WSS 字节互传验收。空间不足/真实断线/两机恢复需组合后继续收证。
