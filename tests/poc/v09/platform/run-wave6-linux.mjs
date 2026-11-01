// Local WSL execution only. No SSH, services, privilege changes or live secrets.
import {spawnSync} from 'node:child_process';
import {mkdirSync,writeFileSync,readFileSync} from 'node:fs';
import {createHash} from 'node:crypto';
import path from 'node:path';
import {root,go,env} from '../../../../scripts/lib.mjs';
const target=process.argv[2]??'auth';
if(!['auth','agent','ai','updates','recovery','plugins','restore'].includes(target))throw new Error('Unknown target');
const folder=path.join(root,'reports/wave6-linux'); mkdirSync(folder,{recursive:true});
function run(name,exe,args,options={}){
 const r=spawnSync(exe,args,{cwd:root,env,encoding:'utf8',timeout:240000,maxBuffer:16*1024*1024,windowsHide:true,...options});
 writeFileSync(path.join(folder,name+'.log'),(r.stdout??'')+(r.stderr??'')+(r.error?.message??''));
 if(r.status!==0)throw new Error(name+' failed; see reports/wave6-linux');
 return r.stdout;
}
const uid=run(target+'-uid','wsl.exe',['-d','Ubuntu','--exec','id','-u']).trim();
if(!/^\d+$/.test(uid)||uid==='0')throw new Error('Existing non-root WSL user required');
const pkg=target==='restore'?'./tests/integration/v09/platform':target==='agent'?'./apps/agent':'./internal/'+target;
const binary=path.join(folder,target+'.test');
run(target+'-build',go,['test','-mod=readonly','-c','-o',binary,pkg],{env:{...env,GOOS:'linux',GOARCH:'amd64',CGO_ENABLED:'0'}});
const linux='/mnt/'+binary[0].toLowerCase()+'/'+binary.slice(3).replaceAll('\\','/');
const output=run(target+'-native','wsl.exe',['-d','Ubuntu','--exec','env','-u','TMPDIR','-u','TMP','-u','TEMP',linux,'-test.v','-test.timeout=180s',...(target==='restore'?['-test.run=TestWave6IsolatedSQLiteRestore']:[])]);
const evidence={target,uid,head:run(target+'-head','git',['rev-parse','HEAD']).trim(),binarySHA256:createHash('sha256').update(readFileSync(binary)).digest('hex'),passed:(output.match(/^--- PASS:/gm)??[]).length,skipped:(output.match(/^--- SKIP:/gm)??[]).length,status:'PASS',limits:'Local WSL amd64; no remote, arm64 or external-provider evidence'};
writeFileSync(path.join(folder,target+'.json'),JSON.stringify(evidence,null,2)+'\n');
console.log(JSON.stringify(evidence));
