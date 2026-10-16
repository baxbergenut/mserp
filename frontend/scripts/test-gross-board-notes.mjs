// Fixture-only browser coverage: no live API, database, or vendor writes.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';

const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = path === '/' ? 'index.html' : extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] ?? 'application/octet-stream' });
    res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const browser = await chromium.launch();
const base = `http://127.0.0.1:${server.address().port}`;

const week='2026-10-12', driverId='00000000-0000-0000-0000-000000000001';
const driver={id:driverId,fullName:'Note Driver',truckId:'',truckUnit:'',dispatcherId:'',dispatcherName:'Unassigned',active:true};
const entry=(date,slot=0)=>({driverId,date,slot,deleted:false,loadNumber:'LOAD-'+slot,loadRecordId:null,dayStatus:'',note:'',originalRate:'400',driverRate:'350',miles:'90',enteredOriginalRate:'400',enteredMiles:'90',version:1,duplicate:false});
let entries=[entry(week),entry('2026-10-13'),entry('2026-10-13',1)];
let readOnly=false, rejectSave=false;
let writes=0;
const errors=[];
try {
 const page=await browser.newPage({viewport:{width:1600,height:1000}});
 page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/api/**',async route=>{
  const req=route.request(), url=new URL(req.url()),path=url.pathname.replace('/lookups/','/').slice(4);
  if(path.startsWith('/gross-board/presence')) return route.fulfill(req.method()==='GET'?{json:[]}:{status:204});
  if(req.method()==='PUT') {
   writes++;
   if(rejectSave) return route.fulfill({status:409,json:{error:'another dispatcher changed this load'}});
   const committed=req.postDataJSON().entries.map(e=>({...e,version:e.version+1}));
   await new Promise(resolve=>setTimeout(resolve,120));
   entries=entries.map(e=>committed.find(c=>c.date===e.date&&c.slot===e.slot)??e);
   return route.fulfill({json:committed});
  }
  const fixtures={
   '/auth/session':{user:{id:'notes-user',username:'Notes',theme:'original',permissions:['board.read',...(readOnly?[]:['board.write'])]},csrfToken:'fixture'},
   '/gross-board':{weekStart:week,drivers:[driver],entries,balances:[]},
   '/gross-board/loads':[], '/tasks/count':{count:0}, '/sidebar-counts':{}
  };
  return route.fulfill({json:fixtures[path]??[]});
 });
 await page.goto(base+'/gross-board?weekStart='+week+'&driverId='+driverId+'&date='+week+'&slot=0&loadNumber=LOAD-0');
 const cell=page.getByLabel('Note Driver, '+week+', load number or status',{exact:true});
 await expect(cell).toHaveValue('LOAD-0');
 await cell.click({button:'right'});
 await page.getByRole('menuitem',{name:'Add note',exact:true}).click();
 const note=page.getByRole('textbox',{name:'Note for LOAD-0, load 1',exact:true});
 await expect(note).toBeFocused();
 await note.fill('Call receiver\nUse gate 2');
 await note.press('End'); await note.pressSequentially(' after 8am',{delay:20});
 await expect.poll(()=>entries[0].note).toBe('Call receiver\nUse gate 2 after 8am');
 expect(entries[0].originalRate).toBe('400');
 await page.getByRole('dialog',{name:'Load note',exact:true}).press('Escape');
 await expect(cell.locator('xpath=ancestor::td').getByRole('img',{name:'Has load note'})).toBeVisible();
 await page.reload(); await expect(cell).toHaveValue('LOAD-0');
 await page.mouse.move(0,0); await cell.hover();
 await expect(note).toHaveValue('Call receiver\nUse gate 2 after 8am');
 await note.fill(''); await expect(note).toBeVisible();
 await note.pressSequentially('Hover edited'); await expect.poll(()=>entries[0].note).toBe('Hover edited');
 await page.screenshot({path:join(tmpdir(),'mserp-gross-board-notes.png'),fullPage:true});
 await page.getByRole('dialog',{name:'Load note',exact:true}).press('Escape');
 // Keyboard context menu and note deletion.
 await cell.click(); await page.keyboard.press('Shift+F10');
 await expect(page.getByRole('menuitem',{name:'Edit note',exact:true})).toBeFocused();
 await page.keyboard.press('ArrowDown'); await page.keyboard.press('Enter');
 await expect.poll(()=>entries[0].note).toBe('');
 await expect(cell.locator('xpath=ancestor::td').getByRole('img',{name:'Has load note'})).toHaveCount(0);
 // Grouped loads receive separate notes directly from the board.
 const group=page.getByRole('button',{name:'Note Driver, 2026-10-13, load numbers and details',exact:true});
 await group.click({button:'right'});
 await page.getByRole('menuitem',{name:'Add note · LOAD-1 (Load 2)',exact:true}).click();
 await page.getByRole('textbox',{name:'Note for LOAD-1, load 2'}).fill('Second load only');
 await expect.poll(()=>entries.find(e=>e.date==='2026-10-13'&&e.slot===1).note).toBe('Second load only');
 expect(entries.find(e=>e.date==='2026-10-13'&&e.slot===0).note).toBe('');
 await page.getByRole('dialog',{name:'Load note',exact:true}).press('Escape');
 await group.click();
 const daily=page.getByRole('dialog',{name:'Daily loads',exact:true});
 await daily.getByLabel('Note Driver, 2026-10-13, load number or status',{exact:true}).first().click({button:'right'});
 await page.getByRole('menuitem',{name:'Add note',exact:true}).click();
 await note.fill('First load in daily editor');
 await page.getByRole('dialog',{name:'Load note',exact:true}).press('Escape');
 await daily.getByRole('button',{name:'Apply changes',exact:true}).click();
 await expect.poll(()=>entries.find(e=>e.date==='2026-10-13'&&e.slot===0).note).toBe('First load in daily editor');
 // Failed autosaves keep the note draft and can be retried.
 rejectSave=true;
 await cell.click({button:'right'}); await page.getByRole('menuitem',{name:'Add note',exact:true}).click();
 await note.fill('Keep draft after conflict');
 await expect(page.getByRole('button',{name:'Retry save',exact:true})).toBeVisible();
 await expect(note).toHaveValue('Keep draft after conflict');
 rejectSave=false;
 await page.getByRole('dialog',{name:'Load note',exact:true}).press('Escape');
 await page.getByRole('button',{name:'Retry save',exact:true}).click();
 await expect.poll(()=>entries[0].note).toBe('Keep draft after conflict');
 // Replacing the load clears the old note.
 await cell.dblclick(); await cell.fill('REPLACED'); await page.keyboard.press('Tab');
 await expect.poll(()=>entries[0].note).toBe('');
 // Read-only users can see notes; editors remain non-writable.
 readOnly=true; await page.reload();
 await group.hover();
 const second=page.getByRole('textbox',{name:'Note for LOAD-1, load 2'});
 await expect(second).toHaveValue('Second load only');
 await expect(second).toHaveAttribute('readonly','');
 const before=writes;
 await group.click({button:'right'}); await expect(page.getByRole('menu',{name:'Load notes'})).toHaveCount(0);
 expect(writes).toBe(before);
 await page.setViewportSize({width:390,height:844});
 const popup=page.getByRole('dialog',{name:'Load note',exact:true});
 await expect(popup).toBeVisible();
 await expect.poll(async()=>{const box=await popup.boundingBox();return !!box&&box.x>=0&&box.x+box.width<=390;}).toBe(true);
 expect(errors).toEqual([]);
 console.log('Gross Board notes passed: add/edit/delete, hover, queued saves, reload, keyboard, grouped loads, daily editor, conflicts, replacement, permissions and narrow layout.');
} finally {await browser.close();await new Promise(resolve=>server.close(resolve));}
