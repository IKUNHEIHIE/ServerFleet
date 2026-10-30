import { mkdirSync } from "node:fs";
import path from "node:path";
import { root, go, run, env } from "../../../../scripts/lib.mjs";

// Compile evidence only. These artifacts still have scaffold main entrypoints;
// never label them a complete v0.9 release or run them on a managed node.
const out=path.join(root,"reports/_review/v09-platform/build");
mkdirSync(out,{recursive:true});
for(const arch of ["amd64","arm64"]){
  for(const component of ["controller","agent"]){
    run(go,["build","-trimpath","-o",path.join(out,`${component}-scaffold-linux-${arch}`),`./apps/${component}`],{
      env:{...env,GOOS:"linux",GOARCH:arch,CGO_ENABLED:"0"},name:`v09-compile-${component}-${arch}`
    });
  }
}
console.log("PASS: four Linux cross-compiles; no runtime/install/health/recovery claim.");
