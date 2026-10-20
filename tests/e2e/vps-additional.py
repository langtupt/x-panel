#!/usr/bin/env python3
import json,ssl,urllib.request,http.cookiejar,time,subprocess
password=__import__('os').environ['XPANEL_ADMIN_PASSWORD']
def session():
 return urllib.request.build_opener(urllib.request.HTTPSHandler(context=ssl._create_unverified_context()),urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
admin=session()
def api(method,path,body=None,op=admin,expected=200):
 request=urllib.request.Request('https://127.0.0.1:8888/api'+path,data=None if body is None else json.dumps(body).encode(),headers={'X-XPanel':'1','Content-Type':'application/json'},method=method)
 try:
  with op.open(request,timeout=120) as response:
   raw=response.read();status=response.status
 except urllib.error.HTTPError as e:
  raw=e.read();status=e.code
 if status!=expected: raise RuntimeError(f'{method} {path}: expected {expected}, got {status}: {raw.decode()[:1000]}')
 return json.loads(raw) if raw else None
api('POST','/auth/login',{'username':'admin','password':password})
for app in ['adminer','phpmyadmin']:
 catalog=api('GET','/apps')['apps'];entry=next(a for a in catalog if a['id']==app)
 if not entry['installed']:
  task=api('POST',f'/apps/{app}/install',{'version':'latest'},expected=202)
  for i in range(600):
   task=api('GET',f"/tasks/{task['id']}")
   if task['status']=='failed': raise RuntimeError(task['log'])
   if task['status']=='done':break
   time.sleep(2)
  else: raise RuntimeError('task timeout')
 print('PASS install '+app,flush=True)
created=[]
try:
 for username in ['pgchecka','pgcheckb']:
  user=api('POST','/users',{'username':username,'password':'Temporary_12345!','role':'user'},expected=201);created.append(user['id'])
  db=api('POST','/databases',{'user_id':user['id'],'name':'data','engine':'postgresql','password':'Temporary_12345!'},expected=201)
  own=session();api('POST','/auth/login',{'username':username,'password':'Temporary_12345!'},op=own)
  api('GET','/apps',op=own,expected=403)
  api('POST','/apps/php/install',{'version':'8.4'},op=own,expected=403)
 probe='value$literal%percent"quote\\end'
 site=api('POST','/sites',{'user_id':created[0],'domain':'envcheck.example.com','aliases':[],'app_type':'php','php_version':'8.3','env':{'XPANEL_PROBE':probe}},expected=201)
 import pathlib
 pathlib.Path('/home/pgchecka/www/envcheck.example.com/env.php').write_text("<?php echo getenv('XPANEL_PROBE');")
 request=urllib.request.Request('http://127.0.0.1/env.php',headers={'Host':'envcheck.example.com'})
 with urllib.request.urlopen(request) as r: assert r.read().decode()==probe, 'environment value not preserved'
 print('PASS per-site environment with dollar, percent, quote and backslash',flush=True)
 node=api('POST','/sites',{'user_id':created[0],'domain':'nodeenv.example.com','aliases':[],'app_type':'nodejs','env':{'XPANEL_PROBE':probe}},expected=201)
 import pwd,os
 root=pathlib.Path('/home/pgchecka/www/nodeenv.example.com');uid=pwd.getpwnam('pgchecka').pw_uid
 for name,content in {'package.json':'{"private":true,"scripts":{"start":"node server.js"}}','server.js':"require('http').createServer((q,r)=>r.end(process.env.XPANEL_PROBE)).listen(process.env.PORT,process.env.HOST)"}.items():
  path=root/name;path.write_text(content);os.chown(path,uid,uid)
 task=api('POST',f"/sites/{node['id']}/build",expected=202)
 for i in range(90):
  task=api('GET',f"/tasks/{task['id']}")
  if task['status']=='failed': raise RuntimeError(task['log'])
  if task['status']=='done': break
  time.sleep(2)
 request=urllib.request.Request('http://127.0.0.1/',headers={'Host':'nodeenv.example.com'})
 with urllib.request.urlopen(request) as r: assert r.read().decode()==probe, 'Node environment not preserved'
 print('PASS Node build/restart and runtime environment',flush=True)
 env=dict(__import__('os').environ,PGPASSWORD='Temporary_12345!')
 for name,expected in [('pgchecka_data',0),('pgcheckb_data',1)]:
  result=subprocess.run(['psql','-h','127.0.0.1','-p','5433','-U','pgchecka_data','-d',name,'-Atc','SELECT 1'],env=env,capture_output=True)
  assert (result.returncode==0)==(expected==0),f'cross-database isolation failed: {name}'
 print('PASS PostgreSQL tenant isolation and Applications RBAC',flush=True)
finally:
 for uid in reversed(created): api('DELETE',f'/users/{uid}',expected=204)
for path in ['/adminer/','/phpmyadmin/']:
 with urllib.request.urlopen('https://127.0.0.1:8888'+path,context=ssl._create_unverified_context()) as r: assert r.status==200
 print('PASS '+path,flush=True)
print('PASS additional VPS checks',flush=True)
