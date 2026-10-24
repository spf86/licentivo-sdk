import {readFileSync,writeFileSync,existsSync} from 'node:fs';
import {Client, APIError} from './licentivo.mjs';

const index = process.argv.indexOf('--config');
const path = index >= 0 ? process.argv[index + 1] : '.local/protocol-fixture.json';
const cfg = JSON.parse(readFileSync(path, 'utf8'));
const cacheTest = process.argv.includes('--cache-test');
let client = new Client(cfg);
try {
    if(process.argv.includes('--logout-wait')){
        if(!client.checkFeature('export'))throw Error('missing initial authorization');writeFileSync(path+'.ready','ready');
        const deadline=Date.now()+30000;while(!existsSync(path+'.continue')){if(Date.now()>deadline)throw Error('logout wait timed out');await new Promise(r=>setTimeout(r,10));}
        if(!(await client.logout()).local_complete||client.checkFeature('export'))throw Error('logout failed');
    }else
    if(process.argv.includes('--logout-retry')){await client.retryPendingLogouts();if(client.checkFeature('export'))throw Error('logout restored access');console.log('JavaScript: pending logout completed');process.exitCode=0;}
    else
    if(process.argv.includes('--improvements-test')) {
        await client.start();const one=await client.reserve('export',1,'cancel-job');if(one.status!=='pending'||one.consumption.used!==0||one.consumption.reserved!==1)throw Error('reserve failed');
        const canceled=await client.cancel(one.reservation_id,'cancel-job');if(canceled.status!=='canceled'||canceled.consumption.used!==0)throw Error('cancel charged');
        const two=await client.reserve('export',1,'commit-job');for(let i=0;i<2;i++){const result=await client.commit(two.reservation_id,'commit-job');if(result.status!=='committed'||result.consumption.used!==1)throw Error('duplicate commit');}
        if((await client.reserve('export',1,'commit-job')).status!=='committed')throw Error('unsafe business replay');
        let failed=false;try{await client.consume('export',1000,'quota-job');}catch(e){failed=e.code==='FEATURE_QUOTA_EXCEEDED'&&!!e.requestID&&!e.retryable;}
        if(!failed||client.status().state!=='quota_exhausted'||!client.checkFeature('export'))throw Error('typed quota status missing');
        await client.close();cfg.license_key='';client=new Client(cfg);await client.start();await client.validate();if(!client.checkFeature('export'))throw Error('normal credential not retained');await client.deactivate();if(client.status().allowed)throw Error('release still grants');console.log('JavaScript: credential / reservation / typed status OK');
    }else if(process.argv.includes('--startup-test')) {
        let begin=performance.now();await client.start();if(performance.now()-begin>300)throw Error('cached startup blocked');await new Promise(r=>setTimeout(r,100));begin=performance.now();if(!client.checkFeature('export')||performance.now()-begin>300)throw Error('local check blocked');console.log('JavaScript: cached startup and local feature checks stay responsive');
    }else     if(process.argv.includes('--offline-export')) {
        writeFileSync(path+'.request.json',JSON.stringify(client.offlineRequest()),{mode:0o600});console.log('JavaScript: offline request exported');
    }else if(process.argv.includes('--transfer-test')||process.argv.includes('--transfer-import')) {
        if(process.argv.includes('--transfer-import'))client.importOffline(JSON.parse(readFileSync(path+'.request.json','utf8')),JSON.parse(readFileSync(path+'.response.json','utf8')));else await client.start();
        await client.validate();await client.heartbeat();if(!client.checkFeature('export'))throw Error('feature denied');console.log('JavaScript: transferred device credential OK');
    }else if(process.argv.includes('--floating-test')) {
        await client.start();const other=new Client(cfg);
        try {
            if(other.checkFeature('export'))throw Error('session cache reused');
            await other.start();
            await client.close();await other.heartbeat();if(!other.checkFeature('export'))throw Error('new session denied');
            console.log('JavaScript: floating seat release OK');
        }finally{await other.close();}
    }else if(process.argv.includes('--advanced-test')||process.argv.includes('--offline-import')) {
        if(process.argv.includes('--offline-import'))client.importOffline(JSON.parse(readFileSync(path+'.request.json','utf8')),JSON.parse(readFileSync(path+'.response.json','utf8')));else await client.start();
        if(!client.checkFeature('export'))throw Error('feature denied');
        if((await client.consume('export',1,'business-job-1')).used!==1||(await client.consume('export',1,'business-job-1')).used!==1)throw Error('consume replay failed');
        const over=await client.consume('export',3,'business-job-2');if(over.used!==4||over.overage!==1)throw Error('overage failed');
        let failed=false;try{await client.consume('export',1,'business-job-3');}catch(e){failed=e.code==='FEATURE_QUOTA_EXCEEDED';}
        if(!failed||!client.checkFeature('export'))throw Error('quota denial failed');await client.deactivate();console.log('JavaScript: offline/metered feature checks OK');
    }else if(process.argv.includes('--refresh-test')) {
        let failed=false;try{await client.refreshOnline();}catch{failed=true;}
        if(!failed||!client.checkFeature('export'))throw Error('refresh failure did not retain offline access');
        await client.refreshOnline();if(!client.checkFeature('updated')||client.checkFeature('export'))throw Error('online refresh did not replace cached policy');
        failed=false;try{await client.refreshOnline();}catch{failed=true;}
        if(!failed||client.checkFeature('updated'))throw Error('refresh denial did not clear cache');
        console.log('JavaScript: online refresh OK');
    }else if(process.argv.includes('--policy-test')){const deny=process.argv.includes('--expect-denied');if(client.checkFeature('export')===deny)throw Error('unexpected cache device/policy access');if(!deny){await client.activate();await client.validate();await client.heartbeat();}console.log('JavaScript: policy cache OK');}else {
    const claims = await client.activate();
    console.log('JavaScript: activated', claims.activation_id);
    if (cacheTest) {
        await client.close();
        client = new Client(cfg);
        await client.validate();
        if (!client.checkFeature('export')) throw Error('cache not restored during server outage');
        let rejected = false;
        try { await client.validate(); } catch (e) { rejected = e instanceof APIError && e.status === 403; }
        if (!rejected || client.checkFeature('export')) throw Error('rejection did not invalidate cache');
        await client.close();
        client = new Client(cfg);
        if (client.checkFeature('export')) throw Error('rejected cache survived restart');
        console.log('JavaScript: cache restored on 503; 403 invalidated access and persisted cache');
    } else {
        await client.validate(); await client.heartbeat();
        console.log('JavaScript: export enabled', client.checkFeature('export'));
        await client.deactivate();
        console.log('JavaScript: deactivated');
    }
} } finally { await client.close(); }
