// Formal adopted CLI/product composition, only local non-root WSL; no overlay.
import fs from'node:fs';import path from'node:path';import{spawnSync}from'node:child_process';import{createHash}from'node:crypto';import{root,go,env}from'../../../../scripts/lib.mjs';
const folder=path.join(root,'reports/wave6-r3');fs.mkdirSync(folder,{recursive:true});
function run(name,exe,args,options={}){const r=spawnSync(exe,args,{cwd:root,env,encoding:'utf8',windowsHide:true,timeout:240000,maxBuffer:24<<20,...options});fs.writeFileSync(path.join(folder,name+'.log'),(r.stdout??'')+(r.stderr??'')+(r.error?.message??''));if(r.status!==0)throw Error(name+' failed: reports/wave6-r3/'+name+'.log');return r.stdout;}
const head=run('head','git',['rev-parse','HEAD']).trim(),tree=run('tree','git',['status','--short']).trim();
if(process.argv.includes('--final')&&tree)throw Error('Final evidence requires clean HEAD');
const uid=run('uid','wsl.exe',['-d','Ubuntu','--exec','id','-u']).trim();if(uid==='0'||!/^\d+$/.test(uid))throw Error('Non-root required');
const cross={env:{...env,GOOS:'linux',GOARCH:'amd64',CGO_ENABLED:'0'}};
const bins={controller:path.join(folder,'controller'),agent:path.join(folder,'agent'),v1:path.join(folder,'agent-v1'),v2:path.join(folder,'agent-v2'),test:path.join(folder,'platform.test')};
run('controller-build',go,['build','-mod=readonly','-o',bins.controller,'./apps/controller'],cross);
run('agent-build',go,['build','-mod=readonly','-o',bins.agent,'./apps/agent'],cross);
for(const [name,version] of [['v1','1.0.0'],['v2','1.0.1']])run(name+'-build',go,['build','-mod=readonly','-ldflags=-X main.agentBuildVersion='+version,'-o',bins[name],'./apps/agent'],cross);
run('test-build',go,['test','-mod=readonly','-c','-o',bins.test,'./tests/integration/v09/platform'],cross);
const linux=p=>'/mnt/'+p[0].toLowerCase()+'/'+p.slice(3).replaceAll('\\','/');
const output=run('native','wsl.exe',['-d','Ubuntu','--exec','env','-u','TMPDIR','-u','TMP','-u','TEMP','SF_R3_CONTROLLER_BINARY='+linux(bins.controller),'SF_R3_AGENT_BINARY='+linux(bins.agent),'SF_R3_AGENT_V1='+linux(bins.v1),'SF_R3_AGENT_V2='+linux(bins.v2),linux(bins.test),'-test.v','-test.timeout=200s','-test.run=TestR3']);
if(process.argv.includes('--final')&&(run('final-head','git',['rev-parse','HEAD']).trim()!==head||run('final-tree','git',['status','--short']).trim()))throw Error('Code changed during final verification');
const evidence={head,workingTree:tree?'DIRTY DEVELOPMENT RUN':'CLEAN',uid,status:'PASS',passed:(output.match(/^--- PASS:/gm)??[]).length,skipped:(output.match(/^--- SKIP:/gm)??[]).length,artifacts:Object.fromEntries(Object.entries(bins).map(([name,p])=>[name,createHash('sha256').update(fs.readFileSync(p)).digest('hex')])),logSHA256:createHash('sha256').update(fs.readFileSync(path.join(folder,'native.log'))).digest('hex'),limits:'Actual local product Controller and two Agent CLIs with adopted CLI/product assembly and 25 formal migrations; no overlay/build tags. Not physical two-machine or production admission.'};
fs.writeFileSync(path.join(folder,'result.json'),JSON.stringify(evidence,null,2)+'\n');console.log(JSON.stringify(evidence));
