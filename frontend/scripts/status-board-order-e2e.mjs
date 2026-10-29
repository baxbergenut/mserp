import { expect } from '@playwright/test';
import { join } from 'node:path';

export async function runStatusBoardOrderE2E({ page, viewer, base, sql, schema, headers, temp, week }) {
  const dispatcher = 'e4500000-0000-0000-0000-000000000071';
  const other = 'e4500000-0000-0000-0000-000000000076';
  const ids = [72,73,74,75].map(n => `e4500000-0000-0000-0000-${String(n).padStart(12,'0')}`);
  const intake = 'e4500000-0000-0000-0000-000000000077';
  sql(`SET search_path TO ${schema},public;
    INSERT INTO dispatchers(id,full_name,normalized_name) VALUES('${dispatcher}','Order Dispatcher','order dispatcher'),('${other}','Order Second','order second');
    INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,dispatcher_id) VALUES
    ('${ids[0]}','Order Alpha','order alpha','cpm',0,'${dispatcher}'),
    ('${ids[1]}','Order Bravo','order bravo','cpm',0,'${dispatcher}'),
    ('${ids[2]}','Order Charlie','order charlie','cpm',0,'${dispatcher}'),
    ('${ids[3]}','Order Other','order other','cpm',0,'${other}');
    INSERT INTO driver_board(driver_id,status,notes) VALUES('${ids[0]}','ENROUTE','Keep dispatch notes'),('${ids[1]}','HOME',''),('${ids[2]}','ENROUTE','');`);
  const read = async () => (await page.request.get(`${base}/api/driver-board?weekStart=${week}`)).json();
  const group = (board, key=dispatcher) => board.drivers.filter(d => d.dispatcherId === key && ids.includes(d.id)).map(d => d.id);
  const order = p => p.locator('tr[data-driver-id]').evaluateAll(rows => rows.map(r => r.dataset.driverId));
  const visualOrder = () => page.locator('tr[data-driver-id]').evaluateAll(rows => rows.sort((a,b)=>a.getBoundingClientRect().top-b.getBoundingClientRect().top).map(r=>r.dataset.driverId));
  await page.goto(`${base}/driver-board`);
  for (const name of ['Dispatcher','Status']) {
    await page.getByLabel(`${name} filter`,{exact:true}).click();
    await page.getByRole('button',{name:'Select all',exact:true}).click();
    await page.keyboard.press('Escape');
  }
  await page.getByLabel('Search Status Board',{exact:true}).fill('Order ');
  await viewer.goto(`${base}/driver-board`);
  await viewer.getByLabel('Search Status Board',{exact:true}).fill('Order ');
  await expect.poll(()=>order(page)).toEqual(ids);
  await expect(page.locator('tr[data-dispatcher-id]').first()).toHaveAttribute('data-dispatcher-id','');
  await expect(viewer.getByRole('button',{name:'Move Order Alpha',exact:true})).toHaveCount(0);
  await expect(page.getByText('Live',{exact:true})).toBeVisible();
  const auth = await (await viewer.request.get(`${base}/api/auth/session`)).json();
  const initial = await read();
  const action = {dispatcherId:dispatcher,driverId:ids[2],targetDriverId:ids[0],after:false,revision:initial.driverOrderRevisions[dispatcher]};
  expect((await viewer.request.post(`${base}/api/driver-board/order`,{headers:{'X-CSRF-Token':auth.csrfToken},data:action})).status()).toBe(403);
  const handle = () => page.getByRole('button',{name:'Move Order Charlie',exact:true});
  const row = id => page.locator(`tr[data-driver-id="${id}"]`);
  const drag = async (target, y=3) => {
    await handle().dragTo(target,{targetPosition:{x:180,y}});
    await expect(page.locator('[data-row-dragging]')).toHaveCount(0);
  };
  // Within-group animation and delayed response preserve the preview until saved.
  const start = await handle().boundingBox(), top = await row(ids[0]).boundingBox();
  await page.mouse.move(start.x+start.width/2,start.y+start.height/2); await page.mouse.down();
  await page.mouse.move(top.x+400,top.y+4,{steps:8});
  await expect.poll(visualOrder).toEqual([ids[2],ids[0],ids[1],ids[3]]);
  expect(group(await read())).toEqual(ids.slice(0,3));
  await page.route('**/api/driver-board/order',async route=>{await new Promise(r=>setTimeout(r,500));await route.continue();},{times:1});
  await page.mouse.up();
  await expect.poll(()=>order(page)).toEqual([ids[2],ids[0],ids[1],ids[3]]);
  await expect.poll(()=>order(viewer)).toEqual([ids[2],ids[0],ids[1],ids[3]]);
  await expect(page.locator('[data-row-dragging]')).toHaveCount(0);
  expect((await page.request.post(`${base}/api/driver-board/order`,{headers,data:action})).status()).toBe(409);
  await handle().focus(); await page.keyboard.press('ArrowDown');
  await expect.poll(()=>order(page)).toEqual([ids[0],ids[2],ids[1],ids[3]]);
  await handle().focus(); await page.keyboard.press('ArrowDown');
  await expect.poll(()=>order(page)).toEqual(ids);
  // Filtering retains hidden members in the full saved order.
  await page.getByLabel('Status filter',{exact:true}).click();
  await page.getByRole('button',{name:'Clear all',exact:true}).click();
  await page.getByRole('checkbox',{name:'ENROUTE',exact:true}).check(); await page.keyboard.press('Escape');
  await drag(row(ids[0]));
  expect(group(await read())).toEqual([ids[2],ids[0],ids[1]]);
  await page.getByLabel('Status filter',{exact:true}).click();
  await page.getByRole('button',{name:'Select all',exact:true}).click(); await page.keyboard.press('Escape');
  // Cross-dispatcher move changes native ownership now and reaches a second viewer.
  const before = Date.now();
  let releaseWrite, releaseReads;
  const heldWrite = new Promise(resolve => { releaseWrite = resolve; });
  const heldReads = new Promise(resolve => { releaseReads = resolve; });
  const boardURL = url => new URL(url).pathname === '/api/driver-board';
  await page.route(boardURL, async route => { await heldReads; await route.continue(); });
  await page.route('**/api/driver-board/order', async route => { await heldWrite; await route.continue(); }, {times:1});
  const normalRows = p => p.locator('tr[data-driver-id]').evaluateAll(rows => rows.every((row,i) =>
    !row.style.transform && !row.dataset.rowDragging && (!i || row.getBoundingClientRect().top >= rows[i-1].getBoundingClientRect().bottom - 1)));
  try {
    await drag(row(ids[3]));
    // The request is still blocked: the local row must already occupy a normal
    // destination slot without covering neighbours or distorting sticky cells.
    await expect.poll(()=>order(page)).toEqual([ids[0],ids[1],ids[2],ids[3]]);
    await expect.poll(()=>normalRows(page)).toBe(true);
    await expect.poll(()=>order(viewer)).toEqual([ids[2],ids[0],ids[1],ids[3]]);
    await expect.poll(()=>normalRows(viewer)).toBe(true);
    expect(group(await read())).toEqual([ids[2],ids[0],ids[1]]);
    await page.screenshot({path:join(temp,'status-board-transfer-pending.png'),fullPage:true});
    releaseWrite();
    await expect.poll(async()=>group(await read(),other)).toEqual([ids[2],ids[3]]);
    // A full-board GET remains blocked. Saving must finish on the POST response.
    await expect(handle()).toBeEnabled();
    await expect.poll(()=>normalRows(page)).toBe(true);
    await expect.poll(()=>order(viewer)).toEqual([ids[0],ids[1],ids[2],ids[3]]);
    await expect.poll(()=>normalRows(viewer)).toBe(true);
  } finally { releaseWrite(); releaseReads(); await page.unrouteAll({behavior:"wait"}); }
  const history = JSON.parse(sql(`SET search_path TO ${schema},public; SELECT json_build_object('at',assigned_at,'source',source,'actor',actor_name) FROM driver_dispatcher_assignments WHERE driver_id='${ids[2]}' AND unassigned_at IS NULL;`).match(/\{[^\n]+\}/)[0]);
  expect(Date.parse(history.at)).toBeGreaterThanOrEqual(before);
  expect(history.source).toBe('Status Board drag');
  expect(history.actor).not.toBe('System');
  // Empty Unassigned header is a drop target, and assignments survive reload.
  await page.route('**/api/driver-board/order',async route=>{const response=await route.fetch();await new Promise(resolve=>setTimeout(resolve,800));await route.fulfill({response});},{times:1});
  await drag(page.locator('tr[data-dispatcher-id=""]'),16);
  await expect.poll(async()=>group(await read(),'')).toEqual([ids[2]]);
  await expect.poll(()=>order(page)).toEqual([ids[2],ids[0],ids[1],ids[3]]);
  await expect.poll(()=>order(viewer)).toEqual([ids[2],ids[0],ids[1],ids[3]]);
  await expect(handle()).toBeEnabled();
  await page.reload(); await expect.poll(()=>order(page)).toEqual([ids[2],ids[0],ids[1],ids[3]]);
  await drag(row(ids[0]));
  await expect.poll(async()=>group(await read())).toEqual([ids[2],ids[0],ids[1]]);
  await expect(handle()).toBeEnabled();
  // Cancellation and failed transfers retain the saved dispatcher and order.
  const begin = await handle().boundingBox(), destination = await row(ids[3]).boundingBox();
  await page.mouse.move(begin.x+begin.width/2,begin.y+begin.height/2);await page.mouse.down();
  await page.mouse.move(destination.x+180,destination.y+4,{steps:8});
  await page.keyboard.press('Escape');await page.mouse.up();
  await expect(page.locator('[data-row-dragging]')).toHaveCount(0);
  expect(group(await read())).toEqual([ids[2],ids[0],ids[1]]);
  await page.route('**/api/driver-board/order',r=>r.fulfill({status:503,json:{error:'Order save unavailable'}}),{times:1});
  await drag(row(ids[3]));
  const alert=page.getByRole('alert').filter({hasText:'Order save unavailable'});
  await expect(alert).toBeVisible();await expect.poll(()=>order(page)).toEqual([ids[2],ids[0],ids[1],ids[3]]);await expect.poll(()=>normalRows(page)).toBe(true);expect(group(await read())).toEqual([ids[2],ids[0],ids[1]]);
  await alert.getByRole('button',{name:'Reload board',exact:true}).click();
  // Keyboard unassignment and touch reassignment use the same saved action.
  await expect(handle()).toBeEnabled();await handle().focus();await page.keyboard.press('Alt+ArrowUp');
  await expect.poll(async()=>group(await read(),'')).toEqual([ids[2]]);
  await expect(handle()).toBeEnabled();
  await page.emulateMedia({reducedMotion:'reduce'});
  const cdp=await page.context().newCDPSession(page);
  await cdp.send('Emulation.setTouchEmulationEnabled',{enabled:true});
  const from=await handle().boundingBox(), to=await row(ids[0]).boundingBox();
  await cdp.send('Input.dispatchTouchEvent',{type:'touchStart',touchPoints:[{x:from.x+from.width/2,y:from.y+from.height/2}]});
  await cdp.send('Input.dispatchTouchEvent',{type:'touchMove',touchPoints:[{x:to.x+180,y:to.y+3}]});
  await cdp.send('Input.dispatchTouchEvent',{type:'touchEnd',touchPoints:[]});
  await expect.poll(async()=>group(await read())).toEqual([ids[2],ids[0],ids[1]]);
  await cdp.detach();await page.emulateMedia({reducedMotion:'no-preference'});
  // A pending hire arrives live without becoming an assignable fleet record.
  sql(`SET search_path TO ${schema},public; INSERT INTO fleetscope_driver_intake(id,company_id,fleetscope_driver_id,driver_data,normalized_name,occurred_at) VALUES('${intake}',gen_random_uuid(),gen_random_uuid(),'{"fullName":"Order New Hire","phone":"4703344443"}','order new hire',now());`);
  const pending=page.locator(`tr[data-pending-intake-id="${intake}"]`);
  await expect(pending.getByText('New driver',{exact:true})).toBeVisible();
  await expect(pending.getByRole('link',{name:'Needs setup',exact:true})).toHaveAttribute('href',`/drivers?setup=${intake}`);
  await expect(pending.locator('button')).toHaveCount(0);
  await page.getByLabel('Dispatcher filter',{exact:true}).click();
  await page.getByRole('checkbox',{name:'Unassigned',exact:true}).uncheck();await page.keyboard.press('Escape');
  await expect(page.locator('tr[data-dispatcher-id=""]')).toHaveCount(0);
  await expect(pending).toHaveCount(0);
  await expect(page.locator(`tr[data-dispatcher-id="${dispatcher}"]`)).toBeVisible();
  await page.getByLabel('Dispatcher filter',{exact:true}).click();
  await page.getByRole('checkbox',{name:'Unassigned',exact:true}).check();await page.keyboard.press('Escape');
  await expect(page.locator('tr[data-dispatcher-id=""]')).toBeVisible();
  await expect(pending).toBeVisible();
  await page.screenshot({path:join(temp,'status-board-pending-hire.png'),fullPage:true});
  expect((await (await viewer.request.get(`${base}/api/driver-board?weekStart=${week}`)).json()).pendingDrivers).toEqual([]);
  await expect(viewer.locator('[data-pending-intake-id]')).toHaveCount(0);
  sql(`SET search_path TO ${schema},public; UPDATE fleetscope_driver_intake SET terminated_at=now() WHERE id='${intake}';`);
  await expect(pending).toHaveCount(0);
  // Unassigned stays pinned, while ordinary dispatcher ordering still works.
  await page.getByLabel('Dispatcher filter',{exact:true}).click();
  await expect(page.getByRole('button',{name:'Move Unassigned',exact:true})).toHaveCount(0);
  const second=page.getByRole('button',{name:'Move Order Second',exact:true});
  await second.focus();await page.keyboard.press('ArrowUp');await page.keyboard.press('Escape');
  await expect.poll(()=>order(page)).toEqual([ids[3],ids[2],ids[0],ids[1]]);
  expect((await read()).entries.find(e=>e.driverId===ids[0]).notes).toBe('Keep dispatch notes');
  await page.screenshot({path:join(temp,'status-board-driver-order.png'),fullPage:true});
  await page.getByLabel('Search Status Board',{exact:true}).fill('');
  console.log('Status Board order E2E passed: within/cross-group mouse, keyboard and touch, empty Unassigned, live viewers, history time/actor, reload, hidden rows, cancellation, failures, stale fences, pending-hire live visibility, and pinned group.');
}
