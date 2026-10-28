# 可选插件业务包

第一版仅批准 Xray 节点管理器，尚无可安装发行物。此目录留给独立按需安装的插件包，不得由核心 Go 二进制导入。`internal/plugins` 仅负责平台能力；WASM Runtime、Host ABI、签名与配额仍待 PoC 和审查。UI 仅可用 JSON/YAML 声明式组件，禁止浏览器脚本和 Agent 直连。
