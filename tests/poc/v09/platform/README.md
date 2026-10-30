# Platform 隔离实验

在本任务worktree使用固定Node执行：

```powershell
& .tools/node-v24.22.1-win-x64/node.exe tests/poc/v09/platform/run.mjs
& .tools/node-v24.22.1-win-x64/node.exe tests/poc/v09/platform/run-eino.mjs
```

脚本将审查用 `.go.txt` 源快照和固定module/sum写入自己忽略的 `reports/_review/v09-platform/{poc,eino-poc}`，使用固定Go、`-mod=readonly`运行。这些实验不进入根模块，不使root tidy自动选择WSS/WASM/Eino生产依赖。两脚本都校验根go.mod/go.sum/package-lock前后hash，改变即失败。所有CA/证书均测试现场短期生成，不保存私钥，不使用真实凭据，不访问外部模型或节点；Go包下载/漏洞数据库查询只读公共来源。

| 实验 | 实际证明 | 仍未证明 |
|---|---|---|
| WSS | 本机真实主动WSS+mTLS、binary Protobuf；生产ControllerLinks及原样复制的Agent应用循环/executor，Hello/真实Ping/一次临时文件副作用/重连新序列/持久去重/StateSync/撤销取消UNKNOWN；拒绝文本/超限/节点替换/外部Origin | DS当前授权/审批/节点任务审计接线、生产注册能力/安装器/轮换/重装、Linux网络故障/重连风暴；业务gate为明确fixture |
| WASM | wazero解释器每内存128KiB上限、未注册env Host拒绝、无限循环50ms超时关闭、后续正常模块返回42 | 全Host内存/RSS/CPU、WASI/实际插件、并发实例/持久生命周期、Linux/1C1G、完整逃逸验收或runtime最终选型 |
| Eino | 真实ToolsNode调用通过Server Fleet策略组件，合成toolcall不能绕过零范围/撤权/root人工审批/伪造approved/未注册工具 | ChatModel/BYOK、持久scope与审批消费/调度、SSE/真实Agent；这是组件集成模拟层，不是POC-06 E2E |

Eino v0.9.22的Sonic v1.15.0在当前Go1.27.3下提示支持到1.26，实际回退encoding/json；不关闭警告，不声称兼容性/性能已通过。根没有新增这些依赖，WSS生产依赖候选另交S04，WASM/Eino未选择生产框架。

漏洞检查含实验测试依赖：先tools:install，再将`.tools/go/bin`加入当前命令PATH，设置本worktree GOCACHE/GOPATH，运行：

```powershell
& .tools/bin/govulncheck.exe -C reports/_review/v09-platform/poc -test ./...
& .tools/bin/govulncheck.exe -C reports/_review/v09-platform/eino-poc -test ./...
```

根npm run check不自动运行这些实验。其确切命令、原始摘要和局限在progress/codex/EVIDENCE.json和HANDOFF记录。局部PASS不关闭G2、AC或完整PoC。

R28新增本机已有Ubuntu WSL的Linux原生运行入口：`node tests/poc/v09/platform/run-linux-wsl.mjs`。固定Go交叉编译，再在UID1000/Linux amd64真实执行；不安装发行版、不使用sudo/外部节点/真实秘密。最新12包212顶层运行PASS（Agent重复执行计入运行数），3项实际Xray二进制SKIP。精确快照与局限见LOCAL-LINUX-VALIDATION.md和progress/codex/LINUX-NATIVE-EVIDENCE.json；这更新了上表“完全没有Linux局部证明”的历史限制，但不关闭arm64/安装/1C1G/Host/runtime准入。prepare-only模式只准备源/锁，不能记作测试PASS。
