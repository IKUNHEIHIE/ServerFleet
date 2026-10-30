# BYOK文本Provider候选（默认不接线）

`NewOpenAITextProvider(*auth.CredentialService, CredentialResolver)`提供一个可选的原生HTTPS Responses适配，不新增根SDK/框架/依赖。不是选择Eino/WASM生产运行时，也不是全部模型的通用兼容承诺。模型无默认值，由当前可信Profile指定精确版本；响应model须完全一致，返回不同snapshot/alias时拒绝，不能默默选另一个模型。main未注册它，无模型/过滤/正式PRIV政策时仍拒绝AI调用，不影响其他核心模块。

Generate冻结Subject/Session/Message/Operation/scopeRevision/Profile及成对脱敏历史，域分离SHA256计划摘要；Resolver收到隔离副本，须核对当前enabled owner配置及持久generating记录。返回CredentialOperation必须精确同subject/credentialRef/operation/digest、Module=ai、Source=ai、Action=use、当前credential revision。auth的独立gate还须核同一次推理与授权，不能把Resolver返回当授权。UseForService先同Tx落永久使用fence与审计，再借用ai_api_key；结束核当前凭据revision/撤销/期限及gate，失败不重放。不能将任意HTTP/model请求构造成通用解密入口。

R26已提供上述AI专用当前闸`ConversationService.CheckCredentialOperation`，必须组合进真实凭据gate；私有、回调后关闭的观察绑定当前持久generating请求，详见[AI-GENERATION-ADMISSION.md](AI-GENERATION-ADMISSION.md)。真实本机串联测试已使用它。独立HTTP协议负向测试仍使用明确测试gate，以分清失败层次。

唯一目标固定 `https://api.openai.com/v1/responses`；客户端TLS≥1.3、系统证书校验、无环境代理、禁止跳转、新HTTP/1连接、30秒总期限/5秒连接与TLS/15秒响应头、头32KiB；不设置Idempotency-Key、不用SDK重试，不对429/5xx/超时重试。请求只含明确model、已脱敏user/assistant历史、tools=[]/tool_choice=none、store=false、stream=false、background=false、truncation=disabled及8192输出token上限。之前保存的回答作为final_answer历史，无system/developer/tool输入、任意URL、附件、远端conversation或previous_response_id。秘密只入认证头，不入请求JSON/日志/模型上下文。请求后删除认证头及尽力清借用/请求/原始响应byte缓冲；Go string/运行时副本不能保证彻底擦除内存，不作此承诺。

最多1MiB JSON响应、深度16/100k节点/64 output items、每消息64内容片段、文本128000字节/32000字符。重复/大小写别名、非JSON/截断/非completed/存储开启/模型不符/error/incomplete_details拒绝；只接受completed assistant output_text最终回答，不支持的refusal/工具调用等关闭失败。reasoning项不输出。API密钥回显拒绝。文本先留在受控回调内，auth最终gate/凭据/审计都完成后只emit一次，ConversationService再做当前scope/Profile/Filter核对及回答同Tx落盘；不能从模型文本生成权威taskIds或执行Shell。网络调用已发出后撤权无法“撤回请求”，只会扣留输出并保留UNKNOWN、禁止自动重试。

依据已实际读取的[官方文本生成说明](https://developers.openai.com/api/docs/guides/text)、[会话状态说明](https://developers.openai.com/api/docs/guides/conversation-state)和Context7返回的[Responses API字段文档](https://developers.openai.com/api/reference/resources/responses/methods/create)。store=false只请求不保存可检索Response，不能据此承诺供应商零保留/零滥用监控或替代完整PRIV；外发授权、模型配置管理、预算/配额、生产脱敏策略和凭据真实准入仍待接收。未调用真实OpenAI或读取任何本机真实Key。

测试使用本机实际TLS服务器、真实auth初始化/凭据密文/使用fence及合成Key，存储/模型/权限配置部分为明确double。覆盖正确HTTP头/纯文本请求、实际Conversation→凭据→TLS→回答、重复成功/未知不再请求、429/500/超时/跳转/拒绝/工具/畸形或超量JSON/密钥回显、请求途中真实撤销凭据扣留输出；不是外部模型质量/可用性或实际BYOK验收。首次取消fixture未读取body且只等服务端ctx，清理阻塞触发测试60秒FAIL；改为300ms有界测试响应等待，不放宽生产期限或重试。首次staticcheck要求Redirect使用常量，修正后PASS。
