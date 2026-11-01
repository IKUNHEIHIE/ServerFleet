// API/root registration remain concrete review patches, never shared mutations.
import fs from 'node:fs';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
import {root} from '../../../../scripts/lib.mjs';
const progress='docs/development/sprints/v09-five-day/progress/codex/wave6-r2/';
const target='api/openapi/revisions/v09-r7/updates-candidate.yaml';
const source=fs.readFileSync(path.join(root,progress,'updates-http-candidate.yaml'),'utf8').replaceAll('\r\n','\n');
let patch=`diff --git a/${target} b/${target}\nnew file mode 100644\n--- /dev/null\n+++ b/${target}\n@@ -0,0 +1,${source.trimEnd().split('\n').length} @@\n`+source.trimEnd().split('\n').map(line=>'+'+line).join('\n')+'\n';
const before=path.join(root,'reports/wave6-r2/redocly.proposed.yaml');
const after=path.join(root,'reports/wave6-r2/redocly.updates-proposed.yaml');
fs.writeFileSync(after,fs.readFileSync(before,'utf8').trimEnd()+'\n  v09-r7-updates:\n    root: api/openapi/revisions/v09-r7/updates-candidate.yaml\n');
const diff=spawnSync('git',['diff','--no-index','--',path.relative(root,before),path.relative(root,after)],{cwd:root,encoding:'utf8',windowsHide:true});
if(diff.status!==1)throw new Error('Update contract registration diff failed');
patch+=diff.stdout.replace(/^diff --git .*$/m,'diff --git a/redocly.yaml b/redocly.yaml').replace(/^--- .*$/m,'--- a/redocly.yaml').replace(/^\+\+\+ .*$/m,'+++ b/redocly.yaml');
fs.writeFileSync(path.join(root,progress,'updates-openapi.patch'),patch);
console.log('Prepared update API/root registration review patch after redocly-ai-budget.patch');
