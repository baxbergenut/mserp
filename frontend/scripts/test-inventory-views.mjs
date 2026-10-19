// Local browser fixtures: no production, Google, AT&T, Samsara or PrePass calls.
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';
import assert from 'node:assert/strict';
import { chromium, expect } from '@playwright/test';

const id = number => `019a0000-0000-4000-8000-${String(number).padStart(12, '0')}`;
const stamp = '2026-10-19T14:00:00Z';
const record = (number, data) => ({ id: id(number), version: 1, data: { active: true, ...data }, updatedAt: stamp, updatedBy: 'Fleet reviewer' });
const trucks = [{ id: id(100), unitNumber: '022', active: true }, { id: id(101), unitNumber: '023', active: true }];
const data = {
  assets: [record(20, { deviceType: 'tablet', imei: '354624630315355', truckId: id(100), make: 'Samsung', model: 'Tab A9', condition: 'good', equipmentState: 'reported_present' }), record(21, { deviceType: 'samsara_gateway', serial: 'GATEWAY-022', truckId: id(100) }), record(22, { deviceType: 'samsara_camera', serial: 'CAMERA-022', gatewayId: id(21) }), record(23, { deviceType: 'samsara_gateway', serial: 'GATEWAY-023', truckId: id(101) })],
  lines: [record(30, { wirelessNumber: '9049217936', assetId: id(20), carrier: 'AT&T', status: 'Active' })],
  locations: [record(1, { name: 'Main storage' })],
  supplies: [record(11, { name: 'Chains', unit: 'sets', reusable: true }), record(14, { name: 'IFTA Sticker', unit: 'pairs', batchYear: '2026' })],
  equipment: [], 'transponder-notes': [], balances: [], movements: [],
};
const trailers = [], writes = [], reads = [];
let readOnly = false, failSave = false;
const prepass = { configured: true, environment: 'production', refreshedAt: stamp, accounts: ['123'], vehicles: [], transponders: [{ accountNumber: '123', transponderNumber: '777111222', vehicleNumber: '', vin: '', vrn: '', costCenter: '' }], operations: [], trucks: [] };
const server = createServer(async (request, response) => {
  const send = (body, code = 200) => { response.writeHead(code, { 'Content-Type': 'application/json' }); response.end(JSON.stringify(body)); };
  try {
    const url = new URL(request.url, 'http://localhost'), path = url.pathname;
    if (path.startsWith('/api/')) {
      if (request.method === 'GET') reads.push(path);
      if (path === '/api/auth/session') return send({ user: { id: id(500), username: 'Fleet reviewer', theme: 'default', permissions: ['inventory.read', 'trailers.read', 'trucks.read', 'tolls.read', ...(readOnly ? [] : ['inventory.manage', 'trailers.manage', 'inventory.import'])] }, csrfToken: 'fixture' });
      if (path === '/api/sidebar-counts') return send({ fuel: 0, weighMyTruck: 0, tolls: 0 });
      if (path === '/api/tasks/count') return send({ count: 0 });
      if (path === '/api/maintenance/access') return send({ canView: false, canCreate: false });
      if (path === '/api/lookups/trucks') return send(trucks);
      if (path === '/api/inventory' && request.method === 'GET') return send(data);
      if (path === '/api/trailers' && request.method === 'GET') return send(trailers);
      if (path.includes('/history')) return send([]);
      if (path === '/api/prepass-fleet') return send(prepass);
      if (path === '/api/inventory/imports') return send([]);
      let raw = ''; for await (const chunk of request) raw += chunk;
      const input = raw ? JSON.parse(raw) : {};
      writes.push({ path, input });
      if (failSave) { failSave = false; return send({ error: 'Inventory changed; reload and review' }, 409); }
      if (path === '/api/inventory/movements') { data.movements.unshift({ ...input, actor: 'Fleet reviewer', createdAt: stamp }); return send({ saved: true }); }
      if (request.method === 'PUT') {
        const collection = path.startsWith('/api/trailers/') ? 'trailers' : path.split('/')[3];
        const target = collection === 'trailers' ? trailers : data[collection];
        const saved = { ...input.record, version: input.record.version + 1, updatedAt: stamp, updatedBy: 'Fleet reviewer' };
        const index = target.findIndex(item => item.id === saved.id); if (index < 0) target.push(saved); else target[index] = saved;
        return send(saved);
      }
      return send({});
    }
    const file = path === '/' ? 'index.html' : extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    if (file.includes('..')) return send({}, 404);
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    response.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] || 'application/octet-stream' }); response.end(body);
  } catch (error) { send({ error: String(error) }, 500); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } });
  const errors = []; page.on('pageerror', error => errors.push(error.message));
  const origin = `http://127.0.0.1:${server.address().port}`;
  await page.goto(`${origin}/fleet/inventory?tab=tablets`);
  await expect(page.getByRole('cell', { name: '354624630315355', exact: true })).toBeVisible();
  await expect(page.getByRole('cell', { name: '9049217936 · Active', exact: true })).toBeVisible();
  assert.equal(writes.length, 0, 'opening inventory must not mutate records or contact vendors');
  await page.getByRole('button', { name: 'Add device', exact: true }).click();
  let dialog = page.getByRole('dialog');
  await dialog.getByLabel('IMEI', { exact: true }).fill('000000000000002');
  await dialog.getByLabel('Make', { exact: true }).fill('Samsung');
  await dialog.getByLabel('Assign device to').selectOption('truckId');
  await dialog.getByLabel('Truck', { exact: true }).selectOption(id(101));
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  const tabletId = writes.at(-1).input.record.id;
  assert.equal(writes.at(-1).input.record.data.imei, '000000000000002', 'IMEI must retain leading zeros');
  await page.getByRole('tab', { name: 'AT&T lines', exact: true }).click();
  await page.getByRole('row').filter({ hasText: '9049217936' }).getByRole('button', { name: 'Edit', exact: true }).click();
  dialog = page.getByRole('dialog');
  await dialog.getByLabel('Linked tablet / phone').selectOption(tabletId);
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  assert.equal(writes.at(-1).input.record.data.assetId, tabletId, 'line replacement must change the link, not copy identifiers');
  await page.getByRole('tab', { name: 'Samsara', exact: true }).click();
  await page.getByRole('row').filter({ hasText: 'CAMERA-022' }).filter({ has: page.getByRole('cell', { name: 'samsara camera', exact: true }) }).getByRole('button', { name: 'Edit' }).click();
  dialog = page.getByRole('dialog');
  await dialog.getByLabel('Linked gateway').selectOption(id(23));
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  assert.equal(writes.at(-1).input.record.data.gatewayId, id(23));
  await page.getByRole('tab', { name: 'Truck equipment', exact: true }).click();
  await page.getByRole('button', { name: 'Add equipment', exact: true }).click();
  dialog = page.getByRole('dialog');
  await dialog.getByLabel('Supply batch (optional)').selectOption(id(14));
  await dialog.getByLabel('Truck', { exact: true }).selectOption(id(100));
  await dialog.getByLabel('Issued / sent', { exact: true }).check();
  await dialog.getByLabel('Attached', { exact: true }).check();
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  assert.equal(writes.at(-1).input.record.data.batchYear, '2026');
  assert.equal(writes.at(-1).input.record.data.attachedDate, undefined, 'unknown installation date must remain unknown');
  await page.getByRole('button', { name: 'Stock movement', exact: true }).click();
  dialog = page.getByRole('dialog');
  await dialog.getByLabel('Quantity', { exact: true }).fill('3');
  await dialog.getByLabel('Reason / count evidence').fill('Physical opening count');
  await dialog.getByRole('button', { name: 'Record movement' }).click();
  await expect(dialog).toHaveCount(0);
  assert.equal(writes.at(-1).input.physicalCount, true);
  await page.getByRole('button', { name: 'Stock movement', exact: true }).click();
  dialog = page.getByRole('dialog');
  await dialog.getByLabel('Movement', { exact: true }).selectOption('issue');
  await dialog.getByLabel('To', { exact: true }).selectOption(`truck:${id(100)}`);
  await dialog.getByLabel('Reason / count evidence').fill('Issue chains');
  await dialog.getByRole('button', { name: 'Record movement' }).click();
  await expect(dialog).toHaveCount(0);
  assert.equal(writes.at(-1).input.from, `location:${id(1)}`);
  await page.getByRole('button', { name: 'Stock movement', exact: true }).click();
  dialog = page.getByRole('dialog');
  await dialog.getByLabel('Movement', { exact: true }).selectOption('return');
  await dialog.getByLabel('From', { exact: true }).selectOption(`truck:${id(100)}`);
  await dialog.getByLabel('Reason / count evidence').fill('Returned chains');
  await dialog.getByRole('button', { name: 'Record movement' }).click();
  await expect(dialog).toHaveCount(0);
  assert.equal(writes.at(-1).input.kind, 'return');
  await page.getByRole('tab', { name: 'Transponders', exact: true }).click();
  await expect(page.getByRole('region', { name: 'PrePass management' })).toContainText('777111222');
  assert.equal(writes.filter(write => write.path.startsWith('/api/prepass')).length, 0, 'shared PrePass view must not call a vendor mutation');
  const readStart = reads.length;
  await page.goto(`${origin}/fleet/trailers`);
  await page.getByRole('button', { name: 'Add trailer' }).click();
  dialog = page.getByRole('dialog');
  await dialog.getByLabel('Unit number', { exact: true }).fill('MS003');
  await dialog.getByLabel('VIN', { exact: true }).fill('1UYVS2530K7824719');
  await dialog.getByLabel('Storage location', { exact: true }).fill('Troy yard');
  failSave = true;
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).toContainText('Inventory changed; reload and review');
  await expect(dialog.getByLabel('Unit number', { exact: true })).toHaveValue('MS003');
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('cell', { name: 'Troy yard', exact: true })).toBeVisible();
  assert.equal(reads.slice(readStart).includes('/api/inventory'), false, 'trailer directory must not require inventory permission');
  await page.getByRole('row').filter({ hasText: 'MS003' }).getByRole('button', { name: 'Edit', exact: true }).click();
  dialog = page.getByRole('dialog');
  await dialog.getByLabel('Active', { exact: true }).uncheck();
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await page.getByLabel('Status filter').selectOption('archived');
  await expect(page.getByRole('button', { name: /MS003/ })).toBeVisible();
  readOnly = true; await page.reload();
  await expect(page.getByRole('button', { name: 'Add trailer' })).toHaveCount(0);
  await page.goto(`${origin}/fleet/inventory?tab=tablets`);
  await expect(page.getByRole('cell', { name: '000000000000002', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Add device' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Edit', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Stock movement' })).toHaveCount(0);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: 'Collapse sidebar', exact: true }).click();
  await page.screenshot({ path: join(tmpdir(), 'mserp-inventory-mobile.png'), animations: 'disabled' });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, 'tables must scroll locally');
  assert.deepEqual(errors, []);
  console.log('Inventory browser checks passed: tablets, line replacement, camera linking, IFTA observations, stock issue/return, shared PrePass, trailer conflicts/archive, read-only roles and mobile layout.');
} finally { await browser.close(); server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); }
