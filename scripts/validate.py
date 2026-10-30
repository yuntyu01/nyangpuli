from pathlib import Path
import subprocess,json,datetime,os,hashlib,socket,time,urllib.request,tempfile,secrets,signal,shutil
root=Path(__file__).resolve().parents[1];os.chdir(root)
go=shutil.which('go')
if not go: raise SystemExit('Install Go and add it to PATH first.')
env=os.environ.copy();env['PATH']=str(Path(go).parent)+':'+env['PATH']
report={'database_tests':'NYANG_TEST_DATABASE_URL' in os.environ,'executed_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'scope':'implementation_and_integration_not_prediction_or_counsel_quality','compiler':subprocess.check_output([go,'version'],text=True).strip(),'node':subprocess.check_output(['node','--version'],text=True).strip(),'live_llm_calls':0,'live_mem0_calls':0,'human_evaluators':0,'real_future_outcomes':0,'checks':[]}
for args,extra in [(['test','-race','-count=1','-json','./...'],{}),(['vet','./...'],{}),(['test','-count=1','-json','./internal/saju'],{'SAJU_INTEGRATION':'1'})]:
 r=subprocess.run([go]+args,env={**env,**extra},capture_output=True,text=True)
 if r.returncode: print(r.stdout,r.stderr);raise SystemExit(r.returncode)
 testevents=[]
 for line in r.stdout.splitlines():
  try: testevents.append(json.loads(line))
  except: pass
 passed=[e for e in testevents if e.get('Action')=='pass' and e.get('Test')]
 skipped=[e for e in testevents if e.get('Action')=='skip' and e.get('Test')]
 report['checks'].append({'command':('SAJU_INTEGRATION=1 ' if extra else '')+'go '+' '.join(args),'exit_code':0,'passed_test_events':len(passed),'skipped_tests':[e['Test'] for e in skipped],'package_test_counts':{pkg:sum(e['Package']==pkg for e in passed) for pkg in sorted(set(e['Package'] for e in passed))}})
 print(report['checks'][-1],flush=True)
(root/'bin').mkdir(exist_ok=True)
subprocess.run([go,'build','-o','bin/nyangpuli','./cmd/nyangpuli'],check=True,env=env)
for domain in ['tarot','saju','daily','compatibility','counsel']:
 r=subprocess.run(['bin/nyangpuli','compare','-domain',domain,'-input',f'fixtures/{domain}.json','-case',f'synthetic-{domain}-001'],capture_output=True,text=True,check=True,env=env)
 j=json.loads(r.stdout)
 assert all(row['status']=='not_run' for row in j['generation'])
 assert len(j['generation'])==2
 report['checks'].append({'command':f'nyangpuli compare -domain {domain} -input fixtures/{domain}.json','exit_code':0,'variants':2,'model_status':'not_run'})
 if domain=='tarot':report['tarot_source_coverage']=j['comparison']['evidence']['coverage']
 print('CLI passed:',domain,flush=True)
subprocess.run([go,'build','-o','bin/conversation-eval','./cmd/conversation-eval'],check=True,env=env)
r=subprocess.run(['bin/conversation-eval'],capture_output=True,text=True,check=True,env=env)
j=json.loads(r.stdout)
assert all(row['status']=='not_run' for row in j['results'])
report['checks'].append({'command':'conversation-eval (dry run)','exit_code':0,'cases':len(j['context_checks']),'context_checks_passed':sum(c['passed'] for c in j['context_checks']),'prepared_inputs':len(j['results']),'model_status':'not_run','scope':j['scope']})
print('Conversation evaluation inputs prepared:',len(j['results']),flush=True)
with socket.socket() as s:
 s.bind(('127.0.0.1',0));port=s.getsockname()[1]
with tempfile.TemporaryDirectory(prefix='nyang-smoke-') as tmp:
 token=secrets.token_hex(32)
 servenv={**env,'NYANG_API_TOKEN':token,'NYANG_USER_ID':'synthetic-api-user'}
 for var in ['NYANG_USERS_JSON','MEM0_BASE_URL','MEM0_API_KEY']:servenv.pop(var,None)
 proc=subprocess.Popen(['bin/nyangpuli','serve','-addr',f'127.0.0.1:{port}','-memory',str(Path(tmp)/'memory.json')],env=servenv,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
 try:
  origin=f'http://127.0.0.1:{port}'
  for attempt in range(30):
   try:
    with urllib.request.urlopen(origin+'/healthz',timeout=1) as r: assert r.status==200
    break
   except OSError: time.sleep(.05)
  else: raise RuntimeError('server startup timeout')
  payload=json.dumps({'case_id':'synthetic-api-001','input':{'question':'연락 기준을 함께 정하고 싶어','topic':'negotiate_contact'},'use_memory':False}).encode()
  req=urllib.request.Request(origin+'/v1/compare/counsel',data=payload,headers={'Authorization':'Bearer '+token,'Content-Type':'application/json'})
  with urllib.request.urlopen(req,timeout=2) as r:
   assert r.status==200;assert json.load(r)['domain']=='counsel'
  report['checks'].append({'command':'compiled binary HTTP health + authenticated counsel POST + graceful shutdown','exit_code':0})
 finally:
  proc.send_signal(signal.SIGINT);proc.communicate(timeout=6)
  assert proc.returncode==0
regression=subprocess.run(['node','scripts/engine-regression.mjs'],capture_output=True,text=True,env=env)
if regression.returncode:
 print(regression.stdout,regression.stderr);raise SystemExit(regression.returncode)
report['checks'].append({'command':'node scripts/engine-regression.mjs','exit_code':0,'scope':'pinned_original_engine_regression_not_independent_accuracy'})
print(regression.stdout.strip(),flush=True)
Path('docs/validation-results.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
print('All validation recorded.',flush=True)
