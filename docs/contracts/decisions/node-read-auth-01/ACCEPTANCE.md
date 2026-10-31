# READ-AUTH-DESIGN-01 · 身份/凭据负向验收计划

状态：**PROPOSED；以下所有未来运行项均UNTESTED**。本轮只核对设计来源和文档，没有实现/运行登录、Cookie、签发、HTTP、存储、代理或浏览器认证测试。规则、来源R01–R22/E01–E03见[DESIGN](DESIGN.md)，命令见[HANDOFF](HANDOFF.md)。A1+B1方向确认不等于下列具体期望已获批。

## 执行方法与证据边界

后续另获实现授权再执行：L1＝真实auth验证器+可控时间/随机源/持久来源；L2＝真实Controller入口及N03-A组合HTTP测试；L3＝真实存储事务/故障/并发；L4＝本地HTTPS真实浏览器/代理与Web首提交、网络、缓存观测；L5＝真实旧客户端兼容。合成账号/秘密只在获批隔离测试生成，不使用真实凭据；mock只能控制外部来源，不能替换待验收的认证/策略或将默认放行装入生产。

每例保留精确代码/配置HEAD、输入类别（秘密仅用替代标记）、服务端提交顺序、来源调用次数、身份/授权世代、响应首部/完整正文、首个Web提交/网络记录和脱敏日志。使用屏障和显式时钟控制Tauth/Tpolicy/Tgate/Tcommit/Tr，不靠sleep猜顺序；不能仅以最终DOM、源码中HttpOnly字符串、最终err或schema通过作证。未实现来源/闸门必须记录缺失，不能在测试替身里制造已实现保证。

## 身份、兼容与预检

| ID / 旧安全缺口 | 输入及可控时序 | 明确预期/需观察结果 | 未来层次/责任；本轮状态 |
|---|---|---|---|
| AU01 / A01,A10 | 空库存；无凭据、伪造subject/角色/ReadGrants、仅已知用户名，各独立请求 | 都无可信身份；策略0/库存0，不返回成功空列表；客户端claims不进入可信结果 | L1/L2，auth+HTTP；UNTESTED |
| AU02 / A01 | 有效A会话，分别在query/body/header传B及更大资源集合；A/B节点集合互斥；账户改名/删除后重建同名 | 主体始终可信目录ID；改名不变ID、重建不继承旧凭据/授权；客户端输入不能变B/扩大权限；只对A授权后输出筛选/计数 | L1/L2/L3/L4，auth+nodes/Web；UNTESTED |
| AU03 / A02 | 单Cookie、单受限Bearer的有效样例；无Cookie的旧非浏览器调用 | 各依批准范围得到相同授权语义；Bearer仍按当前策略∩签发上限；不需Cookie/CSRF，不能调用新写操作 | L1/L2/L5，auth+契约；UNTESTED |
| AU04 / A02 | Cookie+Bearer：A/A、A/B、有效/过期、有效/撤销、空值/有效，两种次序 | 全部歧义拒绝，无fallback；身份策略/库存0次；无原始凭据回显 | L1/L2/L5，auth+HTTP；UNTESTED |
| AU05 / A02 | 重复Authorization、逗号合并、重复Cookie、旧sf_session+新名、仅旧名、未知scheme | 歧义或不支持的固定分类；解析次序不影响结果；普通无关Cookie不误判为认证 | L1/L2及真实代理，auth+HTTP；UNTESTED |
| AU06 / A01,A03 | 伪造JWT、自带roles/aud/exp、Agent注册Token、AI sessionId/claims或插件秘密充当Bearer | 均不能创建管理用户身份；JWT标签不成为签名/角色信任；不得触发Agent/AI执行能力 | L1/L2，auth+安全；UNTESTED |
| AU07 / A10 | 已认证A、可信策略有A空授权；库存分别空/非空 | 都先prepare；策略1/库存1；只有授权后空集合，不泄露隐藏节点或总数；区别AU01 | L2，auth+nodes；UNTESTED；离线同类仅历史PASS |
| AU08 / A10 | 已认证A但策略无A；nil/坏/悬空授权策略；reader返回错误/敏感标记 | 逐例固定策略失败、库存0、不成功空；整体未知主体不是凭据缺失；不回显来源原文 | L1/L2/L3，auth+策略来源；UNTESTED |
| AU09 / A11,A12 | 第1请求读策略P允许N；撤权提交P2；第2请求用同凭据同连接，尝试提交第1scope | 每请求重新认证/准备；第二次观察P2，不能注入旧scope或reader缓存；同次仅prepare1次 | L2/L3，Controller+auth；UNTESTED；离线快照仅历史PASS |
| AU10 / A08,A14 | 库存先有可见N再返回部分数据+错；混合拒绝/故障；坏ID/重复ID（含隐藏节点） | 整体无列表/部分计数；后台坏ID不得映射为“客户端参数400”；错误阶段/来源可区分 | L2，nodes+HTTP；UNTESTED；离线错误无部分结果仅历史PASS |

## 生命周期、来源及撤权窗口

| ID / 旧安全缺口 | 输入及可控时序 | 明确预期/需观察结果 | 未来层次/责任；本轮状态 |
|---|---|---|---|
| AU11 / A03 | 登录成功→空闲期限前/等于/之后；绝对期限前/等于/之后；篡改客户端时钟/过期显示 | 到达任一服务端边界即拒绝；活动不延长绝对期限；清理未跑也不能放行 | L1/L3，auth；UNTESTED |
| AU12 / A03 | 活跃请求更新lastSeen与退出并发；退出存储提交后重放旧Cookie，换连接再试 | 更新不可复活撤销状态；新Tauth拒绝、库存0；仅清Cookie或客户端显示退出不算通过 | L1/L2/L3，auth+存储；UNTESTED |
| AU13 / A03 | 单会话撤销A1；A另有A2，B尝试撤A1；使用公开管理ID登录 | 所有权检查拒绝B；A1失效、A2按单撤销语义保留；管理ID不能认证；无原SID查询 | L1/L2/L3，auth；UNTESTED |
| AU14 / A03 | 停用、密码修改/恢复、角色降权，各在权威提交后用旧会话/API Token重试 | epoch/状态跨所有连接生效；全部旧凭据拒绝，不靠缓存清理；重新登录取当前权限 | L1/L2/L3，auth+存储；UNTESTED |
| AU15 / A03,A11 | 仅撤某节点权，主体继续启用；新请求；另测重新授予、Token上限外新增节点 | 会话可有效而节点不可见；零授权是有效空；重授仅在旧Token原上限内恢复，新增上限外仍拒绝 | L1/L2/L3，auth策略来源；UNTESTED |
| AU16 / A03 | 登录/轮换时客户端指定SID；旧秘密与新秘密并发；丢失新交付响应 | 服务器独立生成；原子换新并撤旧，无宽限；旧值失败不自动恢复；普通GET不Set-Cookie覆盖新会话 | L1/L2/L3/L4，auth；UNTESTED |
| AU17 / A03 | 受限Bearer到期/撤销/轮换；伪造签发subject/限额、超当前权签发，交付失败重取 | 不越过可信签发人/当前权；旧值失效，无自动刷新/重取秘密；失败需撤销重签；秘密不进浏览器 | L1/L2/L3及受控签发入口，auth；UNTESTED |
| AU18 / A01,A03 | 身份/会话来源不可用、坏记录、随机源失败、时钟异常、旧快照缓存；重启/回滚备份 | 不签发、不用旧允许；内部unavailable与invalid区分；撤销epoch/单用标记不回退或恢复后全失效 | L1/L3，auth+存储；UNTESTED |
| AU19 / A13 | Tr在Tauth之前；或资源撤权在Tpolicy之前 | 旧凭据策略/库存0；资源撤权后新范围不含N。按权威事务顺序证明，非不同机器时间推测 | L2/L3，auth+HTTP；UNTESTED |
| AU20 / A13 | Tauth/Tpolicy之后，Tr在Tgate之前；再让来源超时/取消/不可用 | 若实现推荐闸门，观察到状态/世代变化时丢缓冲、无成功/部分字节；策略仍只prepare1次 | L2/L3，HTTP+存储；UNTESTED，当前无闸门 |
| AU21 / A13 | 在Tgate后、首字节前/写入中提交撤权 | 记录实际窗口及已发字节，不能声称撤回；须按最终D07批准边界验收，未批准该窗口不得标PASS | L2/L3/L4，HTTP+安全；UNTESTED，决定PENDING |
| AU22 / A09 | 在认证/策略/库存/序列化前/提交前后逐阶段取消或超时，包装错误带敏感标记 | 已观察取消不成允许/成功空；来源ctx传播、部分输出边界有记录；不合作来源不能伪称可强杀 | L1/L2/L3，auth+HTTP；UNTESTED |

## Cookie、初始化、浏览器及敏感信息

| ID / 旧安全缺口 | 输入及可控时序 | 明确预期/需观察结果 | 未来层次/责任；本轮状态 |
|---|---|---|---|
| AU23 / A02 | HTTPS登录及浏览器重载/恢复；JS读cookie/响应、持久化扫描；查会话过期；同源补取CSRF | 推荐__Host名及Secure/HttpOnly/Lax/Path=/、无Domain；SID不可读且不在JSON/存储；服务端能安全补发绑定CSRF、它只在JS内存且不能登录；浏览器关闭不代替服务器到期 | L2/L4，auth+Web；UNTESTED |
| AU24 / A02 | 敏感GET：同源Origin；缺Origin同源Referer；仅same-origin Metadata；两者皆缺；Origin:null/同站恶意子域/跨源冲突/顶层API导航 | 前三合法组合按§5；其余拒绝无数据；SameSite不会替代同源判定；无CSRF的合法GET仍可读取且无业务写副作用 | L2/L4，HTTP+Web；UNTESTED |
| AU25 / A02 | Cookie变更：正确/缺失/错误/旧会话CSRF；Origin错/null/都缺；伪同站域，跨会话重放 | 仅同源（或精确Referer回退）+当前会话Token同时成立才允许；Token单独正确不能覆盖坏/缺Origin来源；无副作用失败 | L1/L2/L4，auth+HTTP；UNTESTED |
| AU26 / A02 | 登录/初始化：跨源表单、simple content type、缺自定义标记、无Origin/Referer、攻击者指定会话；同源有效输入 | 拒绝login CSRF/固定；只有同源受保护JSON进入验证；未认证不被误要求已有会话CSRF；成功后新会话 | L2/L4，auth+Web；UNTESTED |
| AU27 / A02 | 伪Host/Forwarded/X-Forwarded-Host、直接连接Controller绕代理；缺固定origin/未知代理；凭据CORS跨源 | 不信伪头/用户身份头，失败关闭；无通配带凭据CORS；TLS终止/信任入口需真实配置证据 | L2/L4及部署验证，HTTP+部署；UNTESTED |
| AU28 / A01,A03 | 未初始化空库远程请求；setup过期/重放/两并发；初始化提交故障/重启、之后丢用户表 | 无本机一次能力无法抢管理员；并发1次原子成功；故障无半初始化、重启不重新开放；status不返秘密，先完成再普通登录 | L1/L2/L3，auth+安装；UNTESTED |
| AU29 / A01,A03 | 管理员凭据丢失；尝试匿名重置/用Agent token、CA/恢复私钥上传普通API | 拒绝；受控恢复验证操作人并撤旧epoch；未获PRIV/相关恢复条款不得声称恢复已验收 | L3及授权本机流程，auth+安全；UNTESTED/原文PENDING |
| AU30 / A07 | 各来源/密码/Token/CSRF/坏ID注入不同合成敏感标记，触发全部失败阶段 | 响应/Controller/代理/APM/trace/UI/审计无秘密、原异常链/路径；只存固定类别、受控关联标识；未知用户名/密码错/停用外观一致 | L1/L2/L4及日志审查，安全+各owner；UNTESTED |
| AU31 / A15,A16 | A已显示→退出/到期反馈/撤销反馈→B新会话；另测直接带旧Cookie登录B、切换事务失败；旧A请求迟到成功/失败；同key失效恢复 | 切换前清A显示，成功原子撤A/建B及换CSRF、失败不发B；旧结果/旧秘密不覆盖或认证B；可信适配世代变化，首提交无A行/详情/计数；source.key无秘密且不能向服务器认证 | L2/L3/L4，auth+HTTP+Web；UNTESTED；W05/W06局部历史不可转移 |
| AU32 / A15 | 多标签退出；标签休眠后恢复、pageshow/BFCache、网络断开；跨用户浏览器后退 | 本地已知退出立即清除，跨标签通知只清理不认证；恢复先清旧显示再校验；明确远程未知撤权无法立刻擦除已显示数据 | L4，Web+安全；UNTESTED，残余窗口需共审 |
| AU33 / A15 | 有效/错误/CSRF交付/退出响应经代理、缓存、SW，A→B复用请求URL | no-store实际头与缓存行为有证据；不持久化/跨会话复用授权数据，不能只检查Vary；未有真实缓存链标UNTESTED | L2/L4，HTTP+Web+部署；UNTESTED |
| AU34 / A04 | nodes.read成功后访问任务/AI/root/插件；客户端声明full/all nodes | 成功读取不授执行或AI目录；受限Bearer仅所批操作；原AI交集/人工审批不因本设计放宽 | L1/L2及以后相关能力审查，安全；UNTESTED，A1不扩实现 |

## 可引用的历史证据与接线门槛

AUTH-01B [复审§接收/边界](../../../development/reviews/AUTH-01B-REVIEW.md#L7-L19)已证明真实离线prepare、零授权、源复制、新prepare观察撤权及旧scope快照限制；N03-A [复审](../../../development/reviews/N03-A-REVIEW.md#L7-L21)证明先预检、失败库存0次、同次范围、无部分结果、坏后台ID与取消的离线语义。本轮未重新跑这些测试，不给AU项标新PASS。

W06-R1 [复审及证据范围](../../../development/reviews/W06-R1-REVIEW.md#L5-L39)已接收fixture局部隔离/浏览器补验，修正旧安全证据包的历史W05状态；不是新的legacy列表、真实会话/HTTP/cache或跨主体保证。本轮未运行任何浏览器/真实网络。r03的静态结果也不能作为Cookie/Bearer认证证据。

接线前须由总控组合审查设计和兼容差异，核验适用PRIV等来源，落实真实凭据/当前来源及请求组合后再执行相应AU项；D07窗口需明确批准，未实现层级保持UNTESTED。对L5外部旧签发方/JWT客户端没有盘点证据，不能宣称不透明Bearer兼容所有旧凭据。A1+B1不再待选，参数/具体操作与HTTP媒体仍待审，不自行关闭D项或宣布G2冻结。
