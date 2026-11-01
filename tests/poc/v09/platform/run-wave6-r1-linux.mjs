// Existing local non-root WSL only; no SSH, installation or external credentials.
import {spawnSync} from 'node:child_process';
import {mkdirSync,readFileSync,writeFileSync} from 'node:fs';
import path from 'node:path';
import {createHash} from 'node:crypto';
import {root,go,env} from '../../../../scripts/lib.mjs';
const target=process.argv[2]??'agent';
const packages={agent:'./apps/agent',transport:'./internal/transport',ai:'./internal/ai',budget:'./tests/integration/v09/platform',controller:'./tests/integration/v09/platform',updates:'./internal/updates',recovery:'./internal/recovery'};
if(!packages[target])throw new Error('Unknown R1 target');
const folder=path.join(root,'reports/wave6-r1-linux');mkdirSync(folder,{recursive:true});
function run(name,exe,args,options={}){const r=spawnSync(exe,args,{cwd:root,env,encoding:'utf8',timeout:240000,maxBuffer:16*1024*1024,windowsHide:true,...options});writeFileSync(path.join(folder,name+'.log'),(r.stdout??'')+(r.stderr??'')+(r.error?.message??''));if(r.status!==0)throw new Error(name+' failed; inspect reports/wave6-r1-linux');return r.stdout;}
const uid=run(target+'-uid','wsl.exe',['-d','Ubuntu','--exec','id','-u']).trim();if(!/^\d+$/.test(uid)||uid==='0')throw new Error('Non-root WSL required');
if(target==='controller')run(target+'-overlay',process.execPath,['tests/poc/v09/platform/prepare-wave6-r1-overlays.mjs']);
const binary=path.join(folder,target+'.test');
run(target+'-build',go,['test','-mod=readonly','-c','-o',binary,...(target==='controller'?['-tags=cx_r1_controller','-overlay=reports/wave6-r1/combined-overlay.json']:[]),packages[target]],{env:{...env,GOOS:'linux',GOARCH:'amd64',CGO_ENABLED:'0'}});
const linux='/mnt/'+binary[0].toLowerCase()+'/'+binary.slice(3).replaceAll('\\','/');
const selection=target==='budget'?['-test.run=TestPersistentGenerationBudgetSQLite']:target==='controller'?['-test.run=TestR1ControllerHTTPBorrowed']:[];
const output=run(target+'-native','wsl.exe',['-d','Ubuntu','--exec','env','-u','TMPDIR','-u','TMP','-u','TEMP',linux,'-test.v','-test.timeout=180s',...selection]);
const evidence={target,uid,head:run(target+'-head','git',['rev-parse','HEAD']).trim(),workingTree:run(target+'-tree','git',['status','--short']).trim(),binarySHA256:createHash('sha256').update(readFileSync(binary)).digest('hex'),passed:(output.match(/^--- PASS:/gm)??[]).length,skipped:(output.match(/^--- SKIP:/gm)??[]).length,status:'PASS',limits:'Local WSL amd64 only. Controller target uses explicit unadopted shared patches through Go overlay.'};
writeFileSync(path.join(folder,target+'.json'),JSON.stringify(evidence,null,2)+'\n');console.log(JSON.stringify({...evidence,workingTree:evidence.workingTree?'DIRTY':'CLEAN'}));
