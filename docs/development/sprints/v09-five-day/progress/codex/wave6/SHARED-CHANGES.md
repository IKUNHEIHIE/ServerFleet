# 待总控接收的共享差异

## 密钥入口

`controller-keyring.patch` 是针对起点 server.go 的可应用差异，只保存于本任务 progress，没有直接改 apps/controller。新增 Config.CredentialKeyDir，并替换固定 local-default 构造。CLI 配置路径、目录建立/权限、测试夹具由总控协调；不接收此补丁主线固定密钥缺口仍存在。默认拒绝 gate 暂保留，后续 AI 接线差异替换它，不能全局放行。

兼容性：旧已初始化库无私有密钥文件将拒绝启动；不可自动覆盖/迁移真实数据。Windows 默认拒绝，需 Linux 实测或另经审查的 ACL 实现。API/Proto/gen 本增量无变化。

## Agent 文件候选

当前正在实现独立有界字节能力。必须由 DS 文件任务与总控共同接收数据帧/Proto、源读和目标写分别授权/执行 ID/不可变摘要，再接入真实 WSS。不得把分块放进 TaskOutput 或普通日志；未接线不算跨节点功能完成。
