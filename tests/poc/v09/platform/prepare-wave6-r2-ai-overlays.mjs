// Separate AI registration patch; compose the file candidate for integration only.
import fs from 'node:fs';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
import {root,go,env} from '../../../../scripts/lib.mjs';
import './prepare-wave6-r2-overlays.mjs';
const report=path.join(root,'reports/wave6-r2');
const progress=path.join(root,'docs/development/sprints/v09-five-day/progress/codex/wave6-r2');
const server='apps/controller/server/server.go';
const before='\ts.registerAIRoutes()';
const after=before+'\n\tif err := s.RegisterAIBudgetRoutes(); err != nil {return err}';
function replaceOnce(source){if(source.split(before).length!==2)throw new Error('Nonunique AI registration context');return source.replace(before,after);}
function candidate(name,source,output){
 const target=path.join(report,output);fs.writeFileSync(target,source);
 if(name.endsWith('.go')){const r=spawnSync(path.join(path.dirname(go),process.platform==='win32'?'gofmt.exe':'gofmt'),['-w',target],{cwd:root,env,encoding:'utf8',windowsHide:true});if(r.status!==0)throw new Error(r.stderr);}
 return target;
}
function patch(name,target,output){
 const r=spawnSync('git',['diff','--no-index','--',name,path.relative(root,target)],{cwd:root,encoding:'utf8',windowsHide:true});if(r.status!==1)throw new Error('Candidate diff failed');
 fs.writeFileSync(path.join(progress,output),r.stdout.replace(/^diff --git .*$/m,'diff --git a/'+name+' b/'+name).replace(/^--- .*$/m,'--- a/'+name).replace(/^\+\+\+ .*$/m,'+++ b/'+name));
}
patch(server,candidate(server,replaceOnce(fs.readFileSync(path.join(root,server),'utf8')),'ai-server.proposed.txt'),'controller-ai-budget.patch');
const overlay=JSON.parse(fs.readFileSync(path.join(report,'files-overlay.json'),'utf8'));
overlay.Replace[path.join(root,server)]=candidate(server,replaceOnce(fs.readFileSync(overlay.Replace[path.join(root,server)],'utf8')),'combined-server.proposed.txt');
fs.writeFileSync(path.join(report,'combined-overlay.json'),JSON.stringify(overlay,null,2)+'\n');
const redocly='redocly.yaml';
patch(redocly,candidate(redocly,fs.readFileSync(path.join(root,redocly),'utf8').trimEnd()+'\n  v09-r7-ai-budget:\n    root: api/openapi/revisions/v09-r7/ai-budget-candidate.yaml\n','redocly.proposed.yaml'),'redocly-ai-budget.patch');
console.log('Prepared independent AI route/lint patches and combined test overlay');
