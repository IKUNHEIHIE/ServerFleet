# 工具与真实检查

本次固定版本：Go 1.27.3；Node 24.22.1；npm 11（本轮记录 11.19.1，须实测版本）；React 19.3.1；TypeScript 7.0.2；Vite 8.3.5；Tailwind 4.3.3；Vitest 5.0.5；Buf 1.75.0；Redocly 2.72.0；Protobuf/Go 1.36.12；staticcheck 0.8.2；govulncheck 1.9.0；actionlint 1.7.12。精确 npm 包版本见 manifests/lockfile，Go 工具见 `scripts/tool-versions.json`。shadcn 使用源码组件模式；正式管理台与多业务页面已有实现，具体接收范围和缺口见 README 与当前进度，不能从组件清单推断完整产品验收。

初次安装：

```sh
npm ci
npm run tools:install
npm run check
npm run check:security
```

Go/Node 在系统已安装对应版本时可直接使用，先记录实际版本。Windows 历史环境可 `. ./scripts/env.ps1` 使用项目 `.tools`；这些工具、下载缓存、node_modules、数据库和签名材料未随 Git 迁移。各云端 Linux 环境从官方准备固定版本并核验校验材料，不调用 WSL 包装器或复用 Windows 绝对路径/loopback代理。普通用户及0700私有目录是相关真实用例前提；仅UID0的容器不能冒称已经满足，原始FAIL/SKIP须保留并说明环境限制。

| 命令 | 实际内容 |
|---|---|
| `npm run check:policy` | 原资料哈希、脚本负向测试、分支/提交/PR 标题、Go/Web 依赖边界、actionlint CI 配置验证 |
| `npm run check:go` | gofmt、module verify/tidy diff、双入口构建、Go 单元测试、vet/staticcheck |
| `npm run check:web` | TypeScript strict、Vitest/JUnit、Vite production build |
| `npm run check:contracts` | YAML/ref/ID/路径参数、Redocly、Buf 编译/lint/格式/兼容、生成一致性、Go 往返和负向变异 |
| `npm run generate` | 用本地 protoc-gen-go 从修订候选生成 `gen/go` |
| `npm run check:git` | Git 空白差异检查 |
| `npm run check:security` | npm audit（high/critical 阻断）+ govulncheck |
| `node scripts/diagnose-original-proto.mjs` | 只读诊断原 Proto：编译、lint、Go 生成，记录原候选缺陷 |
| `node scripts/runtime-smoke.mjs` | 编译后实际启动本地 Controller/Agent 并关闭进程 |
| `node scripts/worktree-smoke.mjs` | 创建自建测试 Worktree、验证保护并清理；只供集成负责人运行 |

`npm run check` 顺序运行基础五组，每组失败会记 FAIL 并返回非零；安全扫描独立，便于无网络环境明确报告 BLOCKED，不伪装通过。所有输出和分组 JSON 写入 `reports/`，前端 JUnit 写 `reports/web-junit.xml`。Go 测试 JSON 在 `reports/go-tests.log`。

Buf 自带 Protobuf 编译器，不另外依赖系统 protoc。Windows Buf 格式检查需要 GNU diff；脚本寻找 Git for Windows 自带 `usr/bin/diff.exe`，未发现时应修复工具环境，不跳过格式。npm 新版安装脚本策略可能提示 Buf postinstall 未批准；可选平台二进制已由锁文件安装，本次命令实测正常，不全局放开依赖脚本。

CI：Ubuntu 24.04 两个 job（基础检查与依赖扫描），固定 Action commit、只读权限、npm/Go cache、失败日志保留 7 天，无生产密钥/真实 VPS/付费服务依赖；Linux CI 额外 race 和双架构 Agent cross-build。仓库已为 GitHub 私有仓库，真实 runner 已运行；产品基线51e0ee57的run41615351426两个job均success，Go顶层902 PASS/7 SKIP/0 FAIL、Web348 PASS。详见[接收报告](reviews/V09-CLOUD-R1-REVIEW.md)，不把交叉构建当ARM实际运行或完整发布认证。

## 提交和CI记录

实际合并提交的标题也须满足Conventional Commits。主控通过GitHub合并时显式提供合规标题；默认的“Merge pull request …”会被当前main的git-policy拒绝，不应改规则或重写历史来隐藏失败。

当前工作流在PR opened/synchronize/reopened/edited和main/integration分支push触发，同ref新运行会替换旧运行。最终提交后优先用PR评论补充CI结果，减少修改说明/追加记录提交引发的重跑。被取消的运行不是PASS；审查只接收精确head的实际成功证据，合并使用expected SHA保护。

Redocly支持官方环境开关 `REDOCLY_TELEMETRY=off`，需要关闭遥测时以该开关运行相同检查，不关闭lint规则。公共接口、新库/SDK/CLI文档继续遵守AGENTS的Context7要求；工具或网络前提不足准确记录受影响项，不降版本或伪造成功。
