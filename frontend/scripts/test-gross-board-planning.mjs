// Synthetic fixture only: no live data, database or provider writes.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';
const server=createServer(async(req,res)=>{try{const path=new URL(req.url,'http://localhost').pathname;const file=path==='/'?'index.html':extname(path)?path.slice(1):path.slice(1)+'.html';const body=await readFile(new URL('../out/'+file,import.meta.url));res.writeHead(200,{'Content-Type':{'.html':'text/html','.js':'application/javascript','.css':'text/css','.txt':'text/plain'}[extname(file)]??'application/octet-stream'});res.end(body);}catch{res.writeHead(404);res.end();}});
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
const browser=await chromium.launch();
const base='http://127.0.0.1:'+server.address().port,week='2026-11-02',driverId='00000000-0000-0000-0000-000000000001';
const driver={id:driverId,fullName:'Planning Driver',truckId:'',truckUnit:'42',dispatcherId:'',dispatcherName:'Unassigned',active:true};
const entry=(date,slot=0)=>({driverId,date,slot,deleted:false,loadNumber:'LIVE-'+slot,loadRecordId:100+slot,dayStatus:'',note:'Load-specific note',originalRate:'400',driverRate:'350',miles:'90',enteredOriginalRate:'400',enteredMiles:'90',systemOriginalRate:'400',systemDriverRate:'350',systemMiles:'90',version:1,duplicate:false});
let entries=[entry(week)],planning=[{driverId:'other-driver',date:week,kind:'destination',text:'Foreign driver destination',version:7},{driverId,date:'2026-11-03',kind:'destination',text:'Other day destination',version:4}],readOnly=false,fail=false,hold=null;
// Full-day fixtures include protected non-load content and cleared planning rows.
const blank=(date)=>({...entry(date),loadNumber:'',loadRecordId:null,note:'',originalRate:'',driverRate:'',miles:'',enteredOriginalRate:'',enteredMiles:'',systemOriginalRate:'',systemDriverRate:'',systemMiles:''});
entries.push({...blank('2026-11-04'),dayStatus:'HOME'}, {...blank('2026-11-05'),driverRate:'7'});
planning.push(
 {driverId,date:'2026-11-04',kind:'note',text:'Keep fixed status',version:1},
 {driverId,date:'2026-11-05',kind:'note',text:'Keep entered amount',version:1},
 {driverId,date:'2026-11-06',kind:'note',text:'Available after 3 PM',version:1},
 {driverId,date:'2026-11-07',kind:'note',text:'',version:2},
 {driverId,date:'2026-11-08',kind:'note',text:'LongStatus'.repeat(100),version:1},
 {driverId,date:'2026-11-08',kind:'destination',text:'Dallas, TX',version:1}
);
let loadsWrites=0,planningWrites=0;const errors=[];
const snapshot=(selected=week)=>({weekStart:selected,drivers:[driver],entries:entries.filter(e=>e.date>=selected&&e.date<new Date(Date.parse(selected)+7*86400000).toISOString().slice(0,10)),planningEntries:planning.filter(e=>e.date>=selected&&e.date<new Date(Date.parse(selected)+7*86400000).toISOString().slice(0,10)),balances:[]});
const same=(a,b)=>a.driverId===b.driverId&&a.date===b.date&&a.kind===b.kind;
try{
const page=await browser.newPage({viewport:{width:1600,height:1000}});page.on('pageerror',e=>errors.push(e.message));
await page.addInitScript(()=>{const sources=[];window.emitGross=board=>sources.forEach(source=>source.listeners.grossBoard?.({data:JSON.stringify(board)}));window.EventSource=class{constructor(){this.listeners={};sources.push(this);setTimeout(()=>this.onopen?.(),0)}addEventListener(name,fn){this.listeners[name]=fn}close(){const index=sources.indexOf(this);if(index>=0)sources.splice(index,1)}};});
await page.route('**/api/**',async route=>{
 const req=route.request(),url=new URL(req.url()),path=url.pathname.replace('/lookups/','/').slice(4);
 if(path.startsWith('/gross-board/presence'))return route.fulfill(req.method()==='GET'?{json:[]}:{status:204});
 if(path==='/gross-board/planning-entries'&&req.method()==='PUT'){
  planningWrites++;const requested=req.postDataJSON().entries;
  if(fail)return route.fulfill({status:503,json:{error:'Temporary planning save failure'}});
  const committed=[];for(const sent of requested){const prior=planning.find(e=>same(e,sent));if(prior&&prior.version!==sent.version&&prior.text!==sent.text.trim())return route.fulfill({status:409,json:{error:'Planning entry changed elsewhere'}});const saved=prior?.text===sent.text.trim()?prior:{...sent,text:sent.text.trim(),version:(prior?.version??0)+1};planning=planning.filter(e=>!same(e,sent));planning.push(saved);committed.push(saved);}
  if(hold)await hold.promise;
  return route.fulfill({json:committed});
 }
 if(path==='/gross-board'&&req.method()==='PUT'){
  loadsWrites++;const committed=req.postDataJSON().entries.map(e=>({...e,version:(entries.find(p=>p.date===e.date&&p.slot===e.slot)?.version??0)+1}));
  for(const e of committed){entries=entries.filter(p=>p.date!==e.date||p.slot!==e.slot);entries.push(e);}return route.fulfill({json:{entries:committed,issues:[]}});
 }
 if(path==='/gross-board/loads')return route.fulfill({json:url.searchParams.get('search')==='MATCH-10'?[{id:110,loadNumber:'MATCH-10',originalRate:'500',driverRate:'450',miles:'100'}]:[]});
 const fixtures={'/auth/session':{user:{id:'planning-user',username:'Planner',theme:'original',permissions:['board.read',...(readOnly?[]:['board.write'])]},csrfToken:'fixture'},'/gross-board':snapshot(url.searchParams.get('weekStart')??week),'/tasks/count':{count:0},'/sidebar-counts':{}};return route.fulfill({json:fixtures[path]??[]});
});
await page.goto(base+'/gross-board?weekStart='+week+'&driverId='+driverId+'&date='+week+'&slot=0&loadNumber=LIVE-0');
const dialog=page.getByRole('dialog',{name:'Day entry',exact:true});
const add=(date=week)=>page.getByRole('button',{name:'Add entry for Planning Driver, '+date,exact:true});
const noteButton=()=>page.getByRole('button',{name:'Status / note for Planning Driver, '+week,exact:true});
const text=()=>dialog.getByRole('textbox',{name:'Status / note',exact:true});
const openKind=async(kind,date=week)=>{await add(date).click();await dialog.getByRole('button',{name:kind,exact:true}).click();};
const close=async()=>dialog.getByRole('button',{name:'Close',exact:true}).first().click();
const save=async()=>{await dialog.getByRole('button',{name:'Save',exact:true}).click();await expect(dialog.getByRole('status')).toHaveText('Saved');};
const push=async()=>page.evaluate(board=>window.emitGross(board),snapshot());
const cell=(date)=>page.locator('td[data-gross-driver="'+driverId+'"][data-gross-date="'+date+'"]');
const fullDay=(date)=>cell(date).locator('[data-gross-planning-full-day]');
const standaloneDates=['2026-11-03','2026-11-06','2026-11-08'];
for(const date of standaloneDates){
 await expect(fullDay(date)).toHaveCount(1);
 await expect(cell(date).getByRole('combobox')).toHaveCount(0);
 await expect(cell(date).getByRole('textbox')).toHaveCount(0);
 await expect(cell(date).getByRole('button',{name:/remove load|add another load/})).toHaveCount(0);
 await expect.poll(()=>cell(date).evaluate(td=>{
  const planning=td.querySelector('[data-gross-planning-full-day]');
  return Math.abs(planning.getBoundingClientRect().height-(td.getBoundingClientRect().height-1))<2;
 })).toBe(true);
}
await expect(cell('2026-11-08').getByRole('button',{name:/Status . note for|Planned destination for/})).toHaveCount(2);
await expect(fullDay('2026-11-04')).toHaveCount(0);
await expect(cell('2026-11-04').getByRole('combobox')).toHaveValue('HOME');
await expect(cell('2026-11-04').getByLabel('Planning Driver, 2026-11-04, driver rate',{exact:true})).toHaveCount(0);
await expect(fullDay('2026-11-05')).toHaveCount(0);
await expect(cell('2026-11-05').getByLabel('Planning Driver, 2026-11-05, driver rate',{exact:true})).toHaveValue('$7.00');
await expect(fullDay('2026-11-07')).toHaveCount(0);
await expect(cell('2026-11-07').getByRole('combobox')).toHaveValue('');
// Spreadsheet navigation selects the whole planning cell; Enter and F2 open it.
for(const key of ['Enter','F2']){
 await cell(week).getByRole('combobox').click();await page.keyboard.press('ArrowRight');await expect(cell('2026-11-03')).toBeFocused();await page.keyboard.press(key);
 await expect(dialog.getByRole('textbox',{name:'Planned destination',exact:true})).toHaveValue('Other day destination');await close();
}
// Keyboard editing stays protected; queued load updates apply after closing.
const standaloneNote=cell('2026-11-06').getByRole('button',{name:/Status . note for/});
await standaloneNote.focus();await standaloneNote.press('Enter');await text().fill('Unsaved full-day draft');
entries.push(entry('2026-11-06'));await push();await expect(fullDay('2026-11-06')).toHaveCount(1);await expect(text()).toBeFocused();await expect(text()).toHaveValue('Unsaved full-day draft');await close();await expect(fullDay('2026-11-06')).toHaveCount(0);await standaloneNote.click();await expect(text()).toHaveValue('Unsaved full-day draft');await dialog.getByRole('button',{name:'Cancel',exact:true}).click();
entries=entries.map(e=>e.date==='2026-11-06'?{...blank(e.date),version:2}:e);await push();await expect(fullDay('2026-11-06')).toHaveCount(1);
// Creating and clearing the only planning entry switches layout without load writes.
const planningOnlyWrites=loadsWrites;
await openKind('Status / note','2026-11-07');await text().pressSequentially('Draft only');await expect(text()).toBeFocused();await expect(text()).toHaveValue('Draft only');await close();
await expect(fullDay('2026-11-07')).toHaveCount(1);await page.reload();await expect(fullDay('2026-11-07')).toHaveCount(1);
await cell('2026-11-07').getByRole('button',{name:/Status . note for/}).click();await save();await dialog.getByRole('button',{name:'Clear text',exact:true}).click();await save();await expect(text()).toBeVisible();await close();await expect(fullDay('2026-11-07')).toHaveCount(0);expect(loadsWrites).toBe(planningOnlyWrites);
// Long text stays in its day column on desktop and a horizontally scrolled narrow board.
for(const width of [1600,900,390]){
 await page.setViewportSize({width,height:900});await fullDay('2026-11-08').scrollIntoViewIfNeeded();
 expect(await fullDay('2026-11-08').evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true);
 // At phone widths the existing frozen identity columns cover most of the board;
 // verify overflow here and the usable editor viewport below.
 if(width>=900)await cell('2026-11-08').screenshot({path:join(tmpdir(),'mserp-gross-planning-full-day-'+width+'.png')});
}
await page.setViewportSize({width:1600,height:1000});
// Choosing an entry type cannot alter existing load identity, amounts or per-load note.
const loadBaseline=JSON.stringify(entries);
await openKind('Status / note');await text().fill('Available after 3 PM');await save();await close();
// Recovery GET contains other drivers/dates, but must never import their field identity.
await noteButton().click();await text().fill('Recovery draft');fail=true;await dialog.getByRole('button',{name:'Save',exact:true}).click();await expect(dialog.getByRole('alert')).toContainText('Temporary planning save failure');await close();
await expect(page.getByRole('button',{name:'Planned destination for Planning Driver, '+week,exact:true})).toHaveCount(0);fail=false;
await openKind('Planned destination');await expect(dialog.getByRole('textbox',{name:'Planned destination',exact:true})).toHaveValue('');await dialog.getByRole('textbox',{name:'Planned destination',exact:true}).fill('Dallas, TX');await save();await close();
expect(planning.find(e=>e.driverId==='other-driver').text).toBe('Foreign driver destination');expect(planning.find(e=>e.date==='2026-11-03').text).toBe('Other day destination');
await noteButton().click();await dialog.getByRole('button',{name:'Cancel',exact:true}).click();
await expect(noteButton()).toContainText('Available after 3 PM');await expect(fullDay(week)).toHaveCount(0);await expect(cell(week).getByLabel('Planning Driver, '+week+', driver rate',{exact:true})).toHaveValue('$350.00');expect(loadsWrites).toBe(0);expect(JSON.stringify(entries)).toBe(loadBaseline);
await page.mouse.move(0,0);await page.waitForTimeout(350);await page.screenshot({path:join(tmpdir(),'mserp-gross-planning-cell.png'),fullPage:true});
await add().click();await page.screenshot({path:join(tmpdir(),'mserp-gross-planning-chooser.png'),fullPage:true});await close();
// Cancel, close/reload recovery and independent kind drafts.
await noteButton().click();await text().fill('Discard me');await dialog.getByRole('button',{name:'Cancel',exact:true}).click();await noteButton().click();await expect(text()).toHaveValue('Available after 3 PM');
await text().fill('Recover me');await close();await page.reload();await noteButton().click();await expect(text()).toHaveValue('Recover me');await dialog.getByRole('button',{name:'Cancel',exact:true}).click();
// In-flight acknowledgement preserves newer typing, including row unmount/re-mount.
await noteButton().click();await text().fill('First save');hold={};hold.promise=new Promise(resolve=>hold.resolve=resolve);const before=planningWrites;await dialog.getByRole('button',{name:'Save',exact:true}).click();await expect.poll(()=>planningWrites).toBe(before+1);
await close();const search=page.getByPlaceholder(/Search drivers or load numbers/);await search.fill('No matching driver');await expect(add()).toHaveCount(0);await search.fill('');
// Force one-to-many rendering before opening the replacement instance.
entries.push(entry(week,1));await push();await noteButton().click();await text().fill('Newer during save');await expect(text()).toHaveValue('Newer during save');hold.resolve();hold=null;await expect(dialog.getByRole('status')).toHaveText('Earlier text saved. Your newer edits still need Save.');
await close();await page.reload();await noteButton().click();await expect(text()).toHaveValue('Newer during save');await save();await close();
// Same-field live conflict requires explicit replace and never discards the draft.
await noteButton().click();await text().fill('My revision');planning=planning.map(e=>e.kind==='note'&&e.driverId===driverId&&e.date===week?{...e,text:'Other dispatcher',version:e.version+1}:e);await push();await expect(dialog.getByRole('alert')).toContainText('Other dispatcher');await expect(text()).toHaveValue('My revision');await dialog.getByRole('button',{name:'Replace with my draft',exact:true}).click();await expect(dialog.getByRole('status')).toHaveText('Saved');await close();
// Errors, retry, clear tombstone and recreate.
await noteButton().click();await text().fill('Retry retained');fail=true;await dialog.getByRole('button',{name:'Save',exact:true}).click();await expect(dialog.getByRole('alert')).toContainText('Temporary planning save failure');await expect(text()).toHaveValue('Retry retained');fail=false;await dialog.getByRole('button',{name:'Retry save',exact:true}).click();await expect(dialog.getByRole('status')).toHaveText('Saved');
await dialog.getByRole('button',{name:'Clear text',exact:true}).click();await save();await close();await expect(noteButton()).toHaveCount(0);const clearVersion=planning.find(e=>e.kind==='note'&&e.driverId===driverId&&e.date===week).version;
await openKind('Status / note');await text().fill('Recreated');await save();expect(planning.find(e=>e.kind==='note'&&e.driverId===driverId&&e.date===week).version).toBe(clearVersion+1);await close();
// The 1000-character limit counts Unicode code points, and narrow editor stays inside viewport.
await noteButton().click();await text().fill('🚚'.repeat(1001));await expect(dialog.getByRole('button',{name:'Save',exact:true})).toBeDisabled();await text().fill('🚚'.repeat(1000));await expect(dialog.getByRole('button',{name:'Save',exact:true})).toBeEnabled();await dialog.getByRole('button',{name:'Cancel',exact:true}).click();
await noteButton().click();await page.setViewportSize({width:390,height:844});await expect.poll(async()=>{const box=await dialog.boundingBox();return !!box&&box.x>=0&&box.x+box.width<=390;}).toBe(true);await page.screenshot({path:join(tmpdir(),'mserp-gross-planning-narrow.png'),fullPage:true});await close();await page.setViewportSize({width:1600,height:1000});
// New Load mode reuses the empty slot and Apply blocks unconfirmed text.
const day='2026-11-03';await openKind('Load',day);const daily=page.getByRole('dialog',{name:'Daily loads',exact:true});const loadInput=daily.getByLabel('Planning Driver, '+day+', load number or status',{exact:true});await expect(loadInput).toHaveCount(1);await loadInput.fill('family emergency');const oldWrites=loadsWrites;await daily.getByRole('button',{name:'Apply changes',exact:true}).click();await expect(daily.getByRole('alert')).toContainText('Add as manual load');expect(loadsWrites).toBe(oldWrites);await expect(loadInput).toBeFocused();
await loadInput.press('Escape');await expect(loadInput).toHaveValue('');await loadInput.fill('UNKNOWN-84X0165');await page.getByRole('option',{name:'Add as manual load: UNKNOWN-84X0165',exact:true}).click();await daily.getByRole('button',{name:'Apply changes',exact:true}).click();await expect.poll(()=>entries.find(e=>e.date===day)?.loadNumber).toBe('UNKNOWN-84X0165');
// Arbitrary typed text never autosaves; explicit manual HOME remains a load.
const manual=page.getByLabel('Planning Driver, '+day+', load number or status',{exact:true});await manual.dblclick();await manual.fill('HOME');await page.getByRole('option',{name:'Add as manual load: HOME',exact:true}).click();await expect.poll(()=>entries.find(e=>e.date===day)?.loadNumber).toBe('HOME');expect(entries.find(e=>e.date===day).dayStatus).toBe('');
// Empty query + Enter clears the number, not the first status shortcut.
await manual.dblclick();await manual.fill('');await manual.press('Enter');await expect.poll(()=>entries.find(e=>e.date===day)?.loadNumber).toBe('');expect(entries.find(e=>e.date===day).dayStatus).toBe('');
await expect(fullDay(day)).toHaveCount(1);await openKind('Load',day);await loadInput.fill('HOME');await page.getByRole('option',{name:'HOME',exact:true}).click();await daily.getByRole('button',{name:'Apply changes',exact:true}).click();await expect.poll(()=>entries.find(e=>e.date===day)?.dayStatus).toBe('HOME');await expect(fullDay(day)).toHaveCount(0);
await manual.dblclick();await manual.fill('MATCH-10');await page.getByRole('option',{name:/MATCH-10.*LOAD/}).click();await expect.poll(()=>entries.find(e=>e.date===day)?.loadRecordId).toBe(110);
// Week navigation retains a draft under its original date without posting it elsewhere.
await noteButton().click();await text().fill('Week-scoped draft');await close();await page.getByRole('button',{name:/Next week/i}).click();await expect(add('2026-11-09')).toBeVisible();await page.getByRole('button',{name:/Previous week/i}).click();await noteButton().click();await expect(text()).toHaveValue('Week-scoped draft');await dialog.getByRole('button',{name:'Cancel',exact:true}).click();
// Read-only users can inspect both fields and have no save/add affordances.
readOnly=true;await page.reload();await expect(add()).toHaveCount(0);await expect(fullDay('2026-11-06')).toHaveCount(1);await standaloneNote.click();await expect(text()).toHaveAttribute('readonly','');await close();await noteButton().click();await expect(text()).toHaveAttribute('readonly','');await expect(dialog.getByRole('button',{name:'Save',exact:true})).toHaveCount(0);expect(errors).toEqual([]);
console.log('Planning browser passed: full-day note/destination/both, mixed and protected-content layouts, full-cell height, draft creation/clear/reload and live load transitions, long text at desktop/mobile widths, typed modes, load isolation, manual/status/search choices, guarded Apply, saved labels, conflicts/retry, clear/recreate, Unicode, read-only, drafts across reload/navigation/filter/group changes and narrow layout.');
console.log('Screenshots: '+join(tmpdir(),'mserp-gross-planning-cell.png')+', '+join(tmpdir(),'mserp-gross-planning-chooser.png')+', '+join(tmpdir(),'mserp-gross-planning-narrow.png'));
}finally{if(hold)hold.resolve();await browser.close();await new Promise(resolve=>server.close(resolve));}
