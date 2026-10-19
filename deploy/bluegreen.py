#!/usr/bin/env python3
"""Single-host blue/green releases. Run on hz from the authoritative checkout.
prepare SLOT IMAGE -> smoke -> bootstrap (first time) -> switch SLOT -> drain.
All state is private under deploy/.bluegreen; no credentials are printed.
"""
import argparse, fcntl, json, os, shutil, subprocess, time, urllib.request
from pathlib import Path
ROOT = Path(__file__).resolve().parent.parent
DEPLOY = ROOT / 'deploy'
STATE = DEPLOY / '.bluegreen'
PORTS = {'blue': 28081, 'green': 28082}
PROXY = 'sub2api-bluegreen-proxy'
NETWORK = 'sub2api-staging_sub2api-staging-network'

def run(*args, **kw):
    return subprocess.run(args, check=True, **kw)

def atomic(path, text):
    tmp = path.with_suffix(path.suffix + '.tmp')
    tmp.write_text(text); tmp.chmod(0o600); os.replace(tmp, path)

def owner(slot):
    folder=STATE/'runtime';folder.mkdir(exist_ok=True);folder.chmod(0o755)
    atomic(folder/'scheduler-owner',slot+'\n');(folder/'scheduler-owner').chmod(0o644)

def save(s): atomic(STATE/'state.json', json.dumps(s, indent=2)+'\n')
def load(): return json.loads((STATE/'state.json').read_text())
def inspect(name):
    return json.loads(subprocess.check_output(['docker','inspect',name]))[0]
def health(slot):
    with urllib.request.urlopen(f'http://127.0.0.1:{PORTS[slot]}/health',timeout=5) as r:
        assert r.status == 200

def wait_health(slot):
    for _ in range(45):
        try: health(slot); return
        except Exception: time.sleep(1)
    raise RuntimeError('new slot failed health check')

def nginx(slot):
    return '''worker_processes auto;
worker_shutdown_timeout 3600s;
pid /var/run/nginx.pid;
events { worker_connections 4096; }
http {
 access_log /dev/stdout;
 error_log /dev/stderr warn;
 map $http_upgrade $connection_upgrade { default upgrade; '' close; }
 server {
  listen 8081;
  client_max_body_size 256m;
  location / {
   proxy_pass http://127.0.0.1:PORT;
   proxy_http_version 1.1;
   proxy_set_header Host $host;
   proxy_set_header X-Real-IP $remote_addr;
   proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
   proxy_set_header X-Forwarded-Proto $http_x_forwarded_proto;
   proxy_set_header Upgrade $http_upgrade;
   proxy_set_header Connection $connection_upgrade;
   proxy_buffering off;
   proxy_request_buffering off;
   proxy_read_timeout 3600s;
   proxy_send_timeout 3600s;
   add_header X-Sub2api-Slot SLOT always;
  }
 }
}
'''.replace('PORT',str(PORTS[slot])).replace('SLOT',slot)

def write_proxy(slot): atomic(STATE/'nginx/nginx.conf',nginx(slot))
def proxy_validate(): run('docker','exec',PROXY,'nginx','-t')
def compose(slot, image):
    # Resolve immutable image ID before preparing a release.
    image_id = json.loads(subprocess.check_output(['docker','image','inspect',image]))[0]['Id']
    data = STATE/('data-'+slot)
    if not data.exists():
        data.mkdir()
        run('cp','-a',str(DEPLOY/'staging/data')+'/.',str(data))
        shutil.copystat(DEPLOY/'staging/data',data)
        stat=(DEPLOY/'staging/data').stat();os.chown(data,stat.st_uid,stat.st_gid)
    doc={'services':{slot:{'image':image_id,'container_name':'sub2api-'+slot,'restart':'unless-stopped',
      'env_file':[{'path':str(DEPLOY/'.hz-private/sub2api.env'),'format':'raw'}],
      'environment':{'SUB2API_DEPLOYMENT_SLOT':slot,'SUB2API_SCHEDULER_OWNER_FILE':'/app/deployment/scheduler-owner'},
      'ports':[f'127.0.0.1:{PORTS[slot]}:8080'],
      'volumes':[f'{data}:/app/data',f'{STATE / "runtime"}:/app/deployment:ro'],
      'networks':['shared'],'stop_grace_period':'60s',
      'healthcheck':{'test':['CMD','wget','-q','-T','5','-O','/dev/null','http://localhost:8080/health'],'interval':'10s','timeout':'5s','retries':5},
      'logging':{'driver':'json-file','options':{'max-size':'20m','max-file':'5'}}}},
      'networks':{'shared':{'external':True,'name':NETWORK}}}
    path=STATE/(slot+'.compose.json');atomic(path,json.dumps(doc,indent=2))
    return path,image_id

def prepare(slot,image):
    s=load()
    if slot==s['active']: raise RuntimeError('cannot replace the active slot')
    previous=s['slots'].get(slot)
    if previous:
        try:
            if inspect(previous['container'])['State']['Running']: raise RuntimeError('drain old slot before replacing it')
        except subprocess.CalledProcessError: pass
    path,image_id=compose(slot,image)
    run('docker','compose','-p','sub2api-bg-'+slot,'-f',str(path),'up','-d')
    wait_health(slot)
    s['slots'][slot]={'container':'sub2api-'+slot,'image':image_id,'label':image,'compose':str(path)}
    save(s);print('Prepared',slot,'on',PORTS[slot],'; run authenticated smoke tests before switch.')

def init():
    if (STATE/'state.json').exists(): raise RuntimeError('already initialized')
    old=inspect('sub2api-staging')
    (STATE/'nginx').mkdir(parents=True,exist_ok=True)
    shutil.copy2(DEPLOY/'docker-compose.hz.json',STATE/'legacy-compose.before.json')
    save({'active':'blue','bootstrapped':False,'slots':{'blue':{'container':'sub2api-staging','image':old['Image'],'legacy':True}}})
    owner('blue')
    print('Captured current production image as blue; no services changed.')

def bootstrap():
    s=load()
    if s['bootstrapped']: raise RuntimeError('already bootstrapped')
    health('green')
    write_proxy('blue')
    run('docker','run','--rm','-v',str(STATE/'nginx')+':/etc/nginx:ro','nginx:1.28-alpine','nginx','-t')
    run('docker','create','--name',PROXY,'--restart','unless-stopped','--network','host',
        '--log-opt','max-size=20m','--log-opt','max-file=5',
        '-v',str(STATE/'nginx')+':/etc/nginx:ro','nginx:1.28-alpine')
    path=DEPLOY/'docker-compose.hz.json';old=path.read_text();doc=json.loads(old)
    doc['services']['sub2api']['ports']=['127.0.0.1:28081:8080']
    atomic(path,json.dumps(doc,indent=2)+'\n')
    try:
        run('docker','compose','-p','sub2api-staging','-f',str(path),'up','-d','--no-deps','sub2api')
        run('docker','start',PROXY)
        wait_health('blue')
    except Exception:
        run('docker','rm','-f',PROXY)
        atomic(path,old)
        run('docker','compose','-p','sub2api-staging','-f',str(path),'up','-d','--no-deps','sub2api')
        raise
    s['bootstrapped']=True;save(s);print('Fixed :8081 entry now serves blue; future switches only reload Nginx.')

def switch(slot):
    s=load()
    if not s['bootstrapped']: raise RuntimeError('bootstrap first')
    if slot==s['active']: raise RuntimeError('already active')
    # For rollback, the prior container may have been stopped after draining.
    if not inspect(s['slots'][slot]['container'])['State']['Running']:
        run('docker','start',s['slots'][slot]['container'])
    wait_health(slot)
    old=s['active'];write_proxy(slot)
    try:
        proxy_validate();run('docker','exec',PROXY,'nginx','-s','reload')
        for _ in range(20):
            with urllib.request.urlopen('http://127.0.0.1:8081/health',timeout=5) as r:
                if r.headers.get('X-Sub2api-Slot')==slot: break
            time.sleep(.25)
        else: raise RuntimeError('new worker did not become ready')
    except Exception:
        write_proxy(old);proxy_validate();run('docker','exec',PROXY,'nginx','-s','reload');raise
    s['active']=slot;s['draining']=old;s.pop('drain_not_before',None);save(s)
    # Pause new scans while old streams/jobs drain. The legacy version ignores this marker.
    owner('none')
    print('Traffic switched to',slot,'; old slot',old,'kept running. Run drain to finish.')

def drain():
    s=load();old=s.get('draining')
    if not old: print('Nothing draining');return
    connections=subprocess.check_output(['ss','-Hnt','state','established',f'( dport = :{PORTS[old]} )'],text=True).strip()
    if connections: print('Old upstream still has connections; keep it running and retry drain later.');return
    # Wait for scheduled probes, including legacy runners, before stopping old container.
    # Runtime task gate stops new scans on non-legacy slots; 190s exceeds probe deadline+persist.
    if not s.get('drain_not_before'):
        s['drain_not_before']=time.time()+195;save(s)
        print('Connections drained. Waiting 195 seconds for any in-flight scheduled probes; rerun drain.');return
    if time.time()<s['drain_not_before']:
        print('Waiting for scheduled-probe drain:',round(s['drain_not_before']-time.time()),'seconds');return
    run('docker','stop','--time','60',s['slots'][old]['container'])
    owner(s['active'])
    s.pop('draining',None);s.pop('drain_not_before',None);save(s)
    print('Old slot stopped and retained for rollback; scheduled probes owned by',s['active'])

def main():
    os.umask(0o077);STATE.mkdir(parents=True,exist_ok=True)
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('command',choices=['init','prepare','bootstrap','switch','drain','status']);parser.add_argument('args',nargs='*');args=parser.parse_args()
    with (STATE/'release.lock').open('w') as lock:
        fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
        if args.command=='init':init()
        elif args.command=='prepare':prepare(*args.args)
        elif args.command=='bootstrap':bootstrap()
        elif args.command=='switch':switch(*args.args)
        elif args.command=='drain':drain()
        else:print(json.dumps(load(),indent=2))
if __name__=='__main__':main()
