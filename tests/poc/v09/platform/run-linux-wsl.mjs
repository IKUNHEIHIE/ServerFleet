// Existing LOCAL WSL only. No SSH/cloud, install/service changes, sudo, real
// credentials or external providers. Cross-compile here; execute as WSL's
// non-root default user. Fixtures use Go-created private directories in /tmp.
import {spawn,spawnSync} from 'node:child_process';
import {createHash,randomBytes} from 'node:crypto';
import {mkdirSync,readFileSync,writeFileSync} from 'node:fs';
import path from 'node:path';
import {root,go,env} from '../../../../scripts/lib.mjs';

if(process.platform!=='win32'||process.arch!=='x64')throw new Error('This runner requires Windows amd64 plus an existing local Ubuntu WSL');
const reportRoot=path.join(root,'reports/_review/v09-platform/linux-native');
const runID=new Date().toISOString().replaceAll(':','').replaceAll('.','')+'-'+randomBytes(4).toString('hex');
const work=path.join(reportRoot,runID);
if(path.relative(root,work).startsWith('..'))throw new Error('Invalid output directory');
mkdirSync(work,{recursive:true});
const digest=file=>createHash('sha256').update(readFileSync(file)).digest('hex');
const git=args=>{const r=spawnSync('git',args,{cwd:root,encoding:'utf8'});if(r.status!==0)throw new Error('Git evidence unavailable');return r.stdout.trim()};
const head=git(['rev-parse','HEAD']);
const sourceInventory=()=>[...new Set(git(['ls-files','--cached','--others','--exclude-standard','-z']).split('\0').filter(p=>p.endsWith('.go')||p.startsWith('tests/poc/v09/platform/')&&(p.endsWith('.txt')||p.endsWith('.mjs'))))].sort();
const sourceFiles=sourceInventory();
const protectedFiles=git(['ls-files','go.mod','go.sum','package.json','package-lock.json','dependencies.json','api','proto','gen','.github','AGENTS.md']).split(/\r?\n/).filter(Boolean);
const hashFiles=files=>Object.fromEntries(files.map(p=>[p,digest(path.join(root,p))]));
const beforeSource=hashFiles(sourceFiles),beforeProtected=hashFiles(protectedFiles);
// WSL's Windows diagnostic prefix may be UTF-16 followed by Linux UTF-8 stderr.
const decode=buffer=>{
 if(buffer.length>1&&buffer[1]===0){const end=buffer.indexOf(Buffer.from('\r\n','utf16le'));return end<0?buffer.toString('utf16le'):buffer.subarray(0,end+4).toString('utf16le')+decode(buffer.subarray(end+4))}
 return buffer.toString('utf8');
};

async function command(name,exe,args,options={}){
 const started=Date.now();const chunks={out:[],err:[]};let size=0;
 const child=spawn(exe,args,{cwd:root,env,windowsHide:true,timeout:180000,...options});
 let error;
 child.on('error',e=>{error=e.message});
 for(const [stream,key]of [[child.stdout,'out'],[child.stderr,'err']])stream.on('data',b=>{size+=b.length;if(size>32*1024*1024){error='Output exceeds bound';child.kill()}else chunks[key].push(b)});
 const code=await new Promise(resolve=>child.on('close',resolve));
 const rawOut=Buffer.concat(chunks.out),rawErr=Buffer.concat(chunks.err);
 writeFileSync(path.join(work,name+'.stdout.bin'),rawOut);writeFileSync(path.join(work,name+'.stderr.bin'),rawErr);
 const stdout=decode(rawOut),stderr=decode(rawErr);
 const log=path.join(work,name+'.log');writeFileSync(log,stdout+stderr+(error?'\n'+error:''));
 const result={name,status:code===0&&!error?'PASS':'FAIL',exitCode:code,elapsedMS:Date.now()-started,log:path.relative(root,log).replaceAll('\\','/'),logSHA256:digest(log)};
 if(result.status!=='PASS')throw Object.assign(new Error(`${name} failed; inspect ${result.log}`),{result});
 return {...result,stdout};
}

const flags=process.argv.slice(2);
if(flags.length>1||flags.length===1&&!flags[0].startsWith('--targets='))throw new Error('Use optional --targets=ai,sqlite with known package names');
const requested=flags.length?flags[0].slice('--targets='.length).split(','):null;
if(requested&&(requested.some(x=>!x)||new Set(requested).size!==requested.length))throw new Error('Empty or duplicate package selection');
const evidence={head,runID,startedAt:new Date().toISOString(),kind:'LOCAL_WSL_LINUX_AMD64',selection:requested?'subset':'all',requested,status:'RUNNING',builds:[],results:[],sourceSHA256:beforeSource,limitations:['Not physical VPS/Debian/systemd installation or arm64 runtime','No 1 CPU / 1 GiB resource claim','Domain, runtime, profile and model fixtures remain explicitly scoped','External Xray artifacts remain blocked; native pure tests do not execute Xray']};
const save=()=>{
 writeFileSync(path.join(work,'EVIDENCE.json'),JSON.stringify(evidence,null,2)+'\n');
 writeFileSync(path.join(reportRoot,'latest.json'),JSON.stringify({runID,status:evidence.status,evidence:path.relative(root,path.join(work,'EVIDENCE.json')).replaceAll('\\','/')},null,2)+'\n');
};
save();
try{
 const uid=await command('uid','wsl.exe',['--distribution','Ubuntu','--exec','id','-u']);
 if(!/^\d+$/.test(uid.stdout.trim())||uid.stdout.trim()==='0')throw new Error('A non-root existing WSL user is required');
 const kernel=await command('kernel','wsl.exe',['--distribution','Ubuntu','--exec','uname','-srm']);
 const release=await command('release','wsl.exe',['--distribution','Ubuntu','--exec','cat','/etc/os-release']);
 if(!kernel.stdout.startsWith('Linux ')||!kernel.stdout.includes('x86_64'))throw new Error('Expected native x86_64 Linux');
 evidence.environment={uid:uid.stdout.trim(),kernel:kernel.stdout.trim(),release:release.stdout.trim(),compiler:(await command('compiler',go,['version'])).stdout.trim()};
 for(const script of ['run.mjs','run-identity.mjs'])await command('prepare-'+script,process.execPath,[path.join(root,'tests/poc/v09/platform',script),'--prepare-only']);
 const isolated=path.join(root,'reports/_review/v09-platform');
 const targets=[
  ['agent',root,'./apps/agent'],['auth',root,'./internal/auth'],['transport',root,'./internal/transport'],['ai',root,'./internal/ai'],
  ['updates',root,'./internal/updates'],['recovery',root,'./internal/recovery'],['plugins',root,'./internal/plugins'],['workflows',root,'./internal/workflows'],['xray-pure',root,'./plugins/xray-manager'],
  ['sqlite',path.join(isolated,'identity-poc'),'.'],['wss-wasm',path.join(isolated,'poc'),'.'],['agent-wss',path.join(isolated,'poc'),'./agentapp'],
 ];
 if(requested?.some(name=>!targets.some(t=>t[0]===name)))throw new Error('Unknown native package selection');
 const selected=requested?targets.filter(t=>requested.includes(t[0])):targets;
 const built=[];
 for(const [name,cwd,pkg]of selected){
  console.log(`BUILD linux/amd64 ${name}`);
  const binary=path.join(work,name+'.test');
  const result=await command('build-'+name,go,['test','-mod=readonly','-c','-o',binary,pkg],{cwd,env:{...env,GOOS:'linux',GOARCH:'amd64',CGO_ENABLED:'0'}});
  const linuxBinary='/mnt/'+binary[0].toLowerCase()+'/'+binary.slice(3).replaceAll('\\','/');
  if(!/^[A-Za-z]:[\\/]/.test(binary)||!linuxBinary.includes('/reports/_review/v09-platform/linux-native/'))throw new Error('Unmapped output');
  built.push({name,binary,linuxBinary,build:result,binarySHA256:digest(binary)});
  const {stdout,...buildMetadata}=result;evidence.builds.push({...buildMetadata,package:name,binarySHA256:digest(binary)});save();
 }
 for(const entry of built){
  console.log(`RUN native Linux uid=${evidence.environment.uid} ${entry.name}`);
  const result=await command('native-'+entry.name,'wsl.exe',['--distribution','Ubuntu','--exec','env','-u','SF_XRAY_LOCAL_TEST_BINARY','-u','TMPDIR','-u','TMP','-u','TEMP',entry.linuxBinary,'-test.v','-test.timeout=120s']);
  if(digest(entry.binary)!==entry.binarySHA256)throw new Error('Test executable changed during run');
  const {stdout,...metadata}=result;
  evidence.results.push({...metadata,package:entry.name,binarySHA256:entry.binarySHA256,buildLog:entry.build.log,passed:(stdout.match(/^--- PASS:/gm)||[]).length,skipped:(stdout.match(/^--- SKIP:/gm)||[]).length});
  save();console.log(`PASS ${entry.name}: ${evidence.results.at(-1).passed} tests; ${evidence.results.at(-1).skipped} skipped`);
 }
 if(git(['rev-parse','HEAD'])!==head||JSON.stringify(sourceInventory())!==JSON.stringify(sourceFiles)||JSON.stringify(hashFiles(sourceFiles))!==JSON.stringify(beforeSource)||JSON.stringify(hashFiles(protectedFiles))!==JSON.stringify(beforeProtected))throw new Error('Sources or protected dependencies changed during verification');
 evidence.status='PASS';evidence.finishedAt=new Date().toISOString();save();
 console.log(`PASS native Linux ${evidence.results.length} packages; full release/arm64/1C1G remain unverified`);
}catch(error){
 evidence.status='FAIL';evidence.finishedAt=new Date().toISOString();evidence.failure=error.message;if(error.result)evidence.results.push(error.result);save();throw error;
}
