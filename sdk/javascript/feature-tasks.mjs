import {createHash,randomUUID} from 'node:crypto';
import {readFileSync,writeFileSync,renameSync,unlinkSync,mkdirSync,existsSync} from 'node:fs';
import {dirname} from 'node:path';
import {setTimeout as delay} from 'node:timers/promises';

function taskPath(client, id) {
  if(typeof id !== 'string' || !/^[\x21-\x7e]{1,80}$/.test(id)) throw Error('a stable operationID of 1-80 non-whitespace ASCII characters is required');
  let source=client.hash();
  const legacy=client.cfg.cache_path+'.task-'+createHash('sha256').update(source+'\n'+id).digest('hex')+'.json';
  if(client.cfg.launch_connection_id){
    const claims=client.check();
    source=createHash('sha256').update('LicentivoTask/v2\n'+client.cfg.base_url+'\n'+claims.product_id+'\n'+claims.license_id+'\n'+claims.device_id).digest('hex');
  }
  const path=client.cfg.cache_path + '.task-' + createHash('sha256').update(source+'\n'+id).digest('hex') + '.json';
  if(path!==legacy&&!existsSync(path)&&existsSync(legacy))renameSync(legacy,path);
  return path;
}
function read(path,id) {
  let raw;
  try{raw=readFileSync(path,'utf8');}catch(error){if(error.code==='ENOENT')return {version:1,operation_id:id,reservation_id:'',state:''};throw error;}
  if(Buffer.byteLength(raw)>4096)throw Error('invalid feature task journal');
  const task=JSON.parse(raw);
  if(task?.version!==1 || task.operation_id!==id || typeof task.feature!=='string' || !task.feature || !Number.isSafeInteger(task.quantity) || task.quantity<=0 || typeof task.state!=='string' || typeof task.reservation_id!=='string' || !task.reservation_id)throw Error('invalid feature task journal; do not repeat business');
  return task;
}
function save(path,task) {
  mkdirSync(dirname(path),{recursive:true,mode:0o700});
  const temporary=path+'.'+randomUUID()+'.tmp';
  try{writeFileSync(temporary,JSON.stringify(task),{mode:0o600,flag:'wx'});renameSync(temporary,path);}finally{try{unlinkSync(temporary);}catch(error){if(error.code!=='ENOENT')throw error;}}
}
async function finish(client,path,task,succeeded) {
  let receipt;
  try{receipt=await (succeeded?client.commit(task.reservation_id,task.operation_id):client.cancel(task.reservation_id,task.operation_id));}
  catch(error){throw Error(`confirmation pending for operation ${task.operation_id}; do not repeat business`,{cause:error});}
  const expected=succeeded?'committed':'canceled';
  if(receipt.status!==expected && (succeeded || receipt.status!=='expired'))throw Error('reservation closed without the requested outcome; do not repeat business');
  task.state=expected;save(path,task);
}
function keepReservation(client,feature,quantity,id,hold) {
  const controller=new AbortController();
  const done=(async()=>{
    let until=Date.parse(hold.expires_at),interval=60000;
    while(Number.isFinite(until)&&until>Date.now()){
      try{await delay(Math.min(interval,Math.max(10,(until-Date.now())/3)),undefined,{signal:controller.signal});}
      catch{return;}
      try{
        const renewed=await client.reserve(feature,quantity,id,controller.signal);
        if(renewed.status!=='pending'||renewed.reservation_id!==hold.reservation_id||renewed.operation_id!==id)return;
        until=Date.parse(renewed.expires_at);interval=60000;
      }catch{if(controller.signal.aborted)return;interval=5000;}
    }
  })();
  return async()=>{controller.abort();await done;};
}
export async function runMetered(client,feature,quantity,id,action) {
  if(typeof action!=='function' || !Number.isSafeInteger(quantity) || quantity<=0)throw Error('business action and positive integer quantity are required');
  if(client.featureTaskBusy)throw Error('another feature task is running');
  client.featureTaskBusy=true;
  let stopRenewal=async()=>{};
  try{
    const path=taskPath(client,id),task=read(path,id);
    if(task.state && (task.feature!==feature || task.quantity!==quantity))throw Error('operation ID was already used for another feature or quantity');
    Object.assign(task,{feature,quantity});
    if(task.state==='committed')return;
    if(task.state==='commit_pending')return await finish(client,path,task,true);
    if(task.state==='cancel_pending'){await finish(client,path,task,false);task.state='canceled';}
    if(task.state==='canceled')throw Error('operation canceled; use a new job ID for a new action');
    if(task.state==='business_running')throw Error('business outcome unknown; verify its result and call resolveMeteredFeature');
    if(task.state)throw Error('invalid feature task state');
    await client.runFeature(feature,()=>{});
    const hold=await client.reserve(feature,quantity,id);
    if(hold.status==='committed'){Object.assign(task,{reservation_id:hold.reservation_id,state:'committed'});save(path,task);return;}
    if(hold.status!=='pending')throw Error('reservation is closed; business was not run');
    Object.assign(task,{reservation_id:hold.reservation_id,state:'business_running'});
    try{save(path,task);}catch(error){try{await client.cancel(hold.reservation_id,id);}catch{}throw error;}
    try{await client.runFeature(feature,async()=>{
      const deadline=Date.parse(hold.expires_at);
      if(!Number.isFinite(deadline) || deadline<=Date.now())throw Error('reservation expired; business was not run');
      stopRenewal=keepReservation(client,feature,quantity,id,hold);
      return await action();
    });}catch(error){task.state='cancel_pending';try{save(path,task);await stopRenewal();await finish(client,path,task,false);}catch{}throw error;}
    task.state='commit_pending';
    try{save(path,task);}catch(error){throw Error(`business completed; journal update failed; do not repeat operation ${id}`,{cause:error});}
    await stopRenewal();
    await finish(client,path,task,true);
  }finally{try{await stopRenewal();}finally{client.featureTaskBusy=false;}}
}
export async function resolveMetered(client,id,succeeded) {
  if(client.featureTaskBusy)throw Error('another feature task is running');
  client.featureTaskBusy=true;
  try{
    const path=taskPath(client,id),task=read(path,id);
    if(task.state===(succeeded?'committed':'canceled'))return;
    if(typeof succeeded!=='boolean' || !task.reservation_id || !['business_running','commit_pending','cancel_pending'].includes(task.state) || task.state==='commit_pending'&&!succeeded || task.state==='cancel_pending'&&succeeded)throw Error('task cannot be resolved with that outcome');
    task.state=succeeded?'commit_pending':'cancel_pending';save(path,task);
    await finish(client,path,task,succeeded);
  }finally{client.featureTaskBusy=false;}
}
