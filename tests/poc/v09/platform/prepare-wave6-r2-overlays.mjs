// New R2 candidate only. Shared source is NEVER modified by this preparer.
import fs from 'node:fs';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
import {root,go,env} from '../../../../scripts/lib.mjs';
const report=path.join(root,'reports/wave6-r2');fs.mkdirSync(report,{recursive:true});
const progress=path.join(root,'docs/development/sprints/v09-five-day/progress/codex/wave6-r2');fs.mkdirSync(progress,{recursive:true});
const files=new Map();
function replace(name,before,after){let source=files.get(name)??fs.readFileSync(path.join(root,name),'utf8');if(source.split(before).length!==2)throw new Error('Nonunique candidate context '+name+' '+before.slice(0,70));files.set(name,source.replace(before,after));}
const server='apps/controller/server/server.go',bridge='apps/controller/server/agent_links.go';
replace(server,'\tAgentFileGate    transport.FileFrameGate','\tAgentFileGate    transport.FileFrameGate\n\tFileExecution *FileExecutionConfig');
replace(server,'\tAgentFiles           *transport.FileClient','\tAgentFiles           *transport.FileClient\n\tFileWorker *FileExecutionWorker');
replace(server,'\t\tfiles.Migrations(),','\t\tfiles.Migrations(),\n\t\t{files.ExecutionBindingsMigration()},');
replace(server,'\tagentBridge, err := s.setupAgentTransport(cfg, nodesService, tasksRepo)',`\tif cfg.FileExecution != nil {
  if cfg.AgentFileGate != nil || cfg.EnrollmentIssuer == nil || cfg.AgentAuthority == "" {return cleanup(files.ErrTransferExecution)}
  s.FileWorker, err = NewFileExecutionWorker(s,filesRepo,authorizer,*cfg.FileExecution)
  if err != nil {return cleanup(err)}
  cfg.AgentFileGate = s.FileWorker.Gate
 }
\tagentBridge, err := s.setupAgentTransport(cfg, nodesService, tasksRepo)`);
replace(server,'\ttasksService, err := tasks.NewService(tasks.Options{',`\tworkers:=map[string]tasks.Worker{}
 if s.FileWorker != nil {workers[files.ActionTransfer]=s.FileWorker;agentBridge.fileWorker=s.FileWorker}
\ttasksService, err := tasks.NewService(tasks.Options{\n\t\tWorkers: workers,`);
replace(server,'\tfilesService, err := files.NewService(files.Options{',`\tvar fileSubmitter files.TaskSubmitter = taskBridge
 var validateTransfer func(context.Context,files.TransferCommand)error
 if s.FileWorker != nil {fileSubmitter=s.FileWorker;validateTransfer=s.FileWorker.ValidateTransfer}
\tfilesService, err := files.NewService(files.Options{\n\t\tValidateTransfer: validateTransfer,`);
replace(server,'\t\tSubmitter:  taskBridge,','\t\tSubmitter:  fileSubmitter,');
replace(server,'\ts.Files = filesService','\ts.Files = filesService\n\tif s.FileWorker != nil {if _,err:=s.Files.RecoverTransfers(ctx);err!=nil{return cleanup(err)};if _,err:=s.Tasks.Recover(ctx);err!=nil{return cleanup(err)}}');
replace(server,'\tif s.runCancel != nil {','\tif s.Links != nil {closeCtx,cancel:=context.WithTimeout(context.Background(),5*time.Second);err:=s.Links.Close(closeCtx);cancel();if err!=nil{return err}}\n\tif s.runCancel != nil {');
replace(bridge,'\tfiles        *transport.FileClient','\tfiles        *transport.FileClient\n\tfileWorker *FileExecutionWorker');
replace(bridge,'\tif b.files != nil {\n\t\tconsumed, err := b.files.Deliver',`\tfileChild:=false
 if b.fileWorker != nil {var err error;fileChild,err=b.fileWorker.Observe(ctx,observation,envelope);if err != nil {return err}}
\tif b.files != nil {\n\t\tconsumed, err := b.files.Deliver`);
replace(bridge,'\tif output := envelope.GetTaskOutput(); output != nil {','\tif fileChild {return nil}\n\tif output := envelope.GetTaskOutput(); output != nil {');
replace(bridge,'task.ActionID == transport.FileCapability {','(task.ActionID == transport.FileCapability || task.ActionID == "files.transfer") {');
const overlay={Replace:{}};let patch='';
for(const[name,source]of files){const target=path.join(report,path.basename(name)+'.proposed.txt');fs.writeFileSync(target,source);overlay.Replace[path.join(root,name)]=target;
 const fmt=spawnSync(path.join(path.dirname(go),process.platform==='win32'?'gofmt.exe':'gofmt'),['-w',target],{cwd:root,env,encoding:'utf8',windowsHide:true});if(fmt.status!==0)throw new Error('Candidate formatting failed '+name);
 const diff=spawnSync('git',['diff','--no-index','--',name,path.relative(root,target)],{cwd:root,encoding:'utf8',windowsHide:true});if(diff.status!==1)throw new Error('No candidate diff '+name);
 patch+=diff.stdout.replace(/^diff --git .*$/m,'diff --git a/'+name+' b/'+name).replace(/^--- .*$/m,'--- a/'+name).replace(/^\+\+\+ .*$/m,'+++ b/'+name);
}
fs.writeFileSync(path.join(progress,'controller-files.patch'),patch);
fs.writeFileSync(path.join(report,'files-overlay.json'),JSON.stringify(overlay,null,2)+'\n');
console.log('Prepared R2 central registration / DDL candidate; shared source untouched');
