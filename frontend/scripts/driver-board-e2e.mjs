import { expect } from '@playwright/test';
import { join } from 'node:path';

export async function runDriverBoardE2E({ page, base, sql, schema, temp }) {
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'America/New_York', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date());
  const date = new Date(`${today}T12:00:00Z`); date.setUTCDate(date.getUTCDate() - (date.getUTCDay() + 6) % 7);
  const week = date.toISOString().slice(0, 10);
  sql(`SET search_path TO ${schema},public;
    INSERT INTO dispatchers(id,full_name,normalized_name) VALUES('e4500000-0000-0000-0000-000000000001','Board Dispatcher','board dispatcher');
    INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,phone,dispatcher_id,driver_home) VALUES
    ('e4500000-0000-0000-0000-000000000002','Board Cpm','board cpm','cpm',0.65,'4703344443','e4500000-0000-0000-0000-000000000001','Louisville, KY'),
    ('e4500000-0000-0000-0000-000000000003','Board Percent','board percent','gross_percentage',25,NULL,'e4500000-0000-0000-0000-000000000001','');
    INSERT INTO gross_board_entries(driver_id,service_date,load_number,original_rate,driver_rate,miles) VALUES
    ('e4500000-0000-0000-0000-000000000002','${week}','PLAN-101',1000.10,900.05,500.25),
    ('e4500000-0000-0000-0000-000000000003','${week}','PLAN-102',2000.20,1900.10,800.50);
  `);
  const id = 'e4500000-0000-0000-0000-000000000002';
  const session = await (await page.request.get(`${base}/api/auth/session`)).json();
  const headers = { 'X-CSRF-Token': session.csrfToken };
  const getBoard = async () => (await page.request.get(`${base}/api/driver-board?weekStart=${week}`)).json();
  await page.goto(`${base}/driver-board`);
  await expect(page.getByRole('heading', { name: 'Driver Board', exact: true })).toBeVisible();
  await page.getByLabel('Dispatcher filter').selectOption('e4500000-0000-0000-0000-000000000001');
  await expect(page.getByText('$3,000.30', { exact: true })).toHaveCount(2);
  await expect(page.getByText('$2,800.15', { exact: true })).toHaveCount(2);
  await expect(page.getByText('1,300.75', { exact: true })).toBeVisible();
  await expect(page.getByLabel('Board Cpm · ETA', { exact: true })).toBeInViewport({ ratio: 1 });
  await expect(page.getByText('Owner operator · percentage of gross', { exact: true })).toHaveCount(0);
  const field = name => page.getByLabel(`Board Cpm · ${name}`, { exact: true });
  await expect(field('Driver home')).toHaveValue('Louisville, KY');
  await field('Current load').fill('LOAD 8841');
  await field('Trailer').fill('TA 102');
  await field('Status').selectOption('ENROUTE');
  await field('Origin / destination').fill('Memphis, TN → Louisville, KY');
  await field('ETA').fill('Friday 17:00');
  await field('Notes').fill('Call before arrival');
  await field('Home time').fill('Home next Friday');
  await field('Driver home').fill('Memphis, TN');
  await expect(page.getByText('All changes saved', { exact: true })).toBeVisible({ timeout: 15000 });
  await page.reload();
  await expect(field('Notes')).toHaveValue('Call before arrival');
  await expect(field('Driver home')).toHaveValue('Memphis, TN');
  let profile = await (await page.request.get(`${base}/api/drivers/${id}`)).json();
  expect(profile.driverHome).toBe('Memphis, TN');
  await page.locator('[data-scroll-key="driver-board"]').evaluate(element => { element.scrollLeft = 0; });
  await page.screenshot({ path: join(temp, 'driver-board-desktop.png'), fullPage: true, animations: 'disabled' });
  // Flush before internal navigation, without waiting five seconds.
  await field('Notes').fill('Saved before navigation');
  await page.getByRole('link', { name: 'Board Cpm', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Board Cpm', exact: true })).toBeVisible();
  expect((await getBoard()).entries.find(e => e.driverId === id).notes).toBe('Saved before navigation');
  await page.goto(`${base}/drivers`);
  const row = page.getByRole('row').filter({ hasText: 'Board Cpm' });
  await row.getByRole('button', { name: 'Edit', exact: true }).click();
  await expect(page.getByLabel('Driver home', { exact: true })).toHaveValue('Memphis, TN');
  await page.getByLabel('Driver home', { exact: true }).fill('Dayton, OH');
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await page.goto(`${base}/driver-board`);
  await expect(field('Driver home')).toHaveValue('Dayton, OH');
  // Stale rows fail with 409 and preserve local text until explicitly reloaded.
  let remote = (await getBoard()).entries.find(e => e.driverId === id);
  const changed = await page.request.put(`${base}/api/driver-board`, { headers, data: { entries: [{ ...remote, notes: 'Other dispatcher edit' }] } });
  expect(changed.status()).toBe(200);
  await field('Notes').fill('Local unsaved edit');
  await expect(page.getByRole('main').getByRole('alert')).toContainText('changed', { timeout: 15000 });
  await expect(field('Notes')).toHaveValue('Local unsaved edit');
  page.once('dialog', dialog => dialog.accept());
  await page.getByRole('button', { name: 'Reload', exact: true }).click();
  await expect(field('Notes')).toHaveValue('Other dispatcher edit');
  // A delayed response cannot drop text typed while that save is in flight.
  let release, intercepted;
  const inFlight = new Promise(resolve => { intercepted = resolve; });
  const gate = new Promise(resolve => { release = resolve; });
  await page.route('**/api/driver-board', async route => {
    if (route.request().method() !== 'PUT') return route.continue();
    const response = await route.fetch(); intercepted(); await gate; await route.fulfill({ response });
  }, { times: 1 });
  await field('Notes').fill('First in flight');
  await inFlight;
  await field('Notes').fill('Newer typing preserved');
  release();
  await expect(page.getByText('All changes saved', { exact: true })).toBeVisible({ timeout: 15000 });
  expect((await getBoard()).entries.find(e => e.driverId === id).notes).toBe('Newer typing preserved');
  await field('Notes').fill('');
  await field('Driver home').fill('');
  await expect(page.getByText('All changes saved', { exact: true })).toBeVisible({ timeout: 15000 });
  await page.reload();
  await expect(field('Notes')).toHaveValue('');
  await expect(field('Driver home')).toHaveValue('');
  // Board-only viewers can read but cannot mutate board or fleet records.
  const role = await page.request.post(`${base}/api/settings/roles`, { headers, data: { name: 'Board viewer', permissions: ['driver_board.read'] } });
  expect(role.status()).toBe(204);
  const access = await (await page.request.get(`${base}/api/settings/access`)).json();
  const roleValue = access.roles.find(r => r.name === 'Board viewer');
  const { randomBytes } = await import('node:crypto');
  const password = randomBytes(20).toString('hex');
  const user = await page.request.post(`${base}/api/settings/users`, { headers, data: { username: 'Board Viewer', email: 'board-viewer@example.com', password, roleId: roleValue.id, active: true } });
  expect(user.status()).toBe(200);
  const context = await page.context().browser().newContext();
  try {
    const viewer = await context.newPage();
    await viewer.goto(`${base}/login`);
    await viewer.getByLabel('Email or existing username', { exact: true }).fill('board-viewer@example.com');
    await viewer.getByLabel('Password', { exact: true }).fill(password);
    await viewer.getByRole('button', { name: 'Sign in', exact: true }).click();
    await expect(viewer.getByRole('heading', { name: 'Driver Board', exact: true })).toBeVisible();
    await expect(viewer.getByLabel('Board Cpm · Notes', { exact: true })).toBeDisabled();
    const auth = await (await viewer.request.get(`${base}/api/auth/session`)).json();
    remote = (await getBoard()).entries.find(e => e.driverId === id);
    expect((await viewer.request.put(`${base}/api/driver-board`, { headers: { 'X-CSRF-Token': auth.csrfToken }, data: { entries: [remote] } })).status()).toBe(403);
    expect((await viewer.request.get(`${base}/api/drivers`)).status()).toBe(403);
  } finally { await context.close(); }
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: 'Collapse sidebar' }).click();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: join(temp, 'driver-board-mobile.png'), fullPage: true, animations: 'disabled' });
  await field('Driver home').scrollIntoViewIfNeeded();
  await expect(field('Driver home')).toBeInViewport();
  await page.screenshot({ path: join(temp, 'driver-board-mobile-table.png'), fullPage: true, animations: 'disabled' });
  await page.getByRole('button', { name: 'Expand sidebar' }).click();
  await page.setViewportSize({ width: 1440, height: 1000 });
  console.log('Driver Board E2E passed: totals, autosave, navigation flush, home/profile synchronization, stale conflict, queued typing, clearing, view permissions and mobile layout.');
}
