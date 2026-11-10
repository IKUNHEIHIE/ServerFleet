# R27 可运行核心代码交付

用户分工：本轮继续补齐完整可用代码，用户自行联调与找bug。V09-CLOUD-CODE-COMPLETE-01 / chatgpt-resume-20261110，单会话。源码起点9e96f47；新增核心源码检查点33c2edc，tree=69d092e342a1f327cbf5d3b96883b3c889482731。环境维护曾删除工作目录，已按Git SHA恢复143项差异与16个原提交，并将新源码先保存远端；不把清理后的旧本地结果冒称当前检查。

## 已交付

五项既有核心主路径（普通/Root执行、Agent文件恢复、Agent灰度更新、Controller独立parent自更新、恢复fence/激活）保持。新增本机部署provision、管理员初始化令牌/刷新、离线发行keygen/sign、安全注册令牌文件、HTTPS与内部Agent身份分别配置，以及无节点拒绝式文件注册表。部署说明和七种模板进入构建脚本；没有新增正式Web页面/业务API字段或改写历史SQL/40项迁移、Proto、根锁和CI。

## 实际必要检查

| 检查 | 当前结果 |
|---|---|
| 固定Go1.27.3 `go build -o ... ./apps/controller ./apps/agent` | PASS，Linux amd64两个入口编译成功 |
| 同源 `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ...` | PASS，ARM64两个入口交叉编译成功 |
| `go vet ./apps/controller/... ./apps/agent/... ./internal/files` | PASS |
| `node --check scripts/build-release.mjs` | PASS |
| 七种JSON配置示例解析 / `git diff --check` | PASS / PASS |
| 新增测试或本地联调套件 | **未新增、未执行**；遵照用户最新分工 |
| 正式Web重建/完整发行包重建、实际业务/Root/故障/双机/供应商 | 本轮未运行，交用户按部署说明验证 |

原R25/R26的测试和证据保留，但不用于证明本轮新代码已联调成功。当前编译产物仅作为编译检查，不视为签名发行或已部署服务。准备入口未实际运行、未生成本轮真实发行私钥/注册令牌、未执行sudo或访问生产/外部供应商。

## 使用入口与未混淆范围

见仓库deployment/README.md，从干净源码构建，按示例配置普通用户的provision/start，再在正式界面初始化管理员/注册节点/创建与审批任务。不要直接运行无参数M0 scaffold，也不要用fixturehost或Vite开发服务代替正式Controller的-web-dir。

核心源码和手工验收分开：五项等待用户联调不再列为缺少实现；未知结果、独立审批/签名/Root与恢复边界保持。较低优先级和完整产品/Runtime准入、G2/发布、物理双机/ARM实跑/硬1C1G/真实供应商仍不由本轮宣称关闭。
