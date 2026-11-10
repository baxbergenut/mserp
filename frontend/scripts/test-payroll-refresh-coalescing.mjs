// Controlled-latency browser regression. Counts reads separately from mutations;
// timings here are fixture UI/network timings, not production server latency.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, resolve } from 'node:path';
const root=process.env.PAYROLL_REFRESH_OUT || resolve(import.meta.dirname,'../out');
const baseline=process.env.PAYROLL_REFRESH_BASELINE==='1';
const server=createServer(async(req,res)=>{try{const path=new URL(req.url,'http://localhost').pathname; const file=extname(path)?path.slice(1):`${path.slice(1)}.html`;res.setHeader('Content-Type',({'.html':'text/html','.js':'application/javascript','.css':'text/css'})[extname(file)]||'application/octet-stream');res.end(await readFile(resolve(root,file)));}catch{res.writeHead(404);res.end();}});
await new Promise(r=>server.listen(0,'127.0.0.1',r));
const browser=await chromium.launch();
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const metrics=[];
try{for(const kind of ['driver','investor']){
 const page=await browser.newPage({viewport:{width:1600,height:1000}});
 await page.addInitScript(()=>{window.__paySources=[];window.EventSource=class extends EventTarget{constructor(url){super();this.url=url;this.closed=false;window.__paySources.push(this);setTimeout(()=>{if(!this.closed)this.dispatchEvent(new Event('open'));},10);}close(){this.closed=true;}};window.__payChanged=()=>window.__paySources.filter(s=>!s.closed).forEach(s=>s.dispatchEvent(new Event('changed')));});
 const errors=[]; page.on('pageerror',e=>errors.push(e.message));
 const counts={page:0,detail:0,outgoing:0,save:0,finalize:0,reopen:0,placement:0};let readsInFlight=0,maxReads=0,holdRead=false,releaseRead,failReads=0;const readStarts=[];let maxReadsBeforeNavigation=0;
 const week='2026-10-26', id='18200000-0000-4000-8000-000000000001';
 const row={id,fullName:'Alpha',truckUnit:'101',...(kind==='investor'?{investorId:'owner-1'}:{}),payType:'cpm',payRate:'0.75',fuelTotal:'0.00',tollTotal:'0.00',issues:[],loads:[],statementRevision:'revision-1',expenseRevision:'expenses-1',preparationState:'ready',edits:{driverId:id,weekStart:week,version:1,notes:'',comments:{},adjustments:[],fuelOverride:null,tollOverride:null}};
 const snapshot=(selectedWeek=week)=>({weekStart:selectedWeek,revision:'inputs:fixture',drivers:[{...structuredClone(row),fullName:selectedWeek===week?'Alpha':'Next week',edits:{...structuredClone(row.edits),weekStart:selectedWeek}}],preparation:{state:'ready',generation:'generation-1'}});
 await page.route('**/api/**',async route=>{const req=route.request(),path=new URL(req.url()).pathname.slice(4);
  if(path==='/auth/session')return route.fulfill({json:{user:{id:'user',username:'Test',permissions:[`${kind}_pay.read`,`${kind}_pay.edit`,`${kind}_pay.finalize`,`${kind}_pay.reopen`],theme:'default'},csrfToken:'fixture'}});
  if(path.endsWith('/availability'))return route.fulfill({json:{firstWeek:week,currentWeek:week}});
  if(path.endsWith('/outgoing-transfers')){counts.outgoing++;return route.fulfill({json:{transfers:[]}});}
  if(path===`/${kind}-pay`&&req.method()==='PUT'){counts.save++;await sleep(120);row.edits={...req.postDataJSON(),version:row.edits.version+1};row.statementRevision=`revision-${row.edits.version}`;await page.evaluate(()=>window.__payChanged());return route.fulfill({json:{...row.edits,statementRevision:row.statementRevision,committedStatement:structuredClone(row)}});}
  if(path===`/${kind}-pay/finalize`||path===`/${kind}-pay/reopen`){const reopen=path.endsWith('/reopen');counts[reopen?'reopen':'finalize']++;await sleep(120);if(reopen)delete row.settlement;else row.settlement={finalized:true,version:1,finalizedBy:'Reviewer',finalizedAt:new Date().toISOString()};row.statementRevision+='-settled';await page.evaluate(()=>window.__payChanged());return route.fulfill({json:{...snapshot(),receipt:true}});}
  if(path===`/${kind}-pay/statement-load`){counts.placement++;const input=req.postDataJSON();expect(input.expectedStatementRevision).toBe(row.statementRevision);await sleep(120);const load={date:week,slot:0,loadNumber:'EXTRA-1',loadRecordId:8801,commentKey:'extra:1',pickupDate:week,pickupLocation:'First, OH',deliveryLocation:'Last, PA',originalRate:'1000',driverGross:'1000',totalMiles:'100',loadedMiles:'90',deadheadMiles:'10',fee:'75',issues:[],payrollSupplement:true,payrollPlacementVersion:1,status:'dispatched',notReady:true};row.loads=input.action==='remove'?[]:[load];row.removedLoads=input.action==='remove'?[load]:[];row.statementRevision+='-placement';await page.evaluate(()=>window.__payChanged());return route.fulfill({json:{requestId:input.requestId,affectedStatements:[{kind,id,weekStart:week}]}});}
  if(path===`/${kind}-pay`){counts.page++;readStarts.push(Date.now());readsInFlight++;maxReads=Math.max(maxReads,readsInFlight);if(failReads>0){failReads--;await sleep(80);readsInFlight--;return route.fulfill({status:503,json:{error:"Fixture transient read failure"}});}const result=snapshot(new URL(req.url()).searchParams.get("weekStart")||week);if(holdRead){holdRead=false;await new Promise(r=>releaseRead=r);}await sleep(80);readsInFlight--;return route.fulfill({json:result}).catch(()=>{});}
  if(path.startsWith(`/${kind}-pay/statements/`)){counts.detail++;await sleep(80);return route.fulfill({json:snapshot()});}
  return route.fulfill({json:path==='/tasks/count'?{count:0}:[]});
 });
 await page.goto(`http://127.0.0.1:${server.address().port}/accounting/${kind}-pay?weekStart=${week}`);
 await expect(page.getByRole('button',{name:/^Finalize Alpha/})).toBeEnabled();await sleep(450);
 const initial={...counts};const sourceCount=()=>page.evaluate(()=>window.__paySources.length);
 const initialSources=await sourceCount();
 const start=Date.now();await page.getByRole('button',{name:/^Finalize Alpha/}).click();await expect(page.getByRole('row').filter({hasText:'Alpha'}).locator('td').last()).toHaveText('Reviewer');const confirmedMs=Date.now()-start;await sleep(450);
 const finalized={...counts}, finalizeReads=counts.page-initial.page,finalizeTransfers=counts.outgoing-initial.outgoing;
 await page.getByRole('row').filter({hasText:'Alpha'}).locator('td').last().click({button:'right'});await page.getByRole('menuitem',{name:kind==='investor'?'Reopen truck':'Reopen driver',exact:true}).click();await page.getByRole('button',{name:'Reopen settlement',exact:true}).click();await expect(page.getByRole('button',{name:/^Finalize Alpha/})).toBeEnabled();await sleep(450);
 const reopened={...counts};
 const expand=page.getByRole('button',{name:kind==='investor'?'Alpha 101':'Alpha',exact:true});if(await expand.getAttribute('aria-expanded')==='false')await expand.click();
 await page.getByRole('button',{name:'Alpha, weekly comment',exact:true}).click();await page.getByLabel('Comment',{exact:true}).fill('Keep draft during external change');
 const beforeDraft=counts.page;await page.evaluate(()=>{window.__payChanged();window.__payChanged();window.__payChanged();});await sleep(200);expect(counts.page).toBe(beforeDraft);await expect(page.getByLabel('Comment',{exact:true})).toHaveValue('Keep draft during external change');
 await page.getByRole('button',{name:'Save comment',exact:true}).click();await expect.poll(()=>counts.save).toBe(1);await sleep(800);
 const saved={...counts};
 await page.getByLabel('Alpha, extra load number').fill('EXTRA-1');await page.getByRole('button',{name:'Add to statement',exact:true}).click();await expect(page.getByRole('button',{name:'Remove EXTRA-1 from statement',exact:true})).toBeEnabled();await sleep(450);const added={...counts};
 await page.getByRole('button',{name:'Remove EXTRA-1 from statement',exact:true}).click();await expect(page.getByRole('button',{name:'Add EXTRA-1 back to statement',exact:true})).toBeEnabled();await sleep(450);const removed={...counts};
 if(!baseline){expect(added.page-saved.page).toBe(1);expect(removed.page-added.page).toBe(1);expect(added.detail-saved.detail).toBe(1);expect(removed.detail-added.detail).toBe(1);}

 if(!baseline){expect(finalizeReads).toBe(1);expect(finalizeTransfers).toBe(1);expect(reopened.page-finalized.page).toBe(1);expect(saved.page-reopened.page).toBe(1);expect(await sourceCount()).toBe(initialSources);expect(maxReads).toBe(1);
  // An event during a held snapshot queues one trailing snapshot, not a second concurrent read.
  const beforeBurst=counts.page;holdRead=true;await page.evaluate(()=>window.__payChanged());await expect.poll(()=>counts.page).toBe(beforeBurst+1);row.statementRevision+='-external';await page.evaluate(()=>{window.__payChanged();window.__payChanged();});await sleep(100);expect(counts.page).toBe(beforeBurst+1);releaseRead();await expect.poll(()=>counts.page).toBe(beforeBurst+2);await sleep(200);expect(maxReads).toBe(1);
  // An already-running read must never replace a newly opened editor.
  const beforeEdit=counts.page;holdRead=true;await page.evaluate(()=>window.__payChanged());await expect.poll(()=>counts.page).toBe(beforeEdit+1);
  await page.getByRole('button',{name:'Alpha, weekly comment',exact:true}).click();await page.getByLabel('Comment',{exact:true}).fill('Do not overwrite this draft');releaseRead();await sleep(250);
  await expect(page.getByLabel('Comment',{exact:true})).toHaveValue('Do not overwrite this draft');expect(counts.page).toBe(beforeEdit+1);
  await page.getByRole('button',{name:'Cancel',exact:true}).click();await expect.poll(()=>counts.page).toBe(beforeEdit+2);await sleep(200);
  // A mutation can finish before an older GET. Its canonical receipt must not
  // regress even briefly while the queued fresh read is deliberately held.
  const beforeLate=counts.page;holdRead=true;await page.evaluate(()=>window.__payChanged());await expect.poll(()=>counts.page).toBe(beforeLate+1);const releaseOld=releaseRead;
  await page.getByRole('button',{name:/^Finalize Alpha/}).click();await expect(page.getByRole('row').filter({hasText:'Alpha'}).locator('td').last()).toHaveText('Reviewer');
  holdRead=true;releaseOld();await expect.poll(()=>counts.page).toBe(beforeLate+2);await sleep(100);
  await expect(page.getByRole('row').filter({hasText:'Alpha'}).locator('td').last()).toHaveText('Reviewer');releaseRead();await sleep(200);
  // Failed reads retry with bounded exponential delay, and successful recovery
  // clears the warning without reissuing any financial command.
  const beforeFailure=counts.page, writesBefore=counts.save+counts.finalize+counts.reopen+counts.placement;
  failReads=2;await page.evaluate(()=>window.__payChanged());await expect(page.getByRole('alert').filter({hasText:'Fixture transient read failure'})).toBeVisible();
  await expect.poll(()=>counts.page,{timeout:7000}).toBe(beforeFailure+3);await expect(page.getByRole('alert').filter({hasText:'Fixture transient read failure'})).toHaveCount(0);
  expect(readStarts[beforeFailure+1]-readStarts[beforeFailure]).toBeGreaterThanOrEqual(900);
  expect(readStarts[beforeFailure+2]-readStarts[beforeFailure+1]).toBeGreaterThanOrEqual(1900);
  expect(counts.save+counts.finalize+counts.reopen+counts.placement).toBe(writesBefore);await sleep(200);expect(counts.page).toBe(beforeFailure+3);
  // A late old-week response must not replace the newly selected week. The
  // old scope is cancelled even if the intercepted server response arrives later.
  maxReadsBeforeNavigation=maxReads;const beforeNavigation=counts.page;holdRead=true;await page.evaluate(()=>window.__payChanged());await expect.poll(()=>counts.page).toBe(beforeNavigation+1);const releasePreviousWeek=releaseRead;
  await page.getByLabel('Payroll week',{exact:true}).selectOption('2026-11-02');
  await expect(page.getByRole('button',{name:kind==='investor'?'Next week 101':'Next week',exact:true})).toBeVisible();releasePreviousWeek();await sleep(250);
  await expect(page.getByRole('button',{name:kind==='investor'?'Next week 101':'Next week',exact:true})).toBeVisible();await expect(page.getByRole('button',{name:kind==='investor'?'Alpha 101':'Alpha',exact:true})).toHaveCount(0);
 }
 metrics.push({kind,baseline,initialPageReads:initial.page,finalizePageReads:finalizeReads,finalizeOutgoingReads:finalizeTransfers,reopenPageReads:reopened.page-finalized.page,savePageReads:saved.page-reopened.page,addPageReads:added.page-saved.page,removePageReads:removed.page-added.page,addDetailReads:added.detail-saved.detail,removeDetailReads:removed.detail-added.detail,eventSources:await sourceCount(),maxConcurrentPageReads:maxReadsBeforeNavigation||maxReads,fixtureFinalizeConfirmedMs:confirmedMs});expect(errors).toEqual([]);await page.close();
}console.log(JSON.stringify(metrics,null,2));}finally{await browser.close();server.closeAllConnections();await new Promise(r=>server.close(r));}
