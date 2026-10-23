import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname } from 'node:path';

const id = '11111111-1111-4111-8111-111111111111';
const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] || 'application/octet-stream' });
    res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1360, height: 1000 } });
  const errors = [], extensionRequests = [], writes = [], reads = [];
  let permissions = ['drivers.read','trucks.read','dispatchers.read','updaters.read','investors.read', 'drivers.create','drivers.edit','drivers.delete','trucks.create','trucks.edit','trucks.delete','dispatchers.create','dispatchers.edit','dispatchers.delete','updaters.create','updaters.edit','updaters.delete','investors.create','investors.edit','drivers.private.read','drivers.private.edit','driver_compensation.read','driver_compensation.edit','dispatcher_compensation.read','dispatcher_compensation.edit','escrow.read','escrow.create','escrow.edit','escrow.delete', 'access.manage'];
  let updaters = [{ id, fullName: 'Roy', shift: 'main', extension: 106, version: 1, dispatcherNames: [] }];
  const dispatcher = { id, fullName: 'Test Dispatcher', active: true, email: 'dispatcher@example.test', extension: 105, mainUpdaterId: id, afterHoursUpdaterId: null, payPercentage: null, driverCount: 0 };
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname.replace('/lookups/','/').slice(4);
    const method = route.request().method();
    if (method === 'GET') reads.push(new URL(route.request().url()).pathname.slice(4));
    if (path.startsWith('/updaters') && method !== 'GET') {
      const body = method === 'DELETE' ? null : route.request().postDataJSON();
      writes.push({ method, body });
      if (method === 'POST') updaters.push({ ...body, id: 'new-updater', version: 1, dispatcherNames: [] });
      if (method === 'PUT') updaters = updaters.map(u => u.id === path.split('/')[2] ? { ...u, ...body, version: u.version + 1 } : u);
      if (method === 'DELETE') updaters = updaters.filter(u => u.id !== path.split('/')[2]);
      await route.fulfill({ status: 200, json: { saved: true } });
      return;
    }
    if (path.includes('-extension')) extensionRequests.push(path);
    const fixture = {
      '/auth/session': { user: { id: 'admin', username: 'Test Admin', theme: 'default', permissions }, csrfToken: 'fixture' },
      '/auth/login': { user: { id: 'admin', username: 'Test Admin', theme: 'default', permissions }, csrfToken: 'fixture' },
      [`/dispatchers/${id}`]: dispatcher,
      '/dispatchers': { items: [dispatcher], total: 1, page: 1, pageSize: 25, totalPages: 1 },
      '/drivers': [], '/updaters': updaters,
      '/tasks/count': { count: 0 }, '/sidebar-counts': {},
    }[path];
    await route.fulfill({ status: fixture === undefined ? 404 : 200, json: fixture ?? { error: 'fixture not found' } });
  });
  const origin = `http://127.0.0.1:${server.address().port}`;
  await page.goto(`${origin}/dispatchers/detail?id=${id}`);
  await expect(page.getByRole('heading', { name: 'Assigned drivers' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Edit dispatcher', exact: true })).toBeVisible();
  await expect(page.getByText('105', { exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Dispatcher Chrome extension' })).toHaveCount(0);
  await expect(page.getByLabel('Extension activation code')).toHaveCount(0);
  await page.goto(`${origin}/dispatchers`);
  await expect(page.getByRole('heading', { name: 'Dispatchers', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'HR', exact: true })).toHaveAttribute('aria-expanded', 'true');
  const nav = page.locator('aside nav');
  await expect(nav.getByRole('link', { name: 'Drivers', exact: true })).toHaveAttribute('href', '/drivers');
  await expect(nav.getByRole('link', { name: 'Dispatchers', exact: true })).toHaveAttribute('href', '/dispatchers');
  await expect(nav.getByRole('link', { name: 'Updaters', exact: true })).toHaveAttribute('href', '/updaters');
  await expect(page.getByRole('button', { name: 'Add updater', exact: true })).toHaveCount(0);
  await expect(page.getByRole('cell', { name: 'Roy (106)', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Edit', exact: true }).click();
  await expect(page.getByLabel('Main updater')).toHaveValue(id);
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await nav.getByRole('link', { name: 'Updaters', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Updaters', exact: true })).toBeVisible();
  await expect(page.getByRole('cell', { name: 'Roy', exact: true })).toBeVisible();
  await expect(page.getByRole('cell', { name: '106', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Add updater', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Add updater', exact: true }).click();
  await page.getByLabel('Full name', { exact: true }).fill('Test Updater');
  await page.getByLabel('Shift', { exact: true }).selectOption('after_hours');
  await page.getByLabel('Phone extension', { exact: true }).fill('208');
  await page.getByRole('button', { name: 'Create updater', exact: true }).click();
  let row = page.getByRole('row').filter({ has: page.getByRole('cell', { name: 'Test Updater', exact: true }) });
  await expect(row.getByRole('cell', { name: 'After hours', exact: true })).toBeVisible();
  await row.getByRole('button', { name: 'Edit', exact: true }).click();
  await page.getByLabel('Full name', { exact: true }).fill('Updated Updater');
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  row = page.getByRole('row').filter({ has: page.getByRole('cell', { name: 'Updated Updater', exact: true }) });
  await expect(row).toBeVisible();
  await row.getByRole('button', { name: 'Delete', exact: true }).click();
  await page.locator('.fixed').filter({ has: page.getByRole('heading', { name: 'Delete updater?', exact: true }) }).getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(page.getByRole('cell', { name: 'Updated Updater', exact: true })).toHaveCount(0);
  expect(writes.map(w => w.method)).toEqual(['POST', 'PUT', 'DELETE']);
  expect(writes[0].body).toEqual({ fullName: 'Test Updater', shift: 'after_hours', extension: 208 });
  expect(writes[1].body.version).toBe(1);
  await page.getByRole('button', { name: 'Collapse sidebar', exact: true }).click();
  await expect(nav.locator('a[title="HR"]')).toHaveAttribute('href', '/drivers');
  await page.getByRole('button', { name: 'Expand sidebar', exact: true }).click();

  permissions = ['updaters.read'];
  reads.length = 0;
  await page.goto(`${origin}/updaters`);
  await expect(page.getByRole('cell', { name: 'Roy', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Add updater', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Edit', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Delete', exact: true })).toHaveCount(0);
  await expect(nav.getByRole('link', { name: 'Dispatchers', exact: true })).toHaveCount(0);
  await expect(nav.getByRole('link', { name: 'Drivers', exact: true })).toHaveCount(0);
  expect(reads).not.toContain('/dispatchers');
  await page.getByRole('button', { name: 'Collapse sidebar', exact: true }).click();
  await expect(nav.locator('a[title="HR"]')).toHaveAttribute('href', '/updaters');
  await page.goto(`${origin}/dispatchers`);
  await expect(page.getByRole('heading', { name: 'Access restricted', exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Open an available page' })).toHaveAttribute('href', '/updaters');
  await page.goto(`${origin}/login`);
  await page.getByLabel('Email or existing username').fill('updater-viewer');
  await page.getByLabel('Password', { exact: true }).fill('fixture-password');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(`${origin}/updaters`);
  await expect(page.getByRole('heading', { name: 'Updaters', exact: true })).toBeVisible();

  permissions = ['dispatchers.read'];
  reads.length = 0;
  await page.goto(`${origin}/dispatchers`);
  await expect(page.getByRole('cell', { name: 'Roy (106)', exact: true })).toBeVisible();
  expect(reads).toContain('/lookups/updaters');
  expect(reads).not.toContain('/updaters');
  await expect(nav.getByRole('link', { name: 'Updaters', exact: true })).toHaveCount(0);
  await page.goto(`${origin}/updaters`);
  await expect(page.getByRole('heading', { name: 'Access restricted', exact: true })).toBeVisible();

  permissions = [];
  await page.goto(`${origin}/updaters`);
  await expect(page.getByRole('heading', { name: 'Access restricted', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'HR', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: /Chrome extension/ })).toHaveCount(0);
  if (errors.length || extensionRequests.length) throw new Error(`Unexpected page errors or retired requests: ${errors.join('; ')} / ${extensionRequests.join(', ')}`);
  console.log('PASS: HR navigation, separate dispatcher/updater pages, assignments, updater CRUD, read-only access and login landing; retired controls and API requests are absent.');
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
