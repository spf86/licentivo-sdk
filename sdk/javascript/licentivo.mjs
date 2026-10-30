import {verify as cryptoVerify,createPublicKey,createHash,randomUUID,randomInt} from 'node:crypto';
import {readFileSync,writeFileSync,mkdirSync,renameSync,unlinkSync,statSync,statfsSync} from 'node:fs';
import {dirname,join} from 'node:path';
import {execFileSync} from 'node:child_process';
import {platform,homedir,hostname,cpus,totalmem,arch,release,type} from 'node:os';
import {runMetered,resolveMetered} from './feature-tasks.mjs';
import {loadLogout,deny,logout,retryLogouts,explicitRevocation,authorizationRejected,acceptLogin,rememberOfflineRequest} from './logout.mjs';
const LIMIT=16384,uuid=/^[\da-fA-F]{8}(-[\da-fA-F]{4}){3}-[\da-fA-F]{12}$/;
// Detect duplicate keys before JSON.parse (including escaped versions of the same name).
export function parseJSON(text){if(Buffer.byteLength(text)>LIMIT)throw Error('JSON size limit');let i=0;const ws=()=>{while(/\s/.test(text[i]??'')&&i<text.length)i++;};function str(){const start=i++;while(i<text.length){if(text[i++]==='"')return JSON.parse(text.slice(start,i));if(text[i-1]==='\\')i++;}throw Error('invalid JSON string');}function value(depth=0){if(depth>32)throw Error('JSON depth limit');ws();if(text[i]==='"'){str();return;}if(text[i]==='{'){i++;const seen=new Set();ws();if(text[i]==='}'){i++;return;}for(;;){ws();if(text[i]!=='"')throw Error('invalid object');const key=str();if(seen.has(key))throw Error('duplicate field');seen.add(key);ws();if(text[i++]!==':')throw Error('missing colon');value(depth+1);ws();const end=text[i++];if(end==='}')return;if(end!==',')throw Error('invalid object');}}if(text[i]==='['){i++;ws();if(text[i]===']'){i++;return;}for(;;){value(depth+1);ws();const end=text[i++];if(end===']')return;if(end!==',')throw Error('invalid array');}}const m=/^(?:true|false|null|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)/.exec(text.slice(i));if(!m)throw Error('invalid JSON');i+=m[0].length;}value();ws();if(i!==text.length)throw Error('trailing JSON');return JSON.parse(text);}
function decode(s){if(typeof s!=='string'||!s||!/^[A-Za-z0-9_-]+$/.test(s))throw Error('invalid base64url');const b=Buffer.from(s,'base64url');if(b.toString('base64url')!==s)throw Error('noncanonical base64url');return b;}
function systemDeviceID(product){
 let identity='';
 if(platform()==='win32'){const output=execFileSync('reg.exe',['query','HKLM\\SOFTWARE\\Microsoft\\Cryptography','/v','MachineGuid','/reg:64'],{encoding:'utf8',timeout:3000,windowsHide:true});identity=/MachineGuid\s+REG_SZ\s+([0-9a-f-]+)/i.exec(output)?.[1]??'';}
 else if(platform()==='linux'){for(const path of ['/etc/machine-id','/var/lib/dbus/machine-id']){try{identity=readFileSync(path,'utf8').trim();}catch{}if(identity)break;}}
 else if(platform()==='darwin'){const output=execFileSync('/usr/sbin/ioreg',['-rd1','-c','IOPlatformExpertDevice'],{encoding:'utf8',timeout:3000});identity=/"IOPlatformUUID"\s*=\s*"([0-9a-f-]+)"/i.exec(output)?.[1]??'';}
 identity=identity.trim().toLowerCase();if(!/^(?:[0-9a-f]{32}|[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12})$/.test(identity)||!identity.replaceAll('-','').replaceAll('0',''))throw Error('OS machine identity unavailable; cannot bind license');
 return createHash('sha256').update('LicenovaDevice/v2\n'+product.toLowerCase()+'\n'+identity).digest('hex');
}
export function machineProfile(product){
 const system=systemDeviceID(product);let hardware='',description='';const os=platform();
 try{
  if(os==='win32'){const output=execFileSync('powershell.exe',['-NoProfile','-NonInteractive','-Command','(Get-CimInstance Win32_ComputerSystemProduct).UUID; $v=Get-CimInstance Win32_ComputerSystem; $v.Manufacturer; $v.Model'],{encoding:'utf8',timeout:5000,windowsHide:true});hardware=output.trim().split(/\r?\n/)[0];description=output;}
  else if(os==='linux'){try{hardware=readFileSync('/sys/class/dmi/id/product_uuid','utf8');}catch{}for(const path of ['/sys/class/dmi/id/sys_vendor','/sys/class/dmi/id/product_name'])try{description+=' '+readFileSync(path,'utf8');}catch{}try{statSync('/.dockerenv');description+=' virtual machine';}catch{}}
  else if(os==='darwin'){const output=execFileSync('/usr/sbin/ioreg',['-rd1','-c','IOPlatformExpertDevice'],{encoding:'utf8',timeout:5000});hardware=/"IOPlatformUUID"\s*=\s*"([0-9a-f-]+)"/i.exec(output)?.[1]??'';description=output;}
 }catch{}
 hardware=hardware.trim().toLowerCase();let hw='';if(uuid.test(hardware)&&hardware.replaceAll('-','').replace(/[0f]/g,''))hw=createHash('sha256').update('LicenovaHardware/v1\n'+product.toLowerCase()+'\n'+hardware).digest('hex');
 const profile={version:3,system_id:system,hardware_id:hw,virtual_machine:/vmware|virtualbox|virtual machine|kvm|qemu|xen|parallels/i.test(description),platform:os==='win32'?'windows':os};
 return {profile,device:createHash('sha256').update('LicenovaDevice/v3\n'+product.toLowerCase()+'\n'+system+'\n'+hw).digest('hex')};
}
export const machineDeviceID=product=>machineProfile(product).device;
function extendedClaims(c){
 if(Object.hasOwn(c,'offline_request_id')&&(typeof c.offline_request_id!=='string'||!uuid.test(c.offline_request_id)))throw Error('invalid offline request ID');
 if(!Object.hasOwn(c,'capabilities'))return;const x=c.capabilities,allowed=['license_model','session_id','session_expires_at','app_version','version_released_at','maintenance_until','launch_connection_id','environment','binding_version'];
 if(!x||typeof x!=='object'||Array.isArray(x)||Object.keys(x).some(k=>!allowed.includes(k))||!['node_locked','floating'].includes(x.license_model))throw Error('invalid license capabilities');
 if(['launch_connection_id','environment','binding_version'].some(k=>Object.hasOwn(x,k))&&(!uuid.test(x.launch_connection_id??'')||!['live','sandbox'].includes(x.environment)||!Number.isSafeInteger(x.binding_version)||x.binding_version<1||x.license_model!=='node_locked'||!uuid.test(x.session_id??'')||!(x.session_expires_at>c.issued_at)||c.expires_at>x.session_expires_at||!(c.runtime_policy?.heartbeat_seconds>=600)||policyMode(c)==='permanent'||c.runtime_policy.offline_seconds>86400))throw Error('invalid Launch account session');
 for(const key of ['session_expires_at','version_released_at','maintenance_until'])if(Object.hasOwn(x,key)&&(!Number.isSafeInteger(x[key])||x[key]<0||x[key]>253402300799))throw Error('invalid entitlement date');
 if(Object.hasOwn(x,'app_version')&&(typeof x.app_version!=='string'||!/^(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$/.test(x.app_version)))throw Error('invalid application version');
 if(x.license_model==='floating'&&(typeof x.session_id!=='string'||!uuid.test(x.session_id)||!(x.session_expires_at>c.issued_at)||c.expires_at>x.session_expires_at||!c.runtime_policy?.heartbeat_seconds||policyMode(c)==='permanent'))throw Error('invalid floating session');
 if((x.version_released_at??0)>c.issued_at||(x.maintenance_until??0)>0&&(!x.app_version||!x.version_released_at||x.version_released_at>x.maintenance_until))throw Error('invalid maintenance entitlement');
}
const policyMode=c=>c.runtime_policy?.offline_mode??'limited';
function maxSpan(c){if(!Object.hasOwn(c,'runtime_policy'))return 86400;const p=c.runtime_policy;if(!p||Object.keys(p).sort().join(',')!=='heartbeat_seconds,offline_mode,offline_seconds'||!Number.isSafeInteger(p.heartbeat_seconds)||!Number.isSafeInteger(p.offline_seconds))throw Error('invalid runtime policy');const h=p.heartbeat_seconds,o=p.offline_seconds;if(h!==0&&(h<10||h>86400))throw Error('invalid heartbeat interval');if(p.offline_mode==='permanent'&&o===0)return 253402300799-c.issued_at;if(p.offline_mode==='none'&&o===0)return h===0?10:h+Math.ceil(h/10)+30;if(p.offline_mode==='limited'&&o>=1&&o<=31536000)return o;throw Error('invalid runtime policy');}
export function verifyLease(e,keys,product,device,now=Math.floor(Date.now()/1000)){
 if(Object.keys(e).sort().join(',')!=='key_id,payload,signature,version'||e.version!==1||!keys[e.key_id])throw Error('unknown lease key/version');
 const pub=decode(keys[e.key_id]),raw=decode(e.payload),sig=decode(e.signature);if(pub.length!==32||sig.length!==64||raw.length>LIMIT)throw Error('invalid lengths');
 const key=createPublicKey({key:Buffer.concat([Buffer.from('302a300506032b6570032100','hex'),pub]),format:'der',type:'spki'});
 if(!cryptoVerify(null,Buffer.concat([Buffer.from('LicenovaLease/v1\n'),raw]),key,sig))throw Error('bad signature');
 const c=parseJSON(new TextDecoder('utf-8',{fatal:true}).decode(raw));const fields=['version','key_id','tenant_id','product_id','license_id','device_id','activation_id','mode','features','max_devices','issued_at','expires_at'];
 if(Object.keys(c).sort().join(',')!==[...fields,...['runtime_policy','capabilities','offline_request_id'].filter(k=>Object.hasOwn(c,k))].sort().join(',')||c.version!==1||c.key_id!==e.key_id||c.product_id!==product||c.device_id!==device||! /^[a-f0-9]{64}$/.test(device)||['tenant_id','product_id','license_id','activation_id'].some(f=>typeof c[f]!=='string'||!uuid.test(c[f])))throw Error('lease context mismatch');
 if(!['perpetual','timed','trial'].includes(c.mode)||['issued_at','expires_at','max_devices'].some(f=>!Number.isSafeInteger(c[f]))||c.max_devices<1||c.max_devices>10000||c.issued_at<=0||c.expires_at<=c.issued_at||c.expires_at-c.issued_at>maxSpan(c)||c.expires_at>253402300799||c.issued_at>now+300||now>=c.expires_at)throw Error('expired lease/clock');
 if(!Array.isArray(c.features)||c.features.length>32||c.features.some((f,i)=>typeof f!=='string'||! /^[A-Za-z0-9_.-]{1,64}$/.test(f)||i>0&&c.features[i-1]>=f))throw Error('invalid features');extendedClaims(c);return c;
}
export const deviceID=(product,installationID)=>createHash('sha256').update('licentivo-device-v1\n'+product+'\n'+installationID.trim().toLowerCase()).digest('hex');
export class APIError extends Error{constructor(status,code,requestID='',retryAfterSeconds=0){super(`Licentivo ${code} (${status})`);this.status=status;this.code=code;this.requestID=requestID;this.retryable=status===429||status>=500;this.transient=this.retryable;this.retryAfterSeconds=retryAfterSeconds;}}
function machineDisplayDetails(){
 const info={os_version:'',cpu:'',memory_mb:0,gpu:'',system_type:arch(),storage_gb:0};
 try{info.cpu=cpus()[0]?.model||'';info.memory_mb=Math.floor(totalmem()/1048576);const disk=statfsSync(platform()==='win32'?(process.env.SystemDrive||'C:')+'\\':'/');info.storage_gb=Math.floor(disk.blocks*disk.bsize/1073741824);}catch{}
 const run=(name,args)=>{try{return execFileSync(name,args,{encoding:'utf8',timeout:2000,windowsHide:true,maxBuffer:262144}).trim();}catch{return '';}};
 if(platform()==='win32'){
  const values=run('reg.exe',['query','HKLM\\SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion','/reg:64']);
  const field=key=>values.split(/\r?\n/).map(line=>line.trim().split(/\s+REG_\w+\s+/)).find(parts=>parts[0]===key)?.[1]||'';
  let name=field('ProductName');const build=field('CurrentBuildNumber');if(Number(build)>=22000)name=name.replace('Windows 10','Windows 11');info.os_version=(name+' '+field('DisplayVersion')+' ('+build+')').trim();
  const video=run('reg.exe',['query','HKLM\\SYSTEM\\CurrentControlSet\\Control\\Video','/s','/f','DriverDesc','/t','REG_SZ']);
  info.gpu=[...new Set([...video.matchAll(/DriverDesc\s+REG_SZ\s+([^\r\n]+)/g)].map(match=>match[1].trim()))].join(' / ');
 }else if(platform()==='darwin')info.os_version=run('/usr/bin/sw_vers',['-productName'])+' '+run('/usr/bin/sw_vers',['-productVersion']);
 else{try{info.os_version=readFileSync('/etc/os-release','utf8').split('\n').find(line=>line.startsWith('PRETTY_NAME='))?.slice(12).replace(/^"|"$/g,'')||'';}catch{}
  if(!info.cpu||/^unknown$/i.test(info.cpu.trim()))try{const fields=Object.fromEntries(readFileSync('/proc/cpuinfo','utf8').split('\n').filter(line=>line.includes(':')).map(line=>{const i=line.indexOf(':');return [line.slice(0,i).trim(),line.slice(i+1).trim()];}));info.cpu=['model name','Hardware','Processor'].map(key=>fields[key]).find(Boolean)||'';if(!info.cpu&&fields['CPU part'])info.cpu='ARM CPU (implementer '+(fields['CPU implementer']||'')+', part '+fields['CPU part']+')';}catch{}
 }
 if(!info.os_version.trim())info.os_version=type()+' '+release();
 for(const key of ['os_version','cpu','gpu','system_type'])info[key]=[...String(info[key]).replace(/[\p{Cc}]/gu,'').trim()].slice(0,key==='gpu'?240:key==='system_type'?40:160).join('');
 return info;
}
export class Client{
 constructor(cfg){cfg={license_key:'',timeout_seconds:10,...cfg};cfg.base_url=(cfg.base_url||'https://www.licentivo.com').replace(/\/$/,'');if(!cfg.cache_path)cfg.cache_path=join(process.env.LOCALAPPDATA||process.env.XDG_CACHE_HOME||join(homedir(),'.cache'),'Licentivo',createHash('sha256').update(cfg.base_url+'\n'+cfg.product_id).digest('hex'),'lease.json');if(!Number.isFinite(cfg.timeout_seconds)||cfg.timeout_seconds<1||cfg.timeout_seconds>120)throw Error('timeout_seconds must be 1–120');const machine=machineProfile(cfg.product_id);this.profile=machine.profile;this.sessionID=randomUUID();this.cfg={...cfg,device_id:machine.device};cfg=this.cfg;this.displayDetails=machineDisplayDetails();if(!String(cfg.device_name||'').trim())cfg.device_name=[...hostname().replace(/[\p{Cc}]/gu,'').trim()].slice(0,120).join('')||({windows:'Windows',darwin:'macOS',linux:'Linux'})[this.profile.platform];const u=new URL(cfg.base_url);if(u.username||u.password||u.search||u.hash||u.pathname!=='/')throw Error('base_url must be an origin');if(u.protocol!=='https:'&&!(cfg.allow_http===true&&u.protocol==='http:'&&['localhost','127.0.0.1','[::1]'].includes(u.hostname)))throw Error('HTTPS required');if(! /^[0-9a-f]{64}$/.test(cfg.device_id)||cfg.license_key!==''&&! /^lv_(lic|tmp)_[A-Za-z0-9_-]{43}$/.test(cfg.license_key)||!Object.keys(cfg.trusted_keys).length)throw Error('invalid licensing configuration');for(const k of Object.values(cfg.trusted_keys))if(decode(k).length!==32)throw Error('invalid pinned key');this.interval=3600;this.state=null;this.wall=0;this.tick=performance.now();this.queue=Promise.resolve();this.controller=new AbortController();this.auth=null;this.authDirty=false;this.online=false;this.lastError=null;this.retryAt=0;this.onStatusChanged=null;this.loadCredential();loadLogout(this);if(cfg.license_key===''&&!cfg.identity_token&&!this.auth&&!this.signedOut)throw Error('enter a license key or temporary activation code');try{if(!this.signedOut&&!this.logoutJournal.signed_out&&cfg.cache_path&&statSync(cfg.cache_path).size<=LIMIT){const s=parseJSON(readFileSync(cfg.cache_path,'utf8'));if(s.version===1&&s.license_hash===this.hash()&&Date.now()/1000+300>=s.observed_at){const c=verifyLease(s.lease,cfg.trusted_keys,cfg.product_id,cfg.device_id);if(policyMode(c)!=='none'&&c.capabilities?.license_model!=='floating'&&(c.capabilities?.app_version??'')===(cfg.app_version??'')){this.state=s;this.wall=s.observed_at;this.interval=c.runtime_policy?.heartbeat_seconds??3600;}}}}catch{} }
 hash(){if(this.cfg.identity_token)return createHash('sha256').update(this.cfg.identity_token).digest('hex');return this.cfg.license_key===''&&this.auth?this.auth.source_hash:createHash('sha256').update(this.cfg.license_key).digest('hex');}
 temporary(){return !!this.cfg.license_key||!!this.cfg.identity_token||!!this.auth;}
 runtimeKey(){return this.auth?.device_key||this.cfg.license_key;}
 loadCredential(){if(!this.cfg.cache_path)return;try{if(statSync(this.cfg.cache_path+'.credential.json').size>LIMIT)return;const s=parseJSON(readFileSync(this.cfg.cache_path+'.credential.json','utf8'));if(s.version!==1||s.origin!==this.cfg.base_url||! /^[0-9a-f]{64}$/.test(s.source_hash??'')||(this.cfg.license_key!==''||this.cfg.identity_token)&&s.source_hash!==this.hash()||s.product_id!==this.cfg.product_id||s.device_id!==this.cfg.device_id||!uuid.test(s.request_id??'')||!uuid.test(s.session_id??'')||(s.device_key&&!/^lv_dev_[A-Za-z0-9_-]{43}$/.test(s.device_key)))return;if(this.cfg.license_key===''&&!s.device_key)return;this.auth=s;if(!s.device_key)this.sessionID=s.session_id;}catch{}}
 prepareCredential(){if(!this.temporary())return;if(!this.cfg.cache_path)throw Error('cache_path required to retain a transferred license');if(!this.auth){this.auth={version:1,origin:this.cfg.base_url,source_hash:this.hash(),product_id:this.cfg.product_id,device_id:this.cfg.device_id,device_key:'',request_id:randomUUID(),session_id:this.sessionID};this.authDirty=true;}if(this.authDirty)this.saveCredential();}
 saveCredential(){const path=this.cfg.cache_path+'.credential.json';mkdirSync(dirname(path),{recursive:true,mode:0o700});const tmp=join(dirname(path),'.credential-'+randomUUID());try{writeFileSync(tmp,JSON.stringify(this.auth),{mode:0o600,flag:'wx'});renameSync(tmp,path);this.authDirty=false;}finally{try{unlinkSync(tmp);}catch{}}}
 acceptCredential(data){acceptLogin(this,data.logout_ticket,APIError);if(data.logout_ticket){this.prepareCredential();this.auth.logout_ticket=data.logout_ticket;this.authDirty=true;this.saveCredential();}if(!this.temporary()||this.auth?.device_key)return;if(!/^lv_dev_[A-Za-z0-9_-]{43}$/.test(data.device_key??'')){if(!this.cfg.license_key.startsWith('lv_tmp_')&&!this.cfg.identity_token)return;throw Error('server omitted device credential');}this.prepareCredential();this.auth.device_key=data.device_key;this.authDirty=true;this.saveCredential();}
 serial(fn){const result=this.queue.then(fn);this.queue=result.catch(()=>{});return result;}
 check(){if(this.signedOut)throw new APIError(403,'LAUNCH_LOGIN_REQUIRED');if(this.wall&&Date.now()/1000<this.wall+(performance.now()-this.tick)/1000-300)throw Error('clock rollback; verify online');if(!this.state)throw Error('no valid lease');const c=verifyLease(this.state.lease,this.cfg.trusted_keys,this.cfg.product_id,this.cfg.device_id);this.context(c);return c;}
 invalidate(){this.state=null;if(this.cfg.cache_path&&!this.signedOut)try{unlinkSync(this.cfg.cache_path);}catch{}}
 save(){if(!this.cfg.cache_path)return;mkdirSync(dirname(this.cfg.cache_path),{recursive:true,mode:0o700});const tmp=join(dirname(this.cfg.cache_path),'.lease-'+randomUUID());try{writeFileSync(tmp,JSON.stringify(this.state),{mode:0o600,flag:'wx'});renameSync(tmp,this.cfg.cache_path);}finally{try{unlinkSync(tmp);}catch{}}}
 async request(op, activation, signal, suppliedID, extra={}) {
  if(this.signedOut)throw new APIError(403,'LAUNCH_LOGIN_REQUIRED');
  const wireOp=op==='activate'&&!this.auth?.device_key&&this.cfg.identity_token?'launch/exchange':op;
  this.prepareCredential();
  if(this.cfg.proxy_url&&!this.proxyAgent){let ProxyAgent;try{({ProxyAgent}=await import('undici'));}catch{throw Error('proxy_url requires the optional undici package');}this.proxyAgent=new ProxyAgent(this.cfg.proxy_url);}
  const cfg=this.cfg, body=JSON.stringify({...this.body(activation),...extra}), idem=op==='activate'&&this.temporary()&&!this.auth.device_key&&!this.state?this.auth.request_id:suppliedID??randomUUID();
  let last;
  for (let attempt=0; attempt<2; attempt++) {
   if (signal?.aborted) { const error=new Error('request cancelled',{cause:signal.reason}); error.transient=true; throw error; }
   if (attempt) await new Promise(resolve=>setTimeout(resolve,200));
   const timeout=AbortSignal.timeout(cfg.timeout_seconds*1000);
   let response, reader, chunks=[], bytes=0;
   try {
    response=await fetch(cfg.base_url.replace(/\/$/,'')+'/api/v1/'+wireOp,{dispatcher:this.proxyAgent,method:'POST',headers:{'Content-Type':'application/json','Idempotency-Key':idem},body,redirect:'manual',signal:signal?AbortSignal.any([signal,timeout]):timeout});
    
    reader=response.body?.getReader();
    if (reader) for (;;) {
     const {value,done}=await reader.read(); if (done) break;
     bytes+=value.byteLength;
     if (bytes>LIMIT) { await reader.cancel(); throw new RangeError('oversized response'); }
     chunks.push(value);
    }
   } catch (error) {
    if (error instanceof RangeError) throw error;
    error.transient=true; last=error; continue;
   } finally { reader?.releaseLock(); }
   const raw=new TextDecoder('utf-8',{fatal:true}).decode(Buffer.concat(chunks));
   let envelope;
   try{envelope=parseJSON(raw);}catch(error){if(response.status!==429&&response.status<500)throw error;const apiError=new APIError(response.status,'UNEXPECTED_RESPONSE',response.headers.get('X-Request-ID')??'',Math.min(86400,Math.max(0,Number(response.headers.get('Retry-After'))||0)));if(response.status>=500){last=apiError;continue;}throw apiError;}
   if(response.status!==200||envelope.error){const error=new APIError(response.status,envelope.error?.code??'UNEXPECTED_RESPONSE',envelope.request_id??'',Math.min(86400,Math.max(0,Number(response.headers.get('Retry-After'))||0)));if(response.status>=500){last=error;continue;}if(authorizationRejected(error))this.invalidate();throw error;}
   return envelope.data;
  }
  throw last;
 }
 call(op,signal,forceOnline=false){return this.serial(async()=>{if(this.signedOut)throw new APIError(403,'LAUNCH_LOGIN_REQUIRED');try{const c=this.check();if(!forceOnline&&op==='activate'&&policyMode(c)==='permanent'&&c.runtime_policy.heartbeat_seconds===0)return c;}catch{}let activation='';if(op!=='activate'){if(!this.state)throw Error('activate first');activation=parseJSON(decode(this.state.lease.payload).toString('utf8')).activation_id;}let data;try{data=await this.request(op,activation,signal);}catch(e){if(explicitRevocation(e))deny(this);if(e.transient&&!signal?.aborted)try{const c=this.check();if(!forceOnline&&policyMode(c)!=='none'){this.publish(false,e);return c;}if(policyMode(c)==='none')this.invalidate();}catch{}if(!e.transient)this.invalidate();this.publish(false,e);throw e;}if(this.signedOut){if(data.logout_ticket&&this.auth)this.auth.logout_ticket=data.logout_ticket;throw new APIError(403,'LAUNCH_LOGIN_REQUIRED');}try{const c=verifyLease(data.lease,this.cfg.trusted_keys,this.cfg.product_id,this.cfg.device_id);this.context(c);if(c.activation_id!==data.activation_id)throw Error('activation mismatch');this.acceptCredential(data);this.wall=Date.now()/1000;this.tick=performance.now();this.state={version:1,license_hash:this.hash(),lease:data.lease,observed_at:Math.floor(this.wall)};this.interval=c.runtime_policy?.heartbeat_seconds??Math.max(10,Math.min(86400,data.heartbeat_seconds??3600));this.save();this.publish(true);return c;}catch(e){this.invalidate();throw e;}});}
 activate(signal){return this.call('activate',signal);}validate(signal){return this.call('validate',signal);}heartbeat(signal){return this.call('heartbeat',signal);}refreshOnline(signal){return this.call('activate',signal,true);}
 checkFeature(name){try{return this.check().features.includes(name);}catch{return false;}}
 logout(signal){return logout(this,signal);}
 deactivate(signal){return this.logout(signal);}
 retryPendingLogouts(signal){return this.serial(()=>retryLogouts(this,signal));}
 _heartbeatDelay(){let delay=Math.max(600,this.interval)*(1+randomInt(0,101)/1000)*1000;try{const remaining=this.check().expires_at*1000-Date.now();if(remaining>0)delay=Math.min(delay,Math.max(1000,remaining*.8));}catch{}return Math.max(delay,this.retryAt-Date.now());}
 startHeartbeat(onError=()=>{}){if(this.signedOut)return;try{if(this.interval===0)return;}catch{}if(this.timer)return;this.controller=new AbortController();const next=()=>{if(this.interval===0){this.timer=null;return;}this.timer=setTimeout(async()=>{try{await this.heartbeat(this.controller.signal);}catch(e){if(!this.controller.signal.aborted)onError(e);}if(!this.controller.signal.aborted)next();},this._heartbeatDelay());this.timer.unref?.();};next();}
 context(c){const x=c.capabilities;if((x?.launch_connection_id??'')!==(this.cfg.launch_connection_id??'')||(x?.environment??'')!==(this.cfg.environment??''))throw Error('lease belongs to another Launch connection/environment');if(x&&((x.app_version??'')!==(this.cfg.app_version??'')||x.license_model==='floating'&&x.session_id!==this.sessionID))throw Error('lease belongs to another version/session');}
 body(activation=''){const cfg=this.cfg;return {product_id:cfg.product_id,license_key:this.runtimeKey(),device_id:cfg.device_id,device_name:cfg.device_name??'',app_version:cfg.app_version??'',activation_id:activation,session_id:this.sessionID,device_profile:this.profile,device_info:this.displayDetails,...(!this.auth?.logout_ticket?{request_logout_ticket:true}:{}),...(cfg.launch_connection_id?{launch_connection_id:cfg.launch_connection_id,environment:cfg.environment}:{}),...(!this.auth?.device_key&&cfg.identity_token?{identity_token:cfg.identity_token,launch_connection_id:cfg.launch_connection_id,environment:cfg.environment}:{})};}
 async start(onError=()=>{},signal){let cached;try{cached=this.check();if(policyMode(cached)==='none'||cached.capabilities?.license_model==='floating')cached=null;}catch{}if(cached){this.startHeartbeat(onError);void this.refreshOnline(signal?AbortSignal.any([signal,this.controller.signal]):this.controller.signal).catch(e=>{if(!this.controller.signal.aborted)onError(e);});return cached;}const result=await this.activate(signal);this.startHeartbeat(onError);return result;}
 static async openProduct(productId,trustedKeys,licenseKey='',onError=()=>{}){
  const client=new Client({product_id:productId,trusted_keys:{...trustedKeys},license_key:licenseKey.trim()});
  try{await client.start(onError);return client;}catch(e){await client.close();throw e;}
 }
 static async open(path,onError=()=>{}){const client=new Client(parseJSON(readFileSync(path,'utf8')));try{await client.start(onError);return client;}catch(e){await client.close();throw e;}}
 // Shared product settings; the application supplies the customer's license key.
 static async openWithLicense(path,licenseKey,onError=()=>{}){
  const cfg=parseJSON(readFileSync(path,'utf8'));
  cfg.license_key=licenseKey.trim();
  const client=new Client(cfg);
  try{await client.start(onError);return client;}catch(e){await client.close();throw e;}
 }
 consume(feature,quantity,operationID,signal){return this.serial(async()=>{if(!operationID)throw Error('retain an operationID for retries');const c=this.check();try{return (await this.request('consume',c.activation_id,signal,operationID,{feature,quantity})).consumption;}catch(e){if(explicitRevocation(e))deny(this);this.publish(false,e);throw e;}});}
 offlineRequest(operation='activate'){
  if(this.signedOut)throw Error('enter a new activation input before exporting an offline request');
  if(!['activate','deactivate'].includes(operation))throw Error('invalid offline operation');
  this.prepareCredential();const activation=operation==='deactivate'?this.check().activation_id:'';
  const request={...this.body(activation),version:1,request_id:randomUUID(),created_at:Math.floor(Date.now()/1000),operation};
  if(operation==='activate'){this.auth.offline_request_id=request.request_id;this.authDirty=true;this.saveCredential();rememberOfflineRequest(this,request.request_id);}
  else {const failure=deny(this,false);if(failure)throw failure;}
  return request;
 }
 importOffline(request,response){
  if(this.signedOut)throw new APIError(403,'LAUNCH_LOGIN_REQUIRED');
  const key=request.license_key,token=request.identity_token??'',source=token||key;
  const sameInput=key===this.runtimeKey()&&(!token||token===this.cfg.identity_token);
  const sameSource=typeof source==='string'&&source!==''&&(sameInput||this.auth?.device_key&&createHash('sha256').update(source).digest('hex')===this.auth.source_hash);
  if(response.version!==1||request.operation!=='activate'||response.operation!=='activate'||request.request_id!==response.request_id||['product_id','device_id'].some(k=>request[k]!==this.cfg[k])||!sameSource)throw Error('offline context mismatch');
  const current=this.logoutJournal.signed_out?this.logoutJournal.offline_request_id??'':this.auth?.offline_request_id||this.logoutJournal.offline_request_id||'';
  if(current&&current!==request.request_id||this.logoutJournal.signed_out&&!current)throw Error('offline approval is no longer current; generate a fresh request');
  const data=response.result,c=verifyLease(data.lease,this.cfg.trusted_keys,this.cfg.product_id,this.cfg.device_id);this.context(c);
  if(policyMode(c)==='none'||c.offline_request_id!==request.request_id||c.activation_id!==data.activation_id)throw Error('offline signature context mismatch');
  this.acceptCredential(data);this.prepareCredential();this.auth.offline_request_id=request.request_id;this.authDirty=true;this.saveCredential();
  this.wall=Date.now()/1000;this.tick=performance.now();this.state={version:1,license_hash:this.hash(),lease:data.lease,observed_at:Math.floor(this.wall)};this.interval=c.runtime_policy?.heartbeat_seconds??3600;
  try{this.save();}catch(error){this.invalidate();throw error;}this.publish(false);return c;
 }
 publish(online,error=null){const before=this.status();this.online=online;this.lastError=error;if(error?.retryAfterSeconds)this.retryAt=Date.now()+error.retryAfterSeconds*1000;const after=this.status();if(JSON.stringify(before)!==JSON.stringify(after)&&this.onStatusChanged)queueMicrotask(()=>this.onStatusChanged?.(after));}
 deviceInfo(){const id=this.cfg.device_id;return {name:this.cfg.device_name||'',code:(id.slice(0,4)+'-'+id.slice(-4)).toUpperCase(),platform:this.profile.platform,...this.displayDetails};}
 status(){const e=this.lastError;const result={device:this.deviceInfo(),state:'verification_required',allowed:false,code:e?.code??(e?'NETWORK_UNAVAILABLE':''),request_id:e?.requestID??'',retryable:e?.retryable??!!e,lease_valid_until:0};try{const c=this.check();Object.assign(result,{state:this.online?'active':'offline_valid',allowed:true,lease_valid_until:c.expires_at});}catch{}result.state=({LICENSE_EXPIRED:'expired',LICENSE_REVOKED:'revoked',LICENSE_SUSPENDED:'revoked',DEVICE_RELEASED:'released',DEVICE_CREDENTIAL_REVOKED:'released',FEATURE_QUOTA_EXCEEDED:'quota_exhausted'})[result.code]??result.state;return result;}
 featureStatus(name){const result=this.status();if(result.allowed){try{if(!this.check().features.includes(name)){result.allowed=false;result.code='FEATURE_NOT_ENTITLED';}}catch{result.allowed=false;result.state='verification_required';}}return result;}
 async runFeature(name, action) {
  if(typeof action !== 'function') throw TypeError('business action is required');
  const status=this.featureStatus(name);
  if(!status.allowed) throw Error(`feature denied: ${status.code} (${status.state}); request_id=${status.request_id}`);
  return await action();
 }
 async runMeteredFeature(feature,quantity,operationID,action){return runMetered(this,feature,quantity,operationID,action);}
 async resolveMeteredFeature(operationID,succeeded){return resolveMetered(this,operationID,succeeded);}
 reservation(op,operationID,extra,signal){return this.serial(async()=>{if(!operationID)throw Error('retain operationID for retries');const c=this.check();try{const result=(await this.request('consume/'+op,c.activation_id,signal,operationID,extra)).reservation;this.publish(true);return result;}catch(e){if(explicitRevocation(e))deny(this);this.publish(false,e);throw e;}});}
 reserve(feature,quantity,operationID,signal){return this.reservation('reserve',operationID,{feature,quantity},signal);}
 commit(reservationID,operationID,signal){return this.reservation('commit',operationID,{reservation_id:reservationID},signal);}
 cancel(reservationID,operationID,signal){return this.reservation('cancel',operationID,{reservation_id:reservationID},signal);}
 async close(){this.closed=true;clearTimeout(this.logoutTimer);clearTimeout(this.timer);this.timer=null;this.controller.abort();await this.queue;try{if(this.check().capabilities?.license_model==='floating'){const id=this.check().activation_id;this.invalidate();await this.request('deactivate',id);}}catch{}if(this.proxyAgent)await this.proxyAgent.close();}
}
