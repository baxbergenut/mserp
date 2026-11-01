// Preparation-aware UI, retained evidence, and uncertain financial response recovery.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname } from 'node:path';

const server=createServer(async(req,res)=>{
  try { const path=new URL(req.url,'http://localhost').pathname;
    const file=path==='/'?'index.html':extname(path)?path.slice(1):`${path.slice(1)}.html`;
    const body=await readFile(new URL(`../out/${file}`,import.meta.url));
    res.writeHead(200,{'Content-Type':{'.html':'text/html','.js':'application/javascript','.css':'text/css','.txt':'text/plain'}[extname(file)]??'application/octet-stream'});res.end(body);
  }catch{res.writeHead(404);res.end();}
});
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
const browser=await chromium.launch();
const base=`http://127.0.0.1:${server.address().port}`,week='2026-10-05';
const id='18200000-0000-4000-8000-000000000001';
const revision=n=>`18200000-0000-4000-8000-${String(n).padStart(12,'0')}`;
try {
  for(const kind of ['driver','investor']){
    let firstWeek='2000-01-03';
    const page=await browser.newPage({viewport:{width:1800,height:1000}}),errors=[];
    await page.addInitScript(() => {
      const NativeEventSource = window.EventSource;
      window.__payrollStreams = new Set();
      window.EventSource = class extends EventTarget {
        constructor(url, options) {
          super();
          if (!/\/(driver|investor)-pay\/events$/.test(url)) return new NativeEventSource(url, options);
          window.__payrollStreams.add(this);
        }
        close() { window.__payrollStreams.delete(this); }
      };
      window.__payrollNotify = () => { for (const stream of window.__payrollStreams) stream.dispatchEvent(new Event('changed')); };
    });
    page.on('pageerror',e=>errors.push(e.message));
    let state='preparing',generation=10,retries=0,commits=0,placements=0,retryRelease;
    const requests=[],receipts=new Map(),readWeeks=[];
    let driver={id,fullName:'Prepared Test',truckUnit:'P1',payType:'gross_percentage',payRate:'80',isOwnerOperator:true,
      dispatcherId:'',dispatcherName:'',fuelTotal:'0.00',tollTotal:'0.00',issues:[],statementRevision:revision(1),
      ...(kind==='investor'?{investorId:'owner',truckId:id}:{}),
      loads:[{date:week,slot:0,loadNumber:'PREPARED-1',loadRecordId:1,commentKey:'load1',boardVersion:1,originalRate:'1000.00',driverGross:'1000.00',totalMiles:'400.00',loadedMiles:'400.00',deadheadMiles:'0.00',fee:'800.00',issues:[]}],
      edits:{driverId:id,weekStart:week,version:0,notes:'',comments:{},adjustments:[],fuelOverride:null,tollOverride:null,expectedStatementRevision:revision(1)}};
    await page.route('**/api/**',async route=>{
      const request=route.request(),url=new URL(request.url()),path=url.pathname.slice(4).replace('/lookups/','/');
      if(path===`/${kind}-pay/availability`)return route.fulfill({json:{firstWeek,currentWeek:'2026-10-26'}});
      if(path==='/auth/session')return route.fulfill({json:{user:{id:'user',username:'Test',permissions:['driver_pay.read','driver_pay.edit','driver_pay.finalize','driver_pay.reopen','investor_pay.read','investor_pay.edit','investor_pay.finalize','investor_pay.reopen','drivers.read','trucks.read','investors.read','dispatchers.read','updaters.read'],theme:'solarized-dark'},csrfToken:'fixture'}});
      if(path===`/${kind}-pay/prepare`){retries++;state='ready';generation++;return route.fulfill({status:202,json:{state:'preparing'}});}
      if(path===`/${kind}-pay/statement-load`){
        const input=request.postDataJSON();
        expect(input.requestId).toBeTruthy();expect(input.expectedStatementRevision).toBe(driver.statementRevision);
        expect(input.action).toBe('add');expect(input.loadNumber).toBe('PREPARED-2');
        placements++;generation++;
        driver={...driver,statementRevision:revision(4),loads:[...driver.loads,{...driver.loads[0],slot:1,loadNumber:'PREPARED-2',commentKey:'load2'}]};
        driver.edits={...driver.edits,expectedStatementRevision:driver.statementRevision};
        return route.fulfill({json:{requestId:input.requestId,affectedStatements:[{kind,id,weekStart:week}]}});
      }
      if(path===`/${kind}-pay`&&request.method()==='PUT'){
        const input=request.postDataJSON();requests.push(input);
        if(receipts.has(input.requestId)){
          expect(input).toEqual(requests[0]);
          await new Promise(resolve=>{retryRelease=resolve;});
          return route.fulfill({json:receipts.get(input.requestId)});
        }
        expect(input.expectedStatementRevision).toBe(driver.statementRevision);
        state='ready';commits++;generation++;
        const version=revision(commits+1),edits={...input,version:commits,expectedStatementRevision:version};delete edits.requestId;
        driver={...driver,edits,statementRevision:version};
        const receipt={...edits,statementRevision:version,committedStatement:structuredClone(driver),affectedStatements:[{kind,id,weekStart:week}]};
        receipts.set(input.requestId,receipt);
        if(commits===1)return route.abort('failed'); // Response lost after commit.
        return route.fulfill({json:receipt});
      }
      if(path===`/${kind}-pay/${id}/cost-transactions`){expect(url.searchParams.get('revision')).toBe(driver.statementRevision);return route.fulfill({json:{sourceTotal:'0.00',carry:'0.00',due:'0.00',transactions:[],preparation:{state:'ready'}}});}
      if(path===`/${kind}-pay`||path===`/${kind}-pay/statements/${id}`){
        readWeeks.push(url.searchParams.get('weekStart'));
        if(path.includes('/statements/')){
          if(url.searchParams.has('revision'))expect(url.searchParams.get('revision')).toBe(driver.statementRevision);
          else expect(placements).toBe(1); // Explicit latest detail after committed source action.
        }
        const row=structuredClone(driver),summary=url.searchParams.get('summary')==='1',expanded=url.searchParams.get('detailIds')?.split(',').includes(id);
        if(summary&&!expanded){row.summary={loads:driver.loads.length,missingFees:0,review:0,fee:(800*driver.loads.length).toFixed(2),payable:(800*driver.loads.length).toFixed(2)};row.loads=[];row.edits={driverId:id,weekStart:week,version:driver.edits.version,notes:'',comments:{},adjustments:[],expectedStatementRevision:driver.statementRevision};}
        return route.fulfill({json:{weekStart:week,revision:`inputs:${generation}`,drivers:state==='preparing'?[]:[row],preparation:{state,generation:revision(generation),calculatorVersion:'fixture'}}});
      }
      return route.fulfill({json:path==='/tasks/count'?{count:0}:[]});
    });
    await page.goto(`${base}/accounting/${kind}-pay?weekStart=${week}`);
    await expect(page.getByText('Preparing this week…',{exact:true})).toBeVisible();
    state='ready';generation++;
    const expand=page.getByRole('button',{name:kind==='investor'?'Prepared Test P1':'Prepared Test',exact:true});
    await expect(expand).toBeVisible();await expand.click();
    await expect(page.getByRole('region',{name:'Prepared Test weekly loads'})).toBeVisible();
    state='updating';
    expect(await page.evaluate(() => window.__payrollStreams.size)).toBe(1);
    await page.evaluate(() => window.__payrollNotify());
    await expect(page.getByText('Updating statements. The last complete values are shown.',{exact:true})).toBeVisible({timeout:800});
    await expect(page.getByRole('button',{name:'Prepared Test, weekly comment',exact:true})).toBeDisabled();
    await expect(page.getByRole('region',{name:'Prepared Test weekly loads'})).toContainText('PREPARED-1');
    state='failed';await page.evaluate(() => window.__payrollNotify());
    await page.getByRole('button',{name:'Retry preparation',exact:true}).click();
    await expect.poll(()=>retries).toBe(1);
    const comment=page.getByRole('button',{name:'Prepared Test, weekly comment',exact:true});
    await expect(comment).toBeEnabled();await comment.click();
    await page.getByLabel('Comment',{exact:true}).fill('First committed comment');
    state='updating';generation++;
    await page.evaluate(() => window.__payrollNotify());
    await page.waitForTimeout(150);
    await expect(page.getByLabel('Comment',{exact:true})).toHaveValue('First committed comment');
    await expect(page.getByLabel('Comment',{exact:true})).toBeFocused();
    await page.getByRole('button',{name:'Save comment',exact:true}).click();
    await expect(page.getByRole('button',{name:'Retry save',exact:true})).toBeVisible();expect(commits,(await page.getByRole('alert').allTextContents()).join(' ')).toBe(1);
    await page.getByRole('button',{name:'Retry save',exact:true}).click();await expect.poll(()=>!!retryRelease).toBe(true);
    // A later edit must survive the receipt for the earlier, uncertain request.
    await comment.click();await page.getByLabel('Comment',{exact:true}).fill('Typed while recovering');await page.getByRole('button',{name:'Save comment',exact:true}).click();
    retryRelease();
    await expect.poll(()=>commits).toBe(2);
    await expect(page.getByRole('button',{name:'Retry save',exact:true})).toHaveCount(0);
    expect(requests).toHaveLength(3);expect(requests[0].requestId).toBe(requests[1].requestId);expect(requests[2].requestId).not.toBe(requests[0].requestId);
    expect(requests[2].expectedStatementRevision).toBe(revision(2));expect(driver.edits.notes).toBe('Typed while recovering');
    const add=page.getByRole('button',{name:'Add to statement',exact:true});
    await page.getByLabel('Prepared Test, extra load number',{exact:true}).fill('PREPARED-2');
    await expect(add).toBeEnabled();await add.click();
    await expect.poll(()=>placements).toBe(1);
    await expect(page.getByRole('region',{name:'Prepared Test weekly loads'})).toContainText('PREPARED-2');
    await expect(page.getByLabel('Prepared Test, extra load number',{exact:true})).toHaveValue('');
    await comment.click();await expect(page.getByLabel('Comment',{exact:true})).toHaveValue('Typed while recovering');
    // Future URLs and restored selections cannot request an unopened pay week.
    await expect(page.getByRole('button',{name:'Reopen week',exact:true})).toHaveCount(0);
    await expect(page.getByText(/settlements finalized/)).toHaveCount(0);
    firstWeek='2026-10-19';
    readWeeks.length=0;
    await page.goto(base+'/accounting/'+kind+'-pay?weekStart=2026-10-05');
    await expect(page.getByRole('button',{name:'Previous week',exact:true})).toBeDisabled();
    await expect.poll(()=>readWeeks.length).toBeGreaterThan(0);
    expect(readWeeks.every(w=>w>='2026-10-19')).toBe(true);
    await page.clock.setFixedTime(new Date('2026-11-01T12:00:00Z'));
    readWeeks.length=0;
    await page.goto(base+'/accounting/'+kind+'-pay?weekStart=2026-11-09');
    await expect(page.getByRole('button',{name:'Next week',exact:true})).toBeDisabled();
    await expect.poll(()=>readWeeks.length).toBeGreaterThan(0);
    expect(readWeeks.every(w=>w<='2026-10-26')).toBe(true);
    await page.goto(base+'/accounting/'+kind+'-pay');
    await expect(page.getByRole('button',{name:'Next week',exact:true})).toBeDisabled();
    // Monday midnight in New York opens the next week without a new deployment.
    await page.clock.setFixedTime(new Date('2026-11-02T05:01:00Z'));
    await page.evaluate(()=>window.dispatchEvent(new Event('focus')));
    await expect(page.getByRole('button',{name:'Next week',exact:true})).toBeEnabled();
    await page.getByRole('button',{name:'Next week',exact:true}).click();
    await expect.poll(()=>readWeeks.includes('2026-11-02')).toBe(true);
    await expect(page.getByRole('button',{name:'Next week',exact:true})).toBeDisabled();
    expect(errors).toEqual([]);await page.close();
  }
  console.log('Prepared payroll: preparation, stale/failed values, retry, pinned details, lost response replay, typing during recovery and committed placement receipts passed on both pages.');
}finally{await browser.close();await new Promise(resolve=>server.close(resolve));}
