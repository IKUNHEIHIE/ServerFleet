# ADR-DEPLOY-001：多节点 Nginx 反向代理集中管理、负载均衡与 HTTPS 自动配置

- **状态**：**Accepted / 已正式批准**（项目所有者于 2026-10-26 批准 DEPLOY-D1～D8）
- **决策版本**：v0.2（2026-10-26 获正式批准，纳入 Server Fleet v1.1）
- **日期**：2026-10-26
- **适用总文档**：Server Fleet v1.1（以 v1.0 为基线，新目录增量修订；v1.0 保留）
- **取代的审核草案**：`ADR-DEPLOY-001-网站部署反向代理与HTTPS-v0.1-待审核.md`；保留 v0.1 以备追溯
- **修订缘由**：项目所有者明确要求“主控管理 20 台 VPS，其中 4 台已安装 Nginx，由这 4 台 Nginx 反向代理其他受管节点的服务”，并认可 DEPLOY-D1～D8 修订方向。

## 1. 给项目所有者的结论

**主控只负责管理，不承担网站访问流量。** 已安装 Nginx 的受管 VPS 才是入口网关；后端服务可运行在其余任何受管 VPS 上，同一台也可同时充当入口与业务节点。首版对**现有 Nginx** 提供可审查、可回退的代理规则管理；不会强制在其他 VPS 安装 Nginx、不会在主控启动反向代理，也不会擅自覆盖原有网站。

用例：20 台 VPS 中 A/B/C/D 四台运行 Nginx；E 运行 `blog:3000`、F 运行 `api:8080`，G/H 运行一个 Web 应用。主控管理如下关系：

| 访问域名 | 入口 Nginx VPS | 后端服务 VPS | 行为 |
| --- | --- | --- | --- |
| `blog.example.com` | A、B | E:3000 | 两个入口分别反代同一服务 |
| `api.example.com` | C、D | F:8080 | 两个入口均具备独立 TLS 证书与配置 |
| `app.example.com` | A、B、C、D | G:3000、H:3000 | 每个入口用 Nginx upstream 在 G/H 之间分配请求 |

业务路径：`互联网用户 → DNS/Cloudflare（可选）→ 选定 Nginx 入口节点 → 业务后端节点`。
管理路径：`Go 主控 → 现有 mTLS/Agent 控制通道 → Nginx 节点/后端节点`。

**关键事实**：Agent 的 mTLS 控制通道不是业务 TCP 转发隧道；入口 Nginx 必须实际能够连接后端地址和端口。后端位于私网、NAT 或被防火墙隔离时，先建立明确授权的可达网络（例如已有私网/VPN），此 ADR **不**自动开发内网穿透。

## 2. 八项已正式批准的架构决策（DEPLOY-D1～D8）

| ID | 推荐最终决策 | 约束/说明 |
| --- | --- | --- |
| **DEPLOY-D1** | **主控集中管理多个已有 Nginx 入口节点的反向代理规则**，不自建主控反向代理 | 与旧 v0.1 的“主控管理 Caddy 网站部署”定位不同 |
| **DEPLOY-D2** | 识别受管 VPS 上已安装的 Nginx；**只管理被授权接管的配置片段** | 不强制安装、替换或停用其他节点的 Nginx，不自动覆盖原有站点 |
| **DEPLOY-D3** | 管理员自由选择一个或多个 Nginx 入口、一个或多个已登记业务后端 | 从入口视角验证至后端的实际连通性；同一 VPS 可兼任两种角色 |
| **DEPLOY-D4** | 主控统一协调 Cloudflare DNS、ACME DNS-01 与续期；**每个入口节点独立生成并保管 TLS 私钥** | 不向节点分发 Cloudflare Token；不跨节点复制同一网站私钥；遵守 ADR-WC-001 |
| **DEPLOY-D5** | 独立托管配置；变更前检查/预览、`nginx -t` 校验、重载后探测，失败尽力恢复上一受管配置 | 不修改非受管配置；不承诺 DNS/外部服务事务回滚 |
| **DEPLOY-D6** | 多入口节点配置变更统一经 TASK-001 分批任务引擎 | 先 1 台、再每批最多 5 台；有节点失败暂停后续批次 |
| **DEPLOY-D7** | 支持单入口向多个后端进行 Nginx upstream 负载分配与基础健康检查 | 开源 Nginx 原生是**被动**上游失败检查；主控/Agent 另做探测，不宣称提供商业版主动摘流功能 |
| **DEPLOY-D8** | Web、AI、插件共用主控的代理管理 API、审批、任务与日志 | root 配置变更经主控授权；AI/插件不得跳过审批或直连 Agent |

## 3. 核心对象模型：把“入口”和“后端”分开

- **IngressNode**：一台安装并运行 Nginx 的受管 VPS，含 Agent 节点 ID、Nginx 实例与配置归属、支持的监听端口、已有站点清单等。
- **BackendService**：已登记的受管业务服务，包括节点 ID、允许的目标地址/端口、协议（HTTP/HTTPS）、健康检查路径与所有权。不是任意可填写的第三方 URL。
- **ProxyRoute**：一条域名或路径规则，将一个或多个 IngressNode 指向一个或多个 BackendService，包含 Host/SNI、路径、转发头、超时、健康策略与归属。
- **IngressDeployment**：某条规则在某台 Nginx VPS 上的一次不可变配置版本与任务记录，便于独立验证和回退。
- **CertificateDeployment**：证书与具体入口节点绑定，各节点保管各自的 TLS 私钥和证书链。

同一个域名可以配置多个入口，每个入口又可以有多个上游；这两个维度不可混淆。**DNS 选择哪个入口**与**入口 Nginx 选择哪个后端**是两层不同的负载分配。

## 4. 首版操作流程

1. **发现入口**：主控通过 Agent 查询 `nginx -v`、服务状态、监听端口、受管目录是否已接入 Nginx。只上报必要元数据；读取配置时避免在日志/UI 中泄漏密钥与密码。
2. **选择入口**：管理员从已有 Nginx 节点勾选 A、B 等；未安装 Nginx 的节点可以继续充当后端，但不会自动成为入口。
3. **登记后端**：选择受管节点与其授权服务端口，例如 F:8080；从**每个选中的入口节点**测试 TCP/HTTP 连通性。入口到后端不可达时阻止或明确提示不可用，不能以 Agent 在线替代连通检测。
4. **填写域名与规则**：例如 `api.example.com`；可配置 URL 路径、上游权重及合理超时。主控检验 Host 与现有站点归属冲突、上游服务授权与目标地址限制。
5. **准备 DNS 和证书**：主控在明确授权的 Cloudflare Zone 创建/调整受管记录；对应入口独立产生 TLS 密钥与 CSR，由主控执行 ACME DNS-01，单节点原子部署证书链。默认精确域名证书，通配符依 WC-001 限制。
6. **生成变更计划**：展示“在哪些入口新增/修改哪些 Nginx 配置文件”“域名指向哪些入口”“代理到哪些受管后端”“证书与审批范围”。用户确认后固化目标与参数快照。
7. **灰度更新**：使用 `jobs.submit`，按 1 → 最多 5 台分批；在受管目录写入临时配置、测试 `nginx -t`、原子替换并平滑 reload；每个入口独立记录版本与结果。
8. **核验与回退**：从入口本地和外部通路分别验证 Host/SNI、TLS 与后端响应；发生失败暂停未开始批次，尽力将该入口恢复到上一受管配置、再次测试和 reload。DNS 记录改动及缓存传播另行标识，不假称瞬时全网回退。

> **建议顺序**：新入口应先完成配置、证书与健康检查，再把 DNS 记录导向该入口；如果调整现有记录，需要保留/记录旧值和归属以便人工或受控回退。与既有非受管记录冲突时拒绝擅自修改。

## 5. Nginx 配置安全与所有权边界

- 优先通过**专属受管 include 目录**存放 Server Fleet 生成的 `.conf`；如现有 Nginx 没有引用该目录，必须由管理员明确批准一次性接入，不能默默修改 `nginx.conf`。
- 每个规则文件有明确 `route_id` 与 `deployment_id`，保留上一有效版本；禁止删除/覆盖非 Server Fleet 管理的配置文件。
- 操作前验证全局语法与引用文件：`nginx -t` 成功才允许 `nginx -s reload` 或经验证的 systemd reload；即使配置语法正确，仍需确认运行后代理/证书健康。
- 若出现同一 `server_name`、`listen`、泛域名优先级或路径冲突，显示风险并停止自动写入，不保证自动推断第三方手写规则的意图。
- 反向代理首版限定 HTTP/HTTPS；WebSocket 升级头可以作为标准模板能力；TCP/UDP stream 代理、四层 SNI 复用等另行评审。
- 限制任意请求头注入、CRLF、非预期变量展开、文件路径穿越、开放代理与未授权转发；禁止用户直接上传任意原样生效的 Nginx 配置片段作为默认功能。
- 有权限的主控后台检查、任务审计和管理员确认须覆盖修改，Agent 原有 root 安全模型不变。

## 6. 后端网络与安全边界

1. **网络可达性是硬前提**：入口 Nginx 需能访问后端地址/端口。后端可用其公网地址或事先建好的受信私网地址，但必须验证访问边界与防火墙，避免为了方便把数据库/管理端口暴露公网。
2. **受管后端白名单**：首版上游必须从已授权的 BackendService 中选择，禁止指向主控管理 API、云元数据端点、内网管理地址或任意互联网 URL；用服务登记地址、允许网段、端口和 DNS 解析结果校验抵御 SSRF/DNS rebinding。
3. **传输安全**：若入口到后端跨越不可信公网，优先 HTTPS（启用服务端证书验证）或已有 VPN/专网；不能因为入口浏览器侧已启用 HTTPS，就把公网回源明文视为安全。
4. **可靠性**：Nginx Open Source 提供基于真实请求失败的被动 upstream 检测；主控/Agent 可按固定间隔主动探测管理状态，但只有当有受控的配置更新/明确支持的能力时才能影响真实 Nginx 转发行为；不宣传“开箱即用自动健康摘流”。
5. **同域名多入口**：可手动设置多条 A/AAAA 记录，但简单 DNS 分流并不等同具有承诺的高可用、跨区域调度或故障自动切换。若要主动路由与健康故障切换，应单独评估 Cloudflare Load Balancing 等方案。

## 7. 与已批准安全及架构决策的关系

| 已批准 ADR | 必须保留 |
| --- | --- |
| WC-001 | ACME/DNS 由主控协调；入口节点各自生成并保管网站 TLS 私钥；首版禁止跨节点共享同一通配符私钥 |
| BOOT-001 | Agent 可信安装、mTLS 连接；不能因为加入反代功能重新注册/替换节点身份 |
| PRIV-001 | root Agent 的后台授权、人工确认和审计；禁止插件/AI 自行批准敏感变更 |
| PLUGIN-001 | 前端仍是 React + Vite + shadcn/ui；反代管理页可由统一声明式 UI 提供菜单、表单、表格和进度 |
| TASK-001 | 同一任务引擎，1→5 灰度、失败暂停、过期和未知结果防重复执行 |
| AI-001 | AI 只能调用注册的 `proxy.*` 与任务工具，不能绕过权限或直接下发任意 Nginx 配置 |
| UPDATE-001 | Nginx 规则变更与 Agent 自身自动更新相互独立 |
| PKI-OPS-001 | 备份主控中的域名/规则/部署历史及证书索引；不把节点上的网站 TLS 私钥集中导回主控备份 |

## 8. API 契约草案（不冻结字段）

复用现有 `/api/v1/jobs`、`/api/v1/jobs/{id}/approve`、域名与证书管理接口，新增以下受控资源：

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| GET | `/api/v1/proxies/ingress-nodes` | 发现可受管的 Nginx 入口及冲突 |
| GET/POST | `/api/v1/proxies/backends` | 列出/登记获授权的后端服务 |
| GET/POST | `/api/v1/proxies/routes` | 列出/创建代理规则草案 |
| GET/PATCH | `/api/v1/proxies/routes/{route_id}` | 查看/修改规则草案（不直接生效） |
| POST | `/api/v1/proxies/routes/{route_id}/plans` | 预检：入口、后端连通、Nginx、DNS、TLS 与权限；生成需确认的快照 |
| POST | `/api/v1/proxies/routes/{route_id}/deployments` | 将已批准计划提交给统一任务引擎 |
| GET | `/api/v1/proxies/routes/{route_id}/deployments` | 查询各入口的生效版本、状态与审计 |
| POST | `/api/v1/proxies/routes/{route_id}/rollback-plans` | 准备回退计划并经既有审批执行 |

模型草案：

```json
{
  "route_id": "route-api",
  "hostname": "api.example.com",
  "ingress_node_ids": ["node-A", "node-B"],
  "backend_service_ids": ["service-F-8080"],
  "upstream_policy": {"method": "round_robin"},
  "tls": {"mode": "controller_acme_dns01", "key_custody": "ingress_node_local"},
  "deploy_strategy": "staged_1_then_5"
}
```

“批准”必须绑定入口 Node ID 快照、后端 Service ID/解析地址/端口、域名、证书对象、目标文件清单及负载策略；变更任何重要参数应重新审批。数据层区分代理规则与逐入口实际生效状态，避免一台成功就把整个规则标记为全局成功。

## 9. 验收测试（首版质量门槛）

1. 20 台受管节点中仅 A/B/C/D 安装 Nginx：主控仅识别这些节点可作为入口；E/F/G/H 等保持业务节点身份。
2. A/B 的 Nginx 代理 E:3000，C/D 代理 F:8080；用户请求路径完全不经过主控。
3. A/B/C/D 任意入口指向 G/H upstream，验证负载分发与节点不可达时的真实行为；不得把单次成功误报为高可用。
4. Nginx → 目标节点的网络不通，但 Agent 仍在线：预检必须标记不可达，不得直接宣称可部署。
5. 原有 Nginx 站点、第三方配置、80/443 端口冲突：不擅自覆盖。明确授权的受管目录接入后只操作自有配置。
6. `nginx -t` 失败时禁止重载；重载后业务探测失败时尽力恢复上一受管版本，并保留真实故障状态。
7. 同域名多入口单独签发各自证书、独立本地私钥；主控不存私钥，Agent 不持 Cloudflare Token。
8. 变更 4 个入口：先 1 台、再剩余 3 台；首台失败后不启动后续批次，操作记录和未知状态遵守 TASK-001。
9. 插件与 AI 试图绕过审批、向任意主控/云元数据地址设置 upstream、注入 Nginx 片段等行为被服务端拒绝。
10. 模拟 HTTPS 回源错误、DNS 记录冲突、Cloudflare API 出错；不给出虚假的“域名已全球生效”状态。
11. 主控 Go + SQLite 和 1c1g Agent 资源开销需以真实测试确定，配置部署过程不能无界吞吐或无限缓存日志。

## 10. 非目标与后续拓展

- **不做**主控承载网站流量或自带流量转发代理服务。
- **不强制安装 Nginx**；可后续提供单独受控安装操作，但本 ADR 第一版重点是**管理已安装 Nginx**。
- **不包含**网站静态产物上传发布、Docker Compose 构建或 Git CI/CD 自动构建；这些可以另设“网站发布/容器管理”插件或 ADR。
- **不包含**内网穿透/VPN 自动部署、全局主动高可用故障切换、付费 NGINX Plus 专属功能、任意 TCP/UDP 代理或自动迁移现有复杂配置。
- **不承诺** DNS 多 A 记录提供实时故障切换、回滚 DNS 全球缓存、回滚数据库或第三方外部业务副作用。

## 11. 参考资料

- Nginx 官方命令行与配置测试：<https://nginx.org/en/docs/switches.html>
- NGINX HTTP upstream 健康检查与开源/Plus 区别：<https://docs.nginx.com/nginx/admin-guide/load-balancer/http-health-check/>
- Cloudflare DNS 多记录与轮询：<https://developers.cloudflare.com/dns/manage-dns-records/how-to/round-robin-dns/>
- Cloudflare Load Balancing 文档：<https://developers.cloudflare.com/load-balancing/>
- 本项目基线：Server Fleet v1.0 的 ADR-WC-001、ADR-PRIV-001、ADR-TASK-001、ADR-PLUGIN-001、ADR-PKI-OPS-001。

## 12. 审核与批准记录

- **2026-10-26**：项目所有者指出 v0.1 “主控管理 Caddy/本地网站发布”并非目标；实际要求为“4 台现有 Nginx 入口反代其他受管节点服务”。
- **2026-10-26**：项目所有者对修订后的 DEPLOY-D1～D8 表示“认可”。v0.2 将该认可记为**方向评审通过**；未将其解读为要求立即更新 v1.0 总文档。
- **2026-10-26 正式批准**：项目所有者明确批准 **DEPLOY-D1～D8**，同意将 v0.2 标记为 **Accepted**，并将决策同步至 v1.1 的 PRD / 架构 / API；v0.1 与 v0.2 原审核草案、v1.0 及此前历史版本均原样保留。
- **实施状态**：**架构决策已批准、功能尚未编码与验收**；本文件的 API 路径、JSON 字段、健康探测频率和具体受管 include 目录仍是供开发阶段冻结的契约草案。
