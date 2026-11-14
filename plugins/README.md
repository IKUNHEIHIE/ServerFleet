# 可选插件业务包

首版唯一批准的业务插件为 Xray 节点管理器。plugins/xray-manager 已有业务模型/隔离组件与声明式页面相关实现，**生产 PluginRuntime/XrayTaskHost 装配与准入尚未完成**，不能当作完整可安装运行的插件。

核心 Go 二进制不得导入可选插件业务，核心 Release 不打包插件。internal/plugins 提供平台元数据/生命周期与受控端口；WASM/Host/配额/运行时仍须独立准入，PoC 不代表生产许可。UI 仅声明式组件，禁止注入浏览器脚本或直连 Agent。

最新状态见 [CURRENT-STATE](../docs/development/CURRENT-STATE.md)，不要依据旧工作包推断已交付可运行插件。
