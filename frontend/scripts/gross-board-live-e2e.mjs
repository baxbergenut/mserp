import { expect } from '@playwright/test';
import { randomBytes } from 'node:crypto';
import { join } from 'node:path';

export async function runGrossBoardLiveE2E({page,base,sql,schema,temp,setNetworkLatency}) {
  const date = new Date(new Intl.DateTimeFormat('en-CA',{timeZone:'America/New_York',year:'numeric',month:'2-digit',day:'2-digit'}).format(new Date())+'T12:00:00Z');
  date.setUTCDate(date.getUTCDate()-(date.getUTCDay()+6)%7);
  const week=date.toISOString().slice(0,10);
  const add=days=>{const d=new Date(week+'T12:00:00Z');d.setUTCDate(d.getUTCDate()+days);return d.toISOString().slice(0,10)};
  const id='e5700000-0000-0000-0000-000000000001', other='e5700000-0000-0000-0000-000000000002';
  sql(`SET search_path TO ${schema},public;
    INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES
    ('${id}','Gross Live A','gross live a','cpm',0),('${other}','Gross Live B','gross live b','cpm',0);
    INSERT INTO gross_board_entries(driver_id,service_date,load_number,original_rate,driver_rate,miles,entered_original_rate,entered_miles) VALUES
    ('${id}','${week}','LIVE-A',1000,800,500,1000,500),('${other}','${week}','LIVE-B',2000,1800,600,2000,600),
    ('${id}','${add(1)}','MULTI-A',500,400,200,500,200);
    INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,original_rate,driver_rate,miles,entered_original_rate,entered_miles) VALUES
    ('${id}','${add(1)}',1,'MULTI-B',700,600,300,700,300);`);
  const auth=await(await page.request.get(base+'/api/auth/session')).json(),headers={'X-CSRF-Token':auth.csrfToken};
  const board=async()=>await(await page.request.get(`${base}/api/gross-board?weekStart=${week}`)).json();
  const entry=async(driver,day=week,slot=0)=>(await board()).entries.find(e=>e.driverId===driver&&e.date===day&&e.slot===slot);
  const field=(p,name='original rate',day=week,driver='Gross Live A')=>p.getByLabel(`${driver}, ${day}, ${name}`,{exact:true});
  const numeric=async locator=>Number((await locator.inputValue()).replace(/[$,]/g,''));
  const fill=async(input,value)=>{await input.focus();await input.fill(value)};
  const saved=()=>expect(page.getByText('All changes saved',{exact:true})).toBeVisible();
  await page.goto(base+'/gross-board');
  await expect(page.getByText('Live',{exact:true})).toBeVisible();
  expect((await page.request.post(base+'/api/settings/roles',{headers,data:{name:'Gross live viewer',permissions:['board.read']}})).status()).toBe(204);
  const access=await(await page.request.get(base+'/api/settings/access')).json();
  const role=access.roles.find(r=>r.name==='Gross live viewer');
  const password=randomBytes(20).toString('hex');
  expect((await page.request.post(base+'/api/settings/users',{headers,data:{username:'Gross Viewer',email:'gross-live@example.com',password,roleId:role.id,active:true}})).status()).toBe(200);
  const context=await page.context().browser().newContext({viewport:{width:1800,height:1050}});
  const viewer=await context.newPage();
  const errors=[];viewer.on('pageerror',e=>errors.push(e.message));
  try {
    await page.setViewportSize({width:1800,height:1050});
    await viewer.goto(base+'/login');
    await viewer.getByLabel('Email or existing username',{exact:true}).fill('gross-live@example.com');
    await viewer.getByLabel('Password',{exact:true}).fill(password);
    await viewer.getByRole('button',{name:'Sign in',exact:true}).click();
    await expect(viewer.getByRole('heading',{name:'Gross Board',exact:true})).toBeVisible();
    await expect(viewer.getByText('Live',{exact:true})).toBeVisible();
    await expect(field(viewer)).toBeDisabled();
    await viewer.locator(`[data-gross-driver="${id}"][data-gross-field="truck"]`).click();
    await expect(page.locator('[data-gross-presence="Gross Viewer"]')).toHaveAttribute('aria-label',/truck/);
    const viewerAuth=await(await viewer.request.get(base+'/api/auth/session')).json();
    expect((await viewer.request.put(base+'/api/gross-board',{headers:{'X-CSRF-Token':viewerAuth.csrfToken},data:{weekStart:week,entries:[await entry(id)]}})).status()).toBe(403);
    // Exact stacked field selection is visible to an independent user.
    const cursor=page.locator('[data-gross-presence="Gross Viewer"]');
    const viewerCell=viewer.locator(`[data-gross-driver="${id}"][data-gross-date="${week}"]`);
    await viewerCell.locator('[data-gross-field="driverRate"]').click();
    await expect(cursor).toHaveAttribute('aria-label',/driverRate.*Load 1/);
    await viewerCell.locator('[data-gross-field="miles"]').click();
    await expect(cursor).toHaveAttribute('aria-label',/miles.*Load 1/);
    await page.screenshot({path:join(temp,'gross-board-live-presence.png'),fullPage:true,animations:'disabled'});
    const reads=[];
    const track=req=>{if(req.method()==='GET'&&new URL(req.url()).pathname==='/api/gross-board')reads.push(req.url())};
    viewer.on('request',track);
    await fill(field(page),'1444');
    await expect.poll(()=>numeric(field(viewer)),{timeout:1500}).toBe(1444);
    await saved();viewer.off('request',track);expect(reads).toEqual([]);
    // Notes update through the same stream.
    await page.locator(`[data-gross-driver="${id}"][data-gross-date="${week}"] [data-gross-field="loadNumber"]`).click({button:'right'});
    await page.getByRole('menuitem',{name:'Add note',exact:true}).click();
    await page.getByLabel('Note for LIVE-A, load 1',{exact:true}).fill('Live seal note');
    await saved();await page.keyboard.press('Escape');
    await expect(viewerCell.getByRole('img',{name:'Has load note',exact:true})).toBeVisible();
    // Keep newer typing while a committed response is deliberately delayed.
    let release,started;const gate=new Promise(r=>release=r),inFlight=new Promise(r=>started=r);
    await page.route('**/api/gross-board',async route=>{if(route.request().method()!=='PUT')return route.continue();const response=await route.fetch();started();await gate;await route.fulfill({response})},{times:1});
    await fill(field(page),'1500');await inFlight;await fill(field(page),'1599');
    const remote=await entry(other);
    expect((await page.request.put(base+'/api/gross-board',{headers,data:{weekStart:week,entries:[{...remote,driverRate:'1755'}]}})).status()).toBe(200);
    await expect.poll(()=>numeric(field(page,'driver rate',week,'Gross Live B'))).toBe(1755);
    await expect(field(page)).toHaveValue('1599');release();await saved();
    await expect.poll(()=>numeric(field(viewer))).toBe(1599);
    // Measure actual edit -> other-screen latency, including SSE network frames.
    for(const latency of [0,178]){
      setNetworkLatency(latency);const samples=[];
      try {for(let i=0;i<5;i++){
        const value=1700+latency+i;
        await viewer.evaluate(({value,id,week})=>{window.grossProbe=new Promise((resolve,reject)=>{const timeout=setTimeout(()=>reject(Error('Live update missing')),2000);const check=()=>{const input=document.querySelector(`[data-gross-driver="${id}"][data-gross-date="${week}"] [data-gross-field="originalRate"] input`);if(Number(input.value.replace(/[$,]/g,''))===value){clearTimeout(timeout);resolve(Date.now())}else requestAnimationFrame(check)};check()})},{value,id,week});
        const started=Date.now();await fill(field(page),String(value));samples.push(await viewer.evaluate(()=>window.grossProbe)-started);await saved();
      }}finally{setNetworkLatency(0)}
      console.log(`Gross Board edit-to-other-screen latency, ${latency}ms network RTT: ${samples.join(', ')}ms`);
      expect(Math.max(...samples)).toBeLessThan(1000);
    }
    // A snapshot for another week must never be reused for this week.
    await viewer.getByRole('button',{name:'Next week',exact:true}).click();
    await expect(field(viewer,'original rate',add(7))).toBeVisible();
    await expect(field(viewer,'original rate',add(7))).toHaveValue('');
    await fill(field(page),'1999');await saved();
    await expect(field(viewer,'original rate',add(7))).toHaveValue('');
    await viewer.getByRole('button',{name:'Previous week',exact:true}).click();
    await expect.poll(()=>numeric(field(viewer))).toBe(1999);
    // Presence from Daily loads identifies the exact load slot and field.
    await viewer.getByLabel(`Gross Live A, ${add(1)}, load numbers and details`,{exact:true}).click();
    const viewerDialog=viewer.getByRole('dialog',{name:'Daily loads',exact:true});
    await viewerDialog.locator('[data-gross-slot="1"] [data-gross-field="driverRate"]').click();
    await expect(cursor).toHaveAttribute('aria-label',/driverRate.*Load 2/);
    await page.getByLabel(`Gross Live A, ${add(1)}, load numbers and details`,{exact:true}).click();
    const editor=page.getByRole('dialog',{name:'Daily loads',exact:true});
    await expect(cursor).toHaveAttribute('aria-label',/driverRate.*Load 2/);
    await page.screenshot({path:join(temp,'gross-board-live-daily-loads.png'),fullPage:true,animations:'disabled'});
    const draft=editor.locator('[data-gross-slot="0"] [data-gross-field="driverRate"] input');
    await fill(draft,'333');
    const changed=await entry(id,add(1));
    expect((await page.request.put(base+'/api/gross-board',{headers,data:{weekStart:week,entries:[{...changed,driverRate:'455'}]}})).status()).toBe(200);
    await expect(draft).toHaveValue('333');
    await editor.getByRole('button',{name:'Apply changes',exact:true}).click();
    await expect(page.getByRole('main').getByRole('alert')).toContainText('changed');
    expect((await entry(id,add(1))).driverRate).toBe('455.00');
    page.once('dialog',d=>d.accept());await page.getByRole('button',{name:'Reload',exact:true}).click();
    await viewerDialog.getByRole('button',{name:'Close',exact:true}).last().click();
    // Reconnect receives current data immediately; expired/navigation cursors clear.
    await viewer.context().setOffline(true);await expect(viewer.getByText('Connecting…',{exact:true})).toBeVisible();
    await fill(field(page),'2111');await saved();await viewer.context().setOffline(false);
    await expect(viewer.getByText('Live',{exact:true})).toBeVisible({timeout:10000});
    await expect.poll(()=>numeric(field(viewer)),{timeout:1500}).toBe(2111);
    await viewerCell.locator('[data-gross-field="driverRate"]').click();await expect(cursor).toHaveCount(1);
    sql(`SET search_path TO ${schema},public;UPDATE gross_board_presence SET expires_at=now()-interval '1 second' WHERE user_id=(SELECT id FROM app_users WHERE username='Gross Viewer');SELECT pg_notify('mserp_gross_presence','');`);
    await expect(cursor).toHaveCount(0);
    await viewerCell.locator('[data-gross-field="miles"]').click();await expect(cursor).toHaveCount(1);
    await viewer.goto(base+'/account');await expect(cursor).toHaveCount(0);
    expect(errors).toEqual([]);
    console.log('Gross Board live E2E passed: direct data, no follow-up board GET, numeric caret/queued typing, totals, notes, week isolation, exact Daily loads presence, protected modal versions, read-only access, reconnect, expiry and cleanup.');
  }finally{setNetworkLatency(0);await context.close()}
}
