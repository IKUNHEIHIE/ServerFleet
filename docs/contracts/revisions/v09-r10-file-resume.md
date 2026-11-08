# R10：正式Web持久暂存续传候选

V09-CLOUD-BROWSER-RESUME-01 / chatgpt-takeover-01；起点main f1d715b。主控单写接口/适配/产品差异及组合审查。原三候选、R8/R9、69原文、Proto/gen、根锁/CI和迁移SQL不改；G2/Runtime/发布关卡不关闭。

## 产品行为

创建上传后只为可计算完整SHA-256的文件保存待核验记录；账号+节点隔离，七天内有效/最多32项，不保存字节、密钥/CSRF、偏移、任务状态或成功。存储失败准确提示。用户重新选择原文件，名称/大小/完整摘要先匹配，再GET核对服务端目标、原摘要、大小和整数偏移。最多1MiB分块，精确核对每个字节回执；异常/409/网络错误不自动重试。

已dispatching/completed/failed/cancelled/未知的任务只读回，不重复complete或另造任务。所有请求仍走Controller当前授权、Origin/CSRF、统一任务/审批/审计。每个await后和写入前复核当前scope/权限；旧节点/身份响应不得写新作用域。支持只读回状态；移除本地记录不会取消任务。

## API及兼容性

独立files-resume-candidate.yaml基于R8语义另立。GET显式resumeBinding=true才增加只读destinationPath和可省略sha256Expected；缺省/false及PUT保留原投影，兼容R8的additionalProperties=false。重复/无效布尔参数在身份/授权后返回400。均来自持久绑定，只在owner/当前files.write通过后返回，无新执行权限/DB/Proto变化。旧客户端默认响应不变；新客户端遇旧Controller或没有原摘要则拒绝续传，不从缓存推测目标。原创建/offset/complete/202与三冲突策略语义不改。

## 验收及回退

集中核对真实HTTP绑定/拒绝、缓存隔离/不缓存成功、重新挂载后的续传、分块、错文件/目标/摘要/offset、403/断线/409、双击、换节点/身份和已受理不重放。DOM夹具不冒称真实浏览器/双Agent验收。Controller暂存与Agent执行续传、外部供应商、G2/发布准入分别记录。回退Web不改变持久任务，UNKNOWN不得因回退重放。

待确认任务的sessionStorage也改为账号+节点键；不能证明owner的旧匿名缓存不自动迁移，旧任务仍可在统一任务列表按当前权限查看，不取消或重放。原403/乱序/切节点回归只更新夹具键以包含已登录owner，原断言与触发条件保持。
