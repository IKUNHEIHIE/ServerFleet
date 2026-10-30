// Static artifact inspection only. This script NEVER starts the executable.
import {createHash} from 'node:crypto';
import {createWriteStream,existsSync,mkdirSync,readFileSync,renameSync,writeFileSync} from 'node:fs';
import {Readable,Transform} from 'node:stream';
import {pipeline} from 'node:stream/promises';
import path from 'node:path';
import {root,go,run,goTool} from '../../../../scripts/lib.mjs';

const pins={
 '26.6.1':{archive:'0a1ee8c8a45e11e865c725db195429c79b52ee960407065089c5be586f343248',executable:'9bd2d873d78b5c39303ab16db630a131f9f33836e98d96455f80a8092fecdcd5'},
 '26.10.29':{archive:'fce33e81cccf50e7aab572b175519a50b5e0f9d2f5c8d1ed73012ea5c9ad4d58',executable:'07a332b714a3c0eee047a22a9c0ea749b64df33e4e1ff6d28a8c7f9ef943ce10'},
};
const version=process.argv[2];
if(process.argv.length!==3||!Object.hasOwn(pins,version)||process.platform!=='win32'||process.arch!=='x64')throw new Error('Specify one pinned release; this probe is Windows amd64 only');
const pin=pins[version],work=path.join(root,'reports/_review/v09-platform',`xray-v${version}`);
mkdirSync(work,{recursive:true});
const archive=path.join(work,'Xray-windows-64.zip'),binary=path.join(work,'xray.exe');
const hash=file=>createHash('sha256').update(readFileSync(file)).digest('hex');
if(!existsSync(archive)){
 const response=await fetch(`https://github.com/XTLS/Xray-core/releases/download/v${version}/Xray-windows-64.zip`,{signal:AbortSignal.timeout(120000)});
 if(!response.ok||!response.body)throw new Error('Pinned public archive unavailable');
 let length=0;
 const limit=new Transform({transform(chunk,_encoding,callback){length+=chunk.length;callback(length>64*1024*1024?new Error('Archive exceeds bound'):null,chunk)}});
 // Retain a partial file on failure for diagnosis; never treat it as a cache hit.
 const partial=archive+'.partial';
 await pipeline(Readable.fromWeb(response.body),limit,createWriteStream(partial,{flags:'wx'}));
 if(hash(partial)!==pin.archive)throw new Error('Archive checksum mismatch');
 renameSync(partial,archive);
}
if(hash(archive)!==pin.archive)throw new Error('Cached archive checksum mismatch');
const quote=value=>"'"+value.replaceAll("'","''")+"'";
if(!existsSync(binary))run('powershell.exe',['-NoProfile','-NonInteractive','-Command',`Add-Type -AssemblyName System.IO.Compression.FileSystem; $zip=[IO.Compression.ZipFile]::OpenRead(${quote(archive)}); try { $entries=@($zip.Entries | Where-Object FullName -EQ 'xray.exe'); if ($entries.Count -ne 1 -or $entries[0].Length -gt 134217728) { throw 'Invalid executable entry' }; [IO.Compression.ZipFileExtensions]::ExtractToFile($entries[0],${quote(binary)}) } finally { $zip.Dispose() }`],{name:`xray-${version}-extract`});
if(hash(binary)!==pin.executable)throw new Error('Executable checksum mismatch');
run(go,['version','-m',binary],{name:`xray-${version}-build-info`});
const scan=goTool('govulncheck',['-mode=binary',binary],{name:`xray-${version}-binary-security`,allowFailure:true});
writeFileSync(path.join(work,'static-audit.json'),JSON.stringify({version,source:`https://github.com/XTLS/Xray-core/releases/tag/v${version}`,archiveSHA256:pin.archive,executableSHA256:pin.executable,scannedAt:new Date().toISOString(),exitCode:scan.status,executed:false,result:scan.status===0?'PASS':'FAIL',limitations:'Symbol-level binary scan; not exploit reproduction or production admission'},null,2)+'\n');
if(scan.status!==0||scan.error)process.exitCode=scan.status||1;
