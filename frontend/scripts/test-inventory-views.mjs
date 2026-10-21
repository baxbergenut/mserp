// Isolated browser fixtures only. No production, provider or Google writes.
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';
import assert from 'node:assert/strict';
import { chromium, expect } from '@playwright/test';

const id = number => '019a0000-0000-4000-8000-' + String(number).padStart(12, '0');
const stamp = '2026-10-19T14:00:00Z';
const record = (number, data) => ({ id: id(number), version: 1, data: { active: true, ...data }, updatedAt: stamp, updatedBy: 'Fleet Reviewer' });
const trucks = [
  { id: id(100), unitNumber: '022', active: true, isCompanyOwned: true },
  { id: id(101), unitNumber: '023', active: true, isCompanyOwned: false },
  { id: id(102), unitNumber: '061', active: false, isCompanyOwned: true },
];
const data = {
  assets: [
    record(20, { deviceType: 'tablet', imei: '354624630315355', truckId: id(100), make: 'Samsung', model: 'Tab A9', condition: 'good', equipmentState: 'reported_present' }),
    record(21, { deviceType: 'samsara_gateway', serial: 'GATEWAY-022', truckId: id(100), condition: 'good' }),
    record(22, { deviceType: 'dashcam', brand: 'Samsara', serial: 'CAMERA-022', gatewayId: id(21), equipmentState: 'reported_present' }),
    record(23, { deviceType: 'samsara_gateway', serial: 'GATEWAY-023', truckId: id(101) }),
    record(24, { deviceType: 'dashcam', brand: 'FiveELD', imei: '000000000000024', truckId: id(101), condition: 'good', equipmentState: 'verified_installed', verifiedAt: stamp, verifiedBy: 'Fleet Reviewer' }),
    record(25, { deviceType: 'phone', imei: '000000000000025', make: 'Apple', model: 'iPhone 15 Pro', custodian: 'Fleet Staff', condition: 'good', equipmentState: 'unknown' }),
    record(26, { deviceType: 'other', serial: 'UNCLASSIFIED-DEVICE', truckId: id(100) }),
    record(28, { deviceType: 'samsara_gateway', serial: 'GATEWAY-STORAGE', equipmentState: 'unknown' }),
  ],
  lines: [
    record(30, { wirelessNumber: '9049217936', assetId: id(20), carrier: 'AT&T', status: 'active' }),
    record(31, { wirelessNumber: '9045550100', assetId: id(25), carrier: 'AT&T', status: 'active' }),
  ],
  locations: [record(1, { name: 'Main Storage' })],
  supplies: [
    record(11, { name: 'Chains', unit: 'sets', reusable: true }),
    record(12, { name: 'Extinguisher', unit: 'units', reusable: true }),
    record(13, { name: 'MC Sticker', unit: 'units' }),
    record(14, { name: 'IFTA Sticker', unit: 'pairs', batchYear: '2026' }),
  ],
  equipment: [
    record(40, { name: 'Samsara DashCam', truckId: id(100), equipmentState: 'reported_present' }),
    record(41, { name: 'Chains', truckId: id(100), equipmentState: 'reported_present' }),
    record(42, { name: 'IFTA Sticker', batchYear: '2026', truckId: id(100), equipmentState: 'unknown', sent: true, attached: false }),
  ],
  'transponder-notes': [record(50, { serial: 'OLD-LOCAL-TAG' })], balances: [], movements: [],
};
const trailers = [], writes = [], reads = [];
let readOnly = false, failSave = false, truckAccess = true, trailerOnly = false;
const prepass = { configured: true, environment: 'production', refreshedAt: stamp, accounts: ['123'], vehicles: [], transponders: [{ accountNumber: '123', transponderNumber: '777111222', vehicleNumber: '', vin: '', vrn: '', costCenter: '' }], operations: [], trucks: [] };
const server = createServer(async (request, response) => {
  const send = (body, code = 200) => { response.writeHead(code, { 'Content-Type': 'application/json' }); response.end(JSON.stringify(body)); };
  try {
    const path = new URL(request.url, 'http://localhost').pathname;
    if (path.startsWith('/api/')) {
      if (request.method === 'GET') reads.push(path);
      if (path === '/api/auth/session') return send({ user: { id: id(500), username: 'Fleet Reviewer', theme: 'default', permissions: trailerOnly ? ['trailers.read'] : ['inventory.read', 'trailers.read', 'tolls.read', ...(truckAccess ? ['trucks.read'] : []), ...(readOnly ? [] : ['inventory.manage', 'trailers.manage', 'inventory.import'])] }, csrfToken: 'fixture' });
      if (path === '/api/sidebar-counts') return send({ fuel: 0, weighMyTruck: 0, tolls: 0 });
      if (path === '/api/tasks/count') return send({ count: 0 });
      if (path === '/api/maintenance/access') return send({ canView: false, canCreate: false });
      if (path === '/api/lookups/trucks') return send(trucks);
      if (path === '/api/inventory' && request.method === 'GET') return send(data);
      if (path === '/api/trailers' && request.method === 'GET') return send(trailers);
      if (path === '/api/prepass-fleet') return send(prepass);
      if (path === '/api/inventory/imports') return send([]);
      if (path.includes('/history')) return send([]);
      let raw = ''; for await (const chunk of request) raw += chunk;
      const input = raw ? JSON.parse(raw) : {};
      writes.push({ path, input });
      if (failSave) { failSave = false; return send({ error: 'Inventory changed; reload and review' }, 409); }
      if (request.method === 'PUT') {
        const collection = path.startsWith('/api/trailers/') ? 'trailers' : path.split('/')[3];
        const target = collection === 'trailers' ? trailers : data[collection];
        const index = target.findIndex(item => item.id === input.record.id);
        if (index >= 0 && target[index].version !== input.record.version) return send({ error: 'Inventory changed; reload and review' }, 409);
        const saved = { ...input.record, data: { ...input.record.data }, version: input.record.version + 1, updatedAt: stamp, updatedBy: 'Fleet Reviewer' };
        if (saved.data.equipmentState === 'verified_installed') { saved.data.verifiedAt = stamp; saved.data.verifiedBy = 'Fleet Reviewer'; }
        else { delete saved.data.verifiedAt; delete saved.data.verifiedBy; }
        if (index < 0) target.push(saved); else target[index] = saved;
        return send(saved);
      }
      return send({});
    }
    const file = path === '/' ? 'index.html' : extname(path) ? path.slice(1) : path.slice(1) + '.html';
    if (file.includes('..')) return send({}, 404);
    const body = await readFile(new URL('../out/' + file, import.meta.url));
    response.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] || 'application/octet-stream' }); response.end(body);
  } catch (error) { send({ error: String(error) }, 500); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } });
  const errors = []; page.on('pageerror', error => errors.push(error.message));
  const origin = 'http://127.0.0.1:' + server.address().port;
  const search = () => page.getByPlaceholder('Search identifier, number, truck or notes…');
  const toolbarAligned = async () => {
    const centers = await page.getByRole('group', { name: 'Inventory controls', exact: true }).locator('input, select, button').evaluateAll(elements => elements.map(element => { const rect = element.getBoundingClientRect(); return rect.y + rect.height / 2; }));
    assert.ok(centers.length >= 4);
    assert.ok(Math.max(...centers) - Math.min(...centers) < 3, 'desktop toolbar must stay on one row');
  };
  const tab = name => page.getByRole('tab', { name, exact: true }).click();
  await page.goto(origin + '/fleet/inventory?tab=tablets');
  await expect(page.getByRole('cell', { name: '+1 (904) 921-7936', exact: true })).toBeVisible();
  await expect(page.getByRole('cell', { name: /7936.*Active/ })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Samsung · Tab A9', exact: true })).toBeVisible();
  await expect(page.locator('tbody').getByText('Reported Present', { exact: true })).toBeVisible();
  await expect(page.locator('tbody').getByText('Reported Present', { exact: true })).toHaveClass(/text-amber/);
  for (const name of ['Other devices', 'Other Devices', 'History', 'Samsara dashcams']) await expect(page.getByRole('tab', { name, exact: true })).toHaveCount(0);
  for (const name of ['Stock movement', 'Migration review']) await expect(page.getByRole('button', { name, exact: true })).toHaveCount(0);
  await toolbarAligned(); await page.setViewportSize({ width: 1366, height: 900 }); await toolbarAligned(); await page.setViewportSize({ width: 1600, height: 1000 });
  await page.getByLabel('Assignment Filter').selectOption('unassigned');
  await expect(page.getByRole('cell', { name: 'No matching records.', exact: true })).toHaveAttribute('colspan', '7');
  await page.getByLabel('Assignment Filter').selectOption('');
  await search().fill('+1 (904) 921-7936'); await expect(page.getByRole('button', { name: 'Samsung · Tab A9', exact: true })).toBeVisible(); await search().fill('');
  assert.equal(writes.length, 0);
  await page.getByRole('button', { name: 'Add Tablet', exact: true }).click();
  let dialog = page.getByRole('dialog');
  await expect(dialog.getByLabel('Installation Status').locator('option[value="verified_installed"]')).toHaveJSProperty('disabled', true);
  await expect(dialog.getByLabel('Installation Status').locator('option[value="reported_present"]')).toHaveJSProperty('disabled', true);
  assert.equal(await dialog.getByLabel('Operational Status').evaluate(element => element.tagName), 'SELECT');
  await dialog.getByLabel('IMEI', { exact: true }).fill('000000000000002');
  await dialog.getByLabel('Make', { exact: true }).fill('Samsung');
  await dialog.getByLabel('Assign Device To').selectOption('truckId');
  await dialog.getByLabel('Truck', { exact: true }).selectOption(id(101));
  await expect(dialog.getByLabel('Installation Status').locator('option[value="verified_installed"]')).toHaveJSProperty('disabled', false);
  await dialog.getByLabel('Installation Status').selectOption('verified_installed');
  await dialog.getByRole('button', { name: 'Save', exact: true }).click(); await expect(dialog).toHaveCount(0);
  const tabletId = writes.at(-1).input.record.id;
  assert.equal(writes.at(-1).input.record.data.imei, '000000000000002');
  await expect(page.locator('tbody').getByText('Verified Installed', { exact: true })).toHaveClass(/text-emerald/);
  await tab('Phones');
  await expect(page.getByRole('button', { name: 'Apple · iPhone 15 Pro', exact: true })).toBeVisible();
  await expect(page.getByRole('cell', { name: '+1 (904) 555-0100', exact: true })).toBeVisible();
  await page.getByRole('row').filter({ hasText: '000000000000025' }).getByRole('button', { name: 'Edit', exact: true }).click();
  dialog = page.getByRole('dialog');
  await expect(dialog.getByLabel('Make', { exact: true })).toHaveValue('Apple');
  await expect(dialog.getByLabel('Installation Status').locator('option[value="verified_installed"]')).toHaveJSProperty('disabled', true);
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await tab('AT&T Lines');
  await page.getByRole('row').filter({ hasText: '+1 (904) 921-7936' }).getByRole('button', { name: 'Edit', exact: true }).click();
  dialog = page.getByRole('dialog');
  assert.equal(await dialog.getByLabel('Carrier Status').evaluate(element => element.tagName), 'SELECT');
  await expect(dialog.getByLabel('Linked Tablet / Phone').locator('option[value="' + id(25) + '"]')).toHaveCount(1);
  await dialog.getByLabel('Linked Tablet / Phone').selectOption(tabletId);
  await dialog.getByRole('button', { name: 'Save', exact: true }).click(); await expect(dialog).toHaveCount(0);
  assert.equal(writes.at(-1).input.record.data.assetId, tabletId);
  assert.equal(writes.at(-1).input.record.data.wirelessNumber, '9049217936');

  await tab('Dashcams');
  await expect(page.getByRole('cell', { name: '000000000000024', exact: true })).toBeVisible();
  await expect(page.getByRole('cell', { name: 'CAMERA-022', exact: true })).toBeVisible();
  await toolbarAligned(); await page.setViewportSize({ width: 1366, height: 900 }); await toolbarAligned(); await page.setViewportSize({ width: 1600, height: 1000 });
  await page.getByLabel('Brand Filter').selectOption('FiveELD');
  await expect(page.getByRole('cell', { name: 'CAMERA-022', exact: true })).toHaveCount(0);
  await page.getByLabel('Brand Filter').selectOption('');
  await page.getByRole('button', { name: 'Add Dashcam', exact: true }).click();
  dialog = page.getByRole('dialog');
  await expect(dialog.getByLabel('Device Type')).toHaveValue('dashcam');
  await dialog.getByLabel('Brand', { exact: true }).selectOption('FiveELD');
  await expect(dialog.getByLabel('Serial', { exact: true })).toHaveCount(0);
  await expect(dialog.getByLabel('IMEI', { exact: true })).toBeVisible();
  await expect(dialog.getByLabel('Assign Device To').locator('option[value="gatewayId"]')).toHaveCount(0);
  await dialog.getByLabel('Brand', { exact: true }).selectOption('Samsara');
  await dialog.getByLabel('Assign Device To').selectOption('gatewayId');
  await dialog.getByLabel('Linked Gateway').selectOption(id(28));
  await expect(dialog.getByLabel('Installation Status').locator('option[value="verified_installed"]')).toHaveJSProperty('disabled', true);
  await dialog.getByLabel('Linked Gateway').selectOption(id(23));
  await expect(dialog.getByLabel('Installation Status').locator('option[value="verified_installed"]')).toHaveJSProperty('disabled', false);
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.getByRole('row').filter({ hasText: 'CAMERA-022' }).getByRole('button', { name: 'Edit', exact: true }).click();
  dialog = page.getByRole('dialog');
  await dialog.getByLabel('Linked Gateway').selectOption(id(23));
  await expect(dialog.getByLabel('Installation Status')).toHaveValue('unknown');
  await dialog.getByRole('button', { name: 'Save', exact: true }).click(); await expect(dialog).toHaveCount(0);
  assert.equal(writes.at(-1).input.record.data.gatewayId, id(23));
  await tab('Samsara Gateways');
  await expect(page.getByRole('columnheader', { name: 'Asset Label', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Add Gateway', exact: true }).click();
  dialog = page.getByRole('dialog');
  for (const label of ['Internal Asset Label', 'IMEI', 'Make', 'Model']) await expect(dialog.getByLabel(label, { exact: true })).toHaveCount(0);
  await expect(dialog.getByLabel('Serial', { exact: true })).toBeVisible();
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await tab('Truck Equipment');
  await expect(page.getByRole('button', { name: /Samsara DashCam/, exact: true })).toHaveCount(0);
  await expect(page.getByText('UNCLASSIFIED-DEVICE', { exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Assign Device', exact: true }).click();
  dialog = page.getByRole('dialog');
  await dialog.getByLabel('Device', { exact: true }).selectOption(id(25));
  await dialog.getByLabel('Truck', { exact: true }).selectOption(id(100));
  await dialog.getByRole('button', { name: 'Continue', exact: true }).click();
  dialog = page.getByRole('dialog');
  await expect(dialog.getByLabel('Installation Status')).toHaveValue('unknown');
  await dialog.getByRole('button', { name: 'Save', exact: true }).click(); await expect(dialog).toHaveCount(0);
  assert.equal(writes.at(-1).input.record.id, id(25), 'assign existing device must not create a copy');
  await expect(page.getByRole('cell', { name: '000000000000025', exact: true })).toBeVisible();

  await tab('Supplies');
  const company = page.getByRole('region', { name: 'Company Trucks', exact: true });
  const other = page.getByRole('region', { name: 'Other Trucks', exact: true });
  await expect(company.getByRole('link', { name: '022', exact: true })).toBeVisible();
  await expect(company.getByRole('link', { name: '061', exact: true })).toBeVisible();
  await expect(other.getByRole('link', { name: '023', exact: true })).toBeVisible();
  await expect(company.getByRole('link', { name: '023', exact: true })).toHaveCount(0);
  await toolbarAligned();
  await expect(page.getByRole('columnheader', { name: 'Unit', exact: true })).toHaveCount(0);
  await expect(page.getByRole('columnheader', { name: 'Storage Stock', exact: true })).toHaveCount(0);
  const chains = page.getByRole('checkbox', { name: '022 · Chains', exact: true });
  await expect(chains).toBeChecked({ indeterminate: true });
  await chains.click(); await expect(chains).toBeChecked(); await expect(chains).not.toHaveJSProperty('disabled', true);
  assert.equal(writes.at(-1).input.record.data.equipmentState, 'verified_installed');
  assert.equal(writes.at(-1).input.record.id, id(41), 'existing observation must be updated');
  await expect(chains).toHaveClass(/accent-emerald/);
  await chains.click(); await expect(chains).not.toBeChecked(); await expect(chains).not.toHaveJSProperty('disabled', true);
  assert.equal(writes.at(-1).input.record.data.equipmentState, 'missing');
  const extinguisher = page.getByRole('checkbox', { name: '022 · Extinguisher', exact: true });
  failSave = true; await extinguisher.click();
  await expect(page.getByText('Inventory changed; reload and review', { exact: true })).toBeVisible();
  await expect(extinguisher).not.toBeChecked();
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await page.getByRole('button', { name: 'Add Supply', exact: true }).click();
  dialog = page.getByRole('dialog');
  await expect(dialog.getByLabel('Count Unit', { exact: true })).toHaveCount(0);
  await expect(dialog.getByLabel('Reusable', { exact: true })).toHaveCount(0);
  await dialog.getByLabel('Supply Name').fill('Safety Vest');
  await dialog.getByRole('button', { name: 'Save', exact: true }).click(); await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('columnheader', { name: 'Safety Vest', exact: true })).toHaveCount(2);
  assert.equal(writes.filter(write => write.path.includes('/movements')).length, 0);
  assert.equal(reads.filter(path => path.includes('/history')).length, 0);
  assert.equal(data.equipment.find(item => item.id === id(42)).data.sent, true, 'existing IFTA source flags must remain');
  await page.screenshot({ path: join(tmpdir(), 'mserp-inventory-supplies-desktop.png'), animations: 'disabled' });
  await page.getByLabel('Ownership Filter').selectOption('company');
  await expect(other).toHaveCount(0);
  await page.getByLabel('Ownership Filter').selectOption('all');
  await page.getByLabel('Truck Status Filter').selectOption('active');
  await expect(company.getByRole('link', { name: '061', exact: true })).toHaveCount(0);
  await page.getByLabel('Truck Status Filter').selectOption('all');

  await tab('Transponders');
  await expect(page.getByRole('region', { name: 'PrePass management' })).toContainText('777111222');
  await expect(page.getByRole('button', { name: 'Add local notes', exact: true })).toHaveCount(0);
  await expect(page.getByRole('group', { name: 'Inventory controls', exact: true })).toHaveCount(0);
  assert.equal(writes.filter(write => write.path.startsWith('/api/prepass')).length, 0);

  await page.goto(origin + '/fleet/trailers');
  await page.getByRole('button', { name: 'Add Trailer', exact: true }).click();
  dialog = page.getByRole('dialog');
  await dialog.getByLabel('Unit Number', { exact: true }).fill('MS003');
  await dialog.getByLabel('VIN', { exact: true }).fill('1UYVS2530K7824719');
  await dialog.getByLabel('Storage Location', { exact: true }).fill('Troy Yard');
  await dialog.getByLabel('Year', { exact: true }).fill('2020');
  await dialog.getByLabel('Make', { exact: true }).fill('Utility');
  failSave = true; await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).toContainText('Inventory changed; reload and review');
  await dialog.getByRole('button', { name: 'Save', exact: true }).click(); await expect(dialog).toHaveCount(0);
  const fleetHrefs = await page.locator('aside nav').getByRole('link').evaluateAll(links => links.map(link => link.getAttribute('href')));
  assert.equal(fleetHrefs.filter(href => href === '/trucks').length, 1);
  assert.ok(fleetHrefs.indexOf('/trucks') < fleetHrefs.indexOf('/fleet/trailers'));
  assert.ok(fleetHrefs.indexOf('/fleet/trailers') < fleetHrefs.indexOf('/fleet/inventory'));
  await page.getByRole('link', { name: 'MS003', exact: true }).click();
  await expect(page).toHaveURL(/\/fleet\/trailers\/detail\?id=/);
  await expect(page.getByRole('heading', { name: 'Trailer MS003', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Vehicle details', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Registration & documents', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Edit trailer', exact: true }).click();
  dialog = page.getByRole('dialog');
  await dialog.getByLabel('Storage Location', { exact: true }).fill('Updated Yard');
  await dialog.getByLabel('Notes', { exact: true }).fill('Reviewed trailer notes');
  await dialog.getByLabel('Registration HTTPS Link', { exact: true }).fill('https://example.com/registration');
  await dialog.getByRole('button', { name: 'Save', exact: true }).click(); await expect(dialog).toHaveCount(0);
  await expect(page.getByText('Updated Yard', { exact: true })).toBeVisible();
  await expect(page.getByText('Reviewed trailer notes', { exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Open registration', exact: true })).toHaveAttribute('href', 'https://example.com/registration');
  await page.reload();
  await expect(page.getByText('Reviewed trailer notes', { exact: true })).toBeVisible();
  await page.goBack();
  await expect(page.getByRole('link', { name: 'MS003', exact: true })).toBeVisible();
  await page.getByRole('row').filter({ hasText: 'MS003' }).getByRole('button', { name: 'Edit', exact: true }).click();
  dialog = page.getByRole('dialog'); await dialog.getByLabel('Active', { exact: true }).uncheck();
  await dialog.getByRole('button', { name: 'Save', exact: true }).click(); await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'MS003', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Filter Record', exact: true }).click();
  await page.getByLabel('Record status', { exact: true }).selectOption('inactive');
  await page.keyboard.press('Escape');
  await expect(page.getByRole('link', { name: 'MS003', exact: true })).toBeVisible();
  await page.getByPlaceholder('Search trailers…').fill('Updated Yard');
  await page.getByRole('link', { name: 'MS003', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Trailer MS003', exact: true })).toBeVisible();
  await page.goBack();
  await expect(page.getByPlaceholder('Search trailers…')).toHaveValue('Updated Yard');
  await expect(page.getByRole('link', { name: 'MS003', exact: true })).toBeVisible();
  await page.evaluate(async () => { await Promise.all(document.getAnimations().map(animation => animation.finished.catch(() => {}))); });
  await page.screenshot({ path: join(tmpdir(), 'mserp-trailers-desktop.png'), animations: 'disabled' });

  readOnly = true;
  await page.goto(origin + '/fleet/trailers/detail?id=' + trailers[0].id);
  await expect(page.getByRole('heading', { name: 'Trailer MS003', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Edit trailer', exact: true })).toHaveCount(0);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: 'Collapse sidebar', exact: true }).click();
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  await page.screenshot({ path: join(tmpdir(), 'mserp-trailer-detail-mobile.png'), animations: 'disabled' });
  await page.setViewportSize({ width: 1600, height: 1000 });
  await page.goto(origin + '/fleet/trailers');
  await expect(page.getByRole('button', { name: 'Add Trailer', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Edit', exact: true })).toHaveCount(0);
  await page.goto(origin + '/fleet/trailers/detail?id=missing');
  await expect(page.getByText('Trailer not found.', { exact: true })).toBeVisible();
  await page.goto(origin + '/fleet/inventory?tab=supplies');
  await expect(page.getByRole('checkbox').first()).toHaveJSProperty('disabled', true);
  await expect(page.getByRole('button', { name: 'Add Supply', exact: true })).toHaveCount(0);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: 'Collapse sidebar', exact: true }).click();
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  await page.screenshot({ path: join(tmpdir(), 'mserp-inventory-supplies-mobile.png'), animations: 'disabled' });
  await page.goto(origin + '/fleet/inventory?tab=phones');
  await expect(page.getByRole('cell', { name: '000000000000025', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Edit', exact: true })).toHaveCount(0);
  truckAccess = false;
  const readStart = reads.length;
  await page.goto(origin + '/fleet/inventory?tab=supplies');
  await expect(page.getByText('Truck access is required to view the supplies checklist.', { exact: true })).toBeVisible();
  assert.equal(reads.slice(readStart).includes('/api/lookups/trucks'), false);
  trailerOnly = true;
  trailers.push(...Array.from({ length: 30 }, (_, index) => record(1000 + index, { unitNumber: 'TR' + String(index + 1).padStart(3, '0'), make: 'Utility', status: index % 2 ? 'repair' : 'ready', condition: 'good', storageLocation: 'Main Yard' })));
  const trailerPage = await browser.newPage({ viewport: { width: 1600, height: 1000 } });
  const trailerReadStart = reads.length;
  await trailerPage.goto(origin + '/fleet/trailers');
  await expect(trailerPage.getByRole('link', { name: 'TR001', exact: true })).toBeVisible();
  await expect(trailerPage.getByText('Page 1 of 2', { exact: true })).toBeVisible();
  await trailerPage.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(trailerPage.getByRole('link', { name: 'TR026', exact: true })).toBeVisible();
  await trailerPage.getByRole('link', { name: 'TR026', exact: true }).click();
  await expect(trailerPage.getByRole('heading', { name: 'Trailer TR026', exact: true })).toBeVisible();
  await expect(trailerPage.getByRole('button', { name: 'Edit trailer', exact: true })).toHaveCount(0);
  await trailerPage.evaluate(async () => { await Promise.all(document.getAnimations().map(animation => animation.finished.catch(() => {}))); });
  await trailerPage.screenshot({ path: join(tmpdir(), 'mserp-trailer-detail-desktop.png'), animations: 'disabled' });
  await trailerPage.goBack();
  await expect(trailerPage.getByText('Page 2 of 2', { exact: true })).toBeVisible();
  await trailerPage.getByRole('button', { name: 'Filter Availability', exact: true }).click();
  await trailerPage.getByLabel('Trailer availability', { exact: true }).selectOption('ready');
  await trailerPage.keyboard.press('Escape');
  await expect(trailerPage.getByText('Page 1 of 1', { exact: true })).toBeVisible();
  await expect(trailerPage.getByRole('link', { name: 'TR002', exact: true })).toHaveCount(0);
  await expect(trailerPage.getByRole('link', { name: 'TR001', exact: true })).toBeVisible();
  await trailerPage.getByPlaceholder('Search trailers…').fill('does not exist');
  await expect(trailerPage.getByText('No trailers match your search or filters.', { exact: true })).toBeVisible();
  await trailerPage.getByPlaceholder('Search trailers…').fill('');
  await trailerPage.setViewportSize({ width: 390, height: 844 });
  await trailerPage.getByRole('button', { name: 'Collapse sidebar', exact: true }).click();
  assert.equal(await trailerPage.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  assert.equal(reads.slice(trailerReadStart).some(path => path === '/api/inventory' || path === '/api/lookups/trucks'), false);
  await trailerPage.close();
  assert.deepEqual(errors, []);
  console.log('Inventory base browser checks passed: phones, unified dashcams/brands, controlled statuses/colors, assignment installation rules, device-only truck equipment, company/other supplies checkboxes, conflicts, no stock/history UI, PrePass, trailers, read-only/access guards and mobile layout.');
} finally { await browser.close(); server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); }
