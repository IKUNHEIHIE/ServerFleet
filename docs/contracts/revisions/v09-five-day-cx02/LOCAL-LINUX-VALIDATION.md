# 本机Linux原生验证（不是安装/生产准入）

2026-10-30，codex-02。只读环境核验发现本机既有Ubuntu WSL2，可在普通UID1000下执行本任务自己构建的测试；不是外部VPS/云/其他任务VM。实际Ubuntu24.04.4 LTS、Linux6.18.33.2-microsoft-standard-WSL2、x86_64。此前“Windows实验不等于Linux证据”的限制保留在历史记录；这里新增明确的Linux原生组件证据，不推定arm64/独立安装/1C1G通过。

使用固定Windows Go1.27.3、GOOS=linux/GOARCH=amd64/CGO_ENABLED=0交叉编译12个测试二进制，再以`wsl.exe --distribution Ubuntu --exec`原生运行。没有Linux Go工具链安装、sudo、系统服务/设置修改、远程连接或真实秘密；fixture的/tmp目录由测试自己创建/清理。测试目录明确0700，密钥/记录0600。Node runner限时/限输出、保存原始stdout/stderr及规范化日志、精确二进制/源文件/依赖哈希，前后检查源码、HEAD和受保护根文件未变。Windows诊断UTF16与Linux UTF8混合日志分段处理并保留原始字节。

复现：`node tests/poc/v09/platform/run-linux-wsl.mjs`。需要当前已经存在且可由非root默认用户使用的Ubuntu；不自动安装发行版、不改WSL配置/默认用户，不降级成Windows“Linux模拟”。run.mjs/run-identity.mjs新增prepare-only仅生成隔离源/锁，明确打印NO tests executed，真正的LinuxPASS只来自原生二进制退出码。bootstrap-lock不能与prepare-only合用。

2026-10-30T09:04:02Z结果：12包PASS，212个顶层测试**运行**PASS（含Agent在直接模块/隔离WSS应用包重复执行），3项真实Xray二进制测试SKIP，不计作通过。逐包结果/哈希和源码快照见progress/codex/LINUX-NATIVE-EVIDENCE.json；原始日志在自己reports/_review/v09-platform/linux-native/。

| 包 | 顶层运行PASS | SKIP | 证据类别 |
|---|---:|---:|---|
| Agent | 18 | 0 | 持久副作用前fence/日志/私钥CSR/mTLS/拒绝宽松权限；正常能力/策略仍fixture |
| auth | 56 | 0 | 领域与真实本机HTTPS；独立SQL见下 |
| transport | 44 | 0 | 帧/序列/真实TLS/签发与吊销组件 |
| AI | 24 | 0 | 零资源/当前推理闸/BYOK本机TLS；模型和配置/过滤仍double |
| updates | 12 | 0 | 实际Linux私有文件/Sync/替换/回退；服务/health/审计hook仍double |
| recovery | 5 | 0 | Linux永久hold/权限/当前verifier；离线权威仍double |
| plugins | 5 | 0 | 签名/私有工件/生命周期；正式runtime仍未准入 |
| workflows | 5 | 0 | 顺序/幂等/UNKNOWN；唯一tasks端口仍待接收 |
| Xray纯组件 | 8 | 3 | 模板/custom/精确计数纯逻辑；不执行被拦发布物 |
| 隔离SQLite组合 | 12 | 0 | 实际LinuxSQLite/重启/同Tx审计/旧快照外部隔离/真实HTTPS消息SSE |
| WSS/WASM组合 | 4 | 0 | 原生WSS+mTLS/Proto及WASM限内存/超时局部PoC |
| Agent/WSS组合 | 19 | 0 | 原样应用源+真实WSS/临时文件/脱敏输出/重连/吊销；业务准入仍fixture |

Linux隔离SQLite及WSS/WASM/Agent含测试依赖的govulncheck均No vulnerabilities found。全量Windows根check五套PASS09:04:10Z、security PASS09:02:48Z；根锁/API/Proto/gen/CI/AGENTS相对基线零差异。只变本任务隔离测试锁：modernc libc现有go.mod指定的Linux依赖google/uuid v1.6.0及Go tidy校验和；没有升级原来的依赖版本，修正本地replace为__WORKSPACE__模板。

保留FAIL：首次readonly Linux编译缺uuid校验和，未去掉readonly；第二次Agent测试因t.TempDir子目录实际非0700而被正确拒绝，且两个测试忽略初始化错误导致panic/错误前提；第三次恢复测试同类fixture问题。修正**测试创建目录/检查错误**，生产0700门不放宽，新增Linux宽松目录拒绝负向。失败记录分别归档linux-native-first-build-fail、linux-native-first-runtime-fail、linux-native-recovery-fixture-fail。历史WindowsPASS不声称覆盖这些Linux分支。

剩余：双架构实际安装/打包/更新监管进程、1核1GB实测、完整授权→审批→唯一任务→实际Agent链、完整Host隔离/模型/供应商/灾备仍未验收。main/profile未启用，G2未冻结；两份Xray外部发布物安全FAIL不受本页任何PASS覆盖。
