# G2 必须通过的负向与兼容性测试

以下是**待执行测试清单**，不是通过记录。客户端 Mock 只能证明请求行为，最终应在真实 Controller/Agent 和隔离测试环境复测。

| ID | 请求/故障 | 预期结果 |
|---|---|---|
| AUTH-01 | 未初始化远程直接 POST 创建管理员，不知道本地一次性 token | 拒绝，失败限流，不暴露 token |
| AUTH-02 | 伪造用户角色/过期会话/撤销 session | 401/403，任何更改不执行 |
| AUTH-03 | Cookie 会话缺失/错误 CSRF 提交敏感 POST | 403，不创建任务 |
| AUTH-04 | 登录后停用用户，使用此前会话访问 | 拒绝且撤销 session |
| RBAC-01 | 只读角色查询无权节点及其名字/流量 | 不泄露资产，无权敏感字段不可见 |
| RBAC-02 | AI 服务组授权 A/B 后增加 C | 旧授权仍仅 A/B；移除 B 后立即无 B |
| RBAC-03 | 伪造审批凭据、scopeRevision 或把目标从 A 改成 C | 必须拒绝且审计 |
| RBAC-04 | AI 完全访问发起任意 root Shell | 仍必须真实管理员审批 |
| TASK-01 | 相同 Idempotency-Key 与相同 body 重放 | 返回同一任务，不新建 execution |
| TASK-02 | 相同 Key 但换 body | 409 Conflict |
| TASK-03 | 节点离线 15 分钟以上才上线 | expired，不能补派发 |
| TASK-04 | ACK 丢失，重复发送同一个 executionId | Agent 只执行一次已记录 ID |
| TASK-05 | Agent/主控崩溃导致执行结果不确定 | unknown，修改类不自动重跑 |
| TASK-06 | 第一批金丝雀失败 | 暂停后续批次，本批已运行任务真实收尾 |
| TASK-07 | SSE `Last-Event-ID` 有效 / 失效 | 接续且不重复；过期明确报告缺口 |
| NODE-01 | TLS 证书与消息自称 nodeId 不同 | 拒绝连接并审计 |
| NODE-02 | 吊销节点/主控灾备后重放旧令牌 | 拒绝，不能复活身份 |
| NODE-03 | 巨型 Protobuf 帧/输出泛洪/畸形帧 | 受限拒绝、无 OOM/无限日志 |
| PLUGIN-01 | 禁用 Xray 插件后调用历史动作 URL/AI Tool | 后端拒绝，不依赖菜单隐藏 |
| PLUGIN-02 | 插件请求绕过 Host API 直接调用 Agent/私钥/任意 Shell | 拒绝并记录审计 |
| XRAY-01 | 当前实例已安装且未授权接管 | 不覆盖安装，不删除配置 |
| XRAY-02 | 配置语法正确但重启健康检查失败 | 保留故障状态并尽力恢复旧版 |
| XRAY-03 | Xray 计数器重置、采样缺口 | 累计不下降、不把未知记为 0，不同于 VPS 账单 |
| GATEWAY-01 | 已有 Nginx 站点/80/443 冲突 | 安装/接管前报告，不覆写第三方配置 |
| DR-01 | 使用落后备份恢复已吊销节点 | 只核对隔离模式，禁止自动任务派发 |

测试证据：提交 commit、环境、命令/请求、日志脱敏样本、预期/实际、PASS/FAIL/BLOCKED、关联 issue。失败不得通过修改前端显示文本掩盖。
