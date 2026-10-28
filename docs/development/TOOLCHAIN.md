# 工具与真实检查

本次固定版本：Go 1.27.3；Node 24.22.1；npm 11.19.1（Node 随附）；React 19.3.1；TypeScript 7.0.2；Vite 8.3.5；Tailwind 4.3.3；Vitest 5.0.5；Buf 1.75.0；Redocly 2.72.0；Protobuf/Go 1.36.12；staticcheck 0.8.2；govulncheck 1.9.0；actionlint 1.7.12。精确 npm 包版本见 manifests/lockfile，Go 工具见 `scripts/tool-versions.json`。shadcn 是源码组件模式，已设 CSS token、alias、components.json 和基础 Badge；未引入完整业务 UI。

初次安装：

```sh
npm ci
npm run tools:install
npm run check
npm run check:security
```

Go/Node 在系统已安装对应版本时可直接使用。当前 Windows 可 `. ./scripts/env.ps1` 使用项目 `.tools`，不会修改系统配置。该目录未进入 Git；新 Worktree/新机器从官方安装同版本或由负责人提供现有工具的只读路径。本次 Go/Node ZIP 已核对官方 SHA256，下载缓存保留在 `.tools/downloads`。

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

CI：Ubuntu 24.04 两个 job（基础检查与依赖扫描），固定 Action commit、只读权限、npm/Go cache、失败日志保留 7 天，无生产密钥/真实 VPS/付费服务依赖；Linux CI 额外 race 和双架构 Agent cross-build。远程仓库尚未创建或推送，CI YAML 仅静态验证，GitHub runner 尚未运行。
