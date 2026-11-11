import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { pathToFileURL } from "node:url";
import { parseDocument } from "yaml";
import { go, root, env } from "./lib.mjs";
import { spawnSync } from "node:child_process";

export const canonicalPath = value => value.replace(/\{[^}]+\}/g, "{}");
const methods = ["get", "post", "put", "patch", "delete", "head", "options"];
function yaml(file) {
  const parsed = parseDocument(readFileSync(file, "utf8"), { uniqueKeys: true });
  if (parsed.errors.length) throw new Error(`${file}: invalid YAML`);
  return parsed.toJS({ maxAliasCount: 10000 });
}
export function candidateIndex(candidates) {
  const index = new Map();
  for (const { file, doc } of candidates) {
    for (const [path, item] of Object.entries(doc.paths ?? {})) {
      for (const method of methods) {
        const operation = item[method];
        if (!operation) continue;
        const key = `${method.toUpperCase()} ${canonicalPath(path)}`;
        const references = index.get(key) ?? [];
        references.push({ file, path, operationId: operation.operationId ?? null });
        index.set(key, references);
      }
    }
  }
  return index;
}

export function aliasPath(path) {
  if (path.startsWith("/api/v1/monitoring/")) return path.replace("/api/v1/monitoring/", "/api/v1/");
  if (path === "/api/v1/publishing/preview") return "/api/v1/publications/preview";
  if (path.startsWith("/api/v1/publishing/publications")) return path.replace("/api/v1/publishing/publications", "/api/v1/publications");
  if (path.startsWith("/api/v1/enrollments")) return path.replace("/api/v1/enrollments", "/api/v1/node-enrollments");
  if (path === "/api/v1/enrollment-tokens") return "/api/v1/node-enrollment-tokens";
  return path;
}

export function annotate(routes, candidates) {
  const index = candidateIndex(candidates);
  return routes.map(route => {
    const exact = index.get(`${route.method} ${canonicalPath(route.path)}`);
    const alias = aliasPath(route.path);
    const references = exact ?? index.get(`${route.method} ${canonicalPath(alias)}`) ?? [];
    return { ...route, candidateMatch: exact ? "exact" : references.length ? "compatibility-alias" : "none", candidates: references };
  });
}

export function generate() {
  const result = spawnSync(go, ["run", "scripts/api-inventory.go"], { cwd: root, env, encoding: "utf8", maxBuffer: 8 * 1024 * 1024 });
  if (result.status !== 0 || result.error) throw new Error(result.stderr || String(result.error));
  const routes = JSON.parse(result.stdout);
  const config = yaml("redocly.yaml");
  const files = [...new Set(Object.values(config.apis).map(api => api.root))];
  const candidates = files.map(file => ({ file, doc: yaml(file) }));
  const annotated = annotate(routes, candidates);
  const snapshot = {
    formatVersion: 1,
    scope: "Static Go Handle/HandleFunc registrations, literal route ranges and Patterns returns; declarations include conditional and unavailable profiles. See README.md for non-ServeMux protocol handlers and runtime gates.",
    candidateOrder: "redocly.yaml registration order; matches are references, not proof that legacy schemas describe the current implementation",
    routes: annotated,
  };
  const groups = new Map();
  for (const route of annotated) {
    const group = route.path.startsWith("/api/v1/") ? route.path.split("/")[3] : "health";
    if (!groups.has(group)) groups.set(group, []);
    groups.get(group).push(route);
  }
  let md = "# HTTP 路由声明索引\n\n由 `node scripts/api-docs.mjs --write` 从 Go AST 与已登记 OpenAPI 生成；不要手改。\n\n";
  md += `共 **${annotated.length}** 个不同的 method/path 声明，其中 **${annotated.filter(r => r.candidates.length === 0).length}** 个尚无同路径或明确兼容别名 OpenAPI 候选。声明包含别名、可选配置和拒绝式入口，**不代表全部功能已装配或可执行**。GET 的隐式 HEAD 不单独计数。\n\n`;
  md += "入口、安全规则、手写协议端点和字段规范见 [接口使用说明](README.md)。候选列列出登记顺序中最后一个同路径或明确别名参考（JSON 的 candidateMatch 区分两者）；字段仍须结合该领域实现/修订说明，不能用路径匹配冒充字段审计。完整候选引用见 [JSON](route-inventory.json)。\n\n";
  for (const [group, rows] of [...groups].sort(([a], [b]) => a.localeCompare(b))) {
    md += `## ${group}\n\n| 方法 | 路径 | 源码声明位置 | 最近候选参考 |\n|---|---|---|---|\n`;
    for (const route of rows) {
      const d = route.declarations[0];
      const ref = route.candidates.at(-1);
      md += `| ${route.method} | \`${route.path}\` | [${d.source.split("/").at(-1)}](../../${d.source}) · ${d.owner} | ${ref ? `[${ref.operationId ?? "候选"}](../../${ref.file})` : "未覆盖；以源码和领域说明为准"} |\n`;
    }
    md += "\n";
  }
  return new Map([["docs/api/route-inventory.json", JSON.stringify(snapshot, null, 2) + "\n"], ["docs/api/ROUTES.md", md.trimEnd() + "\n"]]);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  if (!["--check", "--write"].includes(process.argv[2])) throw new Error("Use --check or --write");
  for (const [file, text] of generate()) {
    if (process.argv[2] === "--write") { mkdirSync("docs/api", { recursive: true }); writeFileSync(file, text); }
    else if (readFileSync(file, "utf8") !== text) throw new Error(`${file} is stale; run node scripts/api-docs.mjs --write`);
  }
  console.log(`PASS: API declaration index ${process.argv[2]}`);
}
