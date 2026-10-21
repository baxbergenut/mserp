// Local browser fixtures: no production API or vendor calls.
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';
import assert from 'node:assert/strict';
import { chromium, expect } from '@playwright/test';

const writes=[]; const drivers=[]; const trucks=[]; const truckSetups=new Map(); const setups=new Map(); let readOnly=false;
let intakeReads=0;
const pageOf=items=>({items,total:items.length,totalPages:1,page:1,pageSize:25});
const baseDriver={active:true,status:'active',isOwnerOperator:false,payType:'cpm',payRate:.65,truckId:null,dispatcherId:null,notes:'',updatedAt:new Date().toISOString()};
const policies=['MS Express Company - ELD','MS Express O/O - ELD','Non ELD Policy'].map((name,i)=>({id:`policy-${i}`,name,eld_enabled:i!==2,limits:[],authorization_prompts:[]}));
const item=service=>({service,state:'not_requested',requestState:'not_requested',version:0,message:'',followUpOn:'',configured:true,candidates:[],operations:[]});
const makeSetup=(driver,intents)=>({driverId:driver.id,name:driver.fullName,driverStatus:'active',tomorrow:'2026-10-13',checkedAt:new Date().toISOString(),environment:'production',policies,canLinkRelay:true,items:['relay','weighmytruck','datatruck'].map(service=>{const i=item(service),choice=intents?.find(v=>v.service===service);return choice?{...i,state:choice.action==='add'?'queued':'deferred',requestState:choice.action==='add'?'queued':'deferred',version:1,followUpOn:choice.followUpOn}:i})});
const entry=(driver)=>({driver:{id:'relay-existing',first_name:'ELD',last_name:'Driver',phone:'+15551234567',data_fields:[]},driverId:driver.id,revision:'a'.repeat(64),assignment:null,suggestions:[],status:'active'});
const server=createServer(async(req,res)=>{
 try {
  const url=new URL(req.url,'http://localhost'),p=url.pathname;
  const send=(data,code=200)=>{res.writeHead(code,{'Content-Type':'application/json'});res.end(JSON.stringify(data));};
  if(p.startsWith('/api/')){
   if(p.startsWith('/api/driver-intake/')&&req.method==='GET')intakeReads++;
   if(p==='/api/lookups/investors')return send([{id:'00000000-0000-0000-0000-000000000001',fullName:'MS Express Inc.',isCompany:true,active:true}]);
   if(p==='/api/lookups/drivers')return send(drivers);
   if(['/api/lookups/trucks','/api/lookups/dispatchers'].includes(p))return send([]);
   if(p==='/api/auth/session')return send({user:{id:'test-user',username:'Test User',theme:'default',permissions:['drivers.read','trucks.read','dispatchers.read','updaters.read','investors.read','fuel.read','weighmytruck.read','tasks.read',...(readOnly?[]:['drivers.create','drivers.edit','drivers.delete','trucks.create','trucks.edit','trucks.delete','dispatchers.create','dispatchers.edit','dispatchers.delete','updaters.create','updaters.edit','updaters.delete','investors.create','investors.edit','drivers.private.read','drivers.private.edit','driver_compensation.read','driver_compensation.edit','dispatcher_compensation.read','dispatcher_compensation.edit','escrow.read','escrow.create','escrow.edit','escrow.delete','fuel.drivers.create','fuel.drivers.edit','fuel.drivers.delete','fuel.drivers.recover','fuel.drivers.sync','weighmytruck.create','weighmytruck.delete','weighmytruck.sync','weighmytruck.link','weighmytruck.recover','tasks.create','tasks.edit','tasks.delete','tasks.review'])]},csrfToken:'fixture'});
   if(p==='/api/sidebar-counts')return send({fuel:0,weighMyTruck:0,tolls:0,tollsFrom:'2026-09-01'});
   if(p==='/api/maintenance/access')return send({});
   if(p==='/api/tasks/events'){res.writeHead(200,{'Content-Type':'text/event-stream'});res.write(': fixture\n\n');return;}
   if(p==='/api/tasks/count')return send({count:1});
   if(p==='/api/tasks/users')return send([]);
   if(p==='/api/tasks')return send(pageOf([{id:'task-1',title:'Set up Browser Driver in WeighMyTruck',notes:'Review service setup',systemTaskKind:'weighmytruck_setup',serviceDriverId:'driver-1',status:'open',createdAt:new Date().toISOString(),updatedAt:new Date().toISOString(),completedAt:null,assigneeName:'Test User'}]));
   if(p==='/api/driver-directory')return send(pageOf([]));
   if(p==='/api/drivers/setup-defaults')return send({escrowAmount:'2500.00'});
   if(p==='/api/investors')return send([{id:'00000000-0000-0000-0000-000000000001',fullName:'MS Express Inc.',isCompany:true,active:true}]);
   if(p==='/api/trucks'&&req.method==='GET')return send(pageOf(trucks));
   if(p==='/api/dispatchers')return send([]);
   if(/^\/api\/trucks\/[^/]+\/datatruck$/.test(p)&&req.method==='GET')return send(truckSetups.get(p.split('/').at(-2))||{state:'not_requested',message:'',version:0});
   if(p==='/api/driver-charges')return send({schedules:[]});
   if(p.endsWith('/notes'))return send([]);
   if(p.endsWith('/location'))return send({truckId:null,location:null});
   if(p==='/api/drivers'&&req.method==='GET')return send(drivers);
   if(p.startsWith('/api/drivers/')&&req.method==='GET')return send(drivers.find(d=>p.endsWith(d.id)));
   if(p==='/api/driver-intake/intake-1')return send({id:'intake-1',driver:{fullName:'Onboard Browser',hireDate:'2026-10-12',driverType:'company',phone:'5551234567',email:'onboard@example.test'},candidates:[]});
   if(p==='/api/driver-intake/intake-2')return send({id:'intake-2',driver:{fullName:'Another Browser',hireDate:'2026-10-13',driverType:'company'},candidates:[]});
   if(p.startsWith('/api/driver-services/')&&req.method==='GET')return send({...setups.get(p.split('/').at(-1)),checkedAt:new Date().toISOString()});
   let body='';for await(const c of req)body+=c;const input=body?JSON.parse(body):{};
   writes.push({path:p,input});
   if(p==='/api/trucks'&&req.method==='POST'){const truck={...input,id:`truck-${trucks.length+1}`,driverName:null,createdAt:new Date().toISOString(),updatedAt:new Date().toISOString()};trucks.push(truck);truckSetups.set(truck.id,{state:input.addToDataTruck?'queued':'not_requested',message:'',version:1});return send(truck);}
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
 const refreshAccess=async trigger=>{
  const reads=intakeReads;
  const refreshed=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/auth/session');
  await trigger();await refreshed;
  // Let React commit the session and run effects triggered by its new objects.
  await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
  assert.equal(intakeReads,reads,'routine session refresh must not reopen the intake');
 };
 await dialog.getByLabel('First name',{exact:true}).fill('Browser');
 await dialog.getByLabel('Last name',{exact:true}).fill('Driver');
 await page.screenshot({animations:"disabled",path:join(tmpdir(),'mserp-setup-information.png')});
 await expect(dialog.locator('input[type=file]')).toHaveCount(0);
 await expect(dialog.getByLabel('Driver home',{exact:true})).toHaveCount(0);
 await dialog.getByRole('button',{name:'Continue',exact:true}).click();
 await expect(dialog.getByLabel('Truck',{exact:true})).toHaveValue('__new__');
 await dialog.getByLabel('Rate per mile ($)').fill('0.65');
 await page.screenshot({animations:"disabled",path:join(tmpdir(),'mserp-setup-assignments.png')});
 await dialog.getByRole('button',{name:'Continue',exact:true}).click();
 await dialog.getByLabel('Unit number',{exact:true}).fill('SETUP-TRUCK');
 await page.screenshot({animations:"disabled",path:join(tmpdir(),'mserp-setup-truck.png')});
 await dialog.getByRole('button',{name:'Continue',exact:true}).click();
 const draft=dialog.getByRole('region',{name:'Driver services'});
 await expect(draft.getByRole('button',{name:'Add after saving'}).nth(2)).toHaveAttribute('aria-pressed','true');
 await draft.getByRole('button',{name:'Add after saving'}).nth(0).click();
 await draft.getByRole('button',{name:'Remind me later'}).nth(1).click();
 assert.equal(writes.length,0,`draft must never create vendor writes: ${JSON.stringify(writes)}`);
 await page.screenshot({animations:"disabled",path:join(tmpdir(),'mserp-service-choices.png')});
 await dialog.getByRole('button',{name:'Continue',exact:true}).click();
 await dialog.getByLabel('Driver started working').fill('2026-10-12');
 await dialog.getByRole('button',{name:'Previous',exact:true}).click();
 await expect(dialog.getByRole('button',{name:'Add after saving'}).nth(0)).toHaveAttribute('aria-pressed','true');
 await dialog.getByRole('button',{name:'Continue',exact:true}).click();
 await page.setViewportSize({width:390,height:844});
 await page.screenshot({animations:"disabled",path:join(tmpdir(),'mserp-setup-finalize-mobile.png')});
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>window.innerWidth),false);
 await page.setViewportSize({width:1440,height:1100});
 await dialog.getByRole('button',{name:'Create driver',exact:true}).click();
 await expect(dialog).toHaveAttribute('aria-label','Browser Driver saved');
 assert.equal(writes[0].input.newTruck.unitNumber,'SETUP-TRUCK');
 assert.deepEqual(writes[0].input.services.map(i=>[i.service,i.action]),[['datatruck','add'],['relay','add'],['weighmytruck','defer']]);
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
 await dialog.getByLabel('WeighMyTruck setup').getByRole('button',{name:'Remind me later',exact:true}).click();await dialog.getByRole('button',{name:'Save reminder',exact:true}).click();
 await expect(dialog.getByLabel('WeighMyTruck setup')).toContainText('Follow up');
 await dialog.getByLabel('Verified WeighMyTruck membership').selectOption('added');
 await dialog.getByLabel('I checked the result on the service website.').check();
 await dialog.getByRole('button',{name:'Record verified result',exact:true}).click();
 await expect(dialog.getByLabel('WeighMyTruck setup').getByRole('status').first()).toHaveText('Added');
 await page.screenshot({animations:"disabled",path:join(tmpdir(),'mserp-service-results.png')});
 await dialog.getByRole('button',{name:'Done',exact:true}).click();await expect(dialog).toHaveCount(0);
 // Onboarding uses the same draft cards and persists intents in the completion request.
 await page.goto(`${base}/drivers?setup=intake-1`);await expect(dialog).toHaveAttribute('aria-label','Set up Onboard Browser');
 await dialog.getByLabel('First name',{exact:true}).fill('Navigation draft');
 await page.evaluate(()=>window.history.pushState(null,'','/drivers?setup=intake-2'));
 await expect(dialog).toHaveAttribute('aria-label','Set up Another Browser');
 await expect(dialog.getByLabel('First name',{exact:true})).toHaveValue('');
 await page.evaluate(()=>window.history.pushState(null,'','/drivers?setup=intake-1'));
 await expect(dialog).toHaveAttribute('aria-label','Set up Onboard Browser');
 await expect(dialog.getByLabel('First name',{exact:true})).toHaveValue('');
 await expect(dialog.getByLabel('Last name',{exact:true})).toHaveValue('');
 await dialog.getByLabel('First name',{exact:true}).fill('Onboard');
 await dialog.getByLabel('Last name',{exact:true}).fill('Browser');
 await dialog.getByLabel('City',{exact:true}).fill('Louisville');
 await refreshAccess(()=>page.evaluate(()=>window.dispatchEvent(new Event('focus'))));
 await expect(dialog.getByLabel('First name',{exact:true})).toHaveValue('Onboard');
 await expect(dialog.getByLabel('Last name',{exact:true})).toHaveValue('Browser');
 await expect(dialog.getByLabel('City',{exact:true})).toHaveValue('Louisville');
 await dialog.getByRole('button',{name:'Continue',exact:true}).click();
 await dialog.getByLabel('Truck',{exact:true}).selectOption('');
 await dialog.getByLabel('Rate per mile ($)').fill('0.65');
 await dialog.getByLabel('Escrow amount ($)').fill('1500');
 await refreshAccess(()=>page.evaluate(()=>document.dispatchEvent(new Event('visibilitychange'))));
 await expect(dialog.getByLabel('Truck',{exact:true})).toHaveValue('');
 await expect(dialog.getByLabel('Rate per mile ($)')).toHaveValue('0.65');
 await expect(dialog.getByLabel('Escrow amount ($)')).toHaveValue('1500');
 await dialog.getByRole('button',{name:'Continue',exact:true}).click();
 const before=writes.length;await dialog.getByRole('button',{name:'Add after saving'}).nth(0).click();await dialog.getByRole('button',{name:'Add after saving'}).nth(1).click();assert.equal(writes.length,before);
 await page.clock.install();
 await refreshAccess(async()=>{await page.clock.runFor(30001);await page.clock.resume();});
 await expect(dialog.getByRole('button',{name:'Add after saving'}).nth(0)).toHaveAttribute('aria-pressed','true');
 await expect(dialog.getByRole('button',{name:'Add after saving'}).nth(1)).toHaveAttribute('aria-pressed','true');
 await dialog.getByRole('button',{name:'Continue',exact:true}).click();
 await dialog.getByRole('button',{name:'Complete setup',exact:true}).click();await expect(dialog).toHaveAttribute('aria-label','Onboard Browser saved');
 assert.equal(writes.at(-1).path,'/api/driver-intake/intake-1/complete');assert.equal(writes.at(-1).input.services.length,3);assert.equal(writes.at(-1).input.addWeighMyTruck,undefined);
 assert.equal(writes.at(-1).input.driver.city,'Louisville');
 assert.equal(writes.at(-1).input.driver.fullName,'Onboard Browser');
 assert.equal(writes.at(-1).input.driver.payRate,0.65);
 assert.equal(writes.at(-1).input.driver.escrowAmount,'1500');
 const s2=setups.get('driver-2');Object.assign(s2.items[0],{state:'review',requestState:'review',candidates:[{...entry(drivers[1]),driverId:''}]});Object.assign(s2.items[1],{state:'review',requestState:'review',message:'Add a valid email to the driver profile.'});
 await expect(dialog.getByText('Possible existing account. Confirm the person before linking.')).toBeVisible({timeout:10000});
 await dialog.getByLabel('Existing Relay account').selectOption('relay-existing');await dialog.getByLabel('I confirmed this is Onboard Browser.').check();
 await dialog.getByRole('button',{name:'Confirm and link',exact:true}).click();await expect(dialog.getByLabel('Relay setup')).toContainText('Added · policy needed');
 await page.setViewportSize({width:390,height:844});await page.screenshot({animations:"disabled",path:join(tmpdir(),'mserp-service-mobile.png')});
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>window.innerWidth),false,'mobile overflow');
 await dialog.getByRole('button',{name:'Done',exact:true}).click();
 Object.assign(w,{state:'review',requestState:'review',message:'Enrollment needs attention.'});
 // Task deep link resumes the saved service cards, including for read-only users.
 await page.goto(`${base}/tasks`);await page.getByRole('button',{name:'Set up Browser Driver in WeighMyTruck',exact:true}).click();
 await page.getByRole('link',{name:'Review service setup',exact:true}).click();
 await expect(page.getByLabel('Relay setup')).toContainText('Added');
 await expect(page.getByRole('button',{name:'Remind me later',exact:true})).toHaveCount(0);
 await expect(page.getByLabel('Relay setup').getByRole('link',{name:'Relay',exact:true})).toHaveAttribute('href','/fuel?tab=drivers');
 await expect(page.getByLabel('WeighMyTruck setup').getByRole('link',{name:'WeighMyTruck',exact:true})).toHaveAttribute('href','/weighmytruck');
 await expect(page.getByRole('link',{name:/^(Manage WeighMyTruck|Manage Relay|Open Relay)$/})).toHaveCount(0);
 Object.assign(w,{state:'added',requestState:'done'});
 await page.setViewportSize({width:1440,height:1100});
 await page.goto(`${base}/trucks`);await page.getByRole('button',{name:'Add truck',exact:true}).click();
 await expect(dialog.getByRole('checkbox',{name:/Add to DataTruck after saving/})).toBeChecked();
 await dialog.getByLabel('Unit number',{exact:true}).fill('DT-BROWSER');
 await page.screenshot({animations:'disabled',path:join(tmpdir(),'mserp-datatruck-truck-modal.png')});
 await dialog.getByRole('checkbox',{name:/Add to DataTruck after saving/}).uncheck();
 await dialog.getByRole('button',{name:'Create truck',exact:true}).click();await expect(dialog).toHaveCount(0);
 assert.equal(writes.at(-1).input.addToDataTruck,false);
 await page.getByRole('button',{name:'Add truck',exact:true}).click();
 await expect(dialog.getByRole('checkbox',{name:/Add to DataTruck after saving/})).toBeChecked();
 await dialog.getByLabel('Unit number',{exact:true}).fill('DT-ENROLL');
 await dialog.getByRole('button',{name:'Create truck',exact:true}).click();
 await expect(dialog).toHaveAttribute('aria-label','Truck DT-ENROLL saved');
 await expect(dialog.getByRole('status')).toContainText('Waiting to add');assert.equal(writes.at(-1).input.addToDataTruck,true);
 Object.assign(truckSetups.get('truck-2'),{state:'done',version:2});await expect(dialog.getByRole('status')).toContainText('Added',{timeout:10000});
 await dialog.getByRole('button',{name:'Done',exact:true}).click();
 await page.goto(`${base}/drivers/detail?id=driver-1&services=1`);
 readOnly=true;await page.reload();await expect(page.getByLabel('WeighMyTruck setup')).toContainText('Added');
 await expect(page.getByRole('button',{name:'Remind me later',exact:true})).toHaveCount(0);
 assert.deepEqual(errors,[]);console.log('Driver service browser checks passed: drafts survive session timer/focus/visibility refreshes, intake navigation, both save flows, policy, uncertainty, reminder, account linking, task resume, read-only and mobile.');
 console.log(`Screenshots: ${join(tmpdir(),'mserp-service-results.png')}, ${join(tmpdir(),'mserp-service-mobile.png')}`);
}finally{await browser.close();server.closeAllConnections();await new Promise(r=>server.close(r));}
