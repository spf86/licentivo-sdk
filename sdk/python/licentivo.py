"""Licentivo runtime SDK, Python 3.10+. Pin public keys; never put a vendor API key here."""
import base64
import hashlib
import json
import licentivo_tasks
import licentivo_logout
import os
import platform
import subprocess
import random
import re
import tempfile
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey

LIMIT = 16384
FIELDS = {'version','key_id','tenant_id','product_id','license_id','device_id','activation_id','mode','features','max_devices','issued_at','expires_at'}
UUID = re.compile(r'^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$')

def _pairs(pairs):
    result = {}
    for key,value in pairs:
        if key in result: raise ValueError('duplicate JSON field')
        result[key] = value
    return result

def loads(data):
    if len(data) > LIMIT: raise ValueError('response size limit')
    return json.loads(data, object_pairs_hook=_pairs, parse_constant=lambda x: (_ for _ in ()).throw(ValueError('invalid number')))

def decode(s):
    if not isinstance(s,str) or not re.fullmatch(r'[A-Za-z0-9_-]+',s): raise ValueError('invalid base64url')
    b = base64.urlsafe_b64decode(s + '='*((-len(s))%4))
    if base64.urlsafe_b64encode(b).decode().rstrip('=') != s: raise ValueError('noncanonical base64url')
    return b

def system_device_id(product):
    system=platform.system(); identity=''
    if system=='Windows':
        import winreg
        with winreg.OpenKey(winreg.HKEY_LOCAL_MACHINE,r'SOFTWARE\Microsoft\Cryptography',0,winreg.KEY_READ|winreg.KEY_WOW64_64KEY) as key:
            identity=winreg.QueryValueEx(key,'MachineGuid')[0]
    elif system=='Linux':
        for p in ['/etc/machine-id','/var/lib/dbus/machine-id']:
            try: identity=Path(p).read_text().strip()
            except OSError: continue
            if identity: break
    elif system=='Darwin':
        result=subprocess.check_output(['/usr/sbin/ioreg','-rd1','-c','IOPlatformExpertDevice'],timeout=3,text=True)
        match=re.search(r'"IOPlatformUUID"\s*=\s*"([0-9A-Fa-f-]+)"',result)
        if match: identity=match[1]
    identity=identity.strip().lower()
    if not re.fullmatch(r'(?:[0-9a-f]{32}|[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12})',identity) or not identity.replace('-','').strip('0'):
        raise ValueError('OS machine identity unavailable; cannot bind license')
    return hashlib.sha256(('LicenovaDevice/v2\n'+product.lower()+'\n'+identity).encode()).hexdigest()

def machine_profile(product):
    system=system_device_id(product); hardware='';description='';os_name=platform.system()
    try:
        if os_name=='Windows':
            result=subprocess.check_output(['powershell.exe','-NoProfile','-NonInteractive','-Command','(Get-CimInstance Win32_ComputerSystemProduct).UUID; $v=Get-CimInstance Win32_ComputerSystem; $v.Manufacturer; $v.Model'],timeout=5,text=True,creationflags=subprocess.CREATE_NO_WINDOW)
            hardware=result.strip().splitlines()[0];description=result
        elif os_name=='Linux':
            try: hardware=Path('/sys/class/dmi/id/product_uuid').read_text()
            except OSError: pass
            for path in ['/sys/class/dmi/id/sys_vendor','/sys/class/dmi/id/product_name']:
                try: description+=' '+Path(path).read_text()
                except OSError: pass
            if Path('/.dockerenv').exists(): description+=' virtual machine'
        elif os_name=='Darwin':
            result=subprocess.check_output(['/usr/sbin/ioreg','-rd1','-c','IOPlatformExpertDevice'],timeout=5,text=True)
            hardware=re.search(r'"IOPlatformUUID"\s*=\s*"([0-9A-Fa-f-]+)"',result)[1];description=result
    except (OSError,subprocess.SubprocessError,IndexError,TypeError): pass
    hardware=hardware.strip().lower();hw=''
    if UUID.fullmatch(hardware) and hardware.replace('-','').strip('0f'):
        hw=hashlib.sha256(('LicenovaHardware/v1\n'+product.lower()+'\n'+hardware).encode()).hexdigest()
    profile={'version':3,'system_id':system,'hardware_id':hw,'virtual_machine':bool(re.search(r'vmware|virtualbox|virtual machine|kvm|qemu|xen|parallels',description,re.I)),'platform':{'Windows':'windows','Linux':'linux','Darwin':'darwin'}[os_name]}
    return profile,hashlib.sha256(('LicenovaDevice/v3\n'+product.lower()+'\n'+system+'\n'+hw).encode()).hexdigest()
def machine_device_id(product): return machine_profile(product)[1]

def extended_claims(c):
    if 'offline_request_id' in c and (not isinstance(c['offline_request_id'],str) or not UUID.fullmatch(c['offline_request_id'])): raise ValueError('invalid offline request ID')
    if 'capabilities' not in c: return
    x=c['capabilities'];allowed={'license_model','session_id','session_expires_at','app_version','version_released_at','maintenance_until','launch_connection_id','environment','binding_version'}
    if not isinstance(x,dict) or set(x)-allowed or x.get('license_model') not in ['node_locked','floating']: raise ValueError('invalid license capabilities')
    if any(k in x for k in ['launch_connection_id','environment','binding_version']) and (not UUID.fullmatch(x.get('launch_connection_id','')) or x.get('environment') not in ['live','sandbox'] or type(x.get('binding_version')) is not int or x['binding_version']<1 or x['license_model']!='node_locked' or not UUID.fullmatch(x.get('session_id','')) or x.get('session_expires_at',0)<=c['issued_at'] or c['expires_at']>x['session_expires_at'] or c.get('runtime_policy',{}).get('heartbeat_seconds',0)<600 or policy_mode(c)=='permanent' or c['runtime_policy']['offline_seconds']>86400): raise ValueError('invalid Launch account session')
    for field in ['session_expires_at','version_released_at','maintenance_until']:
        if field in x and (type(x[field]) is not int or not 0<=x[field]<=253402300799): raise ValueError('invalid entitlement date')
    if 'app_version' in x and (not isinstance(x['app_version'],str) or not re.fullmatch(r'(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})',x['app_version'])): raise ValueError('invalid application version')
    if x['license_model']=='floating' and (not isinstance(x.get('session_id'),str) or not UUID.fullmatch(x['session_id']) or x.get('session_expires_at',0)<=c['issued_at'] or c['expires_at']>x['session_expires_at'] or c.get('runtime_policy',{}).get('heartbeat_seconds',0)==0 or policy_mode(c)=='permanent'): raise ValueError('invalid floating session')
    if x.get('version_released_at',0)>c['issued_at'] or x.get('maintenance_until',0)>0 and (not x.get('app_version') or x.get('version_released_at',0)==0 or x['version_released_at']>x['maintenance_until']): raise ValueError('invalid maintenance entitlement')

def policy_mode(c): return c.get('runtime_policy',{}).get('offline_mode','limited')
def max_span(c):
    if 'runtime_policy' not in c: return 86400
    p=c['runtime_policy']
    if not isinstance(p,dict) or set(p)!={'offline_mode','heartbeat_seconds','offline_seconds'} or any(type(p[f]) is not int for f in ['heartbeat_seconds','offline_seconds']): raise ValueError('invalid runtime policy')
    mode,h,o=p['offline_mode'],p['heartbeat_seconds'],p['offline_seconds']
    if h!=0 and not 10<=h<=86400: raise ValueError('invalid heartbeat interval')
    if mode=='permanent' and o==0: return 253402300799-c['issued_at']
    if mode=='none' and o==0: return 10 if h==0 else h+(h+9)//10+30
    if mode=='limited' and 1<=o<=31536000: return o
    raise ValueError('invalid runtime policy')

def verify(envelope, keys, product, device, now=None):
    now = int(time.time()) if now is None else now
    if set(envelope) != {'version','key_id','payload','signature'} or type(envelope['version']) is not int or envelope['version'] != 1: raise ValueError('invalid envelope')
    raw = decode(keys[envelope['key_id']]); payload = decode(envelope['payload']); signature = decode(envelope['signature'])
    if len(raw)!=32 or len(signature)!=64 or len(payload)>LIMIT: raise ValueError('invalid key/signature length')
    Ed25519PublicKey.from_public_bytes(raw).verify(signature,b'LicenovaLease/v1\n'+payload)
    c = loads(payload)
    if set(c) != FIELDS | (set(c)&{'runtime_policy','capabilities','offline_request_id'}): raise ValueError('invalid claims fields')
    if type(c['version']) is not int or c['version']!=1 or c['key_id']!=envelope['key_id'] or c['product_id']!=product or c['device_id']!=device: raise ValueError('lease context mismatch')
    if any(not isinstance(c[f],str) or not UUID.fullmatch(c[f]) for f in ['tenant_id','product_id','license_id','activation_id']): raise ValueError('invalid identifiers')
    if not re.fullmatch(r'[0-9a-f]{64}',device) or c['mode'] not in ['perpetual','timed','trial']: raise ValueError('invalid mode/device')
    if any(type(c[f]) is not int for f in ['max_devices','issued_at','expires_at']): raise ValueError('invalid integer claim')
    if not 1<=c['max_devices']<=10000 or c['issued_at']<=0 or not 0<c['expires_at']-c['issued_at']<=max_span(c) or c['expires_at']>253402300799 or c['issued_at']>now+300 or now>=c['expires_at']: raise ValueError('expired lease/clock')
    fs=c['features']
    if not isinstance(fs,list) or len(fs)>32 or any(not isinstance(f,str) or not re.fullmatch(r'[A-Za-z0-9_.-]{1,64}',f) for f in fs) or sorted(set(fs))!=fs: raise ValueError('invalid features')
    extended_claims(c)
    return c

def device_id(product, installation_id):
    return hashlib.sha256(('licentivo-device-v1\n'+product+'\n'+installation_id.strip().lower()).encode()).hexdigest()

class APIError(Exception):
    def __init__(self, status, code, request_id='', retry_after_seconds=0):
        self.status=status; self.code=code; self.request_id=request_id; self.retryable=status==429 or status>=500; self.retry_after_seconds=retry_after_seconds; super().__init__(f'Licentivo {code} ({status})')

class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,*args,**kwargs): return None

class Client:
    def __init__(self, config):
        self.feature_task_lock = threading.Lock()
        self.config=dict(config); self.config.setdefault('license_key',''); self.config['base_url']=(self.config.get('base_url') or 'https://www.licentivo.com').rstrip('/');
        if not self.config.get('cache_path'): self.config['cache_path']=str(Path(os.environ.get('LOCALAPPDATA') or os.environ.get('XDG_CACHE_HOME') or str(Path.home()/'.cache'))/'Licentivo'/hashlib.sha256((self.config['base_url']+'\n'+self.config['product_id']).encode()).hexdigest()/'lease.json')
        self.profile,self.config['device_id']=machine_profile(config['product_id']); self.session_id=str(uuid.uuid4()); config=self.config; u=urllib.parse.urlsplit(config['base_url'])
        if u.username or not u.hostname or u.query or u.fragment or u.path not in ['','/']: raise ValueError('base_url must be an origin')
        if u.scheme!='https' and not (config.get('allow_http') is True and u.scheme=='http' and u.hostname in ['localhost','127.0.0.1','::1']): raise ValueError('HTTPS required')
        if not re.fullmatch(r'[0-9a-f]{64}',config['device_id']) or config['license_key'] and not re.fullmatch(r'lv_(lic|tmp)_[A-Za-z0-9_-]{43}',config['license_key']): raise ValueError('invalid device/license')
        if not config['trusted_keys'] or any(len(decode(k))!=32 for k in config['trusted_keys'].values()): raise ValueError('pin a public key')
        self.lock=threading.RLock(); self.state=None; self.interval=3600; self.wall=0; self.tick=time.monotonic(); self.stop=threading.Event(); self.thread=None; self.refresh_thread=None; self.closed=False
        self.timeout=config.get('timeout_seconds',10)
        if not isinstance(self.timeout,(int,float)) or not 1<=self.timeout<=120: raise ValueError('timeout_seconds must be 1–120')
        handlers=[_NoRedirect()]
        if config.get('proxy_url'): handlers.append(urllib.request.ProxyHandler({'http':config['proxy_url'],'https':config['proxy_url']}))
        self.opener=urllib.request.build_opener(*handlers);self.on_status_changed=None;self.last_error=None;self.online=False;self.retry_at=0;self.snapshot=(None,0,self.tick)
        self.auth=None;self.auth_dirty=False;self._load_credential();licentivo_logout.load(self)
        if not config['license_key'] and not config.get('identity_token') and not self.auth and not self.signed_out: raise ValueError('enter a license key or temporary activation code')
        try:
            p=Path(config.get('cache_path','')); s=loads(p.read_bytes()) if p.is_file() and p.stat().st_size<=LIMIT else None
            if not self.signed_out and not self.logout_journal['signed_out'] and s and s['version']==1 and s['license_hash']==self._hash() and time.time()+300>=s['observed_at']:
                c=verify(s['lease'],config['trusted_keys'],config['product_id'],config['device_id'])
                if policy_mode(c)!='none' and c.get('capabilities',{}).get('license_model')!='floating' and c.get('capabilities',{}).get('app_version','')==config.get('app_version',''):
                    self.state=s;self.wall=s['observed_at'];self.interval=c.get('runtime_policy',{}).get('heartbeat_seconds',3600)
        except Exception: pass  # A corrupt/expired cache never grants access.
        self._publish(False)

    def _hash(self): return hashlib.sha256(self.config['identity_token'].encode()).hexdigest() if self.config.get('identity_token') else self.auth['source_hash'] if not self.config['license_key'] and self.auth else hashlib.sha256(self.config['license_key'].encode()).hexdigest()
    def _temporary(self): return bool(self.config['license_key']) or bool(self.config.get('identity_token')) or self.auth is not None
    def _runtime_key(self): return self.auth.get('device_key') or self.config['license_key'] if self.auth else self.config['license_key']
    def _load_credential(self):
        if not self.config.get('cache_path'): return
        try:
            path=Path(self.config['cache_path']+'.credential.json')
            if path.stat().st_size>LIMIT: return
            s=loads(path.read_bytes())
            if s.get('version')!=1 or s.get('origin')!=self.config['base_url'] or not re.fullmatch(r'[0-9a-f]{64}',s.get('source_hash','')) or (self.config['license_key'] or self.config.get('identity_token')) and s.get('source_hash')!=self._hash() or any(s.get(k)!=self.config[k] for k in ['product_id','device_id']) or not UUID.fullmatch(s.get('request_id','')) or not UUID.fullmatch(s.get('session_id','')) or s.get('device_key') and not re.fullmatch(r'lv_dev_[A-Za-z0-9_-]{43}',s['device_key']): return
            if not self.config['license_key'] and not s.get('device_key'): return
            self.auth=s
            if not s.get('device_key'): self.session_id=s['session_id']
        except (OSError,ValueError,KeyError,TypeError): pass
    def _prepare_credential(self):
        if not self._temporary(): return
        if not self.config.get('cache_path'): raise ValueError('cache_path required to retain a transferred license')
        if not self.auth:
            self.auth={'version':1,'origin':self.config['base_url'],'source_hash':self._hash(),'product_id':self.config['product_id'],'device_id':self.config['device_id'],'device_key':'','request_id':str(uuid.uuid4()),'session_id':self.session_id}
            self.auth_dirty=True
        if self.auth_dirty: self._save_credential()
    def _save_credential(self):
        p=Path(self.config['cache_path']+'.credential.json');p.parent.mkdir(parents=True,exist_ok=True)
        fd,tmp=tempfile.mkstemp(prefix='.credential-',dir=p.parent)
        try:
            os.chmod(tmp,0o600)
            with os.fdopen(fd,'w',encoding='utf-8') as f: json.dump(self.auth,f,separators=(',',':'))
            os.replace(tmp,p);self.auth_dirty=False
        finally:
            if os.path.exists(tmp): os.unlink(tmp)
    def _accept_credential(self,result):
        licentivo_logout.accept_login(self, result.get('logout_ticket'), APIError)
        if result.get('logout_ticket'):
            self._prepare_credential();self.auth['logout_ticket']=result['logout_ticket'];self.auth_dirty=True;self._save_credential()
        if not self._temporary() or self.auth and self.auth.get('device_key'): return
        if not re.fullmatch(r'lv_dev_[A-Za-z0-9_-]{43}',result.get('device_key','')):
            if not self.config['license_key'].startswith('lv_tmp_') and not self.config.get('identity_token'): return
            raise ValueError('server omitted device credential')
        self._prepare_credential();self.auth['device_key']=result['device_key'];self.auth_dirty=True;self._save_credential()
    def _check(self):
        if self.signed_out: raise APIError(403,'LAUNCH_LOGIN_REQUIRED')
        if self.wall and time.time() < self.wall+(time.monotonic()-self.tick)-300: raise ValueError('clock rollback; verify online')
        if not self.state: raise ValueError('no valid lease')
        c=verify(self.state['lease'],self.config['trusted_keys'],self.config['product_id'],self.config['device_id']);self._context(c);return c
    def _invalidate(self):
        self.state=None
        self._publish(False)
        path=self.config.get('cache_path')
        if path and not self.signed_out:
            try: Path(path).unlink(missing_ok=True)
            except OSError: pass
    def _save(self):
        path=self.config.get('cache_path')
        if not path: return
        p=Path(path);p.parent.mkdir(parents=True,exist_ok=True)
        fd,tmp=tempfile.mkstemp(prefix='.lease-',dir=p.parent)
        try:
            os.chmod(tmp,0o600)
            with os.fdopen(fd,'w',encoding='utf-8') as f: json.dump(self.state,f,separators=(',',':'))
            os.replace(tmp,p)
        finally:
            if os.path.exists(tmp): os.unlink(tmp)
    def _request(self,operation,activation,operation_id=None,extra=None):
        if self.signed_out: raise APIError(403,'LAUNCH_LOGIN_REQUIRED')
        self._prepare_credential()
        wire_operation='launch/exchange' if operation=='activate' and not self.auth.get('device_key') and self.config.get('identity_token') else operation
        cfg=self.config; body=json.dumps(self._body(activation) | (extra or {})).encode()
        idem=operation_id or str(uuid.uuid4());last=None
        if operation=='activate' and self._temporary() and not self.auth.get('device_key') and not self.state: idem=self.auth['request_id']
        for attempt in range(2):
            if attempt: time.sleep(.2)
            req=urllib.request.Request(cfg['base_url'].rstrip('/')+'/api/v1/'+wire_operation,body,{'Content-Type':'application/json','Idempotency-Key':idem},method='POST')
            try:
                try: res=self.opener.open(req,timeout=self.timeout)
                except urllib.error.HTTPError as e: res=e
                with res:
                    status=res.code; request_headers=res.headers; b=res.read(LIMIT+1)
                if len(b)>LIMIT: raise ValueError('oversized response')
                try: env=loads(b)
                except ValueError:
                    if status!=429 and status<500: raise
                    retry=request_headers.get('Retry-After','0')
                    error=APIError(status,'UNEXPECTED_RESPONSE',request_headers.get('X-Request-ID',''),min(86400,int(retry)) if retry.isdigit() else 0)
                    if status>=500: last=error;continue
                    raise error
                if status!=200 or env.get('error'):
                    retry=int(request_headers.get('Retry-After','0')) if request_headers.get('Retry-After','0').isdigit() else 0
                    error=APIError(status,env.get('error',{}).get('code','UNEXPECTED_RESPONSE'),env.get('request_id',''),min(86400,retry))
                    if status>=500: last=error;continue
                    if licentivo_logout.authorization_rejected(error): self._invalidate()
                    raise error
                return env['data']
            except (urllib.error.URLError,TimeoutError,OSError) as e: last=e
        raise last
    def _call(self,operation,force_online=False):
        with self.lock:
            if self.signed_out: raise APIError(403,'LAUNCH_LOGIN_REQUIRED')
            try:
                cached=self._check()
                if not force_online and operation=='activate' and policy_mode(cached)=='permanent' and cached['runtime_policy']['heartbeat_seconds']==0: return cached
            except Exception: pass
            activation=''
            if operation!='activate':
                if not self.state: raise ValueError('activate first')
                activation=loads(decode(self.state['lease']['payload']))['activation_id']
            try: result=self._request(operation,activation)
            except Exception as e:
                if licentivo_logout.revoked(e): licentivo_logout.deny(self)
                transient=isinstance(e,(urllib.error.URLError,TimeoutError,OSError)) or isinstance(e,APIError) and e.retryable
                if transient:
                    try:
                        c=self._check()
                        if not force_online and policy_mode(c)!='none': self._publish(False,e);return c
                        if policy_mode(c)=='none': self._invalidate()
                    except Exception: pass
                    self._publish(False,e);raise e
                self._invalidate();self._publish(False,e);raise
            if self.signed_out:
                if self.auth and result.get('logout_ticket'): self.auth['logout_ticket']=result['logout_ticket']
                raise APIError(403,'LAUNCH_LOGIN_REQUIRED')
            try:
                c=verify(result['lease'],self.config['trusted_keys'],self.config['product_id'],self.config['device_id'])
                self._context(c)
                if c['activation_id']!=result['activation_id']: raise ValueError('activation mismatch')
            except Exception: self._invalidate();raise
            self._accept_credential(result)
            self.wall=time.time();self.tick=time.monotonic();self.state={'version':1,'license_hash':self._hash(),'lease':result['lease'],'observed_at':int(self.wall)}
            self.interval=c.get('runtime_policy',{}).get('heartbeat_seconds',max(10,min(86400,result.get('heartbeat_seconds',3600))));self._save();self._publish(True);return c
    def activate(self): return self._call('activate')
    def validate(self): return self._call('validate')
    def heartbeat(self): return self._call('heartbeat')
    def refresh_online(self): return self._call('activate',True)
    def check_feature(self,name):
        return self.feature_status(name)['allowed']
    def deactivate(self):
        return self.logout()
    def logout(self):
        return licentivo_logout.logout(self)
    def retry_pending_logouts(self):
        return licentivo_logout.retry(self)
    def _heartbeat_delay(self):
        delay=max(600,self.interval)*random.uniform(1,1.1)
        try:
            remaining=self._check()['expires_at']-time.time()
            if remaining>0: delay=min(delay,max(1,remaining*.8))
        except Exception: pass
        return max(delay,self.retry_at-time.time())
    def start_heartbeat(self,on_error=None):
        try:
            if self.interval==0: return
        except Exception: pass
        if self.thread and self.thread.is_alive(): return
        self.stop.clear()
        def run():
            while self.interval and not self.stop.wait(self._heartbeat_delay()):
                try: self.heartbeat()
                except Exception as e:
                    if on_error: on_error(e)
        self.thread=threading.Thread(target=run,daemon=True);self.thread.start()
    def close(self):
        if self.closed: return
        self.closed=True
        self.stop.set()
        for worker in [self.thread,self.refresh_thread]:
            if worker and worker is not threading.current_thread(): worker.join(timeout=5)
        self.thread=None
        self.refresh_thread=None
        try:
            if self._check().get('capabilities',{}).get('license_model')=='floating':
                activation=self._check()['activation_id'];self._invalidate();self._request('deactivate',activation)
        except Exception: pass
    def _context(self,c):
        x=c.get('capabilities')
        if (x or {}).get('launch_connection_id','')!=self.config.get('launch_connection_id','') or (x or {}).get('environment','')!=self.config.get('environment',''): raise ValueError('lease belongs to another Launch connection/environment')
        if x and (x.get('app_version','')!=self.config.get('app_version','') or x['license_model']=='floating' and x.get('session_id')!=self.session_id): raise ValueError('lease belongs to another version/session')
    def _body(self,activation=''):
        identity={k:self.config.get(k,'') for k in ['identity_token','launch_connection_id','environment']} if not (self.auth or {}).get('device_key') and self.config.get('identity_token') else {}
        return {k:self.config.get(k,'') for k in ['product_id','device_id','device_name','app_version']} | {'license_key':self._runtime_key(),'activation_id':activation,'session_id':self.session_id,'device_profile':self.profile} | ({'request_logout_ticket':True} if not (self.auth or {}).get('logout_ticket') else {}) | ({'launch_connection_id':self.config['launch_connection_id'],'environment':self.config.get('environment','')} if self.config.get('launch_connection_id') else {}) | identity
    def start(self,on_error=None):
        if self.closed: raise RuntimeError('client is closed')
        try:
            result=self._check()
            if policy_mode(result)=='none' or result.get('capabilities',{}).get('license_model')=='floating': raise ValueError('online startup required')
        except Exception:
            result=self.activate();self.start_heartbeat(on_error);return result
        self.start_heartbeat(on_error)
        def refresh():
            if self.stop.is_set(): return
            try: self.refresh_online()
            except Exception as e:
                if on_error and not self.stop.is_set(): on_error(e)
        if not self.refresh_thread or not self.refresh_thread.is_alive():
            self.refresh_thread=threading.Thread(target=refresh,daemon=True);self.refresh_thread.start()
        return result
    @classmethod
    def open(cls,path,on_error=None):
        client=cls(loads(Path(path).read_bytes()))
        try: client.start(on_error);return client
        except Exception: client.close();raise
    @classmethod
    def open_product(cls,product_id,trusted_keys,license_key='',on_error=None):
        client=cls({'product_id':product_id,'trusted_keys':dict(trusted_keys),'license_key':license_key.strip()})
        try:
            client.start(on_error)
            return client
        except Exception:
            client.close()
            raise

    @classmethod
    def open_with_license(cls,path,license_key,on_error=None):
        """Load shared product settings and use the customer's license key."""
        config=loads(Path(path).read_bytes())
        config['license_key']=license_key.strip()
        client=cls(config)
        try: client.start(on_error);return client
        except Exception: client.close();raise
    def consume(self,feature,quantity,operation_id):
        if not operation_id: raise ValueError('retain an operation_id for retries')
        with self.lock:
            activation=self._check()['activation_id']
            try: return self._request('consume',activation,operation_id,{'feature':feature,'quantity':quantity})['consumption']
            except APIError as e:
                if licentivo_logout.revoked(e): licentivo_logout.deny(self)
                self._publish(False,e)
                raise
    def offline_request(self,operation='activate'):
        with self.lock:
            if self.signed_out: raise ValueError('enter a new activation input before exporting an offline request')
            if operation not in ['activate','deactivate']: raise ValueError('invalid offline operation')
            self._prepare_credential()
            activation=self._check()['activation_id'] if operation=='deactivate' else ''
            request=self._body(activation) | {'version':1,'request_id':str(uuid.uuid4()),'created_at':int(time.time()),'operation':operation}
            if operation=='activate':
                self.auth['offline_request_id']=request['request_id'];self.auth_dirty=True;self._save_credential();licentivo_logout.remember_offline_request(self,request['request_id'])
            else:
                failure=licentivo_logout.deny(self,False)
                if failure: raise failure
            return request
    def import_offline(self,request,response):
        with self.lock:
            if self.signed_out: raise APIError(403,'LAUNCH_LOGIN_REQUIRED')
            key=request.get('license_key')
            token=request.get('identity_token','');source=token or key
            same_input=key==self._runtime_key() and (not token or token==self.config.get('identity_token'))
            same_source=isinstance(source,str) and bool(source) and (same_input or (self.auth or {}).get('device_key') and hashlib.sha256(source.encode()).hexdigest()==self.auth.get('source_hash'))
            if response.get('version')!=1 or request.get('operation')!='activate' or response.get('operation')!='activate' or response.get('request_id')!=request.get('request_id') or any(request.get(k)!=self.config.get(k) for k in ['product_id','device_id']) or not same_source: raise ValueError('offline context mismatch')
            current=self.logout_journal.get('offline_request_id','') if self.logout_journal['signed_out'] else (self.auth or {}).get('offline_request_id') or self.logout_journal.get('offline_request_id','')
            if current and current!=request.get('request_id') or self.logout_journal['signed_out'] and not current: raise ValueError('offline approval is no longer current; generate a fresh request')
            result=response['result'];c=verify(result['lease'],self.config['trusted_keys'],self.config['product_id'],self.config['device_id']);self._context(c)
            if policy_mode(c)=='none' or c.get('offline_request_id')!=request['request_id'] or c['activation_id']!=result['activation_id']: raise ValueError('offline signature context mismatch')
            self._accept_credential(result);self._prepare_credential();self.auth['offline_request_id']=request['request_id'];self.auth_dirty=True;self._save_credential()
            self.wall=time.time();self.tick=time.monotonic();self.state={'version':1,'license_hash':self._hash(),'lease':result['lease'],'observed_at':int(self.wall)};self.interval=c.get('runtime_policy',{}).get('heartbeat_seconds',3600)
            try: self._save()
            except Exception: self._invalidate();raise
            self._publish(False);return c
    def _publish(self,online,error=None):
        before=self.status();self.snapshot=(self.state,self.wall,self.tick);self.online=online;self.last_error=error
        if isinstance(error,APIError) and error.retry_after_seconds: self.retry_at=time.time()+error.retry_after_seconds
        after=self.status()
        if before!=after and self.on_status_changed:
            callback=self.on_status_changed
            threading.Thread(target=lambda:callback(after),daemon=True).start()
    def status(self):
        if self.signed_out: return dict(state='released',allowed=False,code='LAUNCH_LOGIN_REQUIRED',request_id='',retryable=False,lease_valid_until=0)
        state,wall,tick=self.snapshot
        result={'state':'verification_required','allowed':False,'code':'','request_id':'','retryable':False,'lease_valid_until':0}
        if self.last_error:
            result.update(code=getattr(self.last_error,'code','NETWORK_UNAVAILABLE'),request_id=getattr(self.last_error,'request_id',''),retryable=getattr(self.last_error,'retryable',True))
        try:
            if wall and time.time()<wall+time.monotonic()-tick-300: raise ValueError('clock rollback')
            claims=verify(state['lease'],self.config['trusted_keys'],self.config['product_id'],self.config['device_id']);self._context(claims)
            result.update(state='active' if self.online else 'offline_valid',allowed=True,lease_valid_until=claims['expires_at'])
        except Exception: pass
        result['state']={'LICENSE_EXPIRED':'expired','LICENSE_REVOKED':'revoked','LICENSE_SUSPENDED':'revoked','DEVICE_RELEASED':'released','DEVICE_CREDENTIAL_REVOKED':'released','FEATURE_QUOTA_EXCEEDED':'quota_exhausted'}.get(result['code'],result['state'])
        return result
    def run_feature(self, name, action):
        """Run a non-metered business action only when the signed grant allows it."""
        if not callable(action): raise TypeError('business action is required')
        status = self.feature_status(name)
        if not status['allowed']:
            raise RuntimeError(f"feature denied: {status['code']} ({status['state']}); request_id={status['request_id']}")
        return action()

    def run_metered_feature(self, name, quantity, operation_id, action):
        return licentivo_tasks.run(self, name, quantity, operation_id, action)

    def resolve_metered_feature(self, operation_id, succeeded):
        return licentivo_tasks.resolve(self, operation_id, succeeded)

    def feature_status(self,name):
        result=self.status()
        if result['allowed']:
            try:
                claims=verify(self.snapshot[0]['lease'],self.config['trusted_keys'],self.config['product_id'],self.config['device_id'])
                if name not in claims['features']: result.update(allowed=False,code='FEATURE_NOT_ENTITLED')
            except Exception: result.update(allowed=False,state='verification_required')
        return result
    def _reservation(self,operation,operation_id,extra):
        if not operation_id: raise ValueError('retain operation_id for retries')
        with self.lock:
            activation=self._check()['activation_id']
            try:
                result=self._request('consume/'+operation,activation,operation_id,extra)['reservation'];self._publish(True);return result
            except Exception as e:
                if licentivo_logout.revoked(e): licentivo_logout.deny(self)
                self._publish(False,e);raise
    def reserve(self,feature,quantity,operation_id): return self._reservation('reserve',operation_id,{'feature':feature,'quantity':quantity})
    def commit(self,reservation_id,operation_id): return self._reservation('commit',operation_id,{'reservation_id':reservation_id})
    def cancel(self,reservation_id,operation_id): return self._reservation('cancel',operation_id,{'reservation_id':reservation_id})
    def stop_client(self):
        self.close()
    def __enter__(self): return self
    def __exit__(self,*args): self.close()
