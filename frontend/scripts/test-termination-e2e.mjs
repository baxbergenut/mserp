// Only the disposable review-local environment is allowed. No provider calls.
import { readFile, mkdir } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { randomUUID } from 'node:crypto';
import { chromium, expect } from '@playwright/test';

const accessFile = process.env.MSERP_REVIEW_ACCESS_FILE;
const access = JSON.parse(await readFile(accessFile, 'utf8'));
const database = new URL(process.env.MSERP_REVIEW_TEST_DATABASE_URL);
if (!['127.0.0.1', 'localhost'].includes(new URL(access.base).hostname) || !['127.0.0.1', 'localhost'].includes(database.hostname) || !database.pathname.endsWith('_test') || !/^review_[a-f0-9]+$/.test(access.schema)) throw Error('A disposable local review is required');
const artifacts = join(dirname(accessFile), 'termination');
await mkdir(artifacts, { recursive: true });
const browser = await chromium.launch();
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(access.base + '/login');
  await page.getByLabel('Email or existing username').fill(access.email);
  await page.getByLabel('Password', { exact: true }).fill(access.password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.waitForURL(url => url.pathname !== '/login');
  const session = await (await page.request.get(access.base + '/api/auth/session')).json();
  const headers = { 'X-CSRF-Token': session.csrfToken, Origin: access.base };
  const api = async (method, path, data) => {
    const response = await page.request[method](access.base + '/api' + path, { headers, ...(data ? { data } : {}) });
    if (!response.ok()) throw Error(`${path}: ${response.status()} ${await response.text()}`);
    return response.status() === 204 ? null : response.json();
  };
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'America/New_York' }).format(new Date());
  const suffix = randomUUID().slice(0, 8);
  const driver = name => api('post', '/drivers', { firstName: name, lastName: suffix, fullName: `${name} ${suffix}`, payType: 'cpm', payRate: 0.75, hireDate: today, active: true, status: 'active' });
  const truck = (unitNumber, ownerDriverId, driverId) => api('post', '/trucks', { unitNumber: `${unitNumber}-${suffix}`, active: true, status: 'available', addToDataTruck: false, ...(ownerDriverId ? { ownerDriverId } : {}), driverId });
  const terminate = async value => {
    await page.goto(access.base + '/drivers/detail?id=' + value.id);
    await page.getByRole('button', { name: 'Employment status', exact: true }).click();
    await page.getByLabel('Driver status', { exact: true }).selectOption('terminated');
    await expect(page.getByText(/An existing link to the driver’s own truck stays/)).toBeVisible();
    await page.getByRole('button', { name: 'Save status', exact: true }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    return api('get', '/drivers/' + value.id);
  };
  const owner = await driver('Owner');
  const ownTruck = await truck('OWN', owner.id, owner.id);
  const endedOwner = await terminate(owner);
  expect(endedOwner).toMatchObject({ active: false, dispatcherId: null, truckId: ownTruck.id });
  expect(await api('get', '/trucks/' + ownTruck.id)).toMatchObject({ active: false, driverId: owner.id });
  await expect(page.locator(`a[href="/trucks/detail?id=${ownTruck.id}"]`).first()).toBeVisible();
  await page.screenshot({ path: join(artifacts, 'terminated-owner.png'), fullPage: true });
  const companyDriver = await driver('Company');
  const companyTruck = await truck('COMPANY', null, companyDriver.id);
  expect(await terminate(companyDriver)).toMatchObject({ active: false, dispatcherId: null, truckId: null });
  expect(await api('get', '/trucks/' + companyTruck.id)).toMatchObject({ active: true, driverId: null });
  const sharedOwner = await driver('SharedOwner');
  const activeDriver = await driver('ActiveOperator');
  const sharedTruck = await truck('SHARED', sharedOwner.id, activeDriver.id);
  await terminate(sharedOwner);
  expect(await api('get', '/trucks/' + sharedTruck.id)).toMatchObject({ active: true, driverId: activeDriver.id });
  const tasks = await api('get', '/tasks/custom?status=open&search=' + encodeURIComponent(sharedTruck.unitNumber));
  expect(tasks.total).toBe(0);
  const monday = new Date(today + 'T12:00:00Z');
  monday.setUTCDate(monday.getUTCDate() - (monday.getUTCDay() + 6) % 7);
  const board = await api('get', '/driver-board?weekStart=' + monday.toISOString().slice(0, 10));
  expect(board.drivers.some(value => [owner.id, companyDriver.id, sharedOwner.id].includes(value.id))).toBe(false);
  const anonymous = await browser.newContext();
  expect((await anonymous.request.patch(access.base + '/api/drivers/' + owner.id + '/status', { data: { status: 'active' } })).status()).toBe(401);
  await anonymous.close();
  expect(errors).toEqual([]);
  console.log('Termination E2E passed: owner link retained, owned truck inactive, company truck released, continuing investor fleet unchanged without review tasks, dispatch exclusion and anonymous denial. Screenshots: ' + artifacts);
} finally {
  await browser.close();
}
