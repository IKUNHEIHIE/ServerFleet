import { createHash } from "node:crypto";
import { createWriteStream, existsSync, mkdirSync, readFileSync } from "node:fs";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";
import path from "node:path";
import { root,go,run,env,goTool } from "../../../../scripts/lib.mjs";

if(process.platform!=="win32"||process.arch!=="x64")throw new Error("This local artifact probe is Windows amd64 only; Linux remains untested");
const work=path.join(root,"reports/_review/v09-platform/xray-v26.6.1");mkdirSync(work,{recursive:true});
const archive=path.join(work,"Xray-windows-64.zip"),binary=path.join(work,"xray.exe");
const digest=file=>createHash("sha256").update(readFileSync(file)).digest("hex");
const protectedFiles=["go.mod","go.sum","package-lock.json"].map(file=>path.join(root,file));const before=protectedFiles.map(digest);
if(!existsSync(archive)){
 const response=await fetch("https://github.com/XTLS/Xray-core/releases/download/v26.6.1/Xray-windows-64.zip");
 if(!response.ok||!response.body)throw new Error("Pinned official artifact unavailable");
 await pipeline(Readable.fromWeb(response.body),createWriteStream(archive,{flags:"wx"}));
}
if(digest(archive)!=="0a1ee8c8a45e11e865c725db195429c79b52ee960407065089c5be586f343248")throw new Error("Pinned archive checksum mismatch");
const quote=value=>"'"+value.replaceAll("'","''")+"'";
if(!existsSync(binary))run("powershell.exe",["-NoProfile","-NonInteractive","-Command",`Add-Type -AssemblyName System.IO.Compression.FileSystem; $zip=[IO.Compression.ZipFile]::OpenRead(${quote(archive)}); try { $entry=$zip.Entries | Where-Object FullName -EQ 'xray.exe'; if ($null -eq $entry -or $entry.Length -gt 134217728) { throw 'Invalid executable entry' }; [IO.Compression.ZipFileExtensions]::ExtractToFile($entry,${quote(binary)}) } finally { $zip.Dispose() }`],{name:"extract-pinned-local-xray"});
if(digest(binary)!=="9bd2d873d78b5c39303ab16db630a131f9f33836e98d96455f80a8092fecdcd5")throw new Error("Pinned executable checksum mismatch");
try{
 goTool("govulncheck",["-mode=binary",binary],{name:"actual-local-xray-binary-security"});
 run(go,["test","-count=1","-v","./plugins/xray-manager"],{name:"actual-local-xray-config-start",env:{...env,SF_XRAY_LOCAL_TEST_BINARY:binary}})
}
finally{if(protectedFiles.some((file,i)=>digest(file)!==before[i]))throw new Error("Root dependencies changed")}
console.log("PASS: pinned Xray 26.6.1 config/start, actual loopback TLS/Vision traffic/Stats/non-resetting reads/process reset; Linux amd64/arm64 installation, REALITY client traffic and production collection remain unverified.");
