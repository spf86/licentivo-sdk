import {createInterface} from 'node:readline/promises';
import {stdin,stdout} from 'node:process';
import {writeFileSync} from 'node:fs';
import {Client} from './licentivo.mjs';
// BEGIN LICENTIVO PRODUCT
// Copy Product ID and all key ID/public key pairs from Console > Products > Signing public keys.
const PRODUCT_ID = 'YOUR_PRODUCT_ID';
const TRUSTED_KEYS = {'YOUR_KEY_ID': 'YOUR_PUBLIC_KEY'};
// END LICENTIVO PRODUCT

const input=createInterface({input:stdin,output:stdout});let client;
const lines=input[Symbol.asyncIterator]();
async function ask(prompt){stdout.write(prompt);const line=await lines.next();return line.done?'0':line.value;}
try {
 const key=await ask('License key (empty to restore this device): ');
 client=await Client.openProduct(PRODUCT_ID,TRUSTED_KEYS,key,error=>console.error('Verification:',error.message));
 client.onStatusChanged=status=>console.log('License:',status.state);
 for(;;) {
  const choice=await ask('1 Status  2 Export CSV  3 Pro report  4 Refresh license  5 Release device  0 Exit: ');
  if(choice==='0'||choice==='')break;
  try {
   if(choice==='1')console.log(client.status());
   if(choice==='2'){await client.runFeature('export',()=>writeFileSync('licensed-report.csv','Item,Quantity,Price\nNotebook,2,12.50\nPen,5,3.00\n',{mode:0o600}));console.log('Saved licensed-report.csv');}
   if(choice==='3')await client.runFeature('pro',()=>writeFileSync('pro-report.txt','Sales total: 40.00\n',{mode:0o600}));
   if(choice==='4')await client.refreshOnline();
   if(choice==='5')await client.deactivate();
  }catch(error){console.error('Operation blocked or failed:',error.message);}
 }
} catch(error){console.error(error.message);process.exitCode=2;}
finally{input.close();if(client)await client.close();}
