// Browser regression checks against the static build, using isolated API fixtures.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';

const week = '2026-10-05';
const driver = { id: 'driver', fullName: 'Test Driver', active: true, driverType: 'Company', phone: '' };
const type = { id: 'fee', name: 'Admin fee', amount: '50.00', amounts: ['50.00'], archived: false, version: 1, eligibility: 'calendar', rules: [] };
let trucks = [];
const truckCharges = { terms: [], phases: [], eligibleTruckIds: [], lockedCharges: [] };
const truckWrites = [];
const termWrites = [];
let datedOwners = false;
let readOnly = false;
let renamedOwner = 'Felix & Den';
const bulkWrites = [];
let bulkFailure = '';
let holdBulk = false;
let releaseBulk;
const archivedType = { ...type, id: 'archived', name: 'Archived fee', archived: true };
const extraTypes = Array.from({ length: 6 }, (_, index) => ({ ...type, id: `fee-${index}`, name: `Other fee ${index}` }));
const data = { currentWeek: week, types: [type, ...extraTypes, archivedType], schedules: [] };
const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = path === '/' ? 'index.html' : extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] ?? 'application/octet-stream' });
    res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const base = `http://127.0.0.1:${server.address().port}`;
let browser;
try {
  browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url());
    const path = url.pathname.replace('/lookups/','/').slice(4);
    const saveTruck = input => {
      truckWrites.push(input);
      const existing = truckCharges.phases.find(p => p.truckId === input.truckId && p.typeId === input.typeId && p.weekStart === input.weekStart);
      expect(input.version).toBe(existing?.version ?? 0);
      truckCharges.phases = [...truckCharges.phases.filter(p => p !== existing), { ...input, version: input.version + 1 }];
    };
    if (route.request().method() === 'PUT' && path === '/truck-charges/recurring/bulk') {
      const { updates } = route.request().postDataJSON();
      bulkWrites.push(updates);
      if (holdBulk) await new Promise(resolve => { releaseBulk = resolve; });
      const results = updates.map(input => {
        const error = input.truckId === bulkFailure ? 'Reopen affected finalized settlements before changing truck terms or charges' : undefined;
        if (!error) saveTruck(input);
        return { truckId: input.truckId, typeId: input.typeId, weekStart: input.weekStart, error };
      });
      await route.fulfill({ json: results }); return;
    }
    if (route.request().method() === 'PUT' && path === '/truck-charges/recurring') {
      saveTruck(route.request().postDataJSON());
      await route.fulfill({ status: 204 }); return;
    }
    if (route.request().method() === 'PUT' && path === '/truck-charges/terms') {
      termWrites.push(route.request().postDataJSON());
      await route.fulfill({ status: 204 }); return;
    }
    if (route.request().method() === 'DELETE' && path.startsWith('/driver-charges/types/')) {
      const id = path.split('/').at(-1);
      expect(route.request().postDataJSON()).toEqual({ version: 1 });
      data.types = data.types.filter(t => t.id !== id);
      await route.fulfill({ status: 204 }); return;
    }
    const fixtures = {
      '/auth/session': { user: { id: 'user', username: 'Test user', permissions: readOnly ? ['charges.read'] : ['charges.read', 'charges.create','charges.edit','charges.delete','investor_terms.read','investor_terms.edit', 'drivers.read','trucks.read','dispatchers.read','updaters.read','investors.read'] }, csrfToken: 'fixture' },
      '/driver-charges': url.searchParams.has('driverId') ? { ...data, schedules: data.schedules.filter(s => s.driverId === url.searchParams.get('driverId')) } : data,
      '/drivers': [driver], '/truck-charges': {
        ...truckCharges,
        eligibleTruckIds: [...truckCharges.eligibleTruckIds, ...(url.searchParams.get('weekStart') < week ? ['terminated-truck'] : [])],
        ...(datedOwners ? { owners: trucks.map(t => ({ truckId: t.id, ownerId: t.id === 'truck-a' && url.searchParams.get('weekStart') >= week ? 'new-owner' : t.ownerId, ownerName: t.id === 'truck-a' && url.searchParams.get('weekStart') >= week ? renamedOwner : t.ownerName })) } : {}),
      },
      '/trucks': trucks, '/investors': [{ id: 'owner', fullName: 'Truck Owner', active: true }, { id: 'new-owner', fullName: renamedOwner, active: true }],
    };
    await route.fulfill({ json: path === '/trucks' && url.searchParams.has('page') ? { items: [], total: 0, page: 1, pageSize: 25, totalPages: 1 } : fixtures[path] ?? [] });
  });
  trucks = [
    { id: 'truck-a', unitNumber: '201', ownerId: 'owner', ownerName: 'Truck Owner', driverId: driver.id, driverName: driver.fullName, active: true },
    { id: 'truck-b', unitNumber: '202', ownerId: 'owner', ownerName: 'Truck Owner', active: true },
    { id: 'inactive-truck', unitNumber: 'INACTIVE', ownerId: 'owner', ownerName: 'Truck Owner', active: false },
    { id: 'terminated-truck', unitNumber: 'TERMINATED', ownerId: 'departed-owner', ownerName: 'Departed Owner', active: false },
    { id: 'company-truck', unitNumber: 'COMPANY', ownerId: 'company', ownerName: 'Company', active: true, isCompanyOwned: true },
  ];
  type.amounts = ['50.00', '75.00'];
  truckCharges.eligibleTruckIds = trucks.filter(t => !t.isCompanyOwned && t.id !== 'terminated-truck').map(t => t.id);
  truckCharges.terms = [];
  await page.goto(`${base}/accounting/driver-charges?tab=trucks`);
  await expect(page.getByRole('link', { name: 'INACTIVE', exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'COMPANY', exact: true })).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'TERMINATED', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Previous week', exact: true }).click();
  await expect(page.getByRole('link', { name: 'TERMINATED', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Next week', exact: true }).click();
  await expect(page.getByRole('link', { name: 'TERMINATED', exact: true })).toHaveCount(0);
  await expect(page.getByRole('checkbox', { name: 'TERMINATED, Admin fee', exact: true })).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'Truck Owner', exact: true }).first()).toHaveAttribute('href', '/investors/detail?id=owner');
  await expect(page.getByRole('link', { name: '201', exact: true })).toHaveAttribute('href', '/trucks/detail?id=truck-a');
  await expect(page.getByRole('link', { name: driver.fullName, exact: true })).toHaveAttribute('href', '/drivers/detail?id=driver');
  const truckCheck = page.getByRole('checkbox', { name: '201, Admin fee', exact: true });
  await truckCheck.click();
  await expect(truckCheck).toBeChecked();
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled();
  await page.getByLabel('201, Admin fee amount', { exact: true }).selectOption('75.00');
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled();
  expect(truckWrites.at(-1).amount).toBe('75.00');
  const trucksAll = page.getByRole('checkbox', { name: 'Admin fee: select all trucks', exact: true });
  // A mixed selection selects the remaining trucks rather than clearing it.
  await trucksAll.click();
  await expect(truckCheck).toBeChecked();
  await expect(page.getByRole('checkbox', { name: '202, Admin fee', exact: true })).toBeChecked();
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled();
  await trucksAll.click();
  await expect(truckCheck).not.toBeChecked();
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled();
  await trucksAll.click();
  await expect(page.getByRole('checkbox', { name: '202, Admin fee', exact: true })).toBeChecked();
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled();
  expect(bulkWrites).toHaveLength(3);
  expect(bulkWrites[0]).toHaveLength(2);
  expect(bulkWrites[2]).toHaveLength(3);
  // The entire selection paints before the server responds. A rejected
  // middle truck is restored while later trucks still save.
  holdBulk = true;
  bulkFailure = 'truck-b';
  await trucksAll.click();
  await expect.poll(() => typeof releaseBulk).toBe('function');
  for (const unit of ['201', '202', 'INACTIVE']) {
    await expect(page.getByRole('checkbox', { name: `${unit}, Admin fee`, exact: true })).not.toBeChecked();
  }
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeDisabled();
  holdBulk = false;
  releaseBulk();
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled();
  await expect(page.getByText('2 truck charges updated; 1 could not be saved. Review the trucks below.', { exact: true })).toBeVisible();
  await expect(page.getByRole('list', { name: 'Truck charges that could not be saved' })).toContainText('Truck 202: Reopen affected finalized settlements');
  await expect(truckCheck).not.toBeChecked();
  await expect(page.getByRole('checkbox', { name: '202, Admin fee', exact: true })).toBeChecked();
  await expect(page.getByRole('checkbox', { name: 'INACTIVE, Admin fee', exact: true })).not.toBeChecked();
  expect(bulkWrites).toHaveLength(4);
  bulkFailure = '';
  await trucksAll.click(); // Select remaining trucks after the partial failure.
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled();
  await expect(page.getByRole('list', { name: 'Truck charges that could not be saved' })).toHaveCount(0);

  // Finalized cells are omitted from writes and from the header's checked state,
  // regardless of whether the frozen charge is included or excluded.
  truckCharges.lockedCharges = [{ truckId: 'truck-b', typeId: 'fee' }];
  truckCharges.phases = truckCharges.phases.map(p => ({ ...p, included: false }));
  await page.reload();
  const frozenCheck = page.getByRole('checkbox', { name: '202, Admin fee', exact: true });
  await expect(frozenCheck).toBeDisabled();
  await expect(page.getByLabel('202, Admin fee amount', { exact: true })).toBeDisabled();
  await expect(page.getByText('(1 finalized skipped)', { exact: true })).toBeVisible();
  const beforeFrozen = truckWrites.length;
  await trucksAll.click();
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled();
  await expect(trucksAll).toBeChecked();
  await expect(frozenCheck).not.toBeChecked();
  expect(truckWrites.slice(beforeFrozen).map(v => v.truckId).sort()).toEqual(['inactive-truck', 'truck-a']);
  await trucksAll.click();
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled();
  await expect(frozenCheck).not.toBeChecked();
  truckCharges.phases = truckCharges.phases.map(p => p.truckId === 'truck-b' ? { ...p, included: true } : p);
  await page.reload();
  await trucksAll.click();
  await expect(page.getByRole('button', { name: 'Reload', exact: true })).toBeEnabled();
  await expect(truckCheck).toBeChecked();
  await expect(frozenCheck).toBeChecked();
  expect(truckWrites.slice(beforeFrozen).some(v => v.truckId === 'truck-b')).toBe(false);
  await page.getByRole('button', { name: '202, Admin fee history', exact: true }).click();
  const frozenHistory = page.getByRole('dialog');
  await expect(frozenHistory.getByRole('checkbox')).toBeDisabled();
  await expect(frozenHistory.getByRole('combobox')).toBeDisabled();
  await expect(frozenHistory.getByRole('button', { name: 'Save truck fee', exact: true })).toHaveCount(0);
  await frozenHistory.getByRole('button', { name: 'Close', exact: true }).last().click();
  // Other charge types on the same truck remain independently selectable.
  await expect(page.getByRole('checkbox', { name: '202, Other fee 0', exact: true })).toBeEnabled();
  await page.getByLabel('Search trucks').fill('202');
  await expect(page.getByRole('checkbox', { name: 'Admin fee: select visible trucks', exact: true })).toBeDisabled();
  await page.getByLabel('Search trucks').fill('');
  truckCharges.lockedCharges = [];
  await page.reload();
  await expect(truckCheck).toBeChecked();
  await expect(page.getByLabel('201, Admin fee amount', { exact: true })).toHaveValue('75.00');
  await expect(page.getByRole('link', { name: /^Back/ })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'New charge type', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Fleet', exact: true }).click();
  await page.getByRole('link', { name: 'Trucks', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Trucks', exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: /^Back/ })).toHaveCount(0);
  await page.goBack();
  await expect(page.getByRole('tab', { name: 'Truck charges', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(truckCheck).toBeChecked();
  await expect(page.getByLabel('201, Admin fee amount', { exact: true })).toHaveValue('75.00');
  await page.goForward();
  await expect(page.getByRole('heading', { name: 'Trucks', exact: true })).toBeVisible();
  await page.goBack();
  await expect(truckCheck).toBeChecked();
  await page.locator('table').evaluate(table => { table.parentElement.scrollLeft = 0; });
  await page.screenshot({ path: join(tmpdir(), 'mserp-truck-charges-review.png'), fullPage: true, animations: 'disabled' });
  await expect(page.getByRole('tab', {name:'Driver charges',exact:true})).toHaveCount(0);
  await page.getByRole('button', { name: 'Show trucks included in Admin fee', exact: true }).click();
  await expect(page.getByLabel('Filter truck charge type')).toHaveValue('fee');
  await expect(page.getByLabel('Truck inclusion')).toHaveValue('included');
  await expect(page.locator('tbody tr')).toHaveCount(3);
  await truckCheck.click();
  await expect(page.getByRole('button', {name:'Reload',exact:true})).toBeEnabled();
  await expect(page.getByRole('link', {name:'201',exact:true})).toHaveCount(0);
  await page.getByLabel('Truck inclusion').selectOption('excluded');
  await page.keyboard.press('Escape');
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await expect(page.getByRole('link',{name:'201',exact:true})).toBeVisible();
  await page.reload();
  await expect(page.getByLabel('Truck inclusion')).toHaveValue('excluded');
  await page.getByLabel('Truck inclusion').selectOption('all');
  await page.getByLabel('Filter truck charge type').selectOption('');
  await page.keyboard.press('Escape');
  await page.setViewportSize({width:390,height:844});
  await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBe(true);
  await page.screenshot({path:join(tmpdir(),'mserp-truck-charges-narrow.png'),fullPage:true,animations:'disabled'});
  await page.setViewportSize({width:1440,height:1000});
  // Fleet ownership wins over an older agreement, using the requested week.
  datedOwners = true;
  truckCharges.terms = [{ truckId: 'truck-a', ownerId: 'owner', weekStart: '2026-09-21', sharePercent: '88', version: 1 }];
  await page.getByRole('button', { name: 'Reload', exact: true }).click();
  const ownerRow = page.locator('tbody tr').filter({ has: page.getByRole('link', { name: '201', exact: true }) });
  await expect(ownerRow.getByRole('link', { name: 'Felix & Den', exact: true })).toHaveAttribute('href', '/investors/detail?id=new-owner');
  await expect(ownerRow.getByRole('button', { name: 'Review terms', exact: true })).toHaveAttribute('title', /Truck Owner/);
  await page.getByLabel('Search trucks').fill('Felix');
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await ownerRow.getByRole('button', { name: 'Review terms', exact: true }).click();
  await expect(page.getByRole('dialog').locator('select')).toHaveValue('new-owner');
  await expect(page.getByRole('dialog').getByRole('spinbutton')).toHaveValue('88');
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.getByLabel('Search trucks').fill('');
  await page.getByRole('button', { name: 'Previous week', exact: true }).click();
  await expect(ownerRow.getByRole('link', { name: 'Truck Owner', exact: true })).toBeVisible();
  await expect(ownerRow.getByRole('button', { name: '88%', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Next week', exact: true }).click();
  await expect(ownerRow.getByRole('link', { name: 'Felix & Den', exact: true })).toBeVisible();
  renamedOwner = 'Updated owner';
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(ownerRow.getByRole('link', { name: 'Updated owner', exact: true })).toBeVisible();
  expect(termWrites).toEqual([]);
  await page.getByRole('tab', {name:'Charge types',exact:true}).click();
  await page.getByRole('button',{name:'Delete Other fee 0',exact:true}).click();
  await page.getByRole('button',{name:'Delete type',exact:true}).click();
  await expect(page.getByRole('cell',{name:'Other fee 0',exact:true})).toHaveCount(0);
  await page.evaluate(()=>sessionStorage.setItem('mserp-navigation-v1:user:/accounting/driver-charges:view:page:tab',JSON.stringify('recurring')));
  await page.goto(base+'/accounting/driver-charges');
  await expect(page.getByRole('tab',{name:'Truck charges',exact:true})).toHaveAttribute('aria-selected','true');
  const writesBefore = truckWrites.length;
  readOnly = true;
  await page.reload();
  await page.getByRole('button', {name: '201, Admin fee history', exact:true}).click();
  const history = page.getByRole('dialog');
  await expect(history.getByRole('checkbox')).toBeDisabled();
  await expect(history.getByRole('combobox')).toBeDisabled();
  await expect(history.getByRole('button', {name:'Save truck fee',exact:true})).toHaveCount(0);
  await history.getByRole('button', {name:'Close',exact:true}).last().click();
  await expect(history).toHaveCount(0);
  expect(truckWrites.length).toBe(writesBefore);
  await page.goto(base+'/accounting/driver-charges?action=installment');
  await expect(page.getByRole('tab',{name:'Installment plans',exact:true})).toHaveAttribute('aria-selected','true');
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(errors).toEqual([]);
  console.log('Truck charge views passed: no driver recurring tab, all non-company trucks, amounts, selection, inclusion filters, saved views, deletion and narrow layout.');
} finally {
  await browser?.close();
  await new Promise(resolve => server.close(resolve));
}
