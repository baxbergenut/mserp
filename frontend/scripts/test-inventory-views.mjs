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
    record(25, { deviceType: 'phone', imei: '000000000000025', make: 'Apple', model: 'iPhone 15 Pro', custodian: 'Fleet Staff', condition: 'good', status: 'spare', equipmentState: 'unknown' }),
    record(26, { deviceType: 'other', serial: 'UNCLASSIFIED-DEVICE', truckId: id(100) }),
    record(28, { deviceType: 'samsara_gateway', serial: 'GATEWAY-STORAGE', equipmentState: 'unknown' }),
  ],
  lines: [
    record(30, { wirelessNumber: '9049217936', assetId: id(20), carrier: 'AT&T', status: 'active' }),
    record(31, { wirelessNumber: '9045550100', assetId: id(25), carrier: 'AT&T', status: 'active' }),
  ],
  locations: [record(1, { name: 'Main Storage' })], custodians: [record(2, { name: 'Inventory Staff' })],
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
const trailers = [], writes = [], reads = [], history = []; const operations = new Map();
let readOnly = false, failSave = false, truckAccess = true;
const prepass = { configured: true, environment: 'production', refreshedAt: stamp, accounts: ['123'], vehicles: [], transponders: [{ accountNumber: '123', transponderNumber: '777111222', vehicleNumber: '', vin: '', vrn: '', costCenter: '' }], operations: [], trucks: [] };
const server = createServer(async (request, response) => {
  const send = (body, code = 200) => { response.writeHead(code, { 'Content-Type': 'application/json' }); response.end(JSON.stringify(body)); };
  try {
    const requestURL = new URL(request.url, 'http://localhost'); const path = requestURL.pathname;
    if (path.startsWith('/api/')) {
      if (request.method === 'GET') reads.push(path);
      if (path === '/api/auth/session') return send({ user: { id: id(500), username: 'Fleet Reviewer', theme: 'default', permissions: ['inventory.read', 'trailers.read', 'tolls.read', ...(truckAccess ? ['trucks.read'] : []), ...(readOnly ? [] : ['inventory.manage', 'trailers.manage', 'inventory.import'])] }, csrfToken: 'fixture' });
      if (path === '/api/sidebar-counts') return send({ fuel: 0, weighMyTruck: 0, tolls: 0 });
      if (path === '/api/tasks/count') return send({ count: 0 });
      if (path === '/api/maintenance/access') return send({ canView: false, canCreate: false });
      if (path === '/api/lookups/trucks') return send(trucks);
      if (path === '/api/inventory' && request.method === 'GET') return send(data);
      if (path === '/api/inventory/directory') {
        const q=requestURL.searchParams, collection=q.get('collection');
        const custody=record=>{const d=record.data.gatewayId?data.assets.find(a=>a.id===record.data.gatewayId)?.data||{}:record.data;return d.truckId?'truck':d.locationId?'storage':d.custodianId||d.custodian?'person':'unknown';};
        const holder=record=>record.data.gatewayId?data.assets.find(a=>a.id===record.data.gatewayId)?.data||{}:record.data;
        let records=data[collection].filter(record=>{
          const d=record.data,h=holder(record),search=(q.get('search')||'').toLowerCase();
          if(collection==='assets'&&(q.get('deviceType')==='equipment'?(!h.truckId||d.deviceType==='other'):d.deviceType!==q.get('deviceType')))return false;
          if(q.get('active')==='record:active'&&!d.active||q.get('active')==='record:archived'&&d.active)return false;
          if(q.get('custody')&&custody(record)!==q.get('custody'))return false;
          if(q.get('brand')&&d.brand!==q.get('brand'))return false;
          if(q.get('condition')&&(d.condition||'unknown')!==q.get('condition'))return false;
          if(q.get('lifecycle')&&(d.status||'unknown')!==q.get('lifecycle'))return false;
          const line=data.lines.find(l=>l.data.assetId===record.id),number=line?.data.wirelessNumber||d.wirelessNumber||'';
          return !search||JSON.stringify(d).toLowerCase().includes(search)||number.includes(search.replace(/[^0-9]/g,'').replace(/^1(?=\d{10}$)/,''));
        });
        const total=records.length,size=+(q.get('pageSize')||25),page=Math.max(1,Math.min(+(q.get('page')||1),Math.ceil(total/size)||1));
        return send({records:records.slice((page-1)*size,page*size),context:data,total,page,pageSize:size});
      }
      if (path === '/api/trailers' && request.method === 'GET') return send(trailers);
      if (path === '/api/prepass-fleet') return send(prepass);
      if (path === '/api/inventory/imports') return send([]);
      if (path.includes('/history')) return send(history.filter(e=>e.recordId===requestURL.searchParams.get('id')));
      let raw = ''; for await (const chunk of request) raw += chunk;
      const input = raw ? JSON.parse(raw) : {};
      writes.push({ path, input });
      if (failSave) { failSave = false; return send({ error: 'Inventory changed; reload and review' }, 409); }
      if (path === '/api/inventory/operations') {
        const prior=operations.get(input.requestId);if(prior)return send(prior);
        for(const change of input.changes){const current=data[change.collection].find(r=>r.id===change.record.id);if(!current||current.version!==change.record.version)return send({error:'Inventory changed; reload and review'},409);}
        const result=input.changes.map(change=>{const rows=data[change.collection],index=rows.findIndex(r=>r.id===change.record.id),before=rows[index];const saved={...change.record,data:{...change.record.data},version:before.version+1,updatedAt:stamp,updatedBy:'Fleet Reviewer'};if(saved.data.equipmentState==='verified_installed'){saved.data.verifiedAt=stamp;saved.data.verifiedBy='Fleet Reviewer';}else{delete saved.data.verifiedAt;delete saved.data.verifiedBy;} rows[index]=saved;history.unshift({id:crypto.randomUUID(),collection:change.collection,recordId:saved.id,action:'transfer',before,after:saved,actor:'Fleet Reviewer',createdAt:stamp,reason:input.reason,effectiveDate:input.effectiveDate});return {...change,record:saved};});operations.set(input.requestId,result);return send(result);
      }
      if (request.method === 'PUT') {
        const collection = path.startsWith('/api/trailers/') ? 'trailers' : path.split('/')[3];
        const target = collection === 'trailers' ? trailers : data[collection];
        const index = target.findIndex(item => item.id === input.record.id);
        if (index >= 0 && target[index].version !== input.record.version) return send({ error: 'Inventory changed; reload and review' }, 409);
        const saved = { ...input.record, data: { ...input.record.data }, version: input.record.version + 1, updatedAt: stamp, updatedBy: 'Fleet Reviewer' };
        if (saved.data.equipmentState === 'verified_installed') { saved.data.verifiedAt = stamp; saved.data.verifiedBy = 'Fleet Reviewer'; }
        else { delete saved.data.verifiedAt; delete saved.data.verifiedBy; }
        const before=index<0?null:target[index];if(collection==='assets'&&before?.data.model!==saved.data.model)saved.data.originalModel=before?.data.originalModel||before?.data.model||'';
        history.unshift({id:crypto.randomUUID(),collection,recordId:saved.id,action:before?'edit':'create',before,after:saved,actor:'Fleet Reviewer',createdAt:stamp});
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
let failurePage;
try {
  const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } });
  failurePage=page;
  const errors = []; page.on('pageerror', error => errors.push(error.message));
  const origin = 'http://127.0.0.1:' + server.address().port;
  const setFilter=async(header,label,value)=>{await page.getByRole('button',{name:'Filter '+header,exact:true}).click();await page.getByLabel(label,{exact:true}).selectOption(value);await page.keyboard.press('Escape');};
  const search = () => page.getByPlaceholder('Search identifier, number, truck or notes…');
  const toolbarAligned = async () => {
    const centers = await page.getByRole('group', { name: 'Inventory controls', exact: true }).locator('input, select, button').evaluateAll(elements => elements.map(element => { const rect = element.getBoundingClientRect(); return rect.y + rect.height / 2; }));
    assert.ok(centers.length >= 3);
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
  await setFilter('Truck / Custody','Assignment Filter','unknown');
  await expect(page.getByRole('cell', { name: 'No matching records.', exact: true })).toHaveAttribute('colspan', '7');
  await setFilter('Truck / Custody','Assignment Filter','');
  await search().fill('+1 (904) 921-7936'); await expect(page.getByRole('button', { name: 'Samsung · Tab A9', exact: true })).toBeVisible(); await search().fill('');
  assert.equal(writes.length, 0);
  await page.getByRole('button', { name: 'Add Tablet', exact: true }).click();
  let dialog = page.getByRole('dialog');
  await expect(dialog.getByLabel('Installation Status').locator('option[value="verified_installed"]')).toHaveJSProperty('disabled', true);
  await expect(dialog.getByLabel('Installation Status').locator('option[value="reported_present"]')).toHaveJSProperty('disabled', true);
  assert.equal(await dialog.getByLabel('Lifecycle').evaluate(element => element.tagName), 'SELECT');
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
  await setFilter('Brand','Brand Filter','FiveELD');
  await expect(page.getByRole('cell', { name: 'CAMERA-022', exact: true })).toHaveCount(0);
  await setFilter('Brand','Brand Filter','');
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
  await expect(page.getByRole('button', { name: 'Filter Safety Vest', exact: true })).toHaveCount(2);
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

  // Structured attributes are reviewed before saving; filters survive tab switches.
  await tab('Tablets');
  await page.getByRole('button',{name:'Add Tablet',exact:true}).click();dialog=page.getByRole('dialog');
  await dialog.getByLabel('IMEI',{exact:true}).fill('000000000000004');
  await dialog.getByLabel('Make',{exact:true}).fill('Samsung');
  await dialog.getByLabel('Model',{exact:true}).fill('Galaxy Tab A7 Lite SM-T227U 32GB Gray');
  await dialog.getByLabel('Lifecycle',{exact:true}).selectOption('spare');
  await dialog.getByLabel('Condition',{exact:true}).selectOption('good');
  await dialog.getByLabel('Assign Device To').selectOption('locationId');
  await dialog.getByLabel('Storage Location',{exact:true}).selectOption(id(1));
  await dialog.getByRole('button',{name:'Save',exact:true}).click();await expect(dialog).toHaveCount(0);
  const spareId=writes.at(-1).input.record.id;
  let spareRow=page.getByRole('row').filter({hasText:'000000000000004'});
  await spareRow.getByRole('button',{name:'Edit',exact:true}).click();dialog=page.getByRole('dialog');
  const beforePreview=writes.length;
  await dialog.getByRole('button',{name:'Organize device details',exact:true}).click();
  await expect(dialog.getByText('SM-T227U',{exact:true})).toBeVisible();
  assert.equal(writes.length,beforePreview,'preview must not save');
  await dialog.getByRole('button',{name:'Use these details',exact:true}).click();
  await expect(dialog.getByLabel('Device Color')).toHaveValue('Gray');
  await expect(dialog.getByLabel('Storage (GB)')).toHaveValue('32');
  await dialog.getByLabel('Case Color').fill('Blue');
  await dialog.getByRole('button',{name:'Save',exact:true}).click();await expect(dialog).toHaveCount(0);
  assert.equal(data.assets.find(a=>a.id===spareId).data.originalModel,'Galaxy Tab A7 Lite SM-T227U 32GB Gray');
  await search().fill('000000000000004');await expect(page.getByRole('cell',{name:'000000000000004',exact:true})).toBeVisible();
  await tab('Phones');await tab('Tablets');await expect(search()).toHaveValue('000000000000004');
  await search().fill('');
  const historyReads=reads.filter(p=>p.includes('/history')).length;
  spareRow=page.getByRole('row').filter({hasText:'000000000000004'});
  await spareRow.getByRole('button').first().click();dialog=page.getByRole('dialog');
  assert.equal(reads.filter(p=>p.includes('/history')).length,historyReads);
  await dialog.getByRole('button',{name:'History',exact:true}).click();
  await expect(dialog.getByText(/Original Description:/).first()).toBeVisible();
  await dialog.getByRole('button',{name:'Close',exact:true}).last().click();
  // Atomic replacement retains the draft after failure and explicitly moves its line.
  await page.getByLabel('Actions for 000000000000002',{exact:true}).selectOption('replace');dialog=page.getByRole('dialog');
  await dialog.getByRole('checkbox').check();
  await dialog.getByLabel('Replacement device',{exact:true}).selectOption(spareId);
  await dialog.getByLabel('Select destination',{exact:true}).selectOption(id(1));
  await dialog.getByLabel('Reason',{exact:true}).fill('Replace damaged tablet');
  failSave=true;await dialog.getByRole('button',{name:'Save',exact:true}).click();
  await expect(dialog).toContainText('Inventory changed; reload and review');
  assert.equal(data.lines.find(l=>l.id===id(30)).data.assetId,tabletId);
  await expect(dialog.getByLabel('Reason',{exact:true})).toHaveValue('Replace damaged tablet');
  await dialog.getByRole('button',{name:'Save',exact:true}).click();await expect(dialog).toHaveCount(0);
  assert.equal(data.lines.find(l=>l.id===id(30)).data.assetId,spareId);
  assert.equal(data.assets.find(a=>a.id===spareId).data.truckId,id(101));
  assert.equal(data.assets.find(a=>a.id===tabletId).data.locationId,id(1));
  assert.equal(data.assets.find(a=>a.id===spareId).data.equipmentState,'unknown');
  await page.getByLabel('Actions for 000000000000004',{exact:true}).selectOption('verify');dialog=page.getByRole('dialog');
  await dialog.getByLabel('Reason',{exact:true}).fill('Physically checked on truck');
  await dialog.getByRole('button',{name:'Verify',exact:true}).click();await expect(dialog).toHaveCount(0);
  assert.equal(data.assets.find(a=>a.id===spareId).data.equipmentState,'verified_installed');
  // Review retains unclassified hardware and unknown-year observations.
  data.equipment.push(record(43,{name:'IFTA Sticker',truckId:id(100),equipmentState:'reported_present'}));
  await tab('Needs Attention');await expect(page.locator('tbody').getByText('Unclassified device',{exact:false}).first()).toBeVisible();
  await page.getByRole('row').filter({hasText:'UNCLASSIFIED-DEVICE'}).getByRole('button',{name:'Review',exact:true}).click();dialog=page.getByRole('dialog');
  await expect(dialog.getByLabel('Device Type')).toHaveValue('other');
  await dialog.getByRole('button',{name:'Cancel',exact:true}).click();
  await expect(page.getByRole('button',{name:'022 · IFTA Sticker · Year unknown',exact:true})).toBeVisible();
  await page.getByRole('button',{name:'022 · IFTA Sticker · Year unknown',exact:true}).click();dialog=page.getByRole('dialog');
  await expect(dialog).toContainText('Reported Present');await dialog.getByRole('button',{name:'Close',exact:true}).last().click();
  await page.screenshot({path:join(tmpdir(),'mserp-inventory-attention-desktop.png'),animations:'disabled'});
  await tab('Tablets');await expect(page.getByRole('cell',{name:'000000000000004',exact:true})).toBeVisible();await expect(page.getByRole('status',{name:'Loading…'})).toHaveCount(0);await page.screenshot({path:join(tmpdir(),'mserp-inventory-directory-desktop.png'),animations:'disabled'});
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
  failSave = true; await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).toContainText('Inventory changed; reload and review');
  await dialog.getByRole('button', { name: 'Save', exact: true }).click(); await expect(dialog).toHaveCount(0);
  await page.getByRole('row').filter({ hasText: 'MS003' }).getByRole('button', { name: 'Edit', exact: true }).click();
  dialog = page.getByRole('dialog'); await dialog.getByLabel('Active', { exact: true }).uncheck();
  await dialog.getByRole('button', { name: 'Save', exact: true }).click(); await expect(dialog).toHaveCount(0);
  await setFilter('Unit','Status Filter','record:archived');
  await expect(page.getByRole('button', { name: /MS003/ })).toBeVisible();

  readOnly = true;
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
  assert.deepEqual(errors, []);
  console.log('Inventory base browser checks passed: phones, unified dashcams/brands, controlled statuses/colors, assignment installation rules, device-only truck equipment, company/other supplies checkboxes, structured details, lazy history, atomic replacement/retry, verification, needs attention, conflicts, no stock UI, PrePass, trailers, read-only/access guards and mobile layout.');
} catch(error) { if(failurePage){await failurePage.screenshot({path:join(tmpdir(),'inventory-browser-failure.png')});console.error((await failurePage.locator('[role=dialog]').allTextContents()).join('\n'));} throw error; } finally { await browser.close(); server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); }
