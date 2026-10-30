// Fixture-only browser test: no database, production, or provider writes.
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
const week = '2026-10-19';
const trips = [
  { id: 101, number: 'T-01', driverName: 'Split First', truckUnit: 'A', originalRate: '500', miles: '40', loadedMiles: '0', emptyMiles: '40', startDate: week, endDate: week },
  { id: 102, number: 'T-02', driverName: 'Split Second', truckUnit: 'B', originalRate: '1313', miles: '515', loadedMiles: '475', emptyMiles: '40', startDate: week, endDate: week },
];
const drivers = trips.map((t, i) => ({ id: `driver-${i}`, fullName: t.driverName, active: true, truckId: `truck-${i}`, truckUnit: t.truckUnit, dispatcherId: 'dispatcher', dispatcherName: 'Dispatch' }));
let entries = drivers.map((d, i) => ({ driverId: d.id, date: week, slot: 0, deleted: false, dayStatus: '', loadNumber: 'SPLIT-1', loadRecordId: 8101,
  loadTripId: null, split: true, tripReview: true, trips, originalRate: '', miles: '', driverRate: i ? '1000' : '200', enteredOriginalRate: '1813', enteredMiles: '555', systemOriginalRate: '', systemMiles: '', version: 1, duplicate: false }));

entries = entries.map((e,i)=>({...e,loadTripId:trips[i].id,enteredOriginalRate:trips[i].originalRate,driverRate:i?'1313':'500'}));
trips[0].originalRate='700';
let readOnly=false,saves=0,rejectNextSave=false,reviewVersion=0;
const mismatchReason='Trip gross totals $2,013.00 but the load total is $1,813.00. Review the original gross split independently of DataTruck trip amounts.';
entries=entries.map(e=>({...e,tripReviewReason:mismatchReason}));
try {
 const page=await browser.newPage({viewport:{width:1500,height:1000}});
 await page.clock.setFixedTime(new Date('2026-10-20T12:00:00Z'));
 const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/api/**',async route=>{
  const req=route.request(),path=new URL(req.url()).pathname.slice(4);
  if(path==='/gross-board/loads/8101/split')return route.fulfill({json:{loadId:8101,loadNumber:'SPLIT-1',version:reviewVersion,sourceHash:'source-v1',reason:'',allocations:[],entries:entries.map((entry,i)=>({driverName:drivers[i].fullName,entry})),total:'1813',miles:'555',trips,reviewReason:reviewVersion?'':mismatchReason}});
  if(req.method()==='PUT' && path==='/gross-board') {
   if(rejectNextSave){rejectNextSave=false;return route.fulfill({status:409,json:{error:'The split or DataTruck source changed. Reopen the split editor and review the current values'}})}
   const submitted=req.postDataJSON().entries[0],review=submitted.splitReview;
   expect(review.allocations.map(a=>a.originalRate)).toEqual(['500','1313']);
   expect(review.entries.map(r=>r.entry.driverRate)).toEqual(['700','1313']);
   entries=review.entries.map((row,i)=>({...row.entry,splitReview:undefined,version:row.entry.version+1,tripReview:false,tripReviewReason:'',originalRate:review.allocations[i].originalRate,enteredOriginalRate:review.allocations[i].originalRate,systemOriginalRate:review.allocations[i].originalRate,miles:trips[i].miles,enteredMiles:trips[i].miles,systemMiles:trips[i].miles,acceptSystemValues:false}));
   saves++;reviewVersion++;return route.fulfill({json:entries});
  }
  const fixtures={
   '/auth/session':{user:{id:'split-test',username:'Split Test',theme:'original',permissions:['board.read','loads.read','drivers.read','trucks.read','dispatchers.read',...(readOnly?[]:['board.write'])]},csrfToken:'fixture'},
   '/gross-board':{weekStart:week,entries,drivers,balances:[]},'/sidebar-counts':{},'/tasks/count':{count:0},
   '/loads':{items:[],total:0,page:1,pageSize:25,totalPages:1,statuses:[]}
  };return route.fulfill({json:fixtures[path]??[]});
 });
 await page.goto(base+'/gross-board?weekStart='+week);
 const open=async()=>{await page.getByRole('button',{name:'Split load SPLIT-1 for Split First',exact:true}).click();const d=page.getByRole('dialog',{name:'Review load SPLIT-1',exact:true});await expect(d.getByLabel('Original gross for T-01')).toBeVisible();return d};
 let dialog=await open();
 await expect(dialog).toContainText('$2,013.00');await expect(dialog).toContainText('$1,813.00');
 await expect(dialog.getByLabel('Original gross for T-01')).toHaveValue('500');
 await expect(dialog.getByLabel('Driver gross for Split First, '+week+', 0')).toHaveValue('500');
 await dialog.getByLabel('Driver gross for Split First, '+week+', 0').fill('700');
 await dialog.getByLabel('Split review note').fill('Confirmed against weekly sheet');
 await dialog.getByLabel('Original gross for T-01').fill('700');
 await expect(dialog.getByRole('button',{name:'Apply split',exact:true})).toBeDisabled();
 await expect(dialog).toContainText('Remaining -$200.00');
 await dialog.getByLabel('Original gross for T-01').fill('500');
 await expect(dialog.getByRole('button',{name:'Apply split',exact:true})).toBeEnabled();
 await page.screenshot({path:join(tmpdir(),'clustie-reviewed-split.png'),fullPage:true});
 await dialog.getByRole('button',{name:'Apply split',exact:true}).click();
 await expect.poll(()=>saves).toBe(1);
 await expect(page.getByLabel('Split First, '+week+', total gross',{exact:true})).toHaveValue('$500.00');
 await expect(page.getByLabel('Split First, '+week+', driver rate',{exact:true})).toHaveValue('$700.00');
 await expect(page.getByLabel('Split Second, '+week+', total gross',{exact:true})).toHaveValue('$1,313.00');
 await page.reload();await expect(page.getByLabel('Split First, '+week+', driver rate',{exact:true})).toHaveValue('$700.00');
 // A source revision after opening rejects the entire save and retains its draft.
 dialog=await open();await dialog.getByLabel('Split review note').fill('Rechecked sheet');rejectNextSave=true;
 await dialog.getByRole('button',{name:'Apply split',exact:true}).click();
 await expect(page.getByRole('alert').filter({hasText:'The split or DataTruck source changed'})).toBeVisible();expect(saves).toBe(1);
 page.once('dialog',d=>d.accept());await page.getByRole('button',{name:'Discard unsaved changes',exact:true}).click();
 await expect(page.getByRole('status').filter({hasText:/^Saved$/})).toBeVisible();
 readOnly=true;await page.reload();dialog=await open();
 await expect(dialog.getByLabel('Original gross for T-01')).toBeDisabled();
 await expect(dialog.getByLabel('Driver gross for Split First, '+week+', 0')).toBeDisabled();
 await expect(dialog.getByRole('button',{name:'Apply split',exact:true})).toHaveCount(0);
 expect(saves).toBe(1);expect(errors).toEqual([]);
 console.log('Reviewed split UI passed: separate revenue/driver gross, balanced totals, atomic companion edits, persistence, stale-source rejection/discard, read-only access.');
} finally {await browser.close();await new Promise(resolve=>server.close(resolve))}
