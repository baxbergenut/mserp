// Browser fixtures for both payroll entry points and dispatcher attribution.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname } from 'node:path';

const server=createServer(async(req,res)=>{
 try{const path=new URL(req.url,'http://localhost').pathname;
 const file=path==='/'?'index.html':extname(path)?path.slice(1):`${path.slice(1)}.html`;
 const body=await readFile(new URL(`../out/${file}`,import.meta.url));
 res.writeHead(200,{'Content-Type':{'.html':'text/html','.js':'application/javascript','.css':'text/css','.txt':'text/plain'}[extname(file)]??'application/octet-stream'});res.end(body);
 }catch{res.writeHead(404);res.end();}
});
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
const base=`http://127.0.0.1:${server.address().port}`;
const browser=await chromium.launch();
const week='2026-10-19',next='2026-10-26',source='2026-10-25';
try{
 for(const investor of [false,true]){
  let mode='',version=0,readonly=false,finalized=false,reject=false,omitStatement=false,sameDayLoad=false;
  const requests=[],errors=[];
  const page=await browser.newPage({viewport:{width:1800,height:1100}});
  page.on('pageerror',error=>errors.push(error.message));
  await page.clock.setFixedTime(new Date('2026-10-27T12:00:00Z'));
  const load=()=>({date:mode==='everyone'?next:source,sourceDate:source,slot:0,loadNumber:'WEEKEND',loadRecordId:9101,commentKey:'weekend',boardVersion:7,sourceDriverId:investor?'operator':undefined,
   originalRate:'3000',driverGross:'2800',totalMiles:'600',loadedMiles:'550',deadheadMiles:'50',fee:'700',pickupDate:source,pickupLocation:'Chicago, IL',deliveryLocation:'Newark, NJ',issues:[],
   placementVersion:version,transferVersion:mode?version:0,dispatchDate:mode?next:'',dispatchSlot:1,creditedDispatcherId:'old',creditedDispatcherName:'Old Dispatcher'});
  await page.route('**/api/**',async route=>{
   const request=route.request(),url=new URL(request.url()),path=url.pathname.slice(4),selected=url.searchParams.get('weekStart')||week;
   if(path.endsWith('/transfer-load')){
    const input=request.postDataJSON();requests.push(input);
    expect(input.driverId).toBe(investor?'operator':'statement');expect(input.date).toBe(source);expect(input.boardVersion).toBe(7);expect(input.transferVersion).toBe(version);expect(input.reason.trim()).not.toBe('');
    if(reject){reject=false;return route.fulfill({status:409,json:{error:'Another accountant changed this transfer'}});}
    mode=input.mode==='restore'?'':input.mode;version++;return route.fulfill({status:204});
   }
   const driver={id:'statement',fullName:'Transfer Test',truckUnit:'T1',payType:'gross_percentage',payRate:'25',isOwnerOperator:false,dispatcherId:'old',dispatcherName:'Old Dispatcher',fuelTotal:'0',tollTotal:'0',
    ...(investor?{investorId:'owner',truckId:'statement'}:{}),loads:(mode==='everyone'?selected===next:selected===week)?[load()]:[],settlement:finalized?{finalized:true}:undefined,
    edits:{driverId:'statement',weekStart:selected,version:0,notes:'',comments:{},adjustments:[],fuelOverride:null,tollOverride:null}};
   if(sameDayLoad)driver.loads.push({...load(),date:source,sourceDate:source,loadNumber:'STAYS',commentKey:'stays',slot:2,transferVersion:0});
   const fixtures={
    '/driver-pay/outgoing-transfers':{transfers:mode==='everyone'&&selected===week?[{statementId:'statement',driverId:'statement',driverName:'Transfer Test',truckId:'statement',truckUnit:'T1',loadNumber:'WEEKEND',sourceDate:source,sourceSlot:0,dispatchDate:next,settlementDate:next,boardVersion:7,transferVersion:version}]:[]},
    '/investor-pay/outgoing-transfers':{transfers:mode==='everyone'&&selected===week?[{statementId:'statement',driverId:'operator',driverName:'Transfer Test',truckId:'statement',truckUnit:'T1',loadNumber:'WEEKEND',sourceDate:source,sourceSlot:0,dispatchDate:next,settlementDate:next,boardVersion:7,transferVersion:version}]:[]},
    '/auth/session':{user:{id:'user',username:'Test',theme:'original',permissions:['driver_pay.read','investor_pay.read','board.read','board.write',...(readonly?[]:['driver_pay.edit','investor_pay.edit'])]},csrfToken:'fixture'},
    '/driver-pay':{weekStart:selected,revision:'fixture',drivers:omitStatement?[]:[driver]},'/investor-pay':{weekStart:selected,revision:'fixture',drivers:omitStatement?[]:[driver]},'/tasks/count':{count:0},
   };
   return route.fulfill({json:fixtures[path]??[]});
  });
  const navigate=async selected=>{await page.goto(`${base}/accounting/${investor?'investor':'driver'}-pay?weekStart=${selected}&${investor?'truckId':'driverId'}=statement`);await expect(page.getByRole('region',{name:'Transfer Test weekly loads'})).toBeVisible();};
  await navigate(week);
  // Right click is available on the load row; the button is an accessible alternative.
  await page.getByRole('link',{name:'WEEKEND',exact:true}).click({button:'right'});
  await expect(page.getByRole('dialog')).toBeVisible();
  await page.getByRole('radio',{name:/Keep settlement this week/}).check();
  await page.getByRole('button',{name:'Save transfer',exact:true}).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(page.getByLabel('Dispatcher: next week',{exact:true})).toBeVisible();
  await page.getByRole('button',{name:'Move WEEKEND to next week'}).click();
  await page.getByRole('radio',{name:/Move for everyone/}).check();
  reject=true;await page.getByRole('button',{name:'Save transfer',exact:true}).click();
  await expect(page.getByRole('alert').filter({hasText:'Another accountant'})).toBeVisible();
  await page.getByRole('button',{name:'Save transfer',exact:true}).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(page.getByRole('link',{name:'WEEKEND',exact:true})).toHaveCount(0);
  // The destination is the hidden current week. Restore from the original
  // week even though its financial load row is no longer present.
  const movedRow=page.getByRole('region',{name:'Transfer Test weekly loads'}).locator(`[data-pay-transferred-out="${source}"]`);
  await expect(movedRow).toBeVisible();
  await expect(movedRow.getByRole('rowheader')).toContainText('Sun');
  await expect(movedRow).toContainText('Moved to 10/26');
  expect((await movedRow.boundingBox()).height).toBeLessThanOrEqual(36);
  await expect(page.getByText('Transferred out',{exact:true})).toHaveCount(0);
  await page.getByRole('button',{name:'Return WEEKEND to original date'}).click();
  await page.getByRole('textbox',{name:'Reason',exact:true}).fill('Corrected transfer');
  reject=true;await page.getByRole('button',{name:'Return load',exact:true}).click();
  await expect(page.getByRole('alert').filter({hasText:'Another accountant'})).toBeVisible();
  await page.getByRole('button',{name:'Return load',exact:true}).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(movedRow).toHaveCount(0);
  await expect(page.getByRole('link',{name:'WEEKEND',exact:true})).toBeVisible();
  finalized=true;await navigate(week);
  await page.getByRole('button',{name:'Move WEEKEND to next week'}).click();
  await expect(page.getByRole('radio',{name:/Move for everyone/})).toBeDisabled();
  await page.getByRole('button',{name:'Save transfer',exact:true}).click();await expect(page.getByRole('dialog')).toHaveCount(0);
  readonly=true;mode='everyone';await navigate(week);await expect(page.getByRole('button',{name:'Move WEEKEND to next week'})).toHaveCount(0);
  await expect(movedRow).toBeVisible();
  await expect(page.getByRole('button',{name:'Return WEEKEND to original date'})).toHaveCount(0);
  sameDayLoad=true;await navigate(week);
  const sameDayHeader=page.getByRole('region',{name:'Transfer Test weekly loads'}).getByRole('rowheader',{name:/Sun/});
  await expect(sameDayHeader).toHaveAttribute('rowspan','2');
  await expect(movedRow).toBeVisible();
  await expect(movedRow.getByRole('rowheader')).toHaveCount(0);
  sameDayLoad=false;
  readonly=false;omitStatement=true;await page.reload();
  await expect(page.getByText('No statements match this view.')).toBeVisible();
  await expect(page.getByRole('button',{name:'Return WEEKEND to original date'})).toBeVisible();
  expect(requests.map(r=>r.mode)).toEqual(['dispatch_only','everyone','everyone','restore','restore','dispatch_only']);expect(errors).toEqual([]);
  await page.close();
 }
 const page=await browser.newPage({viewport:{width:2000,height:1100}});
 await page.clock.setFixedTime(new Date('2026-10-27T12:00:00Z'));
 await page.route('**/api/**',async route=>{
  const path=new URL(route.request().url()).pathname.slice(4);
  const transferred={driverId:'driver',date:next,sourceDate:source,sourceSlot:0,slot:1,version:1,transferVersion:1,creditedDispatcherId:'old',creditedDispatcherName:'Old Dispatcher',deleted:false,loadNumber:'WEEKEND',loadRecordId:9101,dayStatus:'',originalRate:'3000',driverRate:'2800',miles:'600',enteredOriginalRate:'',enteredMiles:'',duplicate:false};
  const fixtures={
   '/auth/session':{user:{id:'user',username:'Test',theme:'original',permissions:['board.read','board.write']},csrfToken:'fixture'},
   '/gross-board':{weekStart:next,drivers:[{id:'driver',fullName:'Transfer Driver',active:true,dispatcherId:'new',dispatcherName:'New Dispatcher',truckUnit:'T1',assignments:Array.from({length:7},(_,n)=>({date:`2026-10-${26+n}`,dispatcherId:'new',dispatcherName:'New Dispatcher',truckId:'truck',truckUnit:'T1'}))}],entries:[transferred,{...transferred,transferVersion:0,slot:0,loadNumber:'MONDAY',originalRate:'1000',driverRate:'900',miles:'100'}],balances:[]},'/tasks/count':{count:0},
  };return route.fulfill({json:fixtures[path]??[]});
 });
 await page.goto(`${base}/gross-board?weekStart=${next}`);
 await expect(page.getByRole('row',{name:'Old Dispatcher totals'}).getByText('$3,000.00',{exact:true})).toBeVisible();
 await expect(page.getByRole('row',{name:'New Dispatcher totals'}).getByText('$1,000.00',{exact:true})).toBeVisible();
 await expect(page.getByText(`From ${source}`,{exact:true})).toBeVisible();
 await page.close();
 // A single transferred Monday cell stays read-only while its day can add
 // another load. A view-only user still cannot see an add action.
 {
  const addPage=await browser.newPage({viewport:{width:1800,height:1100}});
  await addPage.clock.setFixedTime(new Date('2026-10-27T12:00:00Z'));
  let editable=true;
  const writes=[];
  const moved={driverId:'add-driver',date:next,sourceDate:source,sourceSlot:0,slot:0,version:2,transferVersion:1,creditedDispatcherId:'old',creditedDispatcherName:'Old Dispatcher',deleted:false,loadNumber:'WEEKEND',loadRecordId:9101,dayStatus:'',originalRate:'3000',driverRate:'2800',miles:'600',enteredOriginalRate:'',enteredMiles:'',duplicate:false};
  let entries=[moved];
  await addPage.route('**/api/**',async route=>{
   const request=route.request(),path=new URL(request.url()).pathname.slice(4);
   if(request.method()==='PUT'&&path==='/gross-board'){
    const changes=request.postDataJSON().entries;writes.push(changes);
    expect(changes.every(e=>!e.transferVersion&&e.slot===1)).toBe(true);
    const committed=changes.map(e=>({...e,version:e.version+1}));
    entries=[...entries.filter(e=>!committed.some(c=>c.date===e.date&&c.slot===e.slot)),...committed];
    return route.fulfill({json:committed});
   }
   const fixtures={
    '/auth/session':{user:{id:'user',username:'Test',theme:'original',permissions:['board.read',...(editable?['board.write']:[])]},csrfToken:'fixture'},
    '/gross-board':{weekStart:next,drivers:[{id:'add-driver',fullName:'Add Driver',active:true,dispatcherId:'old',dispatcherName:'Old Dispatcher',truckUnit:'T1'}],entries,balances:[]},'/tasks/count':{count:0},
   };return route.fulfill({json:fixtures[path]??[]});
  });
  await addPage.goto(`${base}/gross-board?weekStart=${next}`);
  await addPage.getByRole('button',{name:`Add Driver, ${next}, add another load`,exact:true}).click();
  const day=addPage.getByRole('dialog',{name:'Daily loads',exact:true});
  const field=(slot,name)=>day.locator(`[data-gross-slot="${slot}"]`).getByLabel(`Add Driver, ${next}, ${name}`,{exact:true});
  await expect(field(0,'load number or status')).toBeDisabled();
  await expect(field(0,'total gross')).toBeDisabled();
  await field(1,'load number or status').fill('ADDED-MONDAY');
  await field(1,'total gross').fill('250');
  await field(1,'miles').fill('125');
  await day.getByRole('button',{name:'Apply changes',exact:true}).click();
  await expect.poll(()=>entries.find(e=>e.slot===1)?.loadNumber).toBe('ADDED-MONDAY');
  expect(writes).toHaveLength(1);expect(entries.find(e=>e.slot===0)).toEqual(moved);
  await addPage.reload();
  await expect(addPage.getByRole('row',{name:'Old Dispatcher totals'}).getByText('$3,250.00',{exact:true})).toBeVisible();
  editable=false;await addPage.reload();
  await expect(addPage.getByRole('row',{name:'Old Dispatcher totals'})).toBeVisible();
  await expect(addPage.getByRole('button',{name:`Add Driver, ${next}, add another load`,exact:true})).toHaveCount(0);
  await addPage.close();
 }
 // Neither board location needs the hidden destination payroll statement.
 for (const original of [true,false]) {
  const boardPage=await browser.newPage({viewport:{width:1800,height:1100}});
  let active=true,mayReturn=false,blocked=true;
  const selected=original?week:next, calls=[];
  await boardPage.clock.setFixedTime(new Date('2026-10-27T12:00:00Z'));
  await boardPage.route('**/api/**',async route=>{
   const request=route.request(),path=new URL(request.url()).pathname.slice(4);
   if(path==='/driver-pay/transfer-load') {
    const input=request.postDataJSON(); calls.push(input);
    expect(input).toMatchObject({driverId:'15900000-0000-0000-0000-000000000001',date:source,slot:7,boardVersion:9,transferVersion:3,mode:'restore',reason:'Wrong weekend'});
    if(blocked){blocked=false;return route.fulfill({status:400,json:{error:'Reopen affected driver and investor statements in both weeks before moving the settlement'}});}
    active=false;return route.fulfill({status:204});
   }
   const moved={driverId:'15900000-0000-0000-0000-000000000001',date:original?source:next,sourceDate:source,sourceSlot:7,slot:original?7:1,version:9,transferVersion:3,transferOut:original,creditedDispatcherId:'old',creditedDispatcherName:'Old Dispatcher',deleted:false,loadNumber:'RESTORE-ME',loadRecordId:9101,dayStatus:'',originalRate:original?'':'3000',driverRate:original?'':'2800',miles:original?'':'600',enteredOriginalRate:'',enteredMiles:'',duplicate:original};
   const fixtures={
    '/auth/session':{user:{id:'user',username:'Test',theme:'original',permissions:['board.read','board.write',...(mayReturn?['driver_pay.read','driver_pay.edit']:[])]},csrfToken:'fixture'},
    '/gross-board':{weekStart:selected,drivers:[{id:'15900000-0000-0000-0000-000000000001',fullName:'Restore Driver',active:true,dispatcherId:'old',dispatcherName:'Old Dispatcher',truckUnit:'T1'}],entries:active?[moved]:original?[{...moved,transferVersion:0,transferOut:false,duplicate:false,originalRate:'3000',driverRate:'2800',miles:'600'}]:[],balances:[]},'/tasks/count':{count:0},
   };
   return route.fulfill({json:fixtures[path]??[]});
  });
  await boardPage.goto(`${base}/gross-board?weekStart=${selected}&driverId=15900000-0000-0000-0000-000000000001`);
  await expect(boardPage.getByRole('row',{name:'Old Dispatcher totals'})).toBeVisible();
  await expect(boardPage.getByRole('button',{name:'Return RESTORE-ME to original date'})).toHaveCount(0);
  mayReturn=true;await boardPage.reload();
  await boardPage.getByRole('button',{name:'Return RESTORE-ME to original date'}).click();
  await boardPage.getByRole('textbox',{name:'Reason',exact:true}).fill('Wrong weekend');
  await boardPage.getByRole('button',{name:'Return load',exact:true}).click();
  await expect(boardPage.getByRole('alert').filter({hasText:'Reopen affected'})).toBeVisible();
  expect(active).toBe(true);
  await boardPage.getByRole('button',{name:'Return load',exact:true}).click();
  await expect(boardPage.getByRole('dialog')).toHaveCount(0);
  await expect(boardPage.getByRole('button',{name:'Return RESTORE-ME to original date'})).toHaveCount(0);
  expect(active).toBe(false);expect(calls).toHaveLength(2);
  await boardPage.close();
 }
 console.log('Load week transfers passed: both pay pages, hidden-current-week return, both Gross Board locations, permissions, finalized and stale guards, original totals, and adding beside transferred loads.');
}finally{await browser.close();await new Promise(resolve=>server.close(resolve));}
