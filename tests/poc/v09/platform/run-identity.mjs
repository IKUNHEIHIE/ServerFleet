import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import path from "node:path";
import { root, go, run, env } from "../../../../scripts/lib.mjs";

// Immutable committed DS source, used ONLY for this isolated combination test.
// This does not cherry-pick, adopt a public port, or change another checkout.
const dsRef="bb8954988fe7b2f832edf291803529d7745e4f0a";
const work=path.join(root,"reports/_review/v09-platform/identity-poc");
const hash=file=>createHash("sha256").update(readFileSync(path.join(root,file))).digest("hex");
const protectedFiles=["go.mod","go.sum","package-lock.json"];
const before=protectedFiles.map(hash);
const git=args=>{const r=spawnSync("git",args,{cwd:root,env,encoding:"utf8",maxBuffer:8*1024*1024});if(r.status!==0)throw new Error(r.stderr||"git source read failed");return r.stdout};
mkdirSync(path.join(work,"dsstore"),{recursive:true});
mkdirSync(path.join(work,"dsaudit"),{recursive:true});
const files=git(["ls-tree","-r","--name-only",dsRef,"internal/storage"]).trim().split(/\r?\n/).filter(file=>/^internal\/storage\/[^/]+\.go$/.test(file)&&!file.endsWith("_test.go"));
if(files.length===0)throw new Error("No committed DS storage source");
for(const file of files)writeFileSync(path.join(work,"dsstore",path.basename(file)),git(["show",`${dsRef}:${file}`]));
const auditFiles=git(["ls-tree","-r","--name-only",dsRef,"internal/audit"]).trim().split(/\r?\n/).filter(file=>/^internal\/audit\/[^/]+\.go$/.test(file)&&!file.endsWith("_test.go"));
for(const file of auditFiles){const source=git(["show",`${dsRef}:${file}`]).replaceAll('"serverfleet.local/serverfleet/internal/storage"','"serverfleet.local/serverfleet/persistence-poc/dsstore"');writeFileSync(path.join(work,"dsaudit",path.basename(file)),source);}
const bootstrap=process.argv.includes("--bootstrap-lock");
const workspace=JSON.stringify(root.replaceAll("\\","/").replace(/\/$/,""));
const initial=`module serverfleet.local/serverfleet/persistence-poc
go 1.27.3
require (
 serverfleet.local/serverfleet v0.0.0
 modernc.org/sqlite v1.61.0
)
replace serverfleet.local/serverfleet => ${workspace}
`;
writeFileSync(path.join(work,"go.mod"),bootstrap?initial:readFileSync(new URL("identity-module.txt",import.meta.url),"utf8").replace("__WORKSPACE__",workspace));
if(!bootstrap)writeFileSync(path.join(work,"go.sum"),readFileSync(new URL("identity-dependency-sums.txt",import.meta.url)));
writeFileSync(path.join(work,"identity_sql_test.go"),readFileSync(new URL("identity_sql_test.go.txt",import.meta.url)));
writeFileSync(path.join(work,"identity_http_test.go"),readFileSync(new URL("identity_http_test.go.txt",import.meta.url)));
writeFileSync(path.join(work,"identity_audit_test.go"),readFileSync(new URL("identity_audit_test.go.txt",import.meta.url)));
writeFileSync(path.join(work,"identity_credentials_test.go"),readFileSync(new URL("identity_credentials_test.go.txt",import.meta.url)));
writeFileSync(path.join(work,"identity_bearer_test.go"),readFileSync(new URL("identity_bearer_test.go.txt",import.meta.url)));
writeFileSync(path.join(work,"enrollment_sql_test.go"),readFileSync(new URL("enrollment_sql_test.go.txt",import.meta.url)));
writeFileSync(path.join(work,"workflows_sql_test.go"),readFileSync(new URL("workflows_sql_test.go.txt",import.meta.url)));
writeFileSync(path.join(work,"ai_sessions_sql_test.go"),readFileSync(new URL("ai_sessions_sql_test.go.txt",import.meta.url)));
writeFileSync(path.join(work,"approvals_sql_test.go"),readFileSync(new URL("approvals_sql_test.go.txt",import.meta.url)));
try{
 run(go,["test",bootstrap?"-mod=mod":"-mod=readonly","-count=1","-v","-timeout=90s","."],{cwd:work,name:"v09-real-sqlite-identity"});
 if(bootstrap){
  writeFileSync(new URL("identity-module.txt",import.meta.url),readFileSync(path.join(work,"go.mod"),"utf8").replace(workspace,"__WORKSPACE__"));
  writeFileSync(new URL("identity-dependency-sums.txt",import.meta.url),readFileSync(path.join(work,"go.sum")));
 }
}finally{if(protectedFiles.some((file,i)=>hash(file)!==before[i]))throw new Error("Root dependencies changed");}
console.log(`PASS: actual SQLite auth integration with DS source ${dsRef}; isolated only, public integration still pending.`);
