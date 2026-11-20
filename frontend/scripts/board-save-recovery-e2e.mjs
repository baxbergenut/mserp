import { expect } from '@playwright/test';

export async function runBoardSaveRecovery({ page, base, password, sql, schema }) {
  const name = 'Save Test Driver';
  const id = 'e5070000-0000-0000-0000-000000000001';
  sql(`SET search_path TO ${schema},public;
    INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES('${id}','Save Test Driver','save test driver','cpm',0);
    INSERT INTO trucks(id,unit_number,status) VALUES('e5070000-0000-0000-0000-000000000002','SAVE-1','assigned');
    INSERT INTO truck_driver_assignments(truck_id,driver_id) VALUES('e5070000-0000-0000-0000-000000000002','${id}');
    INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES(987652,'SAVE-52','Dispatched',500,500,'{"stops":[{"ordering":1,"stop_type":"pickup","location":{"city":"Dallas","state":"TX"}}]}');
    INSERT INTO app_users(username,email,password_hash,role_id) SELECT 'Save second','save-second@example.test',password_hash,role_id FROM app_users WHERE email='investor-e2e@example.com';`);
  const other = await page.context().browser().newContext({baseURL:base});
  try {
    const login=await other.request.post('/api/auth/login',{data:{email:'save-second@example.test',password}});
    expect(login.status()).toBe(200);
    const session=await (await other.request.get('/api/auth/session')).json();
    const opening = page.waitForResponse(response => new URL(response.url()).pathname === '/api/driver-board' && response.status() === 200);
    await page.goto(base+'/driver-board');
    const week = new URL((await opening).url()).searchParams.get('weekStart');
    const read=async()=> (await (await other.request.get('/api/driver-board?weekStart='+week)).json()).entries.find(row=>row.driverId===id);
    const write=async(patch)=>{
      const entry=await read();
      expect((await other.request.put('/api/driver-board',{headers:{'X-CSRF-Token':session.csrfToken},data:{entries:[{...entry,...patch}]}})).status()).toBe(200);
    };
    const input=field=>page.getByLabel(name+' · '+field,{exact:true});
    await input('Current load').focus();
    await write({notes:'other user note'});
    await input('Current load').fill('SAVE-52');
    await expect.poll(async()=> {const e=await read(); return [e.currentLoad,e.notes];}).toEqual(['SAVE-52','other user note']);
    await expect(page.getByText('Saved',{exact:true})).toBeVisible();
    await page.reload();
    await expect(input('Current load')).toHaveValue('SAVE-52');
    await input('Notes').focus();
    await write({notes:'latest on server'});
    let boardReads = 0;
    const countReads = request => {
      if (request.method() === 'GET' && new URL(request.url()).pathname === '/api/driver-board') boardReads++;
    };
    page.on('request', countReads);
    const rejection = page.waitForResponse(response => response.status() === 409 && new URL(response.url()).pathname === '/api/driver-board');
    await input('Notes').fill('my overlapping change');
    const alert=page.getByRole('main').getByRole('alert');
    await expect(alert).toContainText('latest on server');
    const rejected = await (await rejection).json();
    expect(rejected.currentEntries).toHaveLength(1);
    expect(rejected.currentEntries[0]).toMatchObject({driverId:id, notes:'latest on server'});
    page.off('request', countReads);
    expect(boardReads).toBe(0);
    await expect(input('Notes')).toHaveValue('my overlapping change');
    await alert.getByRole('button',{name:'Apply my edits'}).click();
    await expect.poll(async()=>(await read()).notes).toBe('my overlapping change');
    await expect(page.getByText('Saved',{exact:true})).toBeVisible();
    await input('Notes').focus();
    await write({notes:'second user final'});
    await input('Notes').fill('discard this draft');
    await expect(alert).toContainText('second user final');
    await input('Trailer').fill('KEEP-42');
    await alert.getByRole('button',{name:'Use saved values'}).click();
    await expect.poll(async()=> {const e=await read(); return [e.notes,e.trailerNumber];}).toEqual(['second user final','KEEP-42']);
    await expect(input('Notes')).toHaveValue('second user final');
    await page.reload();
    await expect(input('Notes')).toHaveValue('second user final');
    await expect(input('Trailer')).toHaveValue('KEEP-42');
    const view=await (await other.request.get('/api/driver-board/loads/'+id)).json();
    expect(view.entryVersion).toBe((await read()).version);
    await page.getByRole('button', {name: name+' · Next load', exact: true}).click();
    const drawer = page.getByRole('dialog', {name: name+' · Loads'});
    await drawer.getByLabel('Add next load').fill('TEXT-RACE-52');
    const loadPath = '/api/driver-board/loads/'+id;
    let posts = 0;
    const interveningNote = async route => {
      if (route.request().method() === 'POST') {
        posts++;
        await write({notes: 'Other dispatcher updated notes while adding load'});
      }
      await route.continue();
    };
    await page.route('**'+loadPath, interveningNote);
    const receipt = page.waitForResponse(r => new URL(r.url()).pathname === loadPath && r.request().method() === 'POST');
    await drawer.getByLabel('Add next load').press('Enter');
    const savedLoad = await receipt;
    expect(savedLoad.status()).toBe(200);
    console.log('Load POST across a concurrent Notes edit', savedLoad.headers()['server-timing']);
    await expect(drawer.getByText('TEXT-RACE-52', {exact: true}).first()).toBeVisible();
    await page.unroute('**'+loadPath, interveningNote);
    expect(posts).toBe(1);
    const persisted = await (await other.request.get(loadPath)).json();
    expect(persisted.next.filter(p => p.number === 'TEXT-RACE-52')).toHaveLength(1);
    expect((await read()).notes).toBe('Other dispatcher updated notes while adding load');
    const canonicalStatus = () => sql(`SET search_path TO ${schema},public; SELECT status FROM driver_board WHERE driver_id='${id}';`);
    const setStatus = status => sql(`SET search_path TO ${schema},public;
      UPDATE driver_board SET status='${status}', version=version+1 WHERE driver_id='${id}';`);
    sql(`SET search_path TO ${schema},public; UPDATE fleet_status_control SET enabled=true WHERE singleton;`);
    const changeQueue = async (action, expectedStatus) => {
      const before = await (await other.request.get(loadPath)).json();
      const response = await other.request.post(loadPath, {
        headers: {'X-CSRF-Token': session.csrfToken},
        data: {...action, fromDate: before.fromDate, revision: before.revision, version: before.entryVersion},
      });
      expect(response.status()).toBe(200);
      const {entry, loads} = await response.json();
      expect(entry.status).toBe(expectedStatus);
      expect(canonicalStatus()).toContain(expectedStatus);
      expect(entry.version).toBe(loads.entryVersion);
      const after = await (await other.request.get(loadPath)).json();
      expect(after.revision).toBe(loads.revision);
      expect(after.entryVersion).toBe(entry.version);
    };
    setStatus('ENROUTE');
    expect(canonicalStatus()).toContain('ENROUTE');
    await changeQueue({action:'reset_order'}, 'RESERVED');
    await changeQueue({action:'hide', planId:persisted.next.find(p => p.number === 'TEXT-RACE-52').planId}, 'ENROUTE');
    setStatus('RESERVED');
    expect(canonicalStatus()).toContain('RESERVED');
    await changeQueue({action:'reset_order'}, 'ENROUTE');
    console.log('Two-user Status Board save recovery passed for load edits, unrelated notes, overlapping edits, review, discard and reload.');
  } finally {await other.close();}
}
