import { expect } from '@playwright/test';
import { randomBytes } from 'node:crypto';
import { join } from 'node:path';

export async function runGrossBoardPartialE2E({page, base, sql, schema}) {
  const monday = new Date(new Intl.DateTimeFormat('en-CA', { timeZone: 'America/New_York', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date()) + 'T12:00:00Z');
  monday.setUTCDate(monday.getUTCDate() - (monday.getUTCDay() + 6) % 7);
  const day = offset => {
    const date = new Date(monday);
    date.setUTCDate(date.getUTCDate() + offset);
    return date.toISOString().slice(0, 10);
  };
  const [week, sharedDay, independentDay] = [day(0), day(1), day(2)];
  const driver = 'e5710000-0000-0000-0000-000000000001';
  const name = 'Partial Save Driver';
  const password = randomBytes(20).toString('hex');
  sql(`SET search_path TO ${schema},public;
    INSERT INTO app_roles(name,permissions) VALUES('Gross Board test dispatcher',ARRAY['board.read','board.write']);
    INSERT INTO app_users(username,email,password_hash,role_id)
    SELECT 'Test Dispatcher ' || n,'gross-dispatch-' || n || '@example.com',crypt('${password}',gen_salt('bf')),r.id
    FROM generate_series(1,2) n CROSS JOIN app_roles r WHERE r.name='Gross Board test dispatcher';
    INSERT INTO app_roles(name,permissions) VALUES('Accountant',ARRAY['board.read','board.write']);
    INSERT INTO app_users(username,email,password_hash,role_id)
    SELECT 'Test Accountant','gross-accountant@example.com',crypt('${password}',gen_salt('bf')),id
    FROM app_roles WHERE name='Accountant';
    INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES('${driver}','${name}','partial save driver','cpm',0);
    INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,raw_payload) VALUES
    (99101,'REFRESH-TEST','delivered',1000,1000,500,'{"trips":[{"id":99101,"trip_id":"REFRESH-TRIP","driver__full_name":"Partial Save Driver","total_load_pay":700,"mile":450,"empty_mile":50}]}');
    INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,original_rate,driver_rate,miles) VALUES
    ('${driver}','${week}','REFRESH-TEST',99101,1000,600,500),
    ('${driver}','${sharedDay}','SHARED-TEST',NULL,900,500,400),
    ('${driver}','${independentDay}','INDEPENDENT-TEST',NULL,800,450,350);`);
  const emails = ['gross-dispatch-1@example.com', 'gross-dispatch-2@example.com', 'gross-accountant@example.com'];
  const contexts = await Promise.all(emails.map(() => page.context().browser().newContext({viewport: {width: 1800, height: 1000}})));
  const read = async date => {
    const response = await page.request.get(`${base}/api/gross-board?weekStart=${week}`);
    expect(response.ok()).toBeTruthy();
    return (await response.json()).entries.find(e => e.driverId === driver && e.date === date && e.slot === 0);
  };
  const saved = editor => expect(editor.getByRole('status').filter({hasText: /^Saved$/})).toBeVisible();
  const rate = (editor, date) => editor.getByLabel(`${name}, ${date}, driver rate`, {exact: true});
  const fillRate = async (editor, date, value) => {
    const input = rate(editor, date);
    await input.focus();
    await input.fill(value);
  };
  const review = editor => editor.getByRole('button', {name: `Review rate and miles for ${name}, ${week}`, exact: true});
  let release;
  try {
    const [first, second, accountant] = await Promise.all(contexts.map(async (context, index) => {
      const editor = await context.newPage();
      await editor.goto(base + '/login');
      await editor.getByLabel('Email', {exact: true}).fill(emails[index]);
      await editor.getByLabel('Password', {exact: true}).fill(password);
      await editor.getByRole('button', {name: 'Sign in', exact: true}).click();
      await expect(editor.getByRole('heading', {name: 'Gross Board', exact: true})).toBeVisible();
      await expect(editor.getByText('Live', {exact: true})).toBeVisible();
      return editor;
    }));
    const users = await Promise.all([first, second].map(async editor => (await (await editor.request.get(base + '/api/auth/session')).json()).user.id));
    expect(users[0]).not.toBe(users[1]);
    const captured = first.waitForRequest(req => req.method() === 'PUT' && new URL(req.url()).pathname === '/api/gross-board');
    const gate = new Promise(resolve => { release = resolve; });
    await first.route('**/api/gross-board', async route => {
      if (route.request().method() === 'PUT') await gate;
      await route.continue();
    });
    await review(first).click();
    await first.getByRole('button', {name: 'Accept DataTruck driver gross', exact: true}).click();
    await fillRate(first, sharedDay, '550');
    expect((await captured).postDataJSON().entries).toHaveLength(2);
    // Hold the first request while a provider source update and the second
    // dispatcher's write commit. The production repository refresh path is
    // also exercised with UpsertLoads in TestDataTruckDriverGrossDatabase.
    await fillRate(second, sharedDay, '530');
    sql(`SET search_path TO ${schema},public;
      UPDATE loads SET raw_payload=jsonb_set(raw_payload,'{trips,0,total_load_pay}','820') WHERE id=99101;`);
    await saved(second);
    expect((await read(sharedDay)).driverRate).toBe('530.00');
    release();
    const issue = first.getByRole('alert').filter({hasText: 'Review the edits that could not be saved'});
    await expect(issue).toContainText(`${name} · ${week} · Load 1 · REFRESH-TEST · Driver gross`);
    await expect(issue).toContainText('$700.00');
    await expect(issue).toContainText('$820.00');
    expect((await read(sharedDay)).driverRate).toBe('550.00');
    expect((await read(week)).driverRate).toBe('600.00');
    await rate(second, sharedDay).blur();
    await expect(rate(second, sharedDay)).toHaveValue(/550/);
    // Subsequent edits still save while the original approval is pending.
    await fillRate(first, independentDay, '460');
    await expect.poll(async () => (await read(independentDay)).driverRate).toBe('460.00');
    await expect(issue).toBeVisible();
    await review(first).click();
    await first.getByRole('button', {name: 'Accept DataTruck driver gross', exact: true}).click();
    await saved(first);
    await expect(issue).toHaveCount(0);
    expect((await read(week)).driverRate).toBe('820.00');

    await page.goto(base + '/gross-board');
    const lock = page.getByRole('button', { name: 'Lock week', exact: true });
    await expect(first.getByRole('button', { name: 'Lock week', exact: true })).toHaveCount(0);
    await lock.click();
    await expect(lock).toHaveAttribute('aria-pressed', 'true');
    await expect(rate(first, sharedDay)).toHaveAttribute('readonly', '');
    await expect(rate(second, sharedDay)).toHaveAttribute('readonly', '');
    const { csrfToken } = await (await second.request.get(base + '/api/auth/session')).json();
    const headers = { 'X-CSRF-Token': csrfToken };
    const denied = await second.request.put(base + '/api/gross-board/week-lock', { headers, data: { weekStart: week, locked: false } });
    expect(denied.status()).toBe(403);
    const blocked = await second.request.put(base + '/api/gross-board', {
      headers, data: { weekStart: week, allowPartial: true, entries: [{ driverId: driver, date: sharedDay, slot: 0, driverRate: '999', fields: ['driverRate'] }] },
    });
    expect(blocked.status()).toBe(409);
    expect((await blocked.json()).code).toBe('gross_board_week_locked');
    expect((await read(sharedDay)).driverRate).toBe('550.00');

    await expect(rate(page, sharedDay)).toBeEditable();
    await fillRate(page, sharedDay, '560');
    await expect.poll(async () => (await read(sharedDay)).driverRate).toBe('560.00');
    await expect(accountant.getByRole('button', { name: 'Lock week', exact: true })).toHaveAttribute('aria-pressed', 'true');
    await expect(rate(accountant, sharedDay)).toBeEditable();
    await fillRate(accountant, sharedDay, '570');
    await expect.poll(async () => (await read(sharedDay)).driverRate).toBe('570.00');
    await expect(rate(second, sharedDay)).toHaveValue('$570.00');
    await expect(rate(second, sharedDay)).toHaveAttribute('readonly', '');
    await expect(lock).toHaveAttribute('aria-pressed', 'true');

    const accountantSession = await (await accountant.request.get(base + '/api/auth/session')).json();
    const accountantHeaders = { 'X-CSRF-Token': accountantSession.csrfToken };
    const manualSave = await accountant.request.put(base + '/api/gross-board', {
      headers: accountantHeaders,
      data: { weekStart: week, entries: [{ driverId: driver, date: sharedDay, slot: 0, driverRate: '580', fields: ['driverRate'] }] },
    });
    expect(manualSave.status()).toBe(200);
    const planning = await accountant.request.put(base + '/api/gross-board/planning-entries', {
      headers: accountantHeaders,
      data: { weekStart: week, entries: [{ driverId: driver, date: day(4), kind: 'note', text: 'Plan for Friday', version: 0 }] },
    });
    expect(planning.status()).toBe(200);
    const reset = await accountant.request.post(base + '/api/gross-board/balance/reset', {
      headers: accountantHeaders, data: { weekStart: week, driverId: driver, balance: '100.00' },
    });
    expect(reset.status()).toBe(200);

    await lock.click();
    await expect(rate(second, sharedDay)).toBeEditable();
    await fillRate(second, sharedDay, '590');
    await expect.poll(async () => (await read(sharedDay)).driverRate).toBe('590.00');
    console.log('Gross Board passed locked-week editing for Administrators and Accountants, with dispatcher writes blocked until unlock.');
  } finally {
    release?.();
    await Promise.all(contexts.map(context => context.close()));
  }
}

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
  const field=(p,name='total gross',day=week,driver='Gross Live A')=>p.getByLabel(`${driver}, ${day}, ${name}`,{exact:true});
  const numeric=async locator=>Number((await locator.inputValue()).replace(/[$,]/g,''));
  const fill=async(input,value)=>{await input.focus();await input.fill(value)};
  const saved=()=>expect(page.getByRole('status').filter({hasText:/^Saved$/})).toBeVisible();
  await page.goto(base+'/gross-board');
  await expect(page.getByText('Live',{exact:true})).toBeVisible();
  expect((await page.request.post(base+'/api/settings/roles',{headers,data:{name:'Gross live viewer',permissions:['board.read']}})).status()).toBe(204);
  const access=await(await page.request.get(base+'/api/settings/access')).json();
  const role=access.roles.find(r=>r.name==='Gross live viewer');
  const password=randomBytes(20).toString('hex');
  expect((await page.request.post(base+'/api/settings/users',{headers,data:{username:'Gross Viewer',firstName:'Gross',lastName:'Viewer',email:'gross-live@example.com',password,roleId:role.id,active:true}})).status()).toBe(200);
  const context=await page.context().browser().newContext({viewport:{width:1800,height:1050}});
  const viewer=await context.newPage();
  const errors=[];viewer.on('pageerror',e=>errors.push(e.message));
  try {
    await page.setViewportSize({width:1800,height:1050});
    await viewer.goto(base+'/login');
    await viewer.getByLabel('Email',{exact:true}).fill('gross-live@example.com');
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
    // A second page under the exact same cookie/user must receive data too.
    const sameContext = await page.context().browser().newContext({storageState: await page.context().storageState(),viewport:{width:1800,height:1050}});
    const sameUser = await sameContext.newPage();
    try {
      await sameUser.goto(base+'/gross-board');
      await expect(sameUser.getByText('Live',{exact:true})).toBeVisible();
      await field(sameUser, 'driver rate').focus();
      await fill(field(page, 'driver rate'), '811'); await saved();
      await expect.poll(()=>numeric(field(sameUser, 'driver rate'))).toBe(811);
    } finally { await sameContext.close(); }
    // Notes update through the same stream.
    await page.locator(`[data-gross-driver="${id}"][data-gross-date="${week}"] [data-gross-field="loadNumber"]`).click({button:'right'});
    await page.getByRole('menuitem',{name:'Add note',exact:true}).click();
    await page.getByLabel('Note for LIVE-A, load 1',{exact:true}).fill('Live seal note');
    await saved();await page.keyboard.press('Escape');
    await expect(viewerCell.getByRole('img',{name:'Has load note',exact:true})).toBeVisible();
    // Dispatch-only day planning persists and reaches another user without
    // changing the colocated load or becoming a financial/load-queue field.
    const planningBefore = await entry(id);
    const planningReads = [];
    const planningTrack = req => { if (req.method() === 'GET' && new URL(req.url()).pathname === '/api/gross-board') planningReads.push(req.url()); };
    viewer.on('request', planningTrack);
    for (const [kind, title, text] of [['note', 'Status / note', 'Available after 3 PM'], ['destination', 'Planned destination', 'Dallas, TX']]) {
      await page.getByRole('button', {name:`Add entry for Gross Live A, ${week}`,exact:true}).click();
      const planningDialog = page.getByRole('dialog', {name:'Day entry',exact:true});
      await planningDialog.getByRole('button', {name:title,exact:true}).click();
      await planningDialog.getByRole('textbox', {name:title,exact:true}).fill(text);
      await planningDialog.getByRole('button', {name:'Save',exact:true}).click();
      await expect(planningDialog.getByRole('status')).toHaveText('Saved');
      await planningDialog.getByRole('button', {name:'Close',exact:true}).last().click();
      await expect(planningDialog).toHaveCount(0);
      await expect(viewer.getByLabel(`${title} for Gross Live A, ${week}`, {exact:true})).toContainText(text);
      const rows = (await board()).planningEntries;
      expect(rows.find(e => e.driverId === id && e.date === week && e.kind === kind)?.text).toBe(text);
    }
    viewer.off('request', planningTrack);
    expect(planningReads).toEqual([]);
    const planningAfter = await entry(id);
    for (const field of ['loadNumber','originalRate','driverRate','miles','note','version']) expect(planningAfter[field]).toBe(planningBefore[field]);
    const savedNote = (await board()).planningEntries.find(e => e.driverId === id && e.date === week && e.kind === 'note');
    expect((await viewer.request.put(base+'/api/gross-board/planning-entries', {headers:{'X-CSRF-Token':viewerAuth.csrfToken},data:{weekStart:week,entries:[{...savedNote,text:'Viewer cannot edit'}]}})).status()).toBe(403);
    expect((await page.request.put(base+'/api/gross-board/planning-entries', {headers,data:{weekStart:week,entries:[{...savedNote,version:0,text:'Stale overwrite'}]}})).status()).toBe(409);
    // Legacy full-row saves know nothing about planningEntries and preserve them.
    expect((await page.request.put(base+'/api/gross-board', {headers,data:{weekStart:week,entries:[planningAfter]}})).status()).toBe(200);
    expect((await board()).planningEntries.find(e => e.driverId === id && e.date === week && e.kind === 'note')?.text).toBe('Available after 3 PM');
    await page.reload();
    await expect(page.getByLabel(`Status / note for Gross Live A, ${week}`, {exact:true})).toContainText('Available after 3 PM');
    await expect(page.getByLabel(`Planned destination for Gross Live A, ${week}`, {exact:true})).toContainText('Dallas, TX');
    await expect(field(page, 'load number or status')).toHaveValue('LIVE-A');
    await page.screenshot({path:join(temp,'gross-board-live-planning.png'),fullPage:true,animations:'disabled'});
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
    await expect(field(viewer,'total gross',add(7))).toBeVisible();
    await expect(field(viewer,'total gross',add(7))).toHaveValue('');
    await fill(field(page),'1999');await saved();
    await expect(field(viewer,'total gross',add(7))).toHaveValue('');
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
    await saved();
    expect((await entry(id,add(1))).driverRate).toBe('333.00');
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
    console.log('Gross Board live E2E passed: direct data, no follow-up board GET, numeric caret/queued typing, totals, notes, week isolation, exact Daily loads presence, last-write modal patches, read-only access, reconnect, expiry and cleanup.');
  }finally{setNetworkLatency(0);await context.close()}
}
