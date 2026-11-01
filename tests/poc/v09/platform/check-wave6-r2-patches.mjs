// Apply ONLY to a private throwaway Git index; never to any shared source file.
import fs from'node:fs';import path from'node:path';import{spawnSync}from'node:child_process';import{root,env}from'../../../../scripts/lib.mjs';
const folder=path.join(root,'reports/wave6-r2');fs.mkdirSync(folder,{recursive:true});const index=path.join(folder,'review-patches.index');
function git(args){const r=spawnSync('git',args,{cwd:root,env:{...env,GIT_INDEX_FILE:index},encoding:'utf8',windowsHide:true});if(r.status!==0)throw new Error(args.join(' ')+'\n'+r.stdout+r.stderr);}
git(['read-tree','HEAD']);
const patches=['controller-files.patch','controller-ai-budget.patch','controller-platform.patch','redocly-ai-budget.patch','updates-openapi.patch'];
for(const patch of patches)git(['apply','--cached','--','docs/development/sprints/v09-five-day/progress/codex/wave6-r2/'+patch]);
git(['diff','--cached','--check']);fs.writeFileSync(path.join(folder,'patch-stack.json'),JSON.stringify({status:'PASS',patches,sharedWorkingFilesModified:false,temporaryIndex:path.relative(root,index)},null,2)+'\n');console.log('PASS: complete review patch stack applies cleanly in private index; shared worktree untouched');
