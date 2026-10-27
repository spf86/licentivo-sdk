import {readFileSync,writeFileSync,mkdirSync,renameSync,unlinkSync,statSync,readdirSync,openSync,fsyncSync,closeSync} from 'node:fs';
import {dirname,join,resolve} from 'node:path';
import {homedir} from 'node:os';
import {randomUUID,createHash} from 'node:crypto';

const revoked=new Set(['LICENSE_REVOKED','LICENSE_SUSPENDED','LICENSE_EXPIRED','DEVICE_RELEASED','DEVICE_NOT_ACTIVE','DEVICE_CREDENTIAL_REVOKED','DEVICE_CREDENTIAL_INVALID','AUTHORIZATION_SESSION_REVOKED','AUTHORIZATION_SESSION_EXPIRED','LAUNCH_LOGIN_REQUIRED','LAUNCH_ENTITLEMENT_REVOKED','LAUNCH_ENTITLEMENT_EXPIRED']);
export const explicitRevocation=error=>revoked.has(error?.code);
const authorizationDenials=new Set(['PRODUCT_DISABLED','TENANT_SUSPENDED','LICENSE_INVALID','DEVICE_CREDENTIAL_MISMATCH','LAUNCH_ENVIRONMENT_MISMATCH','SESSION_EXPIRED','VERSION_NOT_ENTITLED','VERSION_NOT_PUBLISHED','MAINTENANCE_EXPIRED','MAINTENANCE_STATE_INVALID','FEATURE_NOT_ENTITLED','HARDWARE_ID_REQUIRED','VIRTUAL_MACHINE_NOT_ALLOWED','SDK_UPGRADE_REQUIRED','LAUNCH_POLICY_UNSUPPORTED','RUNTIME_WINDOW_INVALID']);
export const authorizationRejected=error=>explicitRevocation(error)||authorizationDenials.has(error?.code);
const digest=s=>createHash('sha256').update(s).digest('hex');
const binding=t=>t?`${t.activation_id}:${t.expected_generation}`:'';
function directory(c){return join(process.env.LOCALAPPDATA||process.env.XDG_CACHE_HOME||join(homedir(),'.cache'),'Licentivo','logout',digest([resolve(c.cfg.cache_path),c.cfg.base_url,c.cfg.product_id,c.cfg.device_id].join('\n')));}
function write(path,journal){
 mkdirSync(dirname(path),{recursive:true,mode:0o700});
 const tmp=join(dirname(path),'.logout-'+randomUUID());
 try{writeFileSync(tmp,JSON.stringify(journal),{mode:0o600,flag:'wx'});const fd=openSync(tmp,'r+');try{fsyncSync(fd);}finally{closeSync(fd);}renameSync(tmp,path);}finally{try{unlinkSync(tmp);}catch{}}
}
function save(c){
 c.logoutJournal.revision=Math.max((c.logoutJournal.revision??0)+1,Date.now());
 let fallbackError,primaryError;
 try{write(join(directory(c),digest(c.logoutJournal.source_hash+'\n'+(c.logoutJournal.binding_key??''))+'.json'),c.logoutJournal);}catch(e){fallbackError=e;}
 try{write(c.cfg.cache_path+'.logout.json',c.logoutJournal);}catch(e){primaryError=e;}
 if(fallbackError&&(!c.logoutJournal.signed_out||primaryError))throw fallbackError;
}
export function loadLogout(c){
 c.signedOut=false;c.logoutBindings=new Set();c.logoutJournal={version:1,source_hash:'',signed_out:false,pending:[]};
 const records=new Map();const read=path=>{try{
  if(statSync(path).size>65536)throw Error('invalid logout journal');
  const j=JSON.parse(readFileSync(path,'utf8'));if(j.version!==1||!Array.isArray(j.pending))throw Error('invalid logout journal');
  const scope=j.source_hash+'\n'+(j.binding_key??''),old=records.get(scope);if(!old||(j.revision??0)>(old.revision??0)||(j.revision??0)===(old.revision??0)&&(j.signed_out||!old.signed_out))records.set(scope,j);
 }catch(e){if(e.code!=='ENOENT')c.signedOut=true;}};
 read(c.cfg.cache_path+'.logout.json');
 try{for(const name of readdirSync(directory(c)))if(name.endsWith('.json'))read(join(directory(c),name));}catch(e){if(e.code!=='ENOENT')c.signedOut=true;}
 const hash=c.hash(),key=binding(c.auth?.logout_ticket),pending=new Map();
 for(const j of records.values()){
  if(j.signed_out&&j.binding_key)c.logoutBindings.add(j.binding_key);
  if((j.source_hash===hash||c.cfg.launch_connection_id&&key&&j.binding_key===key||!c.auth&&!c.cfg.identity_token&&!c.cfg.license_key)&&(!j.binding_key||!key||j.binding_key===key)){c.logoutJournal=j;if(j.signed_out&&!c.cfg.license_key)c.signedOut=true;}
  for(const t of j.pending)pending.set(t.operation_id,t);
 }
 c.logoutJournal.pending=[...pending.values()];
 if(c.signedOut||c.logoutJournal.signed_out)c.auth=null;
 if(c.logoutJournal.pending.length)recoverLogout(c);
}
export function deny(c,queueRelease=true){
 c.signedOut=true;
 const hash=c.hash();if(c.cfg.license_key||c.cfg.identity_token||c.auth)c.logoutJournal.source_hash=hash;
 c.logoutJournal.signed_out=true;
 c.logoutJournal.offline_request_id='';
 const ticket=c.auth?.logout_ticket;
 if(ticket){c.logoutJournal.binding_key=binding(ticket);c.logoutBindings.add(binding(ticket));}
 if(queueRelease&&ticket&&!c.logoutJournal.pending.some(p=>p.operation_id===ticket.operation_id))c.logoutJournal.pending.push(ticket);
 let failure;
 try{save(c);}catch(e){failure=e;}
 c.invalidate();clearTimeout(c.timer);c.timer=null;c.controller.abort();c.auth=null;c.authDirty=false;c.cfg.license_key='';c.cfg.identity_token='';
 if(c.logoutJournal.pending.length)recoverLogout(c);
 return failure;
}
export function acceptLogin(c,ticket,ErrorType){
 if(c.cfg.launch_connection_id&&c.logoutBindings.has(binding(ticket))){if(c.auth)c.auth.logout_ticket=ticket;deny(c);throw new ErrorType(403,'LAUNCH_LOGIN_REQUIRED');}
 if(c.logoutJournal.signed_out){c.logoutJournal.signed_out=false;save(c);}
}
export function rememberOfflineRequest(c,id){if(c.logoutJournal.signed_out){c.logoutJournal.offline_request_id=id;save(c);}}
function acknowledge(c,operation){
 let names;try{names=readdirSync(directory(c));}catch(e){if(e.code==='ENOENT')return;throw e;}
 for(const name of names){if(!name.endsWith('.json'))continue;const path=join(directory(c),name);if(statSync(path).size>65536)throw Error('invalid logout journal');const j=JSON.parse(readFileSync(path,'utf8'));const pending=j.pending.filter(t=>t.operation_id!==operation);if(pending.length!==j.pending.length){j.pending=pending;write(path,j);}}
}
export async function retryLogouts(c,signal){
 while(c.logoutJournal.pending.length){
  const ticket=c.logoutJournal.pending[0];const timeout=AbortSignal.timeout(Math.min(c.cfg.timeout_seconds??10,10)*1000);
  const response=await fetch(c.cfg.base_url+'/api/v1/sessions/logout',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(ticket),redirect:'manual',dispatcher:c.proxyAgent,signal:signal?AbortSignal.any([signal,timeout]):timeout});
  const chunks=[];let size=0;const reader=response.body?.getReader();try{if(reader)for(;;){const part=await reader.read();if(part.done)break;size+=part.value.byteLength;if(size>65536){await reader.cancel();throw Error('oversized logout response');}chunks.push(part.value);}}finally{reader?.releaseLock();}
  const data=JSON.parse(new TextDecoder('utf-8',{fatal:true}).decode(Buffer.concat(chunks)));
  if(response.status!==200||!['succeeded','stale'].includes(data.data?.status)||data.data.operation_id!==ticket.operation_id||data.data.activation_id!==ticket.activation_id||data.data.expected_generation!==ticket.expected_generation)throw Error('device revocation pending');
  c.logoutJournal.pending.shift();acknowledge(c,ticket.operation_id);save(c);
 }
}
export function recoverLogout(c){
 if(c.logoutTimer||c.closed||!c.logoutJournal.pending.length)return;
 c.logoutTimer=setTimeout(()=>{c.logoutTimer=null;if(c.closed)return;void c.serial(async()=>{try{await retryLogouts(c);c.logoutDelay=1000;}catch{c.logoutDelay=Math.min((c.logoutDelay??1000)*2,300000);}recoverLogout(c);});},c.logoutDelay??1000);
 c.logoutTimer.unref?.();
}
export function logout(c,signal){
 // This barrier is set synchronously, before the serial queue or network is awaited.
 c.signedOut=true;
 return c.serial(async()=>{
  const storageError=deny(c);try{await retryLogouts(c,signal);}catch{}
  if(c.logoutJournal.pending.length)recoverLogout(c);
  const result={local_complete:!storageError,revocation_pending:c.logoutJournal.pending.length>0};
  if(storageError)result.storage_error='LOCAL_CREDENTIAL_CLEANUP_FAILED';return result;
 });
}
