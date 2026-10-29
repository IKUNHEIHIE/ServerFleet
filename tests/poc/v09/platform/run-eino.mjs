import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { createHash } from "node:crypto";
import path from "node:path";
import { root, go, run } from "../../../../scripts/lib.mjs";

const work=path.join(root,"reports/_review/v09-platform/eino-poc");
const protectedFiles=["go.mod","go.sum","package-lock.json"];
const digest=file=>createHash("sha256").update(readFileSync(path.join(root,file))).digest("hex");
const before=protectedFiles.map(digest);
mkdirSync(work,{recursive:true});
writeFileSync(path.join(work,"go.mod"),readFileSync(new URL("eino-module.txt",import.meta.url),"utf8").replace("__WORKSPACE__",JSON.stringify(root.replaceAll("\\","/").replace(/\/$/,""))));
writeFileSync(path.join(work,"go.sum"),readFileSync(new URL("eino-dependency-sums.txt",import.meta.url)));
writeFileSync(path.join(work,"eino_test.go"),readFileSync(new URL("eino_test.go.txt",import.meta.url)));
try{
  run(go,["test","-mod=readonly","-count=1","-v","-timeout=45s","."],{cwd:work,name:"v09-isolated-eino-poc"});
}finally{
  if(protectedFiles.some((file,i)=>digest(file)!==before[i]))throw new Error("Root dependency files changed during Eino PoC");
}
console.log("PASS: isolated Eino tool gate experiment only; no live model or production framework selection.");
