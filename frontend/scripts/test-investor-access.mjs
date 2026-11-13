// Real API/browser privacy checks against the isolated review-local fixture.
import { chromium, expect } from '@playwright/test';
import { readFile } from 'node:fs/promises';
import { randomBytes } from 'node:crypto';

const access = JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE, 'utf8'));
const { base } = access;
if (!['localhost', '127.0.0.1'].includes(new URL(base).hostname)) throw Error('Local review only');
const browser = await chromium.launch();
const contexts = [];
const token = randomBytes(5).toString('hex');
async function login(email, password) {
  const context = await browser.newContext(); contexts.push(context);
  const response = await context.request.post(`${base}/api/auth/login`, { data: { email, password } });
  expect(response.status(), await response.text()).toBe(200);
  return { context, session: await response.json(), page: await context.newPage() };
}
async function read(user, path) {
  const response = await user.context.request.get(`${base}/api${path}`);
  expect(response.ok(), `${path}: ${await response.text()}`).toBe(true);
  return response.json();
}
try {
  const admin = await login(access.email, access.password);
  async function change(user, method, path, data) {
    const response = await user.context.request.fetch(`${base}/api${path}`, { method, data, headers: { 'X-CSRF-Token': user.session.csrfToken } });
    expect(response.ok(), `${method} ${path}: ${await response.text()}`).toBe(true);
    return response;
  }
  const settings = await read(admin, '/settings/access');
  const categories = settings.expenseCategories.map(c => ({ categoryId: c.id, canView: true, canCreate: true, canEdit: true, canDelete: false }));
  const operational = ['trucks.read', 'trucks.edit', 'drivers.read', 'charges.read', 'escrow.read', 'weighmytruck.read', 'driver_pay.read'];
  async function user(name, permissions) {
    await change(admin, 'POST', '/settings/roles', { name: `${name}-${token}`, permissions, expenseCategoryAccess: categories });
    const role = (await read(admin, '/settings/access')).roles.find(r => r.name === `${name}-${token}`);
    const email = `${name}-${token}@example.test`, password = randomBytes(18).toString('hex');
    await change(admin, 'POST', '/settings/users', { username: `${name}-${token}`, firstName: "Privacy", lastName: `${name}-${token}`, email, password, roleId: role.id, active: true });
    return { ...await login(email, password), role };
  }
  const restricted = await user('investor-restricted', operational);
  const authorized = await user('investor-reader', [...operational, 'investors.read', 'investor_pay.read', 'investor_terms.read']);
  const trucks = await read(admin, '/trucks');
  const truck = trucks.find(t => t.ownerName === 'River Fleet Partners');
  expect(truck).toBeTruthy();
  const owner = (await read(admin, '/investors')).find(i => i.id === truck.ownerId);
  for (const path of ['/trucks', '/lookups/trucks', `/trucks/${truck.id}`, '/lookups/investors', '/expenses/form-choices', '/truck-charges?weekStart=2026-10-05', '/driver-charges', '/escrows']) {
    const hidden = await read(restricted, path);
    expect(JSON.stringify(hidden), path).not.toContain(owner.fullName);
  }
  expect((await read(restricted, `/trucks/${truck.id}`)).ownerName).toBe('Investor');
  expect((await read(restricted, '/lookups/investors')).find(i => i.id === owner.id).fullName).toBe('Investor');
  expect((await read(authorized, `/trucks/${truck.id}`)).ownerName).toBe(owner.fullName);
  const truckEdit = Object.fromEntries(['unitNumber','vin','year','make','model','color','licensePlate','licenseState','isCompanyOwned','status','mileage','nextServiceMiles','driverId','active','ownerId','statusVersion'].map(key => [key, truck[key]]));
  for (const key of ['registrationExpires','insuranceExpires','lastServiceDate']) truckEdit[key] = truck[key]?.slice(0,10) ?? '';
  truckEdit.notes = 'Operational edit with masked owner';
  truckEdit.expectedOwnershipVersion = truck.ownershipVersion;
  const savedTruck = await (await change(restricted, 'PUT', `/trucks/${truck.id}`, truckEdit)).json();
  expect(savedTruck.ownerName).toBe('Investor');
  expect(savedTruck.ownerId).toBe(owner.id);
  expect((await read(admin, `/trucks/${truck.id}`)).ownerId).toBe(owner.id);
  expect((await read(authorized, '/lookups/investors')).find(i => i.id === owner.id).fullName).toBe(owner.fullName);
  const category = settings.expenseCategories.find(c => c.name === 'Other');
  const expense = await (await change(restricted, 'POST', '/expenses', { categoryId: category.id, expenseDate: '2026-10-05', truckId: truck.id, unitNumber: truck.unitNumber, amount: '25.00', expenseType: 'Test charge', coveredBy: 'Truck Owner', ownerId: owner.id, description: 'Investor privacy fixture' })).json();
  expect(expense.ownerName).toBe('Investor');
  expect(expense.ownerId).toBe(owner.id);
  expect((await read(restricted, `/expenses/${expense.id}`)).ownerName).toBe('Investor');
  expect((await read(authorized, `/expenses/${expense.id}`)).ownerName).toBe(owner.fullName);
  for (const path of ['/investors', `/investors/${owner.id}`, '/investor-pay?weekStart=2026-10-05', '/investor-pay/export?weekStart=2026-10-05', '/investor-pay/availability', '/investor-pay/events?weekStart=2026-10-05']) {
    expect((await restricted.context.request.get(`${base}/api${path}`)).status(), path).toBe(403);
  }
  const invalid = await admin.context.request.post(`${base}/api/settings/roles`, { data: { name: `invalid-${token}`, permissions: ['investor_pay.read'], expenseCategoryAccess: [] }, headers: { 'X-CSRF-Token': admin.session.csrfToken } });
  expect(invalid.status()).toBe(400);
  expect(settings.permissions.find(p => p.key === 'investors.read').sensitive).toBe(true);
  expect(settings.permissions.find(p => p.key === 'investor_pay.read').requires).toContain('investors.read');

  await restricted.page.goto(`${base}/trucks/detail?id=${truck.id}`);
  await expect(restricted.page.getByRole('heading', { name: `Truck ${truck.unitNumber}`, exact: true })).toBeVisible();
  await expect(restricted.page.getByText(owner.fullName, { exact: true })).toHaveCount(0);
  await expect(restricted.page.locator(`a[href="/investors/detail?id=${owner.id}"]`)).toHaveCount(0);
  await expect(restricted.page.getByRole('link', { name: 'Investors', exact: true })).toHaveCount(0);
  await expect(restricted.page.getByRole('link', { name: 'Investor Pay', exact: true })).toHaveCount(0);
  await restricted.page.getByRole('button', { name: 'Edit truck', exact: true }).click();
  await expect(restricted.page.getByLabel('Truck owner', { exact: true })).toBeDisabled();
  await expect(restricted.page.getByRole('dialog')).not.toContainText(owner.fullName);
  for (const path of ['/investors', '/accounting/investor-pay']) {
    await restricted.page.goto(`${base}${path}`);
    await expect(restricted.page.getByRole('heading', { name: 'Access restricted', exact: true })).toBeVisible();
  }
  await authorized.page.goto(`${base}/investors`);
  await expect(authorized.page.getByRole('heading', { name: 'Investors', exact: true })).toBeVisible();
  await expect(authorized.page.getByText(owner.fullName, { exact: true })).toBeVisible();
  await authorized.page.goto(`${base}/accounting/investor-pay`);
  await expect(authorized.page.getByRole('heading', { name: 'Investor pay', exact: true })).toBeVisible();
  await expect(authorized.page.getByRole('link', { name: owner.fullName, exact: true })).toBeVisible();
  await authorized.page.goto(`${base}/trucks/detail?id=${truck.id}`);
  await expect(authorized.page.getByText(owner.fullName, { exact: true })).toBeVisible();
  await change(admin, 'PUT', `/settings/roles/${authorized.role.id}`, { ...authorized.role, permissions: operational });
  await authorized.page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(authorized.page.getByText(owner.fullName, { exact: true })).toHaveCount(0);
  await expect(authorized.page.getByRole('link', { name: 'Investor Pay', exact: true })).toHaveCount(0);
  expect((await read(authorized, `/trucks/${truck.id}`)).ownerName).toBe('Investor');
  console.log('Investor privacy passed: raw API, expense routing, sensitive prerequisites, pages/navigation, forms, and live revocation.');
} finally {
  await Promise.all(contexts.map(c => c.close()));
  await browser.close();
}
