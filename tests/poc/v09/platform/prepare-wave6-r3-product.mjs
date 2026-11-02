// Product lifecycle/source assembly; CLI file candidate remains independent.
import './prepare-wave6-r3-cli.mjs';
import fs from'node:fs';import path from'node:path';import{spawnSync}from'node:child_process';import{root,go,env}from'../../../../scripts/lib.mjs';
const folder=path.join(root,'reports/wave6-r3'),progress=path.join(root,'docs/development/sprints/v09-five-day/progress/codex/wave6-r3');
const name='apps/controller/server/server.go';let source=fs.readFileSync(path.join(root,name),'utf8');
function edit(before,after){if(source.split(before).length!==2)throw Error('Nonunique product context '+before);source=source.replace(before,after);}
edit('\tUpdateExecution   *updates.TaskInstallWorkerOptions','\tUpdateProduct *updates.ProductDeploymentOptions\n\tUpdateExecution   *updates.TaskInstallWorkerOptions');
edit('type Server struct {','type Server struct {\n\tProductUpdates updates.ProductDeployment');
edit('\tif cfg.UpdateExecution != nil {',`\tif cfg.UpdateProduct!=nil {
 if cfg.UpdateExecution!=nil||cfg.UpdateApply!=nil||cfg.UpdateResolvePlan!=nil||cfg.UpdatePlan!=nil||cfg.UpdateCheck!=nil{return cleanup(updates.ErrArtifactRejected)}
 s.ProductUpdates,err=s.NewOwnedProductUpdates(*cfg.UpdateProduct);if err!=nil{return cleanup(err)}
 cfg.UpdateExecution=s.ProductUpdates.WorkerOptions();cfg.UpdateResolvePlan=s.ProductUpdates.ResolvePlan;cfg.UpdatePlan=s.ProductUpdates.Plan;cfg.UpdateCheck=s.ProductUpdates.Check
 }
\tif cfg.UpdateExecution != nil {`);
edit('\ts.startTaskScheduler()','\tif s.ProductUpdates!=nil {if err:=s.ProductUpdates.Start(ctx);err!=nil{return cleanup(err)}}\n\ts.startTaskScheduler()');
edit('\t\t_ = db.Close()','\t\tif s!=nil&&s.ProductUpdates!=nil { _=s.ProductUpdates.Close() }\n\t\t_ = db.Close()');
edit('\tif s.DB == nil {','\tif s.ProductUpdates!=nil {if err:=s.ProductUpdates.Close();err!=nil{return err};s.ProductUpdates=nil}\n\tif s.DB == nil {');
const after=path.join(folder,'server.product-proposed.txt');fs.writeFileSync(after,source);
let r=spawnSync(path.join(path.dirname(go),process.platform==='win32'?'gofmt.exe':'gofmt'),['-w',after],{cwd:root,env,encoding:'utf8',windowsHide:true});if(r.status!==0)throw Error(r.stderr);
r=spawnSync('git',['diff','--no-index','--',name,path.relative(root,after)],{cwd:root,encoding:'utf8',windowsHide:true});if(r.status!==1)throw Error('Product patch failed');
fs.writeFileSync(path.join(progress,'controller-product-updates.patch'),r.stdout.replace(/^diff --git .*$/m,'diff --git a/'+name+' b/'+name).replace(/^--- .*$/m,'--- a/'+name).replace(/^\+\+\+ .*$/m,'+++ b/'+name));
const overlay=JSON.parse(fs.readFileSync(path.join(folder,'cli-overlay.json'),'utf8'));overlay.Replace[path.join(root,name)]=after;fs.writeFileSync(path.join(folder,'product-overlay.json'),JSON.stringify(overlay,null,2)+'\n');
console.log('Prepared only new owned product lifecycle/source candidate; no shared mutations');
