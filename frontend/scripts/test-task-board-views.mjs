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
let taskReads = 0;
let taskWrites = 0;
let theme = 'default';
const streams = new Set();
const pushTasks = () => { for (const stream of streams) stream.write('event: changed\ndata: {}\n\n'); };
const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    if (path === '/api/tasks/events') {
      res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' });
      res.write(': connected\n\n'); streams.add(res); req.on('close', () => streams.delete(res)); return;
    }
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
    if (path === '/tasks/events') return route.continue();
    if (path === '/tasks/count') return route.fulfill({ json: { count: tasks.filter(t => !t.completedAt).length } });
    if (path === '/tasks') {
      taskReads++;
      const status = url.searchParams.get('status');
      const items = tasks.filter(t => status === 'all' || status === (t.status ?? (t.completedAt ? 'completed' : 'open')));
      return route.fulfill({ json: { items, total: items.length, page: 1, pageSize: 25, totalPages: 1 } });
    }
    if (method === 'PATCH' && path.startsWith('/tasks/custom/')) {
      taskWrites++;
      const task = tasks.find(t => t.id === path.split('/').at(-1));
      const status = route.request().postDataJSON().status;
      const completed = status === 'completed';
      Object.assign(task, { status, completedAt: completed ? '2026-10-08T17:00:00Z' : null, completedByName: completed ? 'Test user' : '' });
      return route.fulfill({ json: task });
    }
    if (method === 'PUT' && path.startsWith('/settings/system-tasks/')) {
      const input = route.request().postDataJSON();
      Object.assign(assignments.find(v => v.kind === input.kind), input, { version: input.version + 1 });
      await route.fulfill({ status: 204 }); return;
    }
    if ((method === 'POST' || method === 'PUT') && path.startsWith('/tasks/custom')) {
      const input = route.request().postDataJSON();
      const task = { ...input, id: 'custom', assignedBy: 'user', assignerName: 'Test user', assigneeName: users.find(u => u.id === input.assignedTo)?.name ?? '', completedAt: null, createdAt: '2026-10-06T12:00:00Z', systemTaskKind: null };
      tasks = [task]; await route.fulfill({ status: method === 'POST' ? 201 : 200, json: task }); return;
    }
    if (path === '/gross-board') {
      const requested = url.searchParams.get('weekStart'); grossRequests.push(requested);
      await route.fulfill({ json: { weekStart: requested, drivers: [], entries: [], balances: [] } }); return;
    }
    const fixtures = {
      '/auth/session': { user: { id: 'user', username: 'Test user', theme, permissions: ['tasks.read', 'tasks.write', 'fleet.read', 'access.manage', 'board.read', 'driver_board.read'] }, csrfToken: 'fixture' },
      '/settings/access': { users: users.map(u => ({ id: u.id, username: u.name, email: `${u.id}@example.com`, active: true, roleId: 'role', version: 1 })), roles: [], permissions: [] },
      '/settings/system-tasks': assignments,
      '/tasks/users': users,
      '/tasks/custom': { items: tasks, total: tasks.length, page: 1, pageSize: 25, totalPages: 1 },
      '/tasks/relay-identities': { items: [{ id: 'system', name: 'New driver', environment: 'production', relayDriverId: 'relay-fixture', suggestions: [{ driverId: 'driver', name: 'New driver', active: true, reasons: ['Same email'] }], rejectedDriverIds: [], transactionCount: 1 }], total: 1, page: 1, pageSize: 25, totalPages: 1 },
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
  await expect(page.getByRole('cell', { name: 'Teammate', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Edit Follow up', exact: true }).click();
  await page.getByLabel('Assign to', { exact: true }).selectOption('user');
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByRole('row').filter({ hasText: 'Follow up' })).toContainText('Test user');
  const links = await page.locator('aside nav > a').evaluateAll(els => els.slice(0, 3).map(el => el.getAttribute('href')));
  expect(links).toEqual(['/driver-board', '/gross-board', '/tasks']);
  await expect(page.getByLabel('1 open tasks', { exact: true })).toBeVisible();
  await expect.poll(() => streams.size).toBe(1);
  // Idle time no longer triggers task availability requests.
  await page.clock.runFor(1000);
  const readsBeforeIdle = taskReads;
  await page.clock.runFor(60000);
  expect(taskReads).toBe(readsBeforeIdle);
  tasks.push({ id: 'system', title: 'Review Relay account: New driver', notes: '', assigneeName: 'Teammate', assignerName: 'System', systemTaskKind: 'relay_review', completedAt: null, createdAt: '2026-10-08T12:00:00Z' });
  pushTasks(); await page.clock.runFor(1000);
  await expect(page.getByRole('button', { name: 'Review Relay account: New driver', exact: true })).toBeVisible();
  await expect(page.getByLabel('2 open tasks', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Kanban', exact: true }).click();
  const open = page.getByRole('region', { name: 'Open tasks', exact: true });
  const done = page.getByRole('region', { name: 'Completed tasks', exact: true });
  const inProcess = page.getByRole('region', { name: 'In process tasks', exact: true });
  await expect(open.getByRole('article', { name: 'Follow up', exact: true })).toHaveAttribute('draggable', 'true');
  await expect(open.getByRole('article', { name: 'Review Relay account: New driver', exact: true })).toHaveAttribute('draggable', 'false');
  await expect(open.getByLabel('Assignee: Test user', { exact: true })).toBeVisible();
  await expect(open.getByRole('article', { name: 'Follow up', exact: true })).not.toContainText('Assigned by');
  await open.getByRole('article', { name: 'Follow up', exact: true }).dragTo(inProcess);
  await expect(inProcess.getByRole('article', { name: 'Follow up', exact: true })).toBeVisible();
  await page.reload();
  await expect(inProcess.getByRole('article', { name: 'Follow up', exact: true })).toBeVisible();
  await expect(page.getByLabel('2 open tasks', { exact: true })).toBeVisible();
  await inProcess.getByRole('article', { name: 'Follow up', exact: true }).dragTo(done);
  await expect(done.getByRole('article', { name: 'Follow up', exact: true })).not.toContainText('Completed by');
  await expect(done.getByRole('button', { name: 'Follow up', exact: true })).toHaveAttribute('title', /Completed by Test user/);
  expect(taskWrites).toBe(2);
  await done.getByRole('button', { name: 'Reopen Follow up', exact: true }).click();
  await expect(open.getByRole('article', { name: 'Follow up', exact: true })).toBeVisible();
  expect(taskWrites).toBe(3);
  await page.getByRole('button', { name: 'List', exact: true }).click();
  await page.getByRole('button', { name: 'Review Relay account: New driver', exact: true }).click();
  await page.getByRole('button', { name: 'Review account', exact: true }).click();
  await page.getByRole('button', { name: 'Review link', exact: true }).first().click();
  await expect(page.getByRole('dialog')).toContainText('This can change driver settlements.');
  await expect(page.getByRole('button', { name: 'Link account', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  // Source workflow completion arrives on the same stream and remains in history.
  Object.assign(tasks.find(t => t.id === 'system'), { completedAt: '2026-10-08T18:00:00Z', completedByName: 'Teammate' });
  pushTasks(); await page.clock.runFor(1000);
  await page.getByLabel('Task status', { exact: true }).selectOption('completed');
  await expect(page.getByRole('row').filter({ hasText: 'Review Relay account: New driver' })).toContainText('Teammate');
  await expect(page.getByRole('columnheader', { name: 'Completed by', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Review Relay account: New driver', exact: true })).toHaveAttribute('title', /Completed by Teammate/);
  await page.getByLabel('Task status', { exact: true }).selectOption('all');
  for (const palette of ['default', 'solarized-light', 'solarized-dark', 'monokai', 'monokai-dimmed', 'dark-modern', 'default-light']) {
    theme = palette; await page.reload();
    await expect(page.locator('html')).toHaveAttribute('data-theme', palette);
    await expect(page.getByRole('button', { name: 'Follow up', exact: true })).toBeVisible();
    await page.clock.runFor(1000);
    await page.screenshot({ animations: 'disabled', path: join(tmpdir(), `mserp-tasks-list-${palette}.png`) });
    await page.getByRole('button', { name: 'Kanban', exact: true }).click();
    await expect(page.getByRole('article', { name: 'Follow up', exact: true })).toBeVisible();
    expect(await page.getByRole('button', { name: 'Kanban', exact: true }).evaluate(el => el.getBoundingClientRect().height)).toBeGreaterThanOrEqual(32);
    await page.screenshot({ animations: 'disabled', path: join(tmpdir(), `mserp-tasks-kanban-${palette}.png`) });
    await page.setViewportSize({ width: 640, height: 900 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.getByRole('button', { name: 'Edit Follow up', exact: true }).click();
    await expect(page.getByRole('dialog')).toBeVisible();
    const dialogBackground = await page.getByRole('dialog').locator('form').evaluate(el => getComputedStyle(el).backgroundColor);
    expect(dialogBackground).not.toBe('rgba(0, 0, 0, 0)');
    await page.getByRole('button', { name: 'Cancel', exact: true }).click();
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.getByRole('button', { name: 'List', exact: true }).click();
  }
  theme = 'default';
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
  console.log('Task and board UI checks passed: assignments, unified history, push updates without polling, counts, Kanban drag/reopen, locked system tasks, all seven themes, narrow layouts and existing board navigation.');
} finally {
  await browser?.close(); await new Promise(resolve => server.close(resolve));
}
