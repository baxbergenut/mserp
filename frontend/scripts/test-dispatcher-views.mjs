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
  const errors = [], extensionRequests = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname.slice(4);
    if (path.includes('-extension')) extensionRequests.push(path);
    const fixture = {
      '/auth/session': { user: { id: 'admin', username: 'Test Admin', theme: 'default', permissions: ['fleet.read', 'fleet.write', 'access.manage'] }, csrfToken: 'fixture' },
      [`/dispatchers/${id}`]: { id, fullName: 'Test Dispatcher', active: true, email: 'dispatcher@example.test', extension: 105, mainUpdaterId: null, afterHoursUpdaterId: null, payPercentage: null },
      '/dispatchers': { items: [], total: 0, page: 1, pageSize: 25, totalPages: 1 },
      '/drivers': [], '/updaters': [{ id, fullName: 'Roy', shift: 'main', extension: 106, version: 1, dispatcherNames: [] }],
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
  await expect(page.getByRole('cell', { name: 'Roy', exact: true })).toBeVisible();
  await expect(page.getByRole('cell', { name: '106', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Add updater', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: /Chrome extension/ })).toHaveCount(0);
  if (errors.length || extensionRequests.length) throw new Error(`Unexpected page errors or retired requests: ${errors.join('; ')} / ${extensionRequests.join(', ')}`);
  console.log('PASS: dispatcher/updater management and phone extensions remain; retired controls and API requests are absent.');
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
