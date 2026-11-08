# B03 外部 Xray 工件与准入差距包（V09-CLOUD-WB-02）

抓取日期：**2026-11-03（UTC 12:39–13:10）**。全部结论来自当次实际 API 响应与文件校验，命令与原始输出见 `EVIDENCE-INDEX.md`。
状态汇总：**BLOCKED（不可发布/不可安装/不可运行）**，其中 1 项 PASS（校验和一致性）、3 项 FAIL/不可判定、3 项 UNTESTED。**本包不放行任何 Xray 工件**，也不据此关闭 B03。

## 1. 结论先行

| 项 | 结果 | 依据 |
|---|---|---|
| 项目固定的 `v26.10.29` 是否仍存在于官方源 | **FAIL（源不存在）** | `GET /repos/XTLS/Xray-core/releases/tags/v26.10.29` → HTTP 404 |
| 该版本被撤原因 | 官方 v26.10.31 说明 **“v26.10.29 让位，已被撤版”** | release body 原文 |
| 官方最新稳定版 | `v26.10.31`（2026-10-31T13:37:42Z，`prerelease=false`） | releases API |
| 旧固定目标 `v26.6.1` 性质 | `prerelease=**true**`（2026-06-01 发布），非稳定发布 | releases API |
| 许可证 | `MPL-2.0` | `GET /repos/XTLS/Xray-core` → `license.spdx_id` |
| 上游签名/证明 | **无**。仅有 `.dgst` 校验和文本（MD5/SHA1/SHA256/SHA512），无 `.sig/.asc/.pem/.sbom/.intoto` 资产；GitHub attestations 查询 404 | 资产枚举 + `.dgst` 内容 + attestations API |
| Linux amd64 字节与官方 digest 一致性 | **PASS（仅校验和层面）** | 本地 `sha256sum` = `e4bd9119…b214`，与 GitHub `asset.digest` 完全一致（16,692,690 B） |
| 官方已知安全背景 | v26.10.31 为**紧急安全更新**：Linux 内核 IPv4 MPTCP 可被远程拿 root，Go 1.24+ 默认开启 MPTCP，该版默认关闭 MPTCP 并建议尽快升级 | release body 原文 |
| 二进制漏洞扫描（当前稳定版 v26.10.31） | **FAIL（exit 3，3 项，2 个模块）** | govulncheck 1.9.0 `-mode=binary`，见 §5 |
| Linux arm64 运行验证 | **UNTESTED** | 无 ARM 实际环境，禁止用交叉构建冒充运行 |
| 运行/安装/默认启用 | **未发生** | Runtime 未获准入，插件保持执行关闭 |

> 一句话：**本轮无法把任何 Xray 工件认定为“已准入”**。缺的不是某一次扫描，而是“官方签名/来源证明”这一项上游根本不提供，加上固定版本已被上游撤版。

## 2. 版本与来源核对（真实响应）

官方 release 序列（`GET /repos/XTLS/Xray-core/releases?per_page=8`，同次抓取）：

| tag | published_at | prerelease |
|---|---|---|
| v26.10.31 | 2026-10-31T13:37:42Z | false |
| v26.10.21 | 2026-10-21T12:07:11Z | false |
| v26.9.30 | 2026-09-30T07:41:08Z | true |
| v26.9.9 | 2026-09-08T22:28:10Z | true |
| v26.7.28 | 2026-07-28T08:00:45Z | true |
| v26.7.11 | 2026-07-11T15:56:30Z | true |
| v26.6.27 | 2026-06-27T13:20:31Z | true |
| v26.6.22 | 2026-06-22T18:56:38Z | true |

- `v26.10.29`：**不存在**（404）。标签列表 `repos/XTLS/Xray-core/tags` 中亦仅有 `v26.10.31`、`v26.10.21`。
- `v26.6.1`：存在，但 `prerelease=true`。项目此前把它当作“固定兼容目标”，它与“正式稳定发布”不是同一概念。
- `repos/XTLS/Xray-core` → `default_branch=main`、`archived=false`、`license=MPL-2.0`。

**与项目既有文档的差异（需主控确认）**：`docs/contracts/revisions/v09-five-day-cx02/XRAY-ARTIFACT-SECURITY.md` 记录“已只读核对正式发布 v26.10.29（2026-10-29 发布、非 prerelease）”。本次抓取显示该版本已被上游撤下，文档所述基线已失效。本包不改动该历史文档，只登记差异。

## 3. 真实 SHA256（GitHub `asset.digest` 字段）

| release | asset | size (B) | sha256 |
|---|---|---|---|
| v26.10.31 | `Xray-linux-64.zip` | 16,692,690 | `e4bd9119f50ae7c41658b1abd0679ae06f98bd864a2907ff33f2f2cf7081b214` |
| v26.10.31 | `Xray-linux-arm64-v8a.zip` | 15,175,093 | `1811c8f1da0c3b6dc707ebaf1c8390e987ba39859a8f0a7aadd8526157f2ec8d` |
| v26.10.21 | `Xray-linux-64.zip` | 15,100,223 | `0eec3ed4163c273d8c8bf821f7d219e257921cf584aa3fb970715e458e631978` |
| v26.6.1 | `Xray-linux-64.zip` | 21,347,411 | `136e822e99e616692550723e8b607cd8858c62a390aea5704938bc27930904ba` |

下载并本地校验：见 `EVIDENCE-INDEX.md` 的 `artifact-sha256` 记录（本地 `sha256sum` 与上表一致才记 PASS）。

## 4. 上游实际提供的“签名/证明”

- 每个 release 有 64 个资产：`Xray-<os>-<arch>.zip` 与其 `.dgst`。**没有任何** `.sig`、`.asc`、`.pem`、`.minisign`、`.sbom`、`.intoto.jsonl`、`SHA256SUMS`（带签名）类资产。
- `.dgst` 实际内容（v26.10.31 `Xray-linux-64.zip.dgst`，299 B）：

```
MD5= 9f7aaa60ba06e8f60481e019b427a1e2
SHA1= 54ef927c2c17d3fbed9bf6a36833574e0c94c280
SHA2-256= e4bd9119f50ae7c41658b1abd0679ae06f98bd864a2907ff33f2f2cf7081b214
SHA2-512= d507c15ae81f4d658b8bd70070c1e17085b46c0512f4f4030d0e35240bbbe18caecbe7911ecbce679da9a52ea8a410dad03a090ac437594d4feb235648df62ef
```

  这是**未签名的校验和**：任何人都能替换资产并同步改 `.dgst`。它证明“下载完整”，不证明“来源可信”。
- GitHub attestations：`GET /repos/XTLS/Xray-core/attestations/sha256:<digest>` → **404**，无 SLSA/in-toto 构建来源证明。

因此“上游签名”一项不是本轮工作量问题，而是**上游不提供**。要满足原安全门槛，只有三条路，均需主控决策：
1. 明确接受“仅校验和 + 扫描 + 双层人工审批”的准入模型（需改安全门槛，不能由作者自行放宽）；
2. 由主控指定可信镜像/自建构建并签名（属于新的供应链工程，不在本包范围）；
3. 维持 BLOCKED，插件只保留静态校验与拒绝路径。

## 5. 本轮实测：v26.10.31 linux/amd64 二进制（未执行该程序）

```
$ sha256sum ev/Xray-linux-64.zip
e4bd9119f50ae7c41658b1abd0679ae06f98bd864a2907ff33f2f2cf7081b214  Xray-linux-64.zip
$ sha256sum ev/xray/xray
cc4b84d3069569f297a61f04b914f2bb37e9d202fd7b84cb50def1fc30fd40e1
$ go version -m ev/xray/xray
ev/xray/xray: go1.27.3
    path  github.com/xtls/xray-core/main
    mod   github.com/xtls/xray-core  (devel)
$ govulncheck -mode=binary ev/xray/xray   → EXIT 3
Vulnerability #1: GO-2026-6866  x/net@v0.61.0  (fixed v0.62.0)
Vulnerability #2: GO-2026-6860  x/net@v0.61.0  (fixed v0.62.0)
Vulnerability #3: GO-2026-5932  x/crypto@v0.58.0 openpgp（无维护；修复版本为伪版本）
→ "Your code is affected by 3 vulnerabilities from 2 modules."
```

判读（不夸大、不缩小）：
- 三项是**符号级**发现：`html.Parse*`（x/net）与 `openpgp/*`（x/crypto）。在“仅 vless/tls | reality 入站”的典型服务端路径上，`html.Parse` 与 openpgp 未必可达，但**本包无权据此自行豁免**；可达性判定需要真实配置路径与调用图评审。
- 扫描 `exit 3` 按项目规则即 **不得作为安装目标**；它与“有漏洞但不可达”是两个不同结论，不能互相替代。
- 本次**没有执行**该二进制（未 `xray run -test`、未启动进程、未监听端口）：Runtime 未准入。

## 5b. 历史与安全背景

- 历史（项目已有记录，非本轮重跑）：固定 govulncheck 1.9.0 `-mode=binary` 对 **v26.6.1** Windows amd64 扫描 exit 3、55 项符号级发现；对 **v26.10.29** exit 3、3 项（GO-2026-6866、GO-2026-6860 的 `x/net v0.61.0`，修复版 v0.62.0；GO-2026-5932 `x/crypto v0.58.0` openpgp，无修复版本）。
- 本轮新增事实：官方 v26.10.31 说明属于**紧急安全更新**，起因是 Linux 内核 IPv4 MPTCP 可被远程利用拿 root，而 Go 1.24+ 默认启用 MPTCP；该版默认关闭 MPTCP 以缩小攻击面。这使“继续停在旧固定版本”本身成为风险决策。
- 本文件§5已补入作者对v26.10.31的实际扫描记录：exit3、三项发现；此前“未完成扫描”的文字已过时。主控2026-11-08复查官方latest API仍为v26.10.31，资产digest一致；这是元数据复核，未把作者扫描冒称本会话重跑，也不据此认定安全。

## 6. 明确的 BLOCKED / UNTESTED 清单

| 编号 | 项 | 状态 | 精确原因 |
|---|---|---|---|
| B03-01 | 固定版本 `v26.10.29` 来源与 SHA 可核 | BLOCKED | 上游已撤版，404；无对象可核 |
| B03-02 | 上游签名/来源证明 | BLOCKED | 上游仅提供未签名 `.dgst`，无 attestation（404），非本包可弥补 |
| B03-03 | Linux amd64 工件准入 | BLOCKED | 校验和一致（PASS）但**无签名**且二进制扫描 **FAIL**（3 项）→ 不得认定准入 |
| B03-04 | Linux amd64 二进制漏洞扫描（v26.10.31） | **FAIL** | exit 3：GO-2026-6866、GO-2026-6860（x/net v0.61.0→修复 v0.62.0）、GO-2026-5932（x/crypto openpgp，无维护） |
| B03-05 | Linux arm64 运行验证 | UNTESTED | 无 ARM 实机/权限；交叉构建不算运行证据 |
| B03-06 | 安装/启用/默认启用 | 未发生 | Runtime 未获准入，遵守“未准入不运行、不默认启用” |
| B03-07 | 版本选择本身 | BLOCKED | 停在已撤版=已知风险；v26.10.31已有作者扫描FAIL；须先解决漏洞、可信签名/构建来源及验证，再作准入决策 |

## 7. 对下游的影响（本包如何继续）

- 插件生命周期、配置生成/校验、用户与导出等**静态**能力继续推进并可测试（`plugins/xray-manager`），不依赖工件准入。
- 任何“安装/部署/重启”动作保持 `XRAY_RUNTIME_UNAVAILABLE` / 任务提交失败关闭，不提供旁路。
- Web 只展示真实状态（`not_installed` / `unknown`），不显示虚假“运行中”。
- 本包不关闭 B03，也不把本文件当作准入批准。
