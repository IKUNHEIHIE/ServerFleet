# 工具链与验证

更新：2026-11-14。当前实现/发布证据见 [CURRENT-STATE](CURRENT-STATE.md)，历史运行记录不是新 head 的通过证据。

## 版本来源

| 工具 | 当前要求 | 仓库来源 |
|---|---|---|
| Go | 1.27.4 | go.mod、scripts/tool-versions.json |
| Node | 24.22.1 | .node-version、scripts/tool-versions.json |
| npm | 11.x；未固定单一补丁版本 | package.json engines >=11 <12 |
| Go 工具 | protoc-gen-go 1.36.12、staticcheck 0.8.2、govulncheck 1.9.0、actionlint 1.7.12 | scripts/tool-versions.json |
| Web/契约依赖 | manifests 与 lockfile 中的精确版本 | 根及 apps/web/package.json、package-lock.json |

不要从历史报告复制 Go 1.27.3 或某次实际 npm 补丁版本。shadcn 为源码组件模式，不按旧组件清单推断产品完成。

## 代码检查

在对应版本的干净任务目录执行并记录实际工具版本：

```sh
npm ci
npm run tools:install
npm run check
npm run check:security
```

| 命令 | 范围 |
|---|---|
| check:policy | 来源哈希、脚本负向、依赖/标题/工作规则、CI 配置 |
| check:go | 格式、模块一致性、入口构建、测试、vet/staticcheck |
| check:web | TypeScript、Vitest、生产构建 |
| check:contracts | 契约/生成一致性与 API 索引漂移 |
| check:git | git diff --check 空白差异检查；分支/标题规则归 policy |
| check:security | 独立依赖/漏洞检查；不以普通 check 代替 |
| generate | 使用固定 protoc-gen-go 从修订 Proto 生成 gen/go，禁止手写输出 |

具体执行项目以 scripts/check.mjs 和工作流为准；CI 的 race、双架构、安装向导回归等不能仅凭本地普通 check 推断已运行。真实 Nginx 等用例需相应环境，FAIL/SKIP/BLOCKED/UNTESTED 分别记录。

npm run check:go 构建入口后，向导专项可运行：

```sh
SF_SETUP_TEST_CONTROLLER=reports/controller python3 -m unittest discover -s deployment -p test_setup.py -v
```

工具、下载缓存、node_modules、数据库和签名材料不随 Git 迁移。普通用户/0700 目录等测试前提须真实满足；仅 root 容器、交叉构建不冒称普通用户或物理 ARM 验收。reports 中可重现日志按 .gitignore 管理，必要证据另记录精确提交。

## 纯文档验证

按用户要求，纯 Markdown/AGENTS 整理不运行完整 CI。检查修改范围、相对链接、来源/版本/状态、历史资料字节不变、git diff --check；提交及合并标题带 [skip ci]。若影响代码、配置、契约或生成结果，按实际影响恢复对应检查，不把未运行登记 PASS。

## 发布

当前已发布 v0.9.2；源码构建命令见根 README，固定版本/sequence 与离线签名流程见 deployment。文档提交不自动发版、不重写旧 tag/资产，不以旧私有仓库限制描述当前公有仓库。具体成功回执统一见 CURRENT-STATE。
