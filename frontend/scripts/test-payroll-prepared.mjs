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
    let state='preparing',generation=10,retries=0,commits=0,placements=0,retryRelease,readFailures=1;
    let includeIndependent=false,independentState='ready',offPagePayable;
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
      if(path===`/${kind}-pay`||path===`/${kind}-pay/statements/${id}`||path===`/${kind}-pay/statements/${revision(900)}`){
        if(path===`/${kind}-pay`&&readFailures>0){readFailures--;return route.fulfill({status:502,body:'Bad Gateway'});}
        readWeeks.push(url.searchParams.get('weekStart'));
        if(path.includes('/statements/')){
          if(url.searchParams.has('revision'))expect(url.searchParams.get('revision')).toBe(path.endsWith(revision(900))?revision(901):driver.statementRevision);
          else expect(placements).toBe(1); // Explicit latest detail after committed source action.
        }
        const row=structuredClone(driver),summary=url.searchParams.get('summary')==='1',expanded=url.searchParams.get('detailIds')?.split(',').includes(id);
        if(summary&&!expanded){row.summary={loads:driver.loads.length,missingFees:0,review:0,fee:(800*driver.loads.length).toFixed(2),payable:(800*driver.loads.length).toFixed(2)};row.loads=[];row.edits={driverId:id,weekStart:week,version:driver.edits.version,notes:'',comments:{},adjustments:[],expectedStatementRevision:driver.statementRevision};}
        const independent={...structuredClone(driver),id:revision(900),fullName:'Independent Test',truckUnit:'P2',truckId:revision(900),statementRevision:revision(901),preparationState:independentState,edits:{...driver.edits,driverId:revision(900),expectedStatementRevision:revision(901)}};
        const rows=path.endsWith(revision(900))?[independent]:[row];
        if(includeIndependent&&!path.includes('/statements/'))rows.push(independent);
        return route.fulfill({json:{weekStart:week,revision:`inputs:${generation}`,drivers:state==='preparing'?[]:rows,preparation:{state,generation:revision(generation),calculatorVersion:'fixture'},
          ...(offPagePayable && {pagination:{page:1,pageSize:25,total:30,totalPages:2,dispatchers:[],finalized:0,statements:30,loads:30,review:0,fee:'24000.00',payable:offPagePayable}})}});
      }
      return route.fulfill({json:path==='/tasks/count'?{count:0}:[]});
    });
    await page.goto(`${base}/accounting/${kind}-pay?weekStart=${week}`);
    await expect(page.getByRole('alert').filter({hasText:'Retrying automatically'})).toBeVisible();
    await expect(page.getByText('Preparing this week…',{exact:true})).toBeVisible();
    await expect(page.getByRole('alert').filter({hasText:'Retrying automatically'})).toHaveCount(0);
    state='ready';generation++;
    await page.evaluate(() => window.__payrollNotify());
    const expand=page.getByRole('button',{name:kind==='investor'?'Prepared Test P1':'Prepared Test',exact:true});
    await expect(expand).toBeVisible();await expand.click();
    await expect(page.getByRole('region',{name:'Prepared Test weekly loads'})).toBeVisible();
    // Expense balances change without publishing a new statement/generation.
    driver.expenseRevision='live-1';
    driver.edits.expenseDeductions=[{expenseId:'live-expense',name:'Live balance',expenseDate:week,amount:'50.00',total:'100.00',available:'100.00',openingBalance:'100.00',remaining:'50.00',version:1,saved:false,mode:'fixed',liveBalance:true}];
    await page.evaluate(() => window.__payrollNotify());
    await expect(page.getByLabel('Prepared Test, expense Live balance, deduction',{exact:true})).toHaveValue('-50.00');
    driver.expenseRevision='live-2';driver.edits.expenseDeductions[0].amount='25.00';
    await page.evaluate(() => window.__payrollNotify());
    await expect(page.getByLabel('Prepared Test, expense Live balance, deduction',{exact:true})).toHaveValue('-25.00');
    // A balance outside this page still changes the all-filtered total.
    offPagePayable='15000.00';await page.evaluate(() => window.__payrollNotify());
    await expect(page.getByText('$15,000.00',{exact:true})).toBeVisible();
    offPagePayable='14900.00';await page.evaluate(() => window.__payrollNotify());
    await expect(page.getByText('$14,900.00',{exact:true})).toBeVisible();
    offPagePayable=undefined;driver.expenseRevision='live-3';driver.edits.expenseDeductions=[];
    await page.evaluate(() => window.__payrollNotify());
    await expect(page.getByLabel('Prepared Test, expense Live balance, deduction',{exact:true})).toHaveCount(0);
    await page.waitForTimeout(200);
    const idleReads=readWeeks.length;
    await page.waitForTimeout(1600);
    expect(readWeeks.length,'idle pages must not poll statement freshness').toBe(idleReads);
    await page.evaluate(() => { for(const stream of window.__payrollStreams)stream.dispatchEvent(new Event('open')); });
    await expect.poll(()=>readWeeks.length).toBeGreaterThan(idleReads);
    state='updating';
    expect(await page.evaluate(() => window.__payrollStreams.size)).toBe(1);
    await page.evaluate(() => window.__payrollNotify());
    await expect(page.getByRole('button',{name:'Prepared Test, weekly comment',exact:true})).toBeDisabled();
    // Readiness can change while the weekly generation/state stay the same.
    // Only the server-proven independent statement becomes editable.
    driver.preparationState='ready'; await page.evaluate(() => window.__payrollNotify());
    await expect(page.getByRole('button',{name:'Prepared Test, weekly comment',exact:true})).toBeEnabled();
    await expect(page.getByRole('button',{name:kind==='investor'?'Finalize Prepared Test P1':'Finalize Prepared Test',exact:true})).toBeEnabled();
    driver.preparationState='updating'; await page.evaluate(() => window.__payrollNotify());
    await expect(page.getByRole('button',{name:'Prepared Test, weekly comment',exact:true})).toBeDisabled();
    includeIndependent=true;await page.evaluate(() => window.__payrollNotify());
    await page.getByRole('button',{name:kind==='investor'?'Independent Test P2':'Independent Test',exact:true}).click();
    const independentComment=page.getByRole('button',{name:'Independent Test, weekly comment',exact:true});
    await expect(independentComment).toBeEnabled();
    await expect(page.getByText('Updating…',{exact:true})).toHaveCount(1);
    await expect(page.getByRole('button',{name:kind==='investor'?'Finalize Independent Test P2':'Finalize Independent Test',exact:true})).toBeEnabled();
    // A genuinely shared balance can affect both; unchanged generation must
    // still apply each row's new readiness without resetting retained details.
    independentState='updating';await page.evaluate(() => window.__payrollNotify());
    await expect(independentComment).toBeDisabled();await expect(page.getByText('Updating…',{exact:true})).toHaveCount(2);
    independentState='ready';driver.preparationState='ready';await page.evaluate(() => window.__payrollNotify());
    await expect(independentComment).toBeEnabled();await expect(page.getByText('Updating…',{exact:true})).toHaveCount(0);
    includeIndependent=false;driver.preparationState='updating';await page.evaluate(() => window.__payrollNotify());
    await expect(independentComment).toHaveCount(0);
    delete driver.preparationState;
    await expect(page.getByText('Updating statements. The last complete values are shown.',{exact:true})).toHaveCount(0);
    await expect(page.getByText('Updating…',{exact:true})).toHaveCount(1);
    await expect(page.getByRole('region',{name:'Prepared Test weekly loads'})).toContainText('PREPARED-1');
    readFailures=1;
    await page.evaluate(() => window.__payrollNotify());
    await expect(page.getByRole('alert').filter({hasText:'Retrying automatically'})).toBeVisible();
    state='ready';generation++;
    // Recover even when the commit notification is lost, retaining the table.
    await expect(page.getByRole('button',{name:'Prepared Test, weekly comment',exact:true})).toBeEnabled();
    await expect(page.getByRole('alert').filter({hasText:'Retrying automatically'})).toHaveCount(0);
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
    expect(readWeeks.every(w=>w<='2026-10-19')).toBe(true);
    await page.goto(base+'/accounting/'+kind+'-pay');
    await expect(page.getByRole('button',{name:'Next week',exact:true})).toBeDisabled();
    // Monday midnight in New York opens the next week without a new deployment.
    await page.clock.setFixedTime(new Date('2026-11-02T05:01:00Z'));
    await page.evaluate(()=>window.dispatchEvent(new Event('focus')));
    await expect(page.getByRole('button',{name:'Next week',exact:true})).toBeEnabled();
    await page.getByRole('button',{name:'Next week',exact:true}).click();
    await expect.poll(()=>readWeeks.includes('2026-10-26')).toBe(true);
    await expect(page.getByRole('button',{name:'Next week',exact:true})).toBeDisabled();
    expect(errors).toEqual([]);await page.close();
  }
  console.log('Prepared payroll: preparation, stale/failed values, retry, pinned details, lost response replay, typing during recovery and committed placement receipts passed on both pages.');
}finally{await browser.close();await new Promise(resolve=>server.close(resolve));}
