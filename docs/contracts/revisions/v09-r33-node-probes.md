# R33 产品、Web 与 API 同步

任务 V09-CLOUD-NODE-PROBES-01，主控协调 Agent／transport／Controller／monitoring／Web及依赖边界。独立候选 `api/openapi/revisions/v09-r33/node-probes-candidate.yaml`，不覆写 G2／R01—R32，不宣称冻结。使用与边界见[指定节点监控](../../development/NODE-PROBE-MONITORING.md)。

| 类型 | 产品／Web | 接口／传输 |
|---|---|---|
| 新增 | 选择源节点发起 HTTP/TCP/TLS 检查，显示源名称与实际任务 | POST/PATCH availability-monitors 接受并持久化 sourceNodeId；空值保留 Controller 模式 |
| 新增 | 当前来源异常明确为未知，保留最后确定业务结论用于后续变更判断 | Monitor 增 lastProbeUnknown / lastProbeTaskId，列表当前 state=unknown；Check/probe/history 增 sourceNodeId / taskId |
| 修改 | 手动与后台都执行真实节点任务；来源或目标变化使旧任务失效 | monitoring.probe 标准只读节点任务；当前监控管理和源节点显式授权，手动操作校验实际调用者；不可从Web/AI/插件直接执行Agent |
| 修改 | unknown 不累计业务失败／恢复，不发送虚假业务告警 | 离线／能力不足／超时／取消／UNKNOWN／缺回执归unknown，打断连续计数；确定的目标 down 才触发原阈值/outbox |
| 修改 | 手动探测不接收临时目标／来源覆盖 | POST /{monitorId}/probe 无query，只接受空body或{}；沿用原会话、CSRF、Origin与请求标记。保存的可信配置为唯一输入 |
| 新增 | 检查历史可打开任务详情与原有异常人工处置 | GET /{monitorId}/history 最近记录，任务读取仍独立要求tasks.read，原UNKNOWN不改写、不自动结案 |
| 删除 | 节点来源统一返回unsupported_probe_source的限制 | 节点目标仅公网、<=1024字节、超时1—30秒；Controller源原有显式允许策略保持 |

节点探测沿既有TaskOffer/TaskOutput，不新增Proto消息或字段。参数为五项字符串：requestKey（SHA256运行键）、kind、target、timeoutSeconds、profile=public-http-tcp-tls-v1。Agent执行预算在目标超时上追加5秒用于授权／回传；Controller另留调度预算。回执是<=4096字节严格JSON：version=1、requestKey、outcome(up/down/unknown)、固定reason、可选latencyMs/statusCode；没有正文、头、Cookie、凭据或任意命令。现有消息大小／参数长度／逐帧检查不放宽。

新增低层 `internal/netprobe` 仅依赖标准库，monitoring与Agent显式依赖；transport仅使用公开类型与输入校验。旧monitoring API为兼容包装。不是新框架或插件Runtime，没有新增第三方依赖、root、SSH或执行旁路。新监控来源仍须配置保存者当前授权，任务还须实际执行者的节点权限；没有给管理员或旧资源授权自动增加权限。

追加 ds/0087，正式48→49迁移，领域备份子集19→20。历史SQL/checksum/来源夹具不变；原G2/Proto/gen不修改。完整产品、G2及真实节点验收保持独立，不用单项代码或CI关闭。
