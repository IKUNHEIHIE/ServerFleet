# Platform 隔离实验

在本任务worktree使用固定Node执行：

```powershell
& .tools/node-v24.22.1-win-x64/node.exe tests/poc/v09/platform/run.mjs
& .tools/node-v24.22.1-win-x64/node.exe tests/poc/v09/platform/run-eino.mjs
```

脚本将审查用 `.go.txt` 源快照和固定module/sum写入自己忽略的 `reports/_review/v09-platform/{poc,eino-poc}`，使用固定Go、`-mod=readonly`运行。这些实验不进入根模块，不使root tidy自动选择WSS/WASM/Eino生产依赖。两脚本都校验根go.mod/go.sum/package-lock前后hash，改变即失败。所有CA/证书均测试现场短期生成，不保存私钥，不使用真实凭据，不访问外部模型或节点；Go包下载/漏洞数据库查询只读公共来源。

| 实验 | 实际证明 | 仍未证明 |
|---|---|---|
| WSS | 本机真实主动WSS+mTLS、binary Protobuf往返；拒绝文本/超限/节点替换/连接建立后吊销/外部Origin | 完整Hello/序列/去重组合、注册签发/轮换/重装、节点仓储/回调/SSE、Linux网络故障/重连风暴 |
| WASM | wazero解释器每内存128KiB上限、未注册env Host拒绝、无限循环50ms超时关闭、后续正常模块返回42 | 全Host内存/RSS/CPU、WASI/实际插件、并发实例/持久生命周期、Linux/1C1G、完整逃逸验收或runtime最终选型 |
| Eino | 真实ToolsNode调用通过Server Fleet策略组件，合成toolcall不能绕过零范围/撤权/root人工审批/伪造approved/未注册工具 | ChatModel/BYOK、持久scope与审批消费/调度、SSE/真实Agent；这是组件集成模拟层，不是POC-06 E2E |

Eino v0.9.22的Sonic v1.15.0在当前Go1.27.3下提示支持到1.26，实际回退encoding/json；不关闭警告，不声称兼容性/性能已通过。根没有新增这些依赖，WSS生产依赖候选另交S04，WASM/Eino未选择生产框架。

漏洞检查含实验测试依赖：先tools:install，再将`.tools/go/bin`加入当前命令PATH，设置本worktree GOCACHE/GOPATH，运行：

```powershell
& .tools/bin/govulncheck.exe -C reports/_review/v09-platform/poc -test ./...
& .tools/bin/govulncheck.exe -C reports/_review/v09-platform/eino-poc -test ./...
```

根npm run check不自动运行这些实验。其确切命令、原始摘要和局限在progress/codex/EVIDENCE.json和HANDOFF记录。局部PASS不关闭G2、AC或完整PoC。
