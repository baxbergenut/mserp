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
  let mode='',version=0,readonly=false,finalized=false,reject=false;
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
   const fixtures={
    '/auth/session':{user:{id:'user',username:'Test',theme:'original',permissions:['driver_pay.read','investor_pay.read','board.read','board.write',...(readonly?[]:['driver_pay.edit','investor_pay.edit'])]},csrfToken:'fixture'},
    '/driver-pay':{weekStart:selected,revision:'fixture',drivers:[driver]},'/investor-pay':{weekStart:selected,revision:'fixture',drivers:[driver]},'/tasks/count':{count:0},
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
  await navigate(next);await expect(page.getByRole('link',{name:'WEEKEND',exact:true})).toBeVisible();
  await page.getByRole('button',{name:'Move WEEKEND to next week'}).click();
  await page.getByRole('radio',{name:'Restore original placement for everyone'}).check();
  await page.getByRole('textbox',{name:'Reason',exact:true}).fill('Corrected transfer');
  await page.getByRole('button',{name:'Save transfer',exact:true}).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  finalized=true;await navigate(week);
  await page.getByRole('button',{name:'Move WEEKEND to next week'}).click();
  await expect(page.getByRole('radio',{name:/Move for everyone/})).toBeDisabled();
  await page.getByRole('button',{name:'Save transfer',exact:true}).click();await expect(page.getByRole('dialog')).toHaveCount(0);
  readonly=true;await navigate(week);await expect(page.getByRole('button',{name:'Move WEEKEND to next week'})).toHaveCount(0);
  expect(requests.map(r=>r.mode)).toEqual(['dispatch_only','everyone','everyone','restore','dispatch_only']);expect(errors).toEqual([]);
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
 console.log('Load week transfers passed: both pay pages, both modes, original dispatcher totals, stale errors, restore, finalized and read-only controls.');
}finally{await browser.close();await new Promise(resolve=>server.close(resolve));}
