// Local UI fixtures only. No requests are sent to Relay or a production API.
// Build with NEXT_PUBLIC_API_URL=/api. --serve leaves fixtures open for manual QA.
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';

const localDriver = (id, name, status = 'active') => ({ id, name, status, phone: '5551234567', email: 'driver@example.test', truck: '101' });
const entry = (id, name, driverId, status, assignment) => ({
  driver: { id, first_name: name.split(' ')[0], last_name: name.split(' ').slice(1).join(' '), phone: '+15551234567', email: 'driver@example.test', integration_id: `123456789012345${id.length}`, updated_at: '2026-10-11T10:00:00Z', data_fields: [] },
  assignment, revision: 'a'.repeat(64), identityId: `identity-${id}`, driverId, driverName: driverId ? name : '', status,
  warning: status === 'terminated' ? 'Terminated driver is still in Relay.' : status === 'unlinked' ? 'Link this Relay account to check employment status.' : '', pending: false, suggestions: [],
});
const policies = ['MS Express Company - ELD', 'MS Express O/O - ELD', 'Non ELD Policy'].map((name, i) => ({ id: `plc_${i}`, name, eld_enabled: i !== 2, limits: [], authorization_prompts: [] }));
const assignment = { id: 'pa_one', policy_id: 'plc_0', policy_name: policies[0].name, enabled: true, limits: [] };
const data = { configured: true, canLink: true, environment: 'production', refreshedAt: new Date().toISOString(), policies,
  drivers: [localDriver('terminated', 'Terminated Driver', 'terminated'), localDriver('active', 'Active Driver'), localDriver('eld', 'ELD Driver'), localDriver('new', 'New Driver')],
  operations: [], items: [entry('dr_terminated', 'Terminated Driver', 'terminated', 'terminated', { ...assignment }), entry('dr_active', 'Active Driver', 'active', 'active', { ...assignment }), entry('dr_eld', 'ELD Driver', '', 'unlinked', null)] };
data.items[2].suggestions = [{ driverId: 'eld', name: 'ELD Driver', active: true, reasons: ['Same phone'] }];
const tollLinks={dateFrom:'2026-09-01',groups:[{environment:'production',identityKey:'tag:777780673',units:['01','UNASSIGNED DF28HU'],devices:['00413749983','DF28HU'],count:41,amount:'123.45',firstExit:'2026-09-01',lastExit:'2026-10-05'}],trucks:[{id:'truck-01',unit:'01.',vin:'4V4NC9EG4NN297330',plate:'DF28HU'},{id:'truck-001',unit:'001',vin:'3AKJHHDR3MSMN8492',plate:'C329FH'}],mappings:[]};
const originalGroup=structuredClone(tollLinks.groups[0]);
const toll = (id, truckId = null, flag) => ({ id, truckId, truckUnit: truckId ? '001' : '01', postingDate: '2026-10-05', invoiceDate: '2026-10-05', exitDate: '2026-10-01', exitTime: '12:00:00', equipmentUnit: '01', prePassTagId: '777780673', transponderOrPlate: '00413749983', readType: 'DEVICE', amount: 2.5, agency: 'NJ', exitPlaza: 'Exit Plaza', entryPlaza:null, miles:null, tollClass:'5', flag });
tollLinks.transactions = [toll('toll-one'), toll('toll-flagged', 'truck-001', {status:'review', reason:'No recorded truck load covers this transaction.', truckUnit:'001', transactionDate:'2026-10-01', bufferDays:1}), toll('toll-unselected')];
Object.assign(tollLinks.transactions[1],{reviewRevision:'c'.repeat(64),reviewVersion:'',reviewed:false});
const originalFlaggedToll=structuredClone(tollLinks.transactions[1]);
tollLinks.mappings = [{ id:'existing-rule', environment:'production', identityKey:'tag:777780673', truckId:'truck-01', unit:'01.', from:'2026-09-01', to:'', count:41, version:1 }];
const writes = [];
let readOnly = false;
let tollReadOnly = false;
let bulkReviewFailure = false, holdBulkReview = false, finishBulkReview;
const server = createServer(async (req, res) => {
  try {
    const url = new URL(req.url, 'http://localhost'); const path = url.pathname.replace('/lookups/','/');
    const send = (body, status = 200) => { res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(body)); };
    if (path.startsWith('/api/')) {
      if (path === '/api/auth/session') return send({ user: { id: 'relay-fixture-user', username: 'Relay test', theme: 'default', permissions: ['fuel.read', 'drivers.read','trucks.read','dispatchers.read','updaters.read','investors.read', ...(readOnly ? [] : ['fuel.drivers.create','fuel.drivers.edit','fuel.drivers.delete','fuel.drivers.recover','fuel.drivers.sync', 'tasks.create','tasks.edit','tasks.delete','tasks.review', 'tolls.read', ...(tollReadOnly ? [] : ['tolls.link']), 'weighmytruck.read'])] }, csrfToken: 'fixture' });
      if (path === '/api/sidebar-counts') return send({ fuel: data.items.filter(e => e.status === 'terminated').length, weighMyTruck: 2, tolls: 125, tollsFrom: '2026-09-01' });
      if(path==='/api/feedback/messages/count')return send({count:0});
      if(path==='/api/prepass-fleet')return send({configured:true,accounts:[],vehicles:[],transponders:[],trucks:[],operations:[]});
      if(path==='/api/tolls/truck-links'&&req.method==='GET')return send({...tollLinks,transactions:tollLinks.transactions.filter(t=>!t.reviewed||!t.truckId||url.searchParams.get('includeReviewed')==='true')});
      if(path==='/api/tolls')return send({items:[toll('transaction-view')],total:1,totalPages:1,page:1,pageSize:25,options:{units:[],agencies:[]},summary:{amount:2.5,truckCount:0}});
      if(path==='/api/toll-dashboard')return send({error:'Outside fixture'},503);
      if (path === '/api/fuel-dashboard') return send({ error: 'Overview is outside this local driver fixture.' }, 503);
      if (path === '/api/fuel-transactions') return send({ items: [], total: 0, totalPages: 1, page: 1, pageSize: 25, options: { drivers: [], states: [] }, summary: { spend: 0, saved: 0, gallons: 0 } });
      if (path === '/api/relay-drivers' && req.method === 'GET') return send({ ...data, canLink: !readOnly });
      if (path === '/api/relay-drivers/refresh') { data.refreshedAt = new Date().toISOString(); return send({ refreshed: true }); }
      let body = ''; for await (const chunk of req) body += chunk; const input = body ? JSON.parse(body) : {};
      if(path==='/api/tolls/reviews/bulk'){
        writes.push(input);
        if(bulkReviewFailure){bulkReviewFailure=false;return send({error:'A selected flag changed. Refresh the selection.'},409)}
        if(holdBulkReview)await new Promise(resolve=>{finishBulkReview=resolve});
        for(const change of input.changes){const row=tollLinks.transactions.find(t=>t.id===change.tollId);Object.assign(row,{reviewed:true,reviewVersion:change.id,reviewedBy:'Fixture reviewer',reviewedAt:'2026-10-27T12:00:00Z'})}
        return send({reviewed:input.changes.length});
      }
      if(path==='/api/tolls/toll-flagged/review'){writes.push(input);const row=tollLinks.transactions.find(t=>t.id==='toll-flagged');if(input.version!==row.reviewVersion)return send({error:'Review changed.'},409);Object.assign(row,{reviewed:input.reviewed,reviewVersion:input.id,reviewedBy:'Fixture reviewer',reviewedAt:'2026-10-27T12:00:00Z'});return send({saved:true})}
      if(path==='/api/relay-drivers/bulk'){
       writes.push(input);const results=input.changes.map(change=>{const e=data.items.find(e=>e.driver.id===change.relayDriverId);if(!e.assignment)return {relayDriverId:e.driver.id,error:'This driver has no policy assignment.'};e.assignment.enabled=false;return {relayDriverId:e.driver.id}});return send({results});
      }
      if(path==='/api/tolls/truck-links/preview'){writes.push(input);return send({count:input.transactionIds.length,amount:'5.00',units:originalGroup.units,revision:'b'.repeat(64),closed:false})}
      if(path==='/api/tolls/truck-links'&&req.method==='POST'){writes.push(input);tollLinks.transactions=tollLinks.transactions.filter(t=>!input.transactionIds.includes(t.id));return send({saved:true})}
      if(path.startsWith('/api/tolls/truck-links/')&&path.endsWith('/undo')){writes.push(input);tollLinks.groups=[structuredClone(originalGroup)];tollLinks.mappings=[];return send({saved:true})}
      if (path.endsWith('/link')) { const e = data.items.find(e => e.driver.id === path.split('/')[3]); const d = data.drivers.find(d => d.id === input.driverId); Object.assign(e, { driverId: d.id, driverName: d.name, status: d.status, warning: '' }); writes.push(input); return send({ transactionsLinked: 2 }); }
      if (path === '/api/relay-drivers/change') {
        writes.push(input); const e = data.items.find(e => e.driver.id === input.relayDriverId);
        if (input.action === 'disable' || input.action === 'enable') e.assignment.enabled = input.action === 'enable';
        if (input.action === 'unassign') e.assignment = null;
        if (input.action === 'assign') { const p = policies.find(p => p.id === input.policyId); e.assignment = { ...assignment, policy_id: p.id, policy_name: p.name }; }
        if (input.action === 'remove') data.items = data.items.filter(row => row !== e);
        if (input.action === 'edit') Object.assign(e.driver, { first_name: input.firstName, last_name: input.lastName, email: input.email });
        if (input.action === 'add') data.items.push(entry('dr_new', `${input.firstName} ${input.lastName}`, input.driverId, 'active', null));
        return send({ saved: true });
      }
      return send({});
    }
    const file = path === '/' ? 'index.html' : extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    if (file.includes('..')) { res.writeHead(404); res.end(); return; }
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] ?? 'application/octet-stream' }); res.end(body);
  } catch { res.writeHead(500); res.end(); }
});
await new Promise(resolve => server.listen(process.argv.includes('--serve') ? 55477 : 0, '127.0.0.1', resolve));
const base = `http://127.0.0.1:${server.address().port}`;
if (process.argv.includes('--serve')) console.log(`Relay fixture preview: ${base}/fuel`);
else {
  const { chromium, expect } = await import('@playwright/test');
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    const errors = []; page.on('pageerror', e => errors.push(e.message));
    await page.goto(`${base}/fuel`);
    await page.getByRole('tab', { name: 'Drivers', exact: true }).click();
    await expect(page.getByRole('columnheader', { name: 'Relay Driver ID' })).toBeVisible();
    const rows = page.locator('tbody tr');
    await expect(rows.first()).toContainText('Terminated Driver');
    await expect(rows.first().getByLabel('Terminated driver is still in Relay.')).toBeVisible();
    const sidebar = page.locator('aside');
    await expect(sidebar.getByLabel('1 terminated drivers still in Relay', { exact: true })).toHaveText('1');
    await expect(sidebar.getByLabel('2 terminated drivers with confirmed or unresolved WeighMyTruck access', { exact: true })).toHaveText('2');
    const tollBadge = sidebar.getByLabel('125 toll transactions without a linked truck, posted since 2026-09-01', { exact: true });
    await expect(tollBadge).toHaveText('99+');
    await expect(tollBadge).toHaveAttribute('title', /2026-09-01/);
    await page.getByLabel('Driver status', { exact: true }).selectOption('no_policy');
    await expect(rows).toHaveCount(1); await expect(rows.first()).toContainText('ELD Driver');
    await page.getByRole('button', { name: 'Link driver', exact: true }).click();
    await page.getByRole('button', { name: 'ELD Driver · Same phone' }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Link driver', exact: true }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await page.getByLabel('Actions for ELD Driver').selectOption('assign');
    await page.getByRole('dialog').getByLabel('Policy', { exact: false }).selectOption('plc_2');
    await expect(page.getByRole('dialog').getByLabel('Driver Pin', { exact: false })).toHaveAttribute('type', 'password');
    await expect(page.getByRole('dialog').getByLabel('Truck #', { exact: false })).toHaveValue('101');
    await expect(page.getByRole('dialog').getByRole('option', { name: /Flinn/ })).toHaveCount(0);
    await page.getByRole('dialog').getByLabel('Driver Pin', { exact: false }).fill('1234');
    await page.getByRole('dialog').getByRole('button', { name: 'Assign policy', exact: true }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    if (writes.at(-1).driverPin !== '1234' || writes.at(-1).truckNumber !== '101') throw new Error('Non ELD fields not sent');
    await page.getByLabel('Driver status', { exact: true }).selectOption('');
    await page.getByLabel('Actions for Terminated Driver').selectOption('disable');
    await expect(page.getByRole('dialog')).toContainText('usage counters are preserved');
    await page.getByRole('dialog').getByRole('button', { name: 'Disable policy', exact: true }).click();
    await expect(rows.first()).toContainText('Disabled');
    await page.getByRole('button', { name: 'Add driver', exact: true }).click();
    await page.getByRole('dialog').getByLabel('Active Clustie driver').selectOption('new');
    await expect(page.getByRole('dialog').getByLabel('First name')).toHaveValue('New');
    await expect(page.getByRole('dialog').getByRole('option', { name: /Terminated/ })).toHaveCount(0);
    await page.getByRole('dialog').getByRole('button', { name: 'Add driver', exact: true }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect(page.getByLabel('Actions for New Driver')).toBeVisible();
    await page.getByLabel('Actions for Active Driver').selectOption('assign');
    await page.getByRole('dialog').getByLabel('Policy', { exact: false }).selectOption('plc_1');
    await expect(page.getByRole('dialog')).toContainText('existing Relay ELD connection');
    await expect(page.getByRole('dialog')).toContainText('resets usage counters');
    await expect(page.getByRole('dialog').getByLabel('Driver Pin', { exact: false })).toHaveCount(0);
    await page.getByRole('dialog').getByRole('button', { name: 'Cancel', exact: true }).click();
    await page.getByRole('checkbox',{name:'Select Active Driver',exact:true}).check();
    await page.getByRole('checkbox',{name:'Select New Driver',exact:true}).check();
    await page.getByLabel('Action for selected Relay drivers').selectOption('disable');
    await expect(page.getByRole('dialog')).toContainText('2 drivers');
    await page.getByRole('dialog').getByRole('button',{name:'Disable policy',exact:true}).click();
    await expect(page.getByRole('dialog')).toContainText('1 completed · 1 need review');
    await expect(page.getByRole('dialog')).toContainText('New Driver: This driver has no policy assignment.');
    if(writes.at(-1).changes.length!==2 || new Set(writes.at(-1).changes.map(v=>v.requestId)).size!==2)throw new Error('Bulk receipts missing');
    await page.getByRole('dialog').getByRole('button',{name:'Done',exact:true}).click();
    await expect(page.getByText('1 selected',{exact:true})).toBeVisible();
    await page.getByRole('button',{name:'Clear selection',exact:true}).click();
    await page.screenshot({ path: join(tmpdir(), 'mserp-relay-desktop.png'), fullPage: true });
    await page.getByRole('button', { name: 'Collapse sidebar', exact: true }).click();
    const costBadge = sidebar.locator('a[title="Operating Costs"] span[aria-label]');
    await expect(costBadge).toHaveText('99+');
    await expect(costBadge).toHaveClass(/absolute/);
    await expect(costBadge).toHaveAttribute('aria-label', /125 toll transactions without a linked truck/);
    await page.setViewportSize({ width: 390, height: 844 });
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await expect.poll(() => page.locator('main').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
    await page.screenshot({ path: join(tmpdir(), 'mserp-relay-narrow.png'), fullPage: true });
    await page.reload(); await expect(page.getByRole('columnheader', { name: 'Relay Driver ID' })).toBeVisible();
    await page.getByLabel('Actions for Terminated Driver').selectOption('remove');
    await page.getByRole('dialog').getByRole('button', { name: 'Remove driver', exact: true }).click();
    await expect(sidebar.getByLabel('1 terminated drivers still in Relay', { exact: true })).toHaveCount(0);
    readOnly = true; await page.reload();
    await expect(sidebar.getByRole('link', { name: /WeighMyTruck/ })).toHaveCount(0);
    await expect(sidebar.getByRole('link', { name: /Tolls/ })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Add driver', exact: true })).toHaveCount(0);
    await expect(page.getByLabel('Actions for Active Driver')).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Refresh Relay', exact: true })).toHaveCount(0);
    await expect(page.getByRole('checkbox',{name:'Select drivers on this page'})).toHaveCount(0);
    readOnly=false;
    await page.setViewportSize({width:1440,height:1000});
    await page.goto(base+'/tolls');
    await page.getByRole('tab',{name:'Needs attention',exact:true}).click();
    await expect(page.getByRole('button',{name:'Link truck',exact:true})).toHaveCount(3);
    await expect(page.getByText('Flagged',{exact:true})).toBeVisible();
    await page.screenshot({path:join(tmpdir(),'mserp-toll-attention-desktop.png'),fullPage:true});
    await expect(page.getByLabel('Attention tolls posted since')).toHaveValue('2026-09-01');
    await page.getByRole('button',{name:'Review transaction: No recorded truck load covers this transaction.'}).click();
    await expect(page.getByRole('dialog',{name:'Review transaction',exact:true})).toContainText('No recorded truck load');
    await page.getByRole('dialog',{name:'Review transaction',exact:true}).getByLabel('Close',{exact:true}).click();
    await expect(page.getByText('Check data',{exact:true})).toHaveCount(0);
    await page.getByRole('button',{name:'Mark reviewed',exact:true}).click();
    await expect(page.getByLabel('Select transaction toll-flagged',{exact:true})).toHaveCount(0);
    if(writes.at(-1).reviewed!==true||writes.at(-1).revision!=='c'.repeat(64))throw new Error('Reviewed flag snapshot not sent');
    await page.reload();await expect(page.getByLabel('Select transaction toll-flagged',{exact:true})).toHaveCount(0);
    await page.getByRole('checkbox',{name:'Show reviewed',exact:true}).check();
    await expect(page.getByText('Reviewed',{exact:true})).toBeVisible();
    await page.getByRole('button',{name:'Reopen',exact:true}).click();
    await expect(page.getByRole('button',{name:'Mark reviewed',exact:true})).toBeVisible();
    await page.getByRole('checkbox',{name:'Show reviewed',exact:true}).uncheck();
    await expect(page.getByRole('button',{name:'Mark reviewed',exact:true})).toBeVisible();
    tollLinks.transactions.push(...['bulk-one','bulk-two','bulk-reviewed'].map(id=>({...structuredClone(originalFlaggedToll),id,reviewed:id==='bulk-reviewed'})));
    await page.getByRole('checkbox',{name:'Show reviewed',exact:true}).check();
    for(const id of ['bulk-one','bulk-two','bulk-reviewed','toll-one'])await page.getByLabel(`Select transaction ${id}`,{exact:true}).check();
    await expect(page.getByText('4 selected · 2 flagged',{exact:true})).toBeVisible();
    bulkReviewFailure=true;
    await page.getByRole('button',{name:'Mark selected reviewed',exact:true}).click();
    await expect(page.getByText('A selected flag changed. Refresh the selection.',{exact:true})).toBeVisible();
    await expect(page.getByLabel('Select transaction bulk-one',{exact:true})).toBeChecked();
    holdBulkReview=true;
    await page.getByRole('button',{name:'Mark selected reviewed',exact:true}).click();
    await expect(page.getByRole('button',{name:'Mark selected reviewed',exact:true})).toBeDisabled();
    await expect(page.getByLabel('Select transaction toll-one',{exact:true})).toBeDisabled();
    await expect.poll(()=>typeof finishBulkReview).toBe('function');finishBulkReview();holdBulkReview=false;
    await expect(page.getByText('2 transactions marked reviewed.',{exact:true})).toBeVisible();
    if(writes.at(-1).changes.map(t=>t.tollId).join(',')!=='bulk-one,bulk-two'||new Set(writes.at(-1).changes.map(t=>t.id)).size!==2)throw new Error('Bulk review included unlinked/reviewed rows or omitted request receipts');
    await expect(page.getByLabel('Select transaction toll-one',{exact:true})).toBeChecked();
    await expect(page.getByLabel('Select transaction bulk-one',{exact:true})).not.toBeChecked();
    await page.screenshot({path:join(tmpdir(),'mserp-toll-bulk-reviewed.png'),fullPage:true});
    tollLinks.transactions=tollLinks.transactions.filter(t=>!t.id.startsWith('bulk-'));
    await page.getByRole('checkbox',{name:'Show reviewed',exact:true}).uncheck();
    await expect(page.getByLabel('Select transaction bulk-one',{exact:true})).toHaveCount(0);
    await expect(page.getByLabel('Select transaction toll-one',{exact:true})).not.toBeChecked();
    await page.getByRole('button',{name:'Link truck',exact:true}).first().click();
    await expect(page.getByRole('dialog')).toContainText('Only the selected transactions');
    await page.getByRole('dialog').getByRole('button',{name:'Cancel',exact:true}).click();
    await page.getByLabel('Select transaction toll-one',{exact:true}).check();
    await page.getByLabel('Select transaction toll-flagged',{exact:true}).check();
    await page.getByRole('button',{name:'Link selected',exact:true}).click();
    await page.getByRole('dialog').getByLabel(/^Truck/).selectOption('truck-01');
    await expect(page.getByRole('dialog')).toContainText('4V4NC9EG4NN297330');
    await page.getByRole('dialog').getByRole('button',{name:'Preview selected tolls',exact:true}).click();
    await expect(page.getByRole('dialog')).toContainText('2 tolls');
    await page.getByRole('dialog').getByLabel(/^Truck/).selectOption('truck-001');
    await expect(page.getByRole('dialog').getByRole('button',{name:'Preview selected tolls',exact:true})).toBeVisible();
    await page.getByRole('dialog').getByLabel(/^Truck/).selectOption('truck-01');
    await page.getByRole('dialog').getByRole('button',{name:'Preview selected tolls',exact:true}).click();
    await page.getByRole('dialog').getByRole('button',{name:'Link transactions',exact:true}).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect(page.getByRole('heading',{name:'Saved truck mappings'})).toBeVisible();
    if(writes.at(-1).revision!=='b'.repeat(64)||writes.at(-1).truckId!=='truck-01'||writes.at(-1).transactionIds.join(',')!=='toll-one,toll-flagged')throw new Error('Exact reviewed selection not sent');
    await expect(page.getByRole('button',{name:'Link truck',exact:true})).toHaveCount(1);
    await expect(page.getByLabel('Select transaction toll-unselected',{exact:true})).toBeVisible();
    await page.getByRole('button',{name:'Undo mapping',exact:true}).click();
    await expect(page.getByRole('dialog')).toContainText('including later imports');
    await page.getByRole('dialog').getByRole('button',{name:'Undo mapping',exact:true}).click();
    await expect(page.getByRole('button',{name:'Link truck',exact:true})).toBeVisible();
    await page.screenshot({path:join(tmpdir(),'mserp-prepass-trucks.png'),fullPage:true});
    await page.setViewportSize({width:390,height:844});
    await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
    tollLinks.transactions.push(structuredClone(originalFlaggedToll));
    tollReadOnly=true; await page.reload();
    await expect(page.getByText('Flagged',{exact:true})).toBeVisible();
    await expect(page.getByRole('button',{name:'Link truck',exact:true})).toHaveCount(0);
    await expect(page.getByRole('button',{name:'Mark reviewed',exact:true})).toHaveCount(0);
    await expect(page.getByRole('checkbox',{name:'Select transactions on this page'})).toHaveCount(0);
    await page.getByRole('tab',{name:'Transactions',exact:true}).click();
    await expect(page.getByRole('cell').filter({hasText:'Transponder 777780673'})).toBeVisible();
    await expect(page.getByText('DEVICE',{exact:true})).toHaveCount(0);
    if (errors.length) throw new Error(errors.join('\n'));
    console.log('Relay and PrePass UI checks passed: driver linking/policies, individual unlinked and flagged tolls, exact bulk selection, preview invalidation, mapping undo, transponder numbers, read-only permissions and narrow layouts.');
  } finally { await browser.close(); server.close(); }
}
