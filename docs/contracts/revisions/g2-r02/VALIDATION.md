# 实际验证与交接证据

会话 `codex-01` / G2-R01，2026-10-29（Asia/Shanghai）。原始机器结果中的UTC日期可能为2026-10-28，换算+08:00属于本次2026-10-29检查。基线 `15814c6`；本地提交编号由最终交接报告给出，避免提交自引用。

## 环境与复验

Windows amd64；Go `1.27.3`、Node `24.22.1`、npm `11.19.1`、Buf `1.75.0`、Redocly `2.72.0`、protoc-gen-go `1.36.12`。运行时通过只读使用主目录现有 `.tools/go`、`.tools/node-v24.22.1-win-x64` 加入当前进程PATH；npm依赖和固定Go工具安装在本任务Worktree。未修改系统配置、锁文件或共享工具文件。

在本任务工作目录运行（此环境的运行时已在主目录提供）：

```powershell
. 'D:/admin/desktop/codex/myproject/ks/scripts/env.ps1'
npm ci
npm run tools:install
npm run check:contracts
node tests/contracts/g2r02/build-candidate.mjs
node tests/contracts/g2r02/run.mjs
npm run check
npm run check:security
git apply --check docs/contracts/revisions/g2-r02/shared-file-changes.patch
```

本次先生成/检查新候选，再运行全量检查；最后因完善完整ETag比较/406及检查结果输出，再显式复验新候选。全量检查不自动覆盖新OpenAPI/Node样例，不能替代最后独立命令。新Go测试会被`go test ./...`发现，独立入口另以`-count=1 -v`执行。其他机器应使用相同版本本地运行时，而非依赖此绝对路径。

## 最终结果

| 命令/检查 | 实际输出摘要 | 状态 |
|---|---|---|
| 分支/目录/基线 | 指定路径，contract/g2-review-cx01，15814c67beddad0172fac2440fc3f45f502bfcad，初始干净 | PASS |
| npm ci | added 127 packages；Buf postinstall未全局放开，锁定平台二进制可执行 | PASS |
| npm run tools:install | 固定4个Go工具安装完成 | PASS |
| npm run check:contracts | 原始69文件及3工作候选字节一致；Core29/34、Merged147/185、Xray19/23；Redocly/Buf/生成一致性/Go往返 | PASS |
| 既有Proto负向变异 | uint32→string触发breaking；字段号2复用触发编译失败，均为预期拒绝 | PASS |
| build-candidate.mjs | Wrote independent candidate + 185 operation records (151 legacy) | PASS |
| 新候选显式结构检查 | OpenAPI3.1、有限YAML别名/输入、全部ref、185唯一ID、147路径及参数 | PASS |
| 新候选显式Redocly lint | 单独指定新文件，Your API description is valid | PASS |
| 新候选Node正反/模型测试 | tests26 / pass26 / fail0；包括10类候选变异拒绝、重复YAML key拒绝、认证/revision/幂等/任务/SSE/安全样例 | PASS（静态/模型） |
| 原始Proto构建+WIRE breaking | 原descriptor构建成功；P1对其WIRE检查exit0 | PASS |
| 新Go协议测试 | 3项顶层测试，13个oneof子测试；往返、unsafe defaults局限证明、两种截断wire拒绝 | PASS |
| npm run check | policy/contracts/go/web/git五组全部PASS；Web3 tests；新Go测试包含在全包检查中 | PASS |
| npm run check:security | npm: found 0 vulnerabilities；govulncheck: No vulnerabilities found | PASS |
| shared-file-changes.patch | git apply --check成功；仅检查，未应用 | PASS |

快照在 [evidence/](evidence/)：`check-all.json`、`check-contracts.json`、`check-security.json`、`g2r02-results.json`，关键实际输出日志和SHA256清单。新入口结果携带候选和源SHA256，可核对检查确实作用于本次候选。原始完整工具输出仍在本Worktree忽略的`reports/`中。安装和安全扫描使用公开依赖源，不接触云项目、生产或VPS。

## 本轮失败与修复

1. 最初指定工作目录不存在（进程启动返回目录无效）；只读核对主目录和worktree列表后，按用户指定路径/分支从15814c6创建独立Worktree。
2. 初次矩阵构建对“源中尚不存在的新共享enum”计算哈希时处理undefined失败；为缺失schema增加显式哈希标记，重建/重算矩阵通过。
3. 首轮独立测试14/21通过，7失败：禁止第二队列的路径断言过宽，误匹配已有更新作业`.../jobs`；限制为顶层`/api/v1/jobs`。随后21/22通过，剩余负向变异只删除ref但未删除内联If-Match，实际上未改变候选；改为解析参数名后删除。保留拒绝断言，修复后22/22，再加入4组安全样例，最终26/26。
4. 审查发现单纯revision整数不能区分不同资源/授权表示的强ETag；候选改为revision+表示摘要并比较完整标签，增加相同revision错误表示412负向样例，独立复验通过。
5. 共享patch在缩减空白上下文后`git apply --check`一度拒绝任务板hunk；恢复完整前后上下文后检查通过。未实际应用任何共享文件差异。
6. 归档的Redocly日志原有多余末尾空行触发staged diff检查；归档副本仅规范化末尾空白后重新校验，原始完整输出仍保留于reports。

上述是构建/测试工件自身修复，不是删除失败用例或宣称真实业务安全已通过。最终结果以附带快照为准。

## 未验证/待审批

- **UNTESTED**：真实Cookie/CSRF/限流/失效、Bearer签发撤销、初始化本地token与注册CSR持有证明；真实RBAC/多角色冲突/降权、AI root真人审批及撤权竞态。
- **UNTESTED**：Controller任务持久化、幂等TTL/并发/跨重启、原子revision、计划/审批/派发一致性、Agent落盘去重、900秒断连重连、真实取消/超时/恢复隔离、SSE游标存储及水位。
- **UNTESTED**：WSS+mTLS/错误证书/吊销/节点重装轮换/帧与日志限额/版本能力门禁、TS生成与跨语言、1C1G性能；Proto解码可接受空/危险默认值的测试不代表运行门禁存在。
- **UNTESTED**：真实审计字段字符串脱敏、完整性/事务/保留与权限拒绝覆盖；缺失PRIV/WC/PKI-OPS/UPDATE原ADR待正式归档核验。
- **PENDING_APPROVAL**：profile/旧客户端状态拒绝、CSRF/session生命周期、ETag规范化表示、24h幂等TTL/秘密响应重放、全写方法加键范围和逐域202/分页迁移。共享patch只供总控审查。
- Linux CI、race、真实远程PR与独立owner审批均未运行/取得；本轮仅本地候选评审，不形成任何G2冻结结论。

## 文件与提交范围

仅三个授权目录的新增文件：独立OpenAPI；兼容/逐操作矩阵、C01～C07/Proto审查、共享文件提案、本报告及证据快照；可重复候选构建器、显式检查入口、Node正反样例与Go协议测试。现有proto、gen/go、兼容基线、配置、锁文件、源码实现和原始资料最终字节未改。

任务板为共享文件，未直接登记，已在任务README记录授权并在patch提供具体新增行。提交作者使用明确的本地会话占位身份 `codex-01 <codex-01@local.invalid>`，不冒充项目所有者，不修改仓库共享身份配置。本地提交之后立即停止，由总控审查决定是否采纳。
