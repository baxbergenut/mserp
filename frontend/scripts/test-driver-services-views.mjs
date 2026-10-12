// Local browser fixtures: no production API or vendor calls.
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';
import assert from 'node:assert/strict';
import { chromium, expect } from '@playwright/test';

const writes=[]; const drivers=[]; const setups=new Map(); let readOnly=false;
const pageOf=items=>({items,total:items.length,totalPages:1,page:1,pageSize:25});
const baseDriver={active:true,status:'active',isOwnerOperator:false,payType:'cpm',payRate:.65,truckId:null,dispatcherId:null,homeVersion:0,notes:'',updatedAt:new Date().toISOString()};
const policies=['MS Express Company - ELD','MS Express O/O - ELD','Non ELD Policy'].map((name,i)=>({id:`policy-${i}`,name,eld_enabled:i!==2,limits:[],authorization_prompts:[]}));
const item=service=>({service,state:'not_requested',requestState:'not_requested',version:0,message:'',followUpOn:'',configured:true,candidates:[],operations:[]});
const makeSetup=(driver,intents)=>({driverId:driver.id,name:driver.fullName,driverStatus:'active',tomorrow:'2026-10-13',checkedAt:new Date().toISOString(),environment:'production',policies,canLinkRelay:true,items:['relay','weighmytruck'].map(service=>{const i=item(service),choice=intents?.find(v=>v.service===service);return choice?{...i,state:choice.action==='add'?'queued':'deferred',requestState:choice.action==='add'?'queued':'deferred',version:1,followUpOn:choice.followUpOn}:i})});
const entry=(driver)=>({driver:{id:'relay-existing',first_name:'ELD',last_name:'Driver',phone:'+15551234567',data_fields:[]},driverId:driver.id,revision:'a'.repeat(64),assignment:null,suggestions:[],status:'active'});
const server=createServer(async(req,res)=>{
 try {
  const url=new URL(req.url,'http://localhost'),p=url.pathname;
  const send=(data,code=200)=>{res.writeHead(code,{'Content-Type':'application/json'});res.end(JSON.stringify(data));};
  if(p.startsWith('/api/')){
   if(p==='/api/auth/session')return send({user:{id:'test-user',username:'Test User',theme:'default',permissions:['fleet.read','fuel.read','weighmytruck.read','tasks.read',...(readOnly?[]:['fleet.write','fuel.drivers.write','weighmytruck.write','tasks.write'])]},csrfToken:'fixture'});
   if(p==='/api/sidebar-counts')return send({fuel:0,weighMyTruck:0,tolls:0,tollsFrom:'2026-09-01'});
   if(p==='/api/tasks/events'){res.writeHead(200,{'Content-Type':'text/event-stream'});res.write(': fixture\n\n');return;}
   if(p==='/api/tasks/count')return send({count:1});
   if(p==='/api/tasks/users')return send([]);
   if(p==='/api/tasks')return send(pageOf([{id:'task-1',title:'Set up Browser Driver in WeighMyTruck',notes:'Review service setup',systemTaskKind:'weighmytruck_setup',serviceDriverId:'driver-1',status:'open',createdAt:new Date().toISOString(),updatedAt:new Date().toISOString(),completedAt:null,assigneeName:'Test User'}]));
   if(p==='/api/driver-directory')return send(pageOf([]));
   if(p==='/api/drivers/setup-defaults')return send({escrowAmount:'2500.00'});
   if(['/api/trucks','/api/dispatchers','/api/investors'].includes(p))return send([]);
   if(p==='/api/driver-charges')return send({schedules:[]});
   if(p.endsWith('/notes'))return send([]);
   if(p.endsWith('/location'))return send({truckId:null,location:null});
   if(p==='/api/drivers'&&req.method==='GET')return send(drivers);
   if(p.startsWith('/api/drivers/')&&req.method==='GET')return send(drivers.find(d=>p.endsWith(d.id)));
   if(p==='/api/driver-intake/intake-1')return send({id:'intake-1',driver:{fullName:'Onboard Browser',hireDate:'2026-10-12',driverType:'company',phone:'5551234567',email:'onboard@example.test'},candidates:[]});
   if(p.startsWith('/api/driver-services/')&&req.method==='GET')return send({...setups.get(p.split('/').at(-1)),checkedAt:new Date().toISOString()});
   let body='';for await(const c of req)body+=c;const input=body?JSON.parse(body):{};
   writes.push({path:p,input});
   if(p==='/api/drivers'||p==='/api/driver-intake/intake-1/complete'){
    const driver={...baseDriver,...(input.driver||input),id:`driver-${drivers.length+1}`};drivers.push(driver);setups.set(driver.id,makeSetup(driver,input.services));return send(driver);
   }
   if(p.startsWith('/api/driver-services/')){
    const setup=setups.get(p.split('/').at(-1)),i=setup.items.find(i=>i.service===input.service);
    i.version++;i.requestState=input.action==='defer'?'deferred':'queued';if(i.state!=='unconfirmed')i.state=i.requestState;i.followUpOn=input.followUpOn;return send({saved:true});
   }
   if(p==='/api/relay-drivers/change'){
    const i=[...setups.values()].flatMap(s=>s.items).find(i=>i.relay?.driver.id===input.relayDriverId);
    i.state='added';i.message='Account linked and policy enabled.';i.relay.assignment={id:'a',policy_id:input.policyId,policy_name:policies.find(p=>p.id===input.policyId).name,enabled:true};return send({saved:true});
   }
   if(p.endsWith('/verify')){const i=setups.get('driver-1').items[1];i.state='added';i.message='Membership confirmed in MSERP.';i.membership.enrolled=true;i.membership.state='confirmed';return send({saved:true});}
   if(p.endsWith('/link')){const setup=setups.get(input.driverId),i=setup.items[0];i.relay=i.candidates[0];i.relay.driverId=input.driverId;i.candidates=[];i.state='needs_setup';return send({saved:true});}
   return send({});
  }
  const file=p==='/'?'index.html':extname(p)?p.slice(1):`${p.slice(1)}.html`;
  if(file.includes('..'))return send({},404);
  const body=await readFile(new URL(`../out/${file}`,import.meta.url));res.writeHead(200,{'Content-Type':{'.html':'text/html','.js':'application/javascript','.css':'text/css','.txt':'text/plain'}[extname(file)]||'application/octet-stream'});res.end(body);
 }catch(e){res.writeHead(500);res.end(String(e));}
});
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
const base=`http://127.0.0.1:${server.address().port}`;
const browser=await chromium.launch();
try{
 const page=await browser.newPage({viewport:{width:1440,height:1100}});const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(`${base}/drivers`);await page.getByRole('button',{name:'Add driver',exact:true}).click();
 const dialog=page.getByRole('dialog');
 await dialog.getByLabel('Full name',{exact:true}).fill('Browser Driver');
 await dialog.getByLabel('Driver started working').fill('2026-10-12');
 await dialog.getByLabel('Rate per mile ($)').fill('0.65');
 const draft=dialog.getByRole('region',{name:'Driver services'});
 await draft.getByRole('button',{name:'Add after saving'}).nth(0).click();
 await draft.getByRole('button',{name:'Remind me later'}).nth(1).click();
 assert.equal(writes.length,0,'draft must never create vendor writes');
 await page.screenshot({path:join(tmpdir(),'mserp-service-choices.png')});
 await dialog.getByRole('button',{name:'Create driver',exact:true}).click();
 await expect(dialog).toHaveAttribute('aria-label','Browser Driver saved');
 assert.deepEqual(writes[0].input.services.map(i=>[i.service,i.action]),[['relay','add'],['weighmytruck','defer']]);
 await expect(dialog.getByLabel('Relay setup')).toContainText('Waiting to add');
 await expect(dialog.getByLabel('WeighMyTruck setup')).toContainText('Reminder set');
 const setup=setups.get('driver-1'),r=setup.items[0],w=setup.items[1];
 Object.assign(r,{state:'needs_setup',requestState:'review',relay:entry(drivers[0]),message:'Account added. Assign or enable a policy to finish fuel setup.'});
 await expect(dialog.getByLabel('Relay setup')).toContainText('Added · policy needed',{timeout:10000});
 await dialog.getByLabel('Relay policy').selectOption('policy-2');
 await dialog.getByLabel('Driver PIN',{exact:false}).fill('test-pin');await dialog.getByLabel('Truck number',{exact:false}).fill('101');
 await dialog.getByRole('button',{name:'Assign policy',exact:true}).click();
 await expect(dialog.getByLabel('Relay setup')).toContainText('Non ELD Policy · Enabled');
 assert.equal(writes.at(-1).input.driverPin,'test-pin');
 Object.assign(w,{state:'unconfirmed',requestState:'review',version:3,message:'The service did not confirm enrollment.',membership:{id:'membership-1',driverId:'driver-1',version:1,enrolled:false,state:'review',canVerify:true}});
 await page.evaluate(()=>window.dispatchEvent(new Event('focus')));
 await expect(dialog.getByLabel('WeighMyTruck setup')).toContainText('Check result',{timeout:10000});
 await expect(dialog.getByRole('button',{name:'Retry enrollment'})).toHaveCount(0);
 await dialog.getByRole('button',{name:'Remind me later',exact:true}).click();await dialog.getByRole('button',{name:'Save reminder',exact:true}).click();
 await expect(dialog.getByLabel('WeighMyTruck setup')).toContainText('Follow up');
 await dialog.getByLabel('Verified WeighMyTruck membership').selectOption('added');
 await dialog.getByLabel('I checked the result on the service website.').check();
 await dialog.getByRole('button',{name:'Record verified result',exact:true}).click();
 await expect(dialog.getByLabel('WeighMyTruck setup').getByRole('status').first()).toHaveText('Added');
 await page.screenshot({path:join(tmpdir(),'mserp-service-results.png')});
 await dialog.getByRole('button',{name:'Done',exact:true}).click();await expect(dialog).toHaveCount(0);
 // Onboarding uses the same draft cards and persists intents in the completion request.
 await page.goto(`${base}/drivers?setup=intake-1`);await expect(dialog).toHaveAttribute('aria-label','Set up Onboard Browser');
 await dialog.getByLabel('Rate per mile ($)').fill('0.65');
 const before=writes.length;await dialog.getByRole('button',{name:'Add after saving'}).nth(0).click();await dialog.getByRole('button',{name:'Add after saving'}).nth(1).click();assert.equal(writes.length,before);
 await dialog.getByRole('button',{name:'Complete setup',exact:true}).click();await expect(dialog).toHaveAttribute('aria-label','Onboard Browser saved');
 assert.equal(writes.at(-1).path,'/api/driver-intake/intake-1/complete');assert.equal(writes.at(-1).input.services.length,2);assert.equal(writes.at(-1).input.addWeighMyTruck,undefined);
 const s2=setups.get('driver-2');Object.assign(s2.items[0],{state:'review',requestState:'review',candidates:[{...entry(drivers[1]),driverId:''}]});Object.assign(s2.items[1],{state:'review',requestState:'review',message:'Add a valid email to the driver profile.'});
 await expect(dialog.getByText('Possible existing account. Confirm the person before linking.')).toBeVisible({timeout:10000});
 await dialog.getByLabel('Existing Relay account').selectOption('relay-existing');await dialog.getByLabel('I confirmed this is Onboard Browser.').check();
 await dialog.getByRole('button',{name:'Confirm and link',exact:true}).click();await expect(dialog.getByLabel('Relay setup')).toContainText('Added · policy needed');
 await page.setViewportSize({width:390,height:844});await page.screenshot({path:join(tmpdir(),'mserp-service-mobile.png')});
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>window.innerWidth),false,'mobile overflow');
 await dialog.getByRole('button',{name:'Done',exact:true}).click();
 // Task deep link resumes the saved service cards, including for read-only users.
 await page.goto(`${base}/tasks`);await page.getByRole('button',{name:'Set up Browser Driver in WeighMyTruck',exact:true}).click();
 await page.getByRole('link',{name:'Review service setup',exact:true}).click();
 await expect(page.getByLabel('Relay setup')).toContainText('Added');
 readOnly=true;await page.reload();await expect(page.getByLabel('WeighMyTruck setup')).toContainText('Added');
 await expect(page.getByRole('button',{name:'Remind me later',exact:true})).toHaveCount(0);
 assert.deepEqual(errors,[]);console.log('Driver service browser checks passed: drafts, both save flows, policy, uncertainty, reminder, account linking, task resume, read-only and mobile.');
 console.log(`Screenshots: ${join(tmpdir(),'mserp-service-results.png')}, ${join(tmpdir(),'mserp-service-mobile.png')}`);
}finally{await browser.close();server.closeAllConnections();await new Promise(r=>server.close(r));}
