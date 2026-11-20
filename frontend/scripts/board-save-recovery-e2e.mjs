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
    // Establish a saved row, then let the second user win the race after the
    // browser has already submitted its inline dispatch edit.
    await write({notes:'initial note'});
    await page.reload();
    await expect(input('Notes')).toHaveValue('initial note');
    // Compare real keyboard and clipboard input in the focused cell. Record
    // input-to-PUT separately from PostgreSQL and the receipt's round trip.
    await page.context().grantPermissions(['clipboard-read','clipboard-write'], {origin:new URL(base).origin});
    await page.evaluate(() => {
      let lastInput = 0;
      document.addEventListener('input', () => { lastInput = performance.now(); }, true);
      const send = window.fetch.bind(window);
      window.saveStartDelays = [];
      window.fetch = (url, opts) => {
        if (opts?.method === 'PUT' && new URL(url, location.href).pathname === '/api/driver-board') {
          window.saveStartDelays.push(Math.round(performance.now() - lastInput));
        }
        return send(url, opts);
      };
    });
    const notes = input('Notes');
    const noteReceipt = () => page.waitForResponse(r => r.request().method() === 'PUT' && new URL(r.url()).pathname === '/api/driver-board');
    await notes.focus();
    await notes.press('Control+a');
    const typed = noteReceipt();
    await notes.pressSequentially('typed burst');
    expect((await typed).status()).toBe(200);
    await expect(page.getByText('Saved',{exact:true})).toBeVisible();
    expect((await read()).notes).toBe('typed burst');
    await page.evaluate(() => navigator.clipboard.writeText('pasted dispatch note'));
    await notes.press('Control+a');
    const pasted = noteReceipt();
    await notes.press('Control+v');
    expect((await pasted).status()).toBe(200);
    await expect(page.getByText('Saved',{exact:true})).toBeVisible();
    expect((await read()).notes).toBe('pasted dispatch note');
    const delays = await page.evaluate(() => window.saveStartDelays);
    expect(delays).toHaveLength(2);
    console.log('Status note input to PUT (ms)', {typed:delays[0], pasted:delays[1]});
    let inlineWrites = 0;
    const concurrentText = async route => {
      if (route.request().method() === 'PUT') {
        inlineWrites++;
        const command = route.request().postDataJSON().entries[0];
        expect(command.editedFields).toContain('currentLoad');
        expect(command).not.toHaveProperty('notes');
        if (inlineWrites === 1) await write({notes:'other user note',trailerNumber:'OTHER-42'});
      }
      await route.continue();
    };
    await page.route('**/api/driver-board', concurrentText);
    const inlineReceipt = page.waitForResponse(r => r.request().method() === 'PUT' && new URL(r.url()).pathname === '/api/driver-board');
    await input('Current load').fill('SAVE-52');
    expect((await inlineReceipt).status()).toBe(200);
    await expect.poll(async()=> {const e=await read(); return [e.currentLoad,e.notes,e.trailerNumber];}).toEqual(['SAVE-52','other user note','OTHER-42']);
    await page.unroute('**/api/driver-board', concurrentText);
    expect(inlineWrites).toBe(1);
    await expect(page.getByText('Saved',{exact:true})).toBeVisible();
    await page.reload();
    await expect(input('Current load')).toHaveValue('SAVE-52');
    let staleAttempts = 0;
    const competingLoad = async route => {
      if (route.request().method() === 'PUT' && ++staleAttempts === 1) {
        await write({currentLoad:'EARLIER-52',resolveCurrentLoad:true});
        expect((await read()).currentLoad).toBe('EARLIER-52');
      }
      await route.continue();
    };
    await page.route('**/api/driver-board', competingLoad);
    await input('Current load').fill('LATEST-52');
    await expect.poll(async()=>(await read()).currentLoad).toBe('LATEST-52');
    await expect(page.getByText('Saved',{exact:true})).toBeVisible();
    expect(staleAttempts).toBe(1);
    await page.unroute('**/api/driver-board', competingLoad);
    await input('Current load').fill('SAVE-52');
    await expect.poll(async()=>(await read()).currentLoad).toBe('SAVE-52');
    await expect(page.getByText('Saved',{exact:true})).toBeVisible();

    let overlappingWrites = 0;
    const overwriteNote = async route => {
      if (route.request().method() === 'PUT' && ++overlappingWrites === 1) {
        await write({notes:'earlier dispatcher note',trailerNumber:'KEEP-42'});
      }
      await route.continue();
    };
    await page.route('**/api/driver-board', overwriteNote);
    const lastNote = page.waitForResponse(r => r.request().method() === 'PUT' && new URL(r.url()).pathname === '/api/driver-board');
    await input('Notes').fill('latest dispatcher note');
    expect((await lastNote).status()).toBe(200);
    await expect(page.getByText('Saved',{exact:true})).toBeVisible();
    expect(overlappingWrites).toBe(1);
    await page.unroute('**/api/driver-board', overwriteNote);
    await page.reload();
    await expect(input('Notes')).toHaveValue('latest dispatcher note');
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
    console.log('Two-user Status Board save recovery passed for load edits, unrelated notes, latest field edits, retained trailer and reload.');
  } finally {await other.close();}
}
