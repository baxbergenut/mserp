// UI regressions use the static build and isolated API fixtures; authorization
// is separately exercised against real sessions/PostgreSQL by the Go tests.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';

const week = '2026-10-05';
const users = [{ id: 'user', name: 'Test user' }, { id: 'teammate', name: 'Teammate' }];
const assignments = ['driver_onboarding', 'driver_offboarding', 'relay_review'].map(kind => ({ kind, assigneeId: null, version: 1 }));
const driverId = '05400000-0000-0000-0000-000000000001';
const driver = { id: driverId, fullName: 'Board Driver', driverType: '%-O', truckUnit: '123', phone: '', dispatcherId: 'dispatcher', dispatcherName: 'Test Dispatcher', location: null };
const entry = { driverId, currentLoad: '', trailerNumber: '', status: '', destination: '', eta: '', notes: '', homeTime: '', driverHome: '', version: 0, homeVersion: 0 };
const driverBoard = { weekStart: week, drivers: [driver], entries: [entry], grossEntries: [], loads: {}, eld: { configured: false } };
const grossRequests = [];
let tasks = [];
const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = path === '/' ? 'index.html' : extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] ?? 'application/octet-stream' }); res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const base = `http://127.0.0.1:${server.address().port}`;
let browser;
try {
  browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 }, timezoneId: 'Asia/Tokyo' });
  // Tokyo is already Monday while New York is still Sunday. Opening the board
  // must use New York's current week, even when memory contains a later week.
  await page.clock.install({ time: new Date('2026-10-12T02:00:00Z') });
  const errors = []; page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url()), path = url.pathname.slice(4), method = route.request().method();
    if (method === 'PUT' && path.startsWith('/settings/system-tasks/')) {
      const input = route.request().postDataJSON();
      Object.assign(assignments.find(v => v.kind === input.kind), input, { version: input.version + 1 });
      await route.fulfill({ status: 204 }); return;
    }
    if ((method === 'POST' || method === 'PUT') && path.startsWith('/tasks/custom')) {
      const input = route.request().postDataJSON();
      const task = { ...input, id: 'custom', assignedBy: 'user', assigneeName: users.find(u => u.id === input.assignedTo)?.name ?? '', completedAt: null, createdAt: '2026-10-06T12:00:00Z', systemTaskKind: null };
      tasks = [task]; await route.fulfill({ status: method === 'POST' ? 201 : 200, json: task }); return;
    }
    if (path === '/gross-board') {
      const requested = url.searchParams.get('weekStart'); grossRequests.push(requested);
      await route.fulfill({ json: { weekStart: requested, drivers: [], entries: [], balances: [] } }); return;
    }
    const fixtures = {
      '/auth/session': { user: { id: 'user', username: 'Test user', permissions: ['tasks.read', 'tasks.write', 'fleet.read', 'access.manage', 'board.read', 'driver_board.read'] }, csrfToken: 'fixture' },
      '/settings/access': { users: users.map(u => ({ id: u.id, username: u.name, email: `${u.id}@example.com`, active: true, roleId: 'role', version: 1 })), roles: [], permissions: [] },
      '/settings/system-tasks': assignments,
      '/tasks/users': users,
      '/tasks/custom': { items: tasks, total: tasks.length, page: 1, pageSize: 25, totalPages: 1 },
      '/tasks/relay-identities': { items: [], total: 0, page: 1, pageSize: 25, totalPages: 1 },
      '/driver-intake': { items: [], total: 0, page: 1, pageSize: 25, totalPages: 1 },
      '/driver-board': driverBoard, '/drivers': [],
    };
    await route.fulfill({ json: fixtures[path] ?? [] });
  });
  await page.goto(`${base}/settings`);
  await page.getByRole('button', { name: 'System tasks', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Create role', exact: true })).toHaveCount(0);
  await page.getByLabel('Driver onboarding', { exact: true }).selectOption('teammate');
  await page.getByRole('button', { name: 'Save Driver onboarding assignment', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText('Driver onboarding assignment saved.');
  await page.reload();
  await expect(page.getByLabel('Driver onboarding', { exact: true })).toHaveValue('teammate');
  await page.goto(`${base}/tasks`);
  await expect(page.getByRole('heading', { name: 'Tasks', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'System tasks', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Add task', exact: true }).click();
  await page.getByLabel('Title', { exact: true }).fill('Follow up');
  await page.getByLabel('Assign to', { exact: true }).selectOption('teammate');
  await page.getByRole('dialog').getByRole('button', { name: 'Add task', exact: true }).click();
  await expect(page.getByText('Assigned to Teammate', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Edit Follow up', exact: true }).click();
  await page.getByLabel('Assign to', { exact: true }).selectOption('user');
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByText('Assigned to Test user', { exact: true })).toBeVisible();
  await page.goto(`${base}/driver-board`);
  const header = page.getByRole('columnheader', { name: 'Driver type', exact: true });
  await expect(page.getByRole('cell', { name: '%-O', exact: true })).toBeVisible();
  expect(await header.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
  expect(await page.getByRole('cell', { name: '%-O', exact: true }).evaluate(el => getComputedStyle(el).backgroundColor)).toBe('rgba(0, 0, 0, 0)');
  await expect(page.getByRole('button', { name: 'Customize view', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'My view', exact: true }).dblclick();
  await page.getByRole('checkbox', { name: 'Test Dispatcher', exact: true }).check();
  await page.getByRole('button', { name: 'Use as My view', exact: true }).click();
  await page.getByRole('button', { name: 'All drivers', exact: true }).click();
  await page.getByRole('button', { name: 'My view', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await page.getByRole('button', { name: 'My view', exact: true }).dblclick();
  await expect(page.getByRole('dialog')).toBeVisible();
  await expect(page.getByRole('checkbox', { name: 'Test Dispatcher', exact: true })).toBeChecked();
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.screenshot({ path: join(tmpdir(), 'mserp-status-board-views.png') });
  await page.evaluate(() => sessionStorage.setItem('mserp-navigation-v1:user:/gross-board:view:page:week', JSON.stringify('2026-09-07')));
  await page.goto(`${base}/gross-board`);
  await expect.poll(() => grossRequests.at(-1)).toBe(week);
  await page.getByRole('button', { name: 'Previous week', exact: true }).click();
  await expect.poll(() => grossRequests.at(-1)).toBe('2026-09-28');
  await page.reload();
  await expect.poll(() => grossRequests.at(-1)).toBe(week);
  await page.getByRole('button', { name: 'Previous week', exact: true }).click();
  await page.goto(`${base}/tasks`);
  await page.getByRole('link', { name: 'Gross Board', exact: true }).click();
  await expect.poll(() => grossRequests.at(-1)).toBe(week);
  await page.goto(`${base}/gross-board?driverId=${driverId}&date=2026-09-15&slot=0&loadNumber=TEST&from=driver-pay`);
  await expect.poll(() => grossRequests.at(-1)).toBe('2026-09-14');
  expect(errors).toEqual([]);
  console.log('Task and board UI checks passed: Settings assignments, custom user/self assignment, board layout, My view double-click, current New York week and explicit payroll links.');
} finally {
  await browser?.close(); await new Promise(resolve => server.close(resolve));
}
