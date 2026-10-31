#!/usr/bin/env python3
"""Static contract checks only; does NOT replace OpenAPI validator, buf lint, generation or security tests."""
from pathlib import Path
from collections import Counter
import yaml, json, re, hashlib
root=Path(__file__).resolve().parents[2]
g2=root/'g2'
base=yaml.safe_load((root/'ServerFleet-API-Contract-v0.9.yaml').read_text())
core=yaml.safe_load((g2/'G2-Core-OpenAPI-candidate.yaml').read_text())
combined=yaml.safe_load((g2/'ServerFleet-API-Contract-v0.9-G2-merged-candidate.yaml').read_text())
xray=yaml.safe_load((root/'Plugin/xray-manager/Xray-Manager-OpenAPI.yaml').read_text())
methods={'get','put','post','delete','patch','head','options','trace'}
count=lambda doc:sum(m in methods for p in doc['paths'].values() for m in p)
assert len(base['paths'])==119 and count(base)==151
assert len(xray['paths'])==19 and count(xray)==23
assert len(core['paths'])==29 and count(core)==34
assert len(combined['paths'])==147 and count(combined)==185
assert len(combined['paths'])-len(base['paths'])==28
for path,old in base['paths'].items():
 for method,data in old.items():
  assert combined['paths'][path][method]==data, ('Existing path or method altered',path,method)
for k,sch in base['components']['schemas'].items():
 assert combined['components']['schemas'][k]==sch, ('existing schema altered',k)
assert combined['security']==base['security'], 'Existing global API authorization accidentally changed'
assert combined['paths']['/api/v1/agent/enroll']['post']['security']==[]
assert combined['paths']['/api/v1/auth/login']['post']['security']==[]
for doc in (base,core,combined,xray):
 ids=[v.get('operationId') for p in doc['paths'].values() for k,v in p.items() if k in methods]
 assert None not in ids and len(ids)==len(set(ids)), 'operationId duplicate or missing'
 for route,ops in doc['paths'].items():
  pvars=set(re.findall(r'\{([^}]+)\}',route))
  for method,operation in ops.items():
   if method not in methods:continue
   provided={p.get('name') for p in [*ops.get('parameters',[]),*operation.get('parameters',[])] if p.get('in')=='path'}
   # parameter refs resolved from components
   for pr in [*ops.get('parameters',[]),*operation.get('parameters',[])]:
    if '$ref' in pr and pr['$ref'].startswith('#/components/parameters/'):
     p=doc['components']['parameters'][pr['$ref'].rsplit('/',1)[-1]]
     if p.get('in')=='path':provided.add(p['name'])
   assert pvars<=provided,(doc['info']['title'],route,method,pvars,provided)
  
 def checkref(obj):
  if isinstance(obj,dict):
   if '$ref' in obj and obj['$ref'].startswith('#/'):
    cur=doc
    for part in obj['$ref'][2:].split('/'):
     cur=cur[part.replace('~1','/').replace('~0','~')]
   for v in obj.values():checkref(v)
  elif isinstance(obj,list):
   for v in obj:checkref(v)
 checkref(doc)
proto=(g2/'proto/serverfleet/agent/v1/agent.proto').read_text()
assert 'package serverfleet.agent.v1;' in proto and proto.count('message ' )>=12
assert proto.count('{')==proto.count('}'), 'unbalanced proto braces'
assert all(x in proto for x in ('AgentHello','TaskOffer','TaskAck','TaskFinished','StateSyncReply'))
changelog=json.loads((g2/'G2-OpenAPI-ChangeSet.json').read_text())
assert len(changelog['addedOperationIds'])==34
report={
 'status':'STATIC_CHECKS_PASS_ONLY','baseline':{'paths':len(base['paths']),'operations':count(base),'schemas':len(base['components']['schemas'])},
 'coreDraft':{'paths':len(core['paths']),'operations':count(core)},
 'combinedDraft':{'paths':len(combined['paths']),'operations':count(combined),'schemas':len(combined['components']['schemas'])},
 'xrayUnchanged':{'paths':len(xray['paths']),'operations':count(xray)},
 'passed':['YAML parse','JSON Schema $ref resolution','path parameter consistency','unique operationId','base path operation equality','base schema equality','global security equality','new login/bootstrap unauth declarations','Protobuf elementary brace/required-message checks'],
 'notRun':['buf lint','buf breaking','protoc generation','OpenAPI external validator','Go TypeScript cross-language roundtrip','WSS mTLS live integration','negative security tests','performance PoC']
}
(g2/'G2-VALIDATION-RESULTS.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
print(json.dumps(report,ensure_ascii=False,indent=2))
