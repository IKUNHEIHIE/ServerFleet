// This candidate applies AFTER controller-files.patch + controller-ai-budget.patch.
// Only report overlays and review patches are written; shared sources stay intact.
import fs from 'node:fs';import path from 'node:path';import{spawnSync}from'node:child_process';import{root,go,env}from'../../../../scripts/lib.mjs';
import './prepare-wave6-r2-ai-overlays.mjs';
const folder=path.join(root,'reports/wave6-r2'),progress=path.join(root,'docs/development/sprints/v09-five-day/progress/codex/wave6-r2');
const overlay=JSON.parse(fs.readFileSync(path.join(folder,'combined-overlay.json'),'utf8'));const sources=new Map(),originals=new Map();
function edit(name,before,after){const old=sources.get(name)??fs.readFileSync(overlay.Replace[path.join(root,name)]??path.join(root,name),'utf8');if(!originals.has(name))originals.set(name,old);if(old.split(before).length!==2)throw new Error('Nonunique platform patch context '+name+' '+before);sources.set(name,old.replace(before,after));}
const server='apps/controller/server/server.go';
edit(server,'\tUpdateCheck     func','\tRecoveryLatch *recovery.QuarantineLatch\n\tUpdateExecution *updates.TaskInstallWorkerOptions\n\tUpdateResolvePlan func(context.Context,updates.ApplyRequest)(updates.FrozenInstallPlan,error)\n\tUpdateCheck     func');
edit(server,'type Server struct {','type Server struct {\n\truntimeGate func(context.Context)error');
edit(server,'func New(ctx context.Context, cfg Config) (*Server, error) {','func New(ctx context.Context, cfg Config) (*Server, error) {\n\truntimeGate,guardErr:=recovery.RuntimeGuard(ctx,cfg.DBPath,cfg.RecoveryLatch);if guardErr!=nil{return nil,guardErr}');
edit(server,'s = &Server{','s = &Server{\n\t\truntimeGate: runtimeGate,');
edit(server,'func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {','func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {\n\tif s.runtimeGate!=nil && s.runtimeGate(r.Context())!=nil {w.Header().Set("Cache-Control","no-store");w.Header().Set("Content-Type","application/problem+json");w.WriteHeader(503);_ = json.NewEncoder(w).Encode(map[string]any{"type":"about:blank","title":"Service Unavailable","status":503,"code":"CONTROLLER_QUARANTINED"});return}');
edit(server,'\t\tfiles.Migrations(),','\t\tfiles.Migrations(),\n\t\t{{Version:"cx/0009_update_journal",Name:"review candidate installed provenance and audited receipts",Statements:updates.InstallJournalSchemaStatements()}},');
edit(server,'\t\t"ai.budget.configure",','\t\tupdates.ActionInstall,\n\t\t"ai.budget.configure",');
edit(server,'return []auth.PermissionDefinition{','return []auth.PermissionDefinition{\n\t\t{Key:updates.ActionInstall,Description:"Install verified releases on granted owned targets",ResourceType:"node",AdminOnly:true},');
edit(server,'\ttasksService, err := tasks.NewService(tasks.Options{',`\tif cfg.UpdateExecution != nil {
 if cfg.UpdateResolvePlan==nil || cfg.UpdateApply!=nil {return cleanup(updates.ErrArtifactRejected)}
 options:=*cfg.UpdateExecution
 options.Current=func(ctx context.Context,id,plan string)(tasks.Task,error){return s.Tasks.CheckExecution(ctx,id,plan)}
 options.Administrator=s.Identity.AuthorizeAdministrator
 worker,e:=updates.NewTaskInstallWorker(options);if e!=nil{return cleanup(e)};workers[updates.ActionInstall]=worker
 cfg.UpdateApply=func(ctx context.Context,r updates.ApplyRequest)(updates.ApplyResult,error){
  if !r.Principal.Administrator || s.Identity.AuthorizeAdministrator(ctx,r.Principal.SubjectID)!=nil{return updates.ApplyResult{},updates.ErrArtifactRejected}
  p,e:=cfg.UpdateResolvePlan(ctx,r);if e!=nil||p.ID!=r.PlanID||p.SubjectID!=r.Principal.SubjectID||r.Confirmation!=p.Attempted.Version||r.BackupMode!="required"{return updates.ApplyResult{},updates.ErrArtifactRejected}
  request,e:=p.TaskRequest(r.Principal.RequestID);if e!=nil{return updates.ApplyResult{},e};t,e:=s.Tasks.Submit(ctx,request);if e!=nil{return updates.ApplyResult{},e};return updates.ApplyResult{ID:p.ID,TaskID:t.ID,Status:string(t.Status),TargetVersion:p.Attempted.Version},nil
 }
 }
\ttasksService, err := tasks.NewService(tasks.Options{\n\t\tRuntimeGate: runtimeGate,`);
edit('apps/controller/server/enrollment.go','transport.ControllerLinkConfig{','transport.ControllerLinkConfig{\n\t\tRuntimeGate: s.runtimeGate,');
const suffix=process.platform==='win32'?'.exe':'';let patch='';
for(const[name,source]of sources){const before=path.join(folder,path.basename(name)+'.platform-before.txt'),after=path.join(folder,path.basename(name)+'.platform-after.txt');fs.writeFileSync(before,originals.get(name));fs.writeFileSync(after,source);for(const file of[before,after]){const r=spawnSync(path.join(path.dirname(go),'gofmt'+suffix),['-w',file],{cwd:root,env,encoding:'utf8',windowsHide:true});if(r.status!==0)throw new Error(r.stderr);}
const diff=spawnSync('git',['diff','--no-index','--',path.relative(root,before),path.relative(root,after)],{cwd:root,encoding:'utf8',windowsHide:true});if(diff.status!==1)throw new Error('Platform diff failed');patch+=diff.stdout.replace(/^diff --git .*$/m,'diff --git a/'+name+' b/'+name).replace(/^--- .*$/m,'--- a/'+name).replace(/^\+\+\+ .*$/m,'+++ b/'+name);overlay.Replace[path.join(root,name)]=after;}
fs.writeFileSync(path.join(progress,'controller-platform.patch'),patch);fs.writeFileSync(path.join(folder,'platform-overlay.json'),JSON.stringify(overlay,null,2)+'\n');console.log('Prepared platform patch and composition overlay; migration number is candidate for controller approval');
