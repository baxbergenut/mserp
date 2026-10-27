// Browser fixtures only: never contacts PrePass or production Clustie.
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';
import assert from 'node:assert/strict';
import { chromium, expect } from '@playwright/test';

const vin = '1XKADP9X8DJ364740', newVin = '1XKADP9X8DJ364741';
const stamp = '2026-10-12T12:00:00Z';
const baseVehicle = { accountNumber: '123', costCenter: '', vin, vehicleNumber: '003', tempPlate: false, licensePlateNumber: 'ABC123', licensePlateState: 'OH', declaredWeight: 80000, irpNumber: '12345', irpState: 'OH', iftaNumber: '67890', iftaState: 'OH', transponderNumber: '', vrn: '791', hazmatHauler: false, vehicleImage: '', leasedVehicle: false, leasingCompany: '', leaseExpirationDate: '' };
const makeEntry = v => ({ vehicle: v, truckId: '', truckUnit: '', tollsLinked: false, tollFrom: '', revision: 'a'.repeat(64), warning: 'Not linked to a Clustie truck', suggestions: ['truck-1'] });
const data = { configured: true, environment: 'production', refreshedAt: null, accounts: [], vehicles: [], transponders: [], operations: [], trucks: [
  { id: 'truck-1', unit: '003', vin, plate: 'ABC123', plateState: 'OH', active: true, updatedAt: stamp },
  { id: 'truck-2', unit: '004', vin: newVin, plate: 'DEF456', plateState: 'OH', active: true, updatedAt: stamp },
  { id: 'truck-3', unit: 'LONG UNIT 005', vin: '', plate: '', plateState: '', active: true, updatedAt: stamp },
  { id: 'truck-4', unit: '006', vin: 'INVALID', plate: '', plateState: '', active: true, updatedAt: stamp },
] };
const writes = []; let readOnly = false, reject = false, refreshFailure = false, noVendorEdit = false, finishAssignment;
const server = createServer(async (req, res) => {
  const send = (body, code = 200) => { res.writeHead(code, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(body)); };
  try {
    const url = new URL(req.url, 'http://localhost'), p = url.pathname.replace('/lookups/','/');
    if (p.startsWith('/api/')) {
      if (p === '/api/auth/session') return send({ user: { id: 'test-user', username: 'Fleet Reviewer', theme: 'default', permissions: ['tolls.read', 'drivers.read','trucks.read','dispatchers.read','updaters.read','investors.read', ...(readOnly ? [] : ['trucks.edit', 'tolls.link', 'tolls.fleet.create',...(noVendorEdit ? [] : ['tolls.fleet.edit']),'tolls.fleet.delete','tolls.fleet.recover','tolls.fleet.sync'])] }, csrfToken: 'fixture' });
      if (p === '/api/sidebar-counts') return send({ fuel: 0, weighMyTruck: 0, tolls: 0, tollsFrom: '2026-09-01' });
      if (p === '/api/maintenance/access') return send({ canView: false, canCreate: false, canEdit: false, canDelete: false });
      if (p === '/api/tasks/count') return send({ count: 0 });
      if (p === '/api/toll-dashboard') return send({ totals: { spend: 0, transactionCount: 0, truckCount: 0 }, monthly: [], weekly: [], trucks: [], states: [], agencies: [] });
      if (p === '/api/tolls') return send({ items: [], total: 0, page: 1, pageSize: 25, totalPages: 1, summary: { amount: 0, trucks: 0 }, options: { units: [], agencies: [] } });
      if (p === '/api/tolls/truck-links') return send({ dateFrom: '2026-09-01', transactions: [], groups: [], trucks: [], mappings: [] });
      if (p === '/api/prepass-fleet' && req.method === 'GET') return send(data);
      let raw = ''; for await (const c of req) raw += c;
      const input = raw ? JSON.parse(raw) : {};
      writes.push({ path: p, input });
      if (p === '/api/prepass-fleet/refresh') {
        if (refreshFailure) return send({ error: 'PrePass refresh failed. Previous roster retained.' }, 502);
        data.refreshedAt = stamp; data.accounts = ['123'];
        if (!data.vehicles.length) data.vehicles = [makeEntry({ ...baseVehicle })];
        if (!data.transponders.length) data.transponders = [
          { accountNumber: '123', transponderNumber: '777111222', vin: '', vehicleNumber: '', vrn: '', costCenter: '' },
          { accountNumber: '123', transponderNumber: '777111333', vin: 'ANOTHERVIN00000000', vehicleNumber: '099', vrn: '', costCenter: '' },
        ]; return send({ refreshed: true });
      }
      if (p === '/api/prepass-fleet/link/preview') return send({ count: 0, amount: '0.00', units: [], closed: false, revision: 'b'.repeat(64) });
      if (p.endsWith('/verify')) { data.operations[0].state = 'verified'; return send({ verified: true }); }
      if (p === '/api/prepass-fleet/change' || p === '/api/prepass-fleet/link') {
        if (reject) { reject = false; return send({ error: 'PrePass changed. Refresh and review before trying again.' }, 409); }
        let e = data.vehicles.find(e => e.vehicle.vin === input.vin);
        if (input.action === 'link') { e.tollsLinked = true; e.tollFrom = input.tollFrom; e.truckId = input.truckId; e.truckUnit = data.trucks.find(t => t.id === input.truckId).unit; e.warning = ''; }
        if (input.action === 'add') { e = makeEntry({ ...baseVehicle, vin: input.vin }); e.truckId = input.truckId; e.truckUnit = data.trucks.find(t => t.id === input.truckId).unit; e.warning = ''; data.vehicles.push(e); }
        if (input.plateSource) {
          const t = data.trucks.find(t => t.id === input.truckId);
          if (input.plateSource === 'mserp') { e.vehicle.licensePlateNumber = t.plate; e.vehicle.licensePlateState = t.plateState; }
          else { t.plate = e.vehicle.licensePlateNumber; t.plateState = e.vehicle.licensePlateState; }
        } else if (input.action === 'add' || input.action === 'edit') { e.vehicle.vehicleNumber = input.vehicle.vehicleNumber; e.vehicle.licensePlateNumber = input.vehicle.licensePlate.number; }
        if (input.action === 'assign') {
          await new Promise(resolve => { finishAssignment = resolve; });
          data.operations.push({ id: input.requestId, action: 'assign', vin: input.vin, accountNumber: input.accountNumber, transponderNumber: input.transponderNumber, actor: 'Fleet Reviewer', state: 'queued', createdAt: stamp });
        }
        if (input.action === 'remove') data.vehicles = data.vehicles.filter(e => e.vehicle.vin !== input.vin);
        return send({ saved: true });
      }
      return send({});
    }
    const file = p === '/' ? 'index.html' : extname(p) ? p.slice(1) : `${p.slice(1)}.html`;
    if (file.includes('..')) return send({}, 404);
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] || 'application/octet-stream' }); res.end(body);
  } catch (e) { send({ error: String(e) }, 500); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = []; page.on('pageerror', e => errors.push(e.message));
  await page.goto(`http://127.0.0.1:${server.address().port}/tolls`);
  await page.getByRole('tab', { name: 'Trucks', exact: true }).click();
  const region = page.getByRole('region', { name: 'PrePass management' });
  await expect(region.getByRole('button', { name: 'Add truck', exact: true })).toBeDisabled();
  assert.equal(writes.length, 0, 'opening tab must not contact vendor: '+JSON.stringify(writes));
  await region.getByRole('button', { name: 'Refresh PrePass' }).click();
  await expect(region.getByRole('cell', { name: vin, exact: true })).toBeVisible();
  await region.getByRole('button', { name: 'Link', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel(/^Truck/).selectOption('truck-1');
  await expect(dialog.getByRole('button', { name: 'Preview link', exact: true })).toBeDisabled();
  await dialog.getByLabel('Toll linking start date').fill('2026-10-01');
  await dialog.getByRole('button', { name: 'Preview link', exact: true }).click();
  await expect(dialog).toContainText('0 unlinked tolls');
  await dialog.getByLabel('Toll linking start date').fill('2026-10-02');
  await expect(dialog.getByRole('button', { name: 'Preview link', exact: true })).toBeVisible();
  await dialog.getByRole('button', { name: 'Preview link', exact: true }).click();
  await dialog.getByRole('button', { name: 'Link truck and tolls', exact: true }).click();
  await expect(dialog).toHaveCount(0); assert.equal(writes.at(-1).path, '/api/prepass-fleet/link');
  assert.equal(writes.at(-1).input.tollFrom, '2026-10-02');
  assert.equal(writes.at(-1).input.tollRevision, 'b'.repeat(64));
  await expect(region.getByRole('link', { name: '003', exact: true })).toHaveAttribute('href', '/trucks/detail?id=truck-1');
  for (const name of ['Account', 'Clustie truck', 'PrePass unit']) await expect(region.getByRole('columnheader', { name, exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Truck linking', exact: true })).toHaveCount(0);
  await expect(region.getByRole('tab', { name: 'Trucks', exact: true })).toHaveCount(0);
  await expect(region).not.toContainText('PrePass checked');
  await expect(region).not.toContainText('1 trucks');
  // Resolve a mismatch using each source, preserving the displayed truck version.
  data.trucks[0].plate = 'LOCAL12'; data.trucks[0].plateState = 'IN';
  await region.getByRole('button', { name: 'Refresh PrePass' }).click();
  await region.getByRole('button', { name: 'Resolve plate', exact: true }).click();
  await expect(dialog).toContainText('Clustie: LOCAL12 IN');
  await expect(dialog).toContainText('PrePass: ABC123 OH');
  await page.screenshot({ path: join(tmpdir(), 'mserp-prepass-resolve-plate.png'), animations: 'disabled' });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: join(tmpdir(), 'mserp-prepass-resolve-plate-mobile.png'), animations: 'disabled' });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, 'plate dialog must fit mobile');
  await page.setViewportSize({ width: 1440, height: 1000 });
  await expect(dialog.getByRole('button', { name: 'Resolve plate', exact: true })).toBeDisabled();
  await dialog.getByRole('radio', { name: 'Clustie: LOCAL12 IN' }).check();
  await dialog.getByRole('button', { name: 'Resolve plate', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  assert.equal(writes.at(-1).input.plateSource, 'mserp');
  assert.equal(writes.at(-1).input.truckUpdatedAt, stamp);
  assert.equal(writes.at(-1).input.vehicle, undefined);
  await expect(region.getByRole('button', { name: 'Resolve plate', exact: true })).toHaveCount(0);
  noVendorEdit = true;
  data.vehicles[0].vehicle.licensePlateNumber = 'REMOTE1';
  data.vehicles[0].vehicle.licensePlateState = 'IL';
  await page.reload();
  await expect(region).toBeVisible();
  await region.getByRole('button', { name: 'Refresh PrePass' }).click();
  await region.getByRole('button', { name: 'Resolve plate', exact: true }).click();
  await expect(dialog.getByRole('radio', { name: 'Clustie: LOCAL12 IN' })).toBeDisabled();
  await dialog.getByRole('radio', { name: 'PrePass: REMOTE1 IL' }).check();
  await expect(dialog.getByRole('checkbox')).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Resolve plate', exact: true })).toBeEnabled();
  await dialog.getByRole('button', { name: 'Resolve plate', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  assert.equal(writes.at(-1).input.plateSource, 'prepass');
  assert.equal(data.trucks[0].plate, 'REMOTE1');
  assert.equal(data.trucks[0].plateState, 'IL');
  assert.equal(writes.at(-1).input.fleetServiceConfirmed, undefined);
  noVendorEdit = false; await page.reload();
  await expect(region).toBeVisible();
  await region.getByRole('button', { name: 'Assign', exact: true }).click();
  await expect(dialog.getByLabel(/^Truck/)).toHaveValue(vin);
  await expect(dialog.getByLabel('Available transponder').locator('option')).toHaveCount(2);
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.getByRole('tab', { name: 'Transponders', exact: true }).click();
  await expect(page.getByRole('tab', { name: 'Transponders', exact: true })).toHaveAttribute('aria-selected', 'true');
  await region.getByRole('button', { name: 'Assign to truck', exact: true }).click();
  await expect(dialog.getByRole('button', { name: 'Assign transponder', exact: true })).toBeDisabled();
  await dialog.getByLabel(/^Truck/).selectOption(vin);
  const available = dialog.getByLabel('Available transponder');
  await expect(available.locator('option')).toHaveCount(2);
  await available.selectOption('777111222');
  await expect(dialog.getByRole('checkbox')).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Assign transponder', exact: true })).toBeEnabled();
  await dialog.getByRole('button', { name: 'Assign transponder', exact: true }).click();
  await expect(dialog.getByRole('button', { name: 'Assigning…', exact: true })).toBeDisabled();
  await expect(dialog.getByLabel('Available transponder')).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Cancel', exact: true })).toBeDisabled();
  assert.equal(writes.filter(w => w.input.action === 'assign').length, 1);
  finishAssignment();
  await expect(dialog).toHaveCount(0); assert.equal(writes.at(-1).input.transponderNumber, '777111222');
  await expect(region).toContainText('Waiting for PrePass to finish processing');
  await expect(region.getByRole('button', { name: 'Review result', exact: true })).toHaveCount(0);
  await expect(region.getByRole('cell', { name: 'Assigning...', exact: true })).toBeVisible();
  await page.getByRole('tab', { name: 'Trucks', exact: true }).click();
  await expect(region.getByRole('button', { name: 'Edit', exact: true })).toBeDisabled();
  await expect(region.getByRole('button', { name: 'Add truck', exact: true })).toBeEnabled();
  data.vehicles[0].vehicle.transponderNumber = '777111222';
  data.transponders[0].vin = vin; data.transponders[0].vehicleNumber = '003';
  data.operations[0].state = 'confirmed';
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(region).not.toContainText('Waiting for PrePass to finish processing');
  await expect(region).toContainText('Transponder assigned.');
  assert.equal(writes.filter(w => w.input.action === 'assign').length, 1);
  await page.getByRole('tab', { name: 'Trucks', exact: true }).click();
  await region.getByRole('button', { name: 'Edit', exact: true }).click();
  await dialog.getByLabel('Plate number', { exact: true }).fill('XYZ789');
  reject = true; await dialog.getByRole('button', { name: 'Edit PrePass truck', exact: true }).click();
  await expect(dialog).toContainText('PrePass changed'); await expect(dialog.getByLabel('Plate number', { exact: true })).toHaveValue('XYZ789');
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await region.getByRole('button', { name: 'Add truck', exact: true }).click();
  await expect(dialog.getByLabel(/^VIN/)).toHaveCount(0);
  await expect(dialog.getByLabel('Declared weight (lb)')).toHaveCount(0);
  await expect(dialog.getByLabel('PrePass account', { exact: true })).toHaveCount(0);
  await dialog.getByLabel(/^Truck/).selectOption('truck-4');
  await expect(dialog).toContainText('The saved VIN is invalid');
  await expect(dialog.getByRole('button', { name: 'Add truck', exact: true })).toBeDisabled();
  await dialog.getByLabel(/^Truck/).selectOption('truck-3');
  await expect(dialog.getByLabel(/^VIN/)).toBeVisible();
  await expect(dialog.getByLabel(/^PrePass unit number/)).toBeVisible();
  await expect(dialog.getByLabel('Plate number', { exact: true })).toBeVisible();
  await expect(dialog.getByLabel('Plate state', { exact: true })).toBeVisible();
  await dialog.getByLabel(/^VIN/).fill('1XKADP9X8DJ364742');
  await dialog.getByLabel('IRP account number').fill('11111');
  await dialog.getByLabel(/^Truck/).selectOption('truck-2');
  await expect(dialog.getByLabel(/^VIN/)).toHaveCount(0);
  await expect(dialog.getByLabel(/^PrePass unit number/)).not.toBeVisible();
  await expect(dialog.getByLabel('Plate number', { exact: true })).not.toBeVisible();
  await expect(dialog.getByLabel('Plate state', { exact: true })).not.toBeVisible();
  await expect(dialog.getByLabel('Saved truck details')).toContainText(newVin);
  await expect(dialog.getByLabel('IRP account number')).toHaveValue('');
  await expect(dialog.getByLabel('Declared weight (lb)')).toHaveValue('');
  data.trucks[1].updatedAt = '2026-10-12T12:01:00Z';
  const refreshedChoice = page.waitForResponse(r => r.url().endsWith('/api/prepass-fleet') && r.request().method() === 'GET');
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await refreshedChoice;
  await dialog.getByLabel('Declared weight (lb)').fill('80000');
  await dialog.getByLabel('IRP account number').fill('12345'); await dialog.getByLabel('IRP state').fill('OH');
  await dialog.getByLabel('IFTA account number').fill('67890'); await dialog.getByLabel('IFTA state').fill('OH');
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: join(tmpdir(), 'mserp-prepass-add-mobile.png'), animations: 'disabled' });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, 'mobile form overflow');
  await dialog.getByRole('button', { name: 'Add truck', exact: true }).click(); await expect(dialog).toHaveCount(0);
  assert.equal(writes.at(-1).input.vin, newVin); assert.equal(writes.at(-1).input.truckUpdatedAt, stamp);
  assert.equal(writes.at(-1).input.vehicle.vehicleNumber, '004');
  assert.deepEqual(writes.at(-1).input.vehicle.licensePlate, { temporary: false, number: 'DEF456', state: 'OH' });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.screenshot({ path: join(tmpdir(), 'mserp-prepass-trucks.png'), animations: 'disabled' });
  const newRow = region.getByRole('row').filter({ hasText: newVin });
  await newRow.getByRole('button', { name: 'Remove', exact: true }).click();
  await expect(dialog).toContainText('historical tolls are retained');
  await dialog.getByRole('button', { name: 'Remove from PrePass', exact: true }).click(); await expect(dialog).toHaveCount(0);
  assert.equal(writes.at(-1).input.action, 'remove');
  await region.getByRole('button', { name: 'Add truck', exact: true }).click();
  await dialog.getByLabel(/^Truck/).selectOption('truck-3');
  await dialog.getByLabel(/^VIN/).fill('1XKADP9X8DJ364742');
  await dialog.getByLabel(/^PrePass unit number/).fill('005');
  await dialog.getByLabel('Plate number', { exact: true }).fill('NEW005');
  await dialog.getByLabel('Plate state', { exact: true }).fill('OH');
  await dialog.getByLabel('Declared weight (lb)').fill('78000');
  await dialog.getByLabel('IRP account number').fill('12345'); await dialog.getByLabel('IRP state').fill('OH');
  await dialog.getByLabel('IFTA account number').fill('67890'); await dialog.getByLabel('IFTA state').fill('OH');
  await dialog.getByRole('button', { name: 'Add truck', exact: true }).click(); await expect(dialog).toHaveCount(0);
  assert.equal(writes.at(-1).input.vin, '1XKADP9X8DJ364742');
  assert.equal(writes.at(-1).input.vehicle.vehicleNumber, '005');
  assert.equal(writes.at(-1).input.vehicle.declaredWeight, 78000);
  refreshFailure = true; await region.getByRole('button', { name: 'Refresh PrePass' }).click();
  await expect(region).toContainText('Previous roster retained'); await expect(region.getByRole('cell', { name: vin, exact: true })).toBeVisible();
  data.operations = [{ id: 'operation-1', action: 'edit', vin, accountNumber: '123', actor: 'Fleet Reviewer', state: 'pending', createdAt: stamp }];
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(region.getByRole('button', { name: 'Add truck', exact: true })).toBeDisabled();
  await region.getByRole('button', { name: 'Review result', exact: true }).click();
  await dialog.getByLabel('I checked the outcome in PrePass.').check();
  await dialog.getByRole('button', { name: 'Record reviewed result' }).click(); await expect(dialog).toHaveCount(0);
  assert.equal(writes.at(-1).path, '/api/prepass-fleet/operations/operation-1/verify');
  await page.getByRole('tab', { name: 'Transponders', exact: true }).click();
  await expect(region.getByRole('cell', { name: '777111222', exact: true })).toBeVisible();
  await expect(region.getByRole('link', { name: '003', exact: true })).toHaveAttribute('href', '/trucks/detail?id=truck-1');
  await page.getByRole('tab', { name: 'Needs attention', exact: true }).click();
  const postedBox = await page.getByLabel('Attention tolls posted since').boundingBox();
  const refreshBox = await page.getByRole('button', { name: 'Refresh PrePass', exact: true }).boundingBox();
  assert.ok(Math.abs(postedBox.y - refreshBox.y) < 8, 'Posted since and Refresh PrePass share a toolbar');
  await page.getByRole('tab', { name: 'Transponders', exact: true }).click();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: 'Collapse sidebar', exact: true }).click();
  await region.getByRole('cell', { name: '777111222', exact: true }).scrollIntoViewIfNeeded();
  await page.screenshot({ path: join(tmpdir(), 'mserp-prepass-transponders-mobile.png'), animations: 'disabled' });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, 'table must scroll locally');
  readOnly = true; await page.reload();
  await expect(page.getByRole('region', { name: 'PrePass management' })).toBeVisible();
  await expect(region.getByRole('button', { name: 'Add truck', exact: true })).toHaveCount(0);
  await page.getByRole('tab', { name: 'Trucks', exact: true }).click();
  for (const name of ['Edit', 'Remove', 'Link', 'Relink', 'Assign', 'Resolve plate']) await expect(region.getByRole('button', { name, exact: true })).toHaveCount(0);
  await page.getByRole('tab', { name: 'Needs attention', exact: true }).click();

  await expect(region).toContainText('No toll transactions need attention in this date range.');
  assert.deepEqual(errors, []); console.log('PrePass browser workflows passed; screenshots saved in OS temp directory.');
} finally { await browser.close(); server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); }
