// Construct Go overlays in ignored reports. NEVER patch shared source files.
import fs from 'node:fs';
import path from 'node:path';
import {root} from '../../../../scripts/lib.mjs';
const progress=path.join(root,'docs/development/sprints/v09-five-day/progress/codex/wave6-r1');
const report=path.join(root,'reports/wave6-r1');fs.mkdirSync(report,{recursive:true});
const files=new Map();
for(const patchName of ['controller-files.patch','controller-ai.patch']){
 const patch=fs.readFileSync(path.join(progress,patchName),'utf8').replaceAll('\r\n','\n').split('\n');let current;
 for(let i=0;i<patch.length;i++){
  if(patch[i].startsWith('+++ b/')){
   current=patch[i].slice(6);if(!current.startsWith('apps/controller/server/')||current.includes('..'))throw new Error('Unexpected shared patch path');
   if(!files.has(current))files.set(current,fs.readFileSync(path.join(root,current),'utf8').split('\n'));
  }else if(patch[i].startsWith('@@ ')){
   const before=[],after=[];
   for(i++;i<patch.length&&!patch[i].startsWith('@@ ')&&!patch[i].startsWith('--- ');i++){
    const line=patch[i];if(!line)continue;const kind=line[0],value=line.slice(1);
    if(kind===' '||kind==='-')before.push(value);if(kind===' '||kind==='+')after.push(value);
   }i--;
   const lines=files.get(current),matches=[];
   for(let pos=0;pos+before.length<=lines.length;pos++)if(before.every((value,n)=>lines[pos+n]===value))matches.push(pos);
   if(matches.length!==1)throw new Error('Patch context does not match uniquely: '+current);
   lines.splice(matches[0],before.length,...after);
  }
 }
}
const overlay={Replace:{}};
for(const [name,lines]of files){const target=path.join(report,path.basename(name)+'.combined.txt');fs.writeFileSync(target,lines.join('\n'));overlay.Replace[path.join(root,name)]=target;}
const target=path.join(report,'combined-overlay.json');fs.writeFileSync(target,JSON.stringify(overlay,null,2)+'\n');
console.log('Prepared shared Controller overlays without modifying source.');
