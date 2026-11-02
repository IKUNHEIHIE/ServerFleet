// New CLI candidate only. Never replay adopted Wave6-R2 registration patches.
import fs from 'node:fs';import path from 'node:path';import{spawnSync}from'node:child_process';import{root,go,env}from'../../../../scripts/lib.mjs';
const folder=path.join(root,'reports/wave6-r3'),progress=path.join(root,'docs/development/sprints/v09-five-day/progress/codex/wave6-r3');
fs.mkdirSync(folder,{recursive:true});fs.mkdirSync(progress,{recursive:true});
const name='apps/controller/bootstrap/bootstrap.go';let source=fs.readFileSync(path.join(root,name),'utf8');
function edit(before,after){if(source.split(before).length!==2)throw Error('Nonunique CLI context');source=source.replace(before,after);}
edit('\tCredentialKeyDir string','\tFileDeployment string\n\tCredentialKeyDir string');
edit('\tif err := flags.Parse(args);','\tflags.StringVar(&options.FileDeployment,"file-deployment","","private startup node-root registry (Linux; requires management TLS mode)")\n\tif err := flags.Parse(args);');
edit('func (o Options) Validate() error {','func (o Options) Validate() error {\n if o.FileDeployment!="" && o.Scaffold(){return errors.New("controller: -file-deployment requires -db and direct HTTPS")}');
edit('func Compose(ctx context.Context, options Options) (*server.Server, *localpki.Material, error) {','func Compose(ctx context.Context, options Options) (*server.Server, *localpki.Material, error) {\n if err:=options.Validate();err!=nil{return nil,nil,err}');
edit('\tdirectory, err := options.LocalPKIDirectory()','\tvar fileConfig *server.FileExecutionConfig\n if options.FileDeployment!="" {var err error;fileConfig,err=server.LoadFileExecutionConfig(options.FileDeployment);if err!=nil{return nil,nil,err}}\n\tdirectory, err := options.LocalPKIDirectory()');
edit('\t\tDBPath:           options.DBPath,','\t\tFileExecution: fileConfig,\n\t\tDBPath:           options.DBPath,');
const after=path.join(folder,'bootstrap.proposed.txt');fs.writeFileSync(after,source);
let r=spawnSync(path.join(path.dirname(go),process.platform==='win32'?'gofmt.exe':'gofmt'),['-w',after],{cwd:root,env,encoding:'utf8',windowsHide:true});if(r.status!==0)throw Error(r.stderr);
r=spawnSync('git',['diff','--no-index','--',name,path.relative(root,after)],{cwd:root,encoding:'utf8',windowsHide:true});if(r.status!==1)throw Error('CLI patch failed');
fs.writeFileSync(path.join(progress,'controller-file-cli.patch'),r.stdout.replace(/^diff --git .*$/m,'diff --git a/'+name+' b/'+name).replace(/^--- .*$/m,'--- a/'+name).replace(/^\+\+\+ .*$/m,'+++ b/'+name));
fs.writeFileSync(path.join(folder,'cli-overlay.json'),JSON.stringify({Replace:{[path.join(root,name)]:after}},null,2)+'\n');
console.log('Prepared only new startup CLI patch; adopted central registration/DDL remain untouched');
