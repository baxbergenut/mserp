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
const writes = [];
let readOnly = false;
const server = createServer(async (req, res) => {
  try {
    const url = new URL(req.url, 'http://localhost'); const path = url.pathname;
    const send = (body, status = 200) => { res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(body)); };
    if (path.startsWith('/api/')) {
      if (path === '/api/auth/session') return send({ user: { id: 'relay-fixture-user', username: 'Relay test', theme: 'default', permissions: ['fuel.read', 'fleet.read', ...(readOnly ? [] : ['fuel.drivers.write', 'tasks.write', 'tolls.read', 'weighmytruck.read'])] }, csrfToken: 'fixture' });
      if (path === '/api/sidebar-counts') return send({ fuel: data.items.filter(e => e.status === 'terminated').length, weighMyTruck: 2, tolls: 125, tollsFrom: '2026-09-01' });
      if (path === '/api/fuel-dashboard') return send({ error: 'Overview is outside this local driver fixture.' }, 503);
      if (path === '/api/fuel-transactions') return send({ items: [], total: 0, totalPages: 1, page: 1, pageSize: 25, options: { drivers: [], states: [] }, summary: { spend: 0, saved: 0, gallons: 0 } });
      if (path === '/api/relay-drivers' && req.method === 'GET') return send({ ...data, canLink: !readOnly });
      if (path === '/api/relay-drivers/refresh') { data.refreshedAt = new Date().toISOString(); return send({ refreshed: true }); }
      let body = ''; for await (const chunk of req) body += chunk; const input = body ? JSON.parse(body) : {};
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
    await page.getByRole('button', { name: 'Drivers', exact: true }).click();
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
    await page.getByRole('dialog').getByLabel('Active MSERP driver').selectOption('new');
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
    await page.screenshot({ path: join(tmpdir(), 'mserp-relay-desktop.png'), fullPage: true });
    await page.getByRole('button', { name: 'Collapse sidebar', exact: true }).click();
    await expect(tollBadge).toBeVisible();
    await expect(tollBadge).toHaveClass(/absolute/);
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
    await expect(page.getByRole('button', { name: 'Refresh Relay', exact: true })).toBeVisible();
    if (errors.length) throw new Error(errors.join('\n'));
    console.log('Relay driver UI checks passed: linking, policy fields, usage warning, termination flags, add, read-only permissions, tab memory and narrow layout.');
  } finally { await browser.close(); server.close(); }
}
