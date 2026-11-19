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
    console.log('Two-user Status Board save recovery passed for load edits, unrelated notes, overlapping edits, review, discard and reload.');
  } finally {await other.close();}
}
