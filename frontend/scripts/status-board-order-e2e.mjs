import { expect } from '@playwright/test';
import { join } from 'node:path';

export async function runStatusBoardOrderE2E({ page, viewer, base, sql, schema, headers, temp, week }) {
  const dispatcher = 'e4500000-0000-0000-0000-000000000071';
  const ids = [72,73,74,75].map(n => `e4500000-0000-0000-0000-${String(n).padStart(12,'0')}`);
  sql(`SET search_path TO ${schema},public;
    INSERT INTO dispatchers(id,full_name,normalized_name) VALUES('${dispatcher}','Order Dispatcher','order dispatcher');
    INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,dispatcher_id) VALUES
    ('${ids[0]}','Order Alpha','order alpha','cpm',0,'${dispatcher}'),
    ('${ids[1]}','Order Bravo','order bravo','cpm',0,'${dispatcher}'),
    ('${ids[2]}','Order Charlie','order charlie','cpm',0,'${dispatcher}'),
    ('${ids[3]}','Order Other','order other','cpm',0,NULL);
    INSERT INTO driver_board(driver_id,status,notes) VALUES('${ids[0]}','ENROUTE','Keep dispatch notes'),('${ids[1]}','HOME',''),('${ids[2]}','ENROUTE','');`);
  const read = async () => (await page.request.get(`${base}/api/driver-board?weekStart=${week}`)).json();
  const group = board => board.drivers.filter(d => d.dispatcherId === dispatcher).map(d => d.id);
  await page.getByLabel('Dispatcher filter', { exact: true }).click();
  await page.getByRole('button', { name: 'Select all', exact: true }).click();
  await page.keyboard.press('Escape');
  await page.getByLabel('Status filter', { exact: true }).click();
  await page.getByRole('button', { name: 'Select all', exact: true }).click();
  await page.keyboard.press('Escape');
  await page.getByLabel('Search Status Board', { exact: true }).fill('Order ');
  await viewer.goto(`${base}/driver-board`);
  await viewer.getByLabel('Search Status Board', { exact: true }).fill('Order ');
  const order = p => p.locator('tr[data-driver-id]').evaluateAll(rows => rows.map(r => r.dataset.driverId));
  const visualOrder = () => page.locator('tr[data-driver-id]').evaluateAll(rows => rows.sort((a,b) => a.getBoundingClientRect().top - b.getBoundingClientRect().top).map(r => r.dataset.driverId));
  await expect.poll(() => order(page)).toEqual(ids);
  await expect(viewer.getByRole('button', { name: 'Move Order Alpha', exact: true })).toHaveCount(0);
  const auth = await (await viewer.request.get(`${base}/api/auth/session`)).json();
  const initial = await read();
  const action = { dispatcherId: dispatcher, driverId: ids[2], targetDriverId: ids[0], after: false, revision: initial.driverOrderRevisions[dispatcher] };
  expect((await viewer.request.post(`${base}/api/driver-board/order`, { headers: { 'X-CSRF-Token': auth.csrfToken }, data: action })).status()).toBe(403);
  // A real mouse drag saves, broadcasts and survives reload; no board refetch is needed.
  await expect(page.getByText('Live', { exact: true })).toBeVisible();
  await expect(viewer.getByText('Live', { exact: true })).toBeVisible();
  const reads = [];
  const track = req => { if (req.method()==='GET' && new URL(req.url()).pathname==='/api/driver-board') reads.push(req.url()); };
  viewer.on('request',track);
  let previewWrites = 0;
  const trackPreviewWrite = req => { if (new URL(req.url()).pathname === '/api/driver-board/order') previewWrites++; };
  page.on('request', trackPreviewWrite);
  const grip = page.getByRole('button', { name: 'Move Order Charlie', exact: true });
  const start = await grip.boundingBox();
  const initialBoxes = await page.locator('tr[data-driver-id]').evaluateAll(rows => rows.map(r => { const b=r.getBoundingClientRect(); return {top:b.top,bottom:b.bottom,left:b.left}; }));
  await page.evaluate(() => { window.nativeDrags=0; document.addEventListener('dragstart',()=>window.nativeDrags++); });
  await page.mouse.move(start.x + start.width / 2, start.y + start.height / 2);
  await page.mouse.down();
  const pass = async (id, after) => {
    const box = await page.locator(`tr[data-driver-id="${id}"]`).boundingBox();
    await page.mouse.move(box.x + 300, box.y + (after ? box.height - 3 : 3), { steps: 8 });
  };
  // The actual row cannot leave its dispatcher or follow the mouse horizontally.
  await page.mouse.move(start.x + 500, initialBoxes[2].bottom + 150);
  let dragged = await page.locator(`tr[data-driver-id="${ids[2]}"]`).boundingBox();
  expect(Math.abs(dragged.y + dragged.height - initialBoxes[2].bottom)).toBeLessThan(2);
  expect(Math.abs(dragged.x - initialBoxes[2].left)).toBeLessThan(2);
  await page.evaluate(id => {
    window.slideSamples=[];
    const row=document.querySelector(`tr[data-driver-id="${id}"]`);
    const sample=()=>{ window.slideSamples.push(row.getBoundingClientRect().top); if(window.slideSamples.length<25) requestAnimationFrame(sample); };
    requestAnimationFrame(sample);
  }, ids[0]);
  await page.mouse.move(start.x + 500, initialBoxes[0].top - 100);
  await expect.poll(() => visualOrder()).toEqual([ids[2],ids[0],ids[1],ids[3]]);
  dragged = await page.locator(`tr[data-driver-id="${ids[2]}"]`).boundingBox();
  expect(Math.abs(dragged.y - initialBoxes[0].top)).toBeLessThan(2);
  await expect.poll(() => page.evaluate(() => window.slideSamples.length)).toBe(25);
  const samples = await page.evaluate(() => window.slideSamples);
  const finalAlpha = (await page.locator(`tr[data-driver-id="${ids[0]}"]`).boundingBox()).y;
  expect(samples.some(y => y > initialBoxes[0].top + 1 && y < finalAlpha - 1)).toBe(true);
  await page.screenshot({path:join(temp,'status-board-drag-clamped.png'),fullPage:true});
  expect(group(await read())).toEqual(ids.slice(0,3));
  await pass(ids[1], true);
  await expect.poll(() => visualOrder()).toEqual(ids);
  await pass(ids[0], false);
  await expect.poll(() => visualOrder()).toEqual([ids[2],ids[0],ids[1],ids[3]]);
  expect(previewWrites).toBe(0);
  // A slow response must never flash the old order between release and commit.
  await page.route('**/api/driver-board/order', async route => { await new Promise(resolve=>setTimeout(resolve,650)); await route.continue(); }, {times:1});
  await page.evaluate(id => {
    window.dropSamples=[];
    const sample=()=>{ window.dropSamples.push(document.querySelector(`tr[data-driver-id="${id}"]`).getBoundingClientRect().top); if(window.dropSamples.length<60) requestAnimationFrame(sample); };
    requestAnimationFrame(sample);
  }, ids[2]);
  await page.mouse.up();
  page.off('request', trackPreviewWrite);
  const expected = [ids[2],ids[0],ids[1],ids[3]];
  await expect.poll(async () => group(await read())).toEqual(expected.slice(0,3));
  await expect.poll(() => order(page)).toEqual(expected);
  await expect.poll(() => order(viewer)).toEqual(expected);
  await expect.poll(() => page.evaluate(() => window.dropSamples.length)).toBe(60);
  expect(Math.max(...await page.evaluate(() => window.dropSamples)) - initialBoxes[0].top).toBeLessThan(2);
  expect(await page.evaluate(() => window.nativeDrags)).toBe(0);
  viewer.off('request',track);
  expect(reads).toEqual([]);
  await page.reload();
  await expect.poll(() => order(page)).toEqual(expected);
  expect(group(await read())).toEqual(expected.slice(0,3));
  expect((await page.request.post(`${base}/api/driver-board/order`, { headers, data: action })).status()).toBe(409);
  // Keyboard alternative and both directions of movement.
  const handle = page.getByRole('button', { name: 'Move Order Charlie', exact: true });
  await handle.focus(); await page.keyboard.press('ArrowDown');
  await expect.poll(() => order(page)).toEqual([ids[0],ids[2],ids[1],ids[3]]);
  await expect(page.getByText('All changes saved', { exact: true })).toBeVisible();
  await handle.focus(); await page.keyboard.press('ArrowDown');
  await expect.poll(() => order(page)).toEqual(ids);
  // Hidden Bravo remains in the full order; filtering does not replace the roster.
  await page.getByLabel('Status filter', { exact: true }).click();
  await page.getByRole('button', { name: 'Clear all', exact: true }).click();
  await page.getByRole('checkbox', { name: 'ENROUTE', exact: true }).check();
  await page.keyboard.press('Escape');
  await expect.poll(() => order(page)).toEqual([ids[0],ids[2]]);
  await handle.dragTo(page.locator(`tr[data-driver-id="${ids[0]}"]`), { targetPosition: { x: 400, y: 3 } });
  await expect.poll(async () => group(await read())).toEqual([ids[2],ids[0],ids[1]]);
  await page.getByLabel('Status filter', { exact: true }).click();
  await page.getByRole('button', { name: 'Select all', exact: true }).click();
  await page.keyboard.press('Escape');
  await expect.poll(() => order(page)).toEqual(expected);
  // Moving over another dispatcher clamps to this dispatcher's last row.
  let calls=0;
  const trackWrites=req=>{if(new URL(req.url()).pathname==='/api/driver-board/order')calls++;};
  page.on('request',trackWrites);
  await handle.dragTo(page.locator(`tr[data-driver-id="${ids[3]}"]`), { targetPosition: { x: 400, y: 3 } });
  await expect.poll(() => order(page)).toEqual(ids);
  page.off('request',trackWrites);
  expect(calls).toBe(1);
  const latest=await read();
  expect((await page.request.post(`${base}/api/driver-board/order`, { headers, data: { ...action, targetDriverId: ids[3], revision: latest.driverOrderRevisions[dispatcher] } })).status()).toBe(409);
  await handle.dragTo(page.locator(`tr[data-driver-id="${ids[0]}"]`), { targetPosition: { x: 400, y: 3 } });
  await expect.poll(() => order(page)).toEqual(expected);
  // Escape restores the saved order without writing.
  const cancelStart=await handle.boundingBox();
  await page.mouse.move(cancelStart.x+cancelStart.width/2,cancelStart.y+cancelStart.height/2);
  await page.mouse.down(); await pass(ids[1],true);
  await expect.poll(visualOrder).toEqual(ids);
  await page.keyboard.press('Escape'); await page.mouse.up();
  await expect.poll(visualOrder).toEqual(expected);
  await expect(page.locator('[data-row-dragging]')).toHaveCount(0);
  expect(group(await read())).toEqual(expected.slice(0,3));
  // Failed requests leave the saved order visible and offer recovery.
  await page.route('**/api/driver-board/order',route=>route.fulfill({status:503,json:{error:'Order save unavailable'}}),{times:1});
  await handle.dragTo(page.locator(`tr[data-driver-id="${ids[1]}"]`), { targetPosition: { x: 400, y: 30 } });
  const orderAlert = page.getByRole('alert').filter({hasText:'Order save unavailable'});
  await expect(orderAlert).toBeVisible();
  await expect(page.locator('[data-row-dragging]')).toHaveCount(0);
  await expect.poll(visualOrder).toEqual(expected);
  await expect.poll(() => order(page)).toEqual(expected);
  await orderAlert.getByRole('button',{name:'Reload board',exact:true}).click();
  await expect(orderAlert).toHaveCount(0);
  // Touch uses the same bounded gesture; reduced motion disables row transitions.
  await page.emulateMedia({reducedMotion:'reduce'});
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('Emulation.setTouchEmulationEnabled',{enabled:true});
  const touch = async (type,x,y) => cdp.send('Input.dispatchTouchEvent',{type,touchPoints:type==='touchEnd'||type==='touchCancel'?[]:[{x,y}]});
  let touchBox=await handle.boundingBox();
  const bottomBox=await page.locator(`tr[data-driver-id="${ids[1]}"]`).boundingBox();
  await touch('touchStart',touchBox.x+touchBox.width/2,touchBox.y+touchBox.height/2);
  await touch('touchMove',touchBox.x+touchBox.width/2,bottomBox.y+bottomBox.height+70);
  await expect.poll(visualOrder).toEqual(ids);
  expect(await page.locator(`tr[data-driver-id="${ids[0]}"]`).evaluate(row=>getComputedStyle(row).transitionDuration)).toBe('0s');
  await touch('touchEnd');
  await expect.poll(()=>order(page)).toEqual(ids);
  expect(group(await read())).toEqual(ids.slice(0,3));
  touchBox=await handle.boundingBox();
  const topBox=await page.locator(`tr[data-driver-id="${ids[0]}"]`).boundingBox();
  await touch('touchStart',touchBox.x+touchBox.width/2,touchBox.y+touchBox.height/2);
  await touch('touchMove',touchBox.x+touchBox.width/2,topBox.y-30);
  await expect.poll(visualOrder).toEqual(expected);
  await touch('touchCancel');
  await expect(page.locator('[data-row-dragging]')).toHaveCount(0);
  await expect.poll(visualOrder).toEqual(ids);
  expect(group(await read())).toEqual(ids.slice(0,3));
  await cdp.detach();
  await page.emulateMedia({reducedMotion:'no-preference'});
  expect((await read()).entries.find(e=>e.driverId===ids[0]).notes).toBe('Keep dispatch notes');
  // Dispatcher groups preview inside the filter; the shared board order changes on drop.
  await page.getByLabel('Dispatcher filter', {exact:true}).click();
  const dispatcherGrip = page.getByRole('button', {name:'Move Order Dispatcher',exact:true});
  const unassignedGrip = page.getByRole('button', {name:'Move Unassigned',exact:true});
  const beforeDispatcherOrder = (await read()).drivers.map(d=>d.dispatcherId);
  const from = await dispatcherGrip.boundingBox(), to = await unassignedGrip.boundingBox();
  await page.mouse.move(from.x+from.width/2,from.y+from.height/2);
  await page.mouse.down();
  await page.mouse.move(from.x+from.width/2,to.y+to.height+20,{steps:8});
  await expect.poll(async()=>{
    const a=await dispatcherGrip.boundingBox(), b=await unassignedGrip.boundingBox();
    return a.y>b.y;
  }).toBe(true);
  expect((await read()).drivers.map(d=>d.dispatcherId)).toEqual(beforeDispatcherOrder);
  await page.mouse.up();
  await expect.poll(async()=> (await read()).drivers.map(d=>d.dispatcherId).indexOf('') < (await read()).drivers.map(d=>d.dispatcherId).indexOf(dispatcher)).toBe(true);
  await expect.poll(()=>order(viewer)).toEqual([ids[3],ids[0],ids[1],ids[2]]);
  await page.keyboard.press('Escape');
  await page.screenshot({path:join(temp,'status-board-driver-order.png'),fullPage:true});
  await page.getByLabel('Search Status Board',{exact:true}).fill('');
  console.log('Status Board row ordering E2E passed: bounded pointer drag with no native ghost, animated neighbors, forward/back preview, delayed-save continuity, Escape, failed-drag rollback, keyboard moves, reload persistence, two-user live order without refetch, filtered hidden rows, dispatcher boundaries, stale revision and read-only authorization.');
}
