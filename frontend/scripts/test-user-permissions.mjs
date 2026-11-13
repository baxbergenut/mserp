// Real API and browser coverage against the isolated review-local environment.
import { chromium, expect } from '@playwright/test';
import { readFile, mkdir } from 'node:fs/promises';
import { randomBytes } from 'node:crypto';

const access = JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE, 'utf8'));
const { base } = access;
if (!['localhost', '127.0.0.1'].includes(new URL(base).hostname)) throw Error('Local review only');
const browser = await chromium.launch();
const contexts = [];
const token = randomBytes(5).toString('hex');
async function login(email, password) {
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 } }); contexts.push(context);
  const response = await context.request.post(`${base}/api/auth/login`, { data: { email, password } });
  expect(response.status(), await response.text()).toBe(200);
  return { context, session: await response.json(), page: await context.newPage() };
}
try {
  const admin = await login(access.email, access.password);
  const change = async (method, path, data, expected = 200) => {
    const response = await admin.context.request.fetch(`${base}/api${path}`, { method, data, headers: { 'X-CSRF-Token': admin.session.csrfToken } });
    expect(response.status(), await response.text()).toBe(expected); return response;
  };
  const data = async () => (await (await admin.context.request.get(`${base}/api/settings/access`)).json());
  await change('POST', '/settings/roles', { name: `Override role ${token}`, permissions: ['drivers.read', 'drivers.edit', 'loads.read'], expenseCategoryAccess: [] }, 204);
  const role = (await data()).roles.find(r => r.name === `Override role ${token}`);
  const email = `override-${token}@example.test`, password = randomBytes(18).toString('hex');
  const created = await change('POST', '/settings/users', { firstName: 'Override', lastName: token, email, password, roleId: role.id, active: true });
  const { id } = await created.json();
  const getUser = async () => (await data()).users.find(u => u.id === id);
  const initial = await login(email, password);
  expect((await initial.context.request.get(`${base}/api/drivers`)).status()).toBe(200);

  await admin.page.goto(`${base}/settings`);
  await admin.page.getByRole('tab', { name: 'Users', exact: true }).click();
  const row = admin.page.getByRole('row').filter({ hasText: email });
  await row.getByRole('button', { name: 'Edit', exact: true }).click();
  const dialog = admin.page.getByRole('dialog', { name: 'Edit user' });
  const section = dialog.getByRole('region', { name: 'User permissions' });
  await expect(section).toBeVisible();
  expect(await dialog.locator('label').filter({ hasText: 'Active account' }).evaluate((label) => Boolean(label.compareDocumentPosition(document.querySelector('section[aria-label="User permissions"]')) & Node.DOCUMENT_POSITION_FOLLOWING))).toBe(true);
  await section.getByLabel('View Drivers override', { exact: true }).selectOption('deny');
  await section.getByLabel('View Trucks override', { exact: true }).selectOption('allow');
  await expect(section.getByRole('row').filter({ hasText: /^Edit Drivers/ }).getByRole('cell').last()).toHaveText('Denied');
  await section.getByLabel('Search user permissions').fill('Loads');
  await expect(section.getByLabel('View Loads override', { exact: true })).toBeVisible();
  await section.getByLabel('Search user permissions').fill('');
  const category = (await data()).expenseCategories.find(c => c.active);
  await section.getByLabel(`${category.name} View override`, { exact: true }).selectOption('allow');
  await section.getByLabel(`${category.name} Add override`, { exact: true }).selectOption('allow');
  await mkdir('artifacts', { recursive: true });
  await section.scrollIntoViewIfNeeded();
  await admin.page.screenshot({ path: 'artifacts/user-permissions-desktop.png' });
  await dialog.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect((await initial.context.request.get(`${base}/api/drivers`)).status()).toBe(401);
  const saved = await getUser();
  expect(saved.permissionOverrides).toEqual({ 'drivers.read': 'deny', 'trucks.read': 'allow' });
  const restricted = await login(email, password);
  expect(restricted.session.user.permissions).toContain('trucks.read');
  expect(restricted.session.user.permissions).not.toContain('drivers.read');
  expect(restricted.session.user.permissions).not.toContain('drivers.edit');
  expect(restricted.session.user.expenseCategoryAccess.find(c => c.categoryId === category.id).canCreate).toBe(true);
  expect((await restricted.context.request.get(`${base}/api/drivers`)).status()).toBe(403);
  expect((await restricted.context.request.get(`${base}/api/trucks`)).status()).toBe(200);
  expect((await restricted.context.request.get(`${base}/api/settings/access`)).status()).toBe(403);
  await restricted.page.goto(`${base}/trucks`);
  await expect(restricted.page.locator('aside').getByRole('link', { name: 'Drivers', exact: true })).toHaveCount(0);
  await expect(restricted.page.locator('aside').getByRole('link', { name: 'Trucks', exact: true })).toBeVisible();

  await change('PUT', `/settings/users/${id}`, { ...saved, permissionOverrides: { unknown: 'allow' } }, 400);
  await change('PUT', `/settings/users/${id}`, { ...saved, permissionOverrides: { 'loads.read': null } }, 400);
  await change('PUT', `/settings/users/${id}`, { ...saved, expenseCategoryOverrides: { invalid: { canView: 'allow' } } }, 400);
  await change('PUT', `/settings/users/${id}`, { ...saved, version: saved.version - 1 }, 409);
  // Role edits continue to affect inherited access without losing explicit exceptions.
  await change('PUT', `/settings/roles/${role.id}`, { ...role, permissions: ['drivers.read', 'drivers.edit', 'fuel.read'] }, 204);
  const refreshed = await (await restricted.context.request.get(`${base}/api/auth/session`)).json();
  expect(refreshed.user.permissions).toContain('fuel.read');
  expect(refreshed.user.permissions).not.toContain('loads.read');
  expect(refreshed.user.permissions).not.toContain('drivers.read');
  expect(refreshed.user.permissions).toContain('trucks.read');

  await admin.page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await row.getByRole('button', { name: 'Edit', exact: true }).click();
  await expect(section.getByLabel('View Drivers override', { exact: true })).toHaveValue('deny');
  await admin.page.setViewportSize({ width: 390, height: 844 });
  await section.scrollIntoViewIfNeeded();
  await admin.page.screenshot({ path: 'artifacts/user-permissions-mobile.png' });
  const sectionBox = await section.boundingBox();
  expect(sectionBox.x).toBeGreaterThanOrEqual(30);
  expect(sectionBox.x + sectionBox.width).toBeLessThanOrEqual(360);
  expect(await admin.page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await section.getByRole('button', { name: /Reset to role/ }).click();
  await expect(section.getByLabel('View Drivers override', { exact: true })).toHaveValue('inherit');
  await dialog.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  const reset = await login(email, password);
  expect(reset.session.user.permissions).toContain('drivers.edit');
  expect(reset.session.user.permissions).not.toContain('trucks.read');
  expect((await getUser()).expenseCategoryOverrides).toEqual({});
  console.log('User overrides: browser editing, persistence, prerequisites, API enforcement, session revocation, role changes, validation and reset passed.');
} finally {
  await Promise.all(contexts.map(c => c.close()));
  await browser.close();
}
