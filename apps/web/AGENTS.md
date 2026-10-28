# Web 会话边界

仅修改任务分配的 src 子目录。共享 App 入口、组件样式、package.json/锁文件需集成协调。只调用 Controller REST/SSE；禁止 Agent WebSocket、数据库、真实节点或密钥。候选 G2 通过可替换适配层使用；未冻结时只用测试 Mock，不扩展业务实现。生产不得导入 test/mock/fixture。新增组件要有可访问名称、加载/空/失败状态及类型检查。验证：根目录 `npm run check:web` 与 `npm run check:policy`。
