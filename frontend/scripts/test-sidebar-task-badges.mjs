// Fixture browser checks; assignment/count authorization is covered by the real DB suite.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname } from 'node:path';

const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = path === '/' ? 'index.html' : extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.png': 'image/png' }[extname(file)] ?? 'application/octet-stream' });
    res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(done => server.listen(0, '127.0.0.1', done));
const browser = await chromium.launch();
const base = `http://127.0.0.1:${server.address().port}`;
try {
  for (const administrator of [false, true]) {
    const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    await page.clock.setFixedTime(new Date('2026-10-27T16:00:00Z'));
    const errors = [];
    let sidebarRequests = 0;
    page.on('pageerror', error => errors.push(error.message));
    await page.route('**/api/**', route => {
      const path = new URL(route.request().url()).pathname;
      if (path === '/api/auth/session') return route.fulfill({ json: { user: {
        id: 'badge-fixture', username: 'Badge fixture', administrator, theme: 'default',
        permissions: ['tasks.read', 'fuel.read', 'tolls.read', 'weighmytruck.read'],
      }, csrfToken: 'fixture' } });
      // Deliberately return nonzero counts even to admin to check UI suppression.
      if (path === '/api/tasks/count') return route.fulfill({ json: { count: 3 } });
      if (path === '/api/tasks') return route.fulfill({ json: { items: [
        { id: 'open', title: 'Employee system task', systemTaskKind: 'maintenance_pm', assigneeName: 'Employee', createdAt: '2026-10-24T16:00:00Z', completedAt: null },
        { id: 'completed', title: 'Completed task', assigneeName: 'Employee', createdAt: '2026-10-20T12:00:00Z', completedAt: '2026-10-20T14:00:00Z' },
      ], total: 2, page: 1, pageSize: 25, totalPages: 1 } });
      if (path === '/api/feedback/messages') return route.fulfill({ json: { items: [], total: 0, page: 1, pageSize: 10, totalPages: 1 } });
      if (path === '/api/feedback/messages/count') return route.fulfill({ json: { count: 2 } });
      if (path === '/api/sidebar-counts') {
        sidebarRequests++;
        return route.fulfill({ json: { fuel: 7, tolls: 8, weighMyTruck: 9 } });
      }
      if (path === '/api/tasks/events') return route.fulfill({ contentType: 'text/event-stream', body: ': fixture\n\n' });
      return route.fulfill({ json: [] });
    });
    await page.goto(`${base}/settings`);
    const sidebar = page.locator('aside');
    await expect(sidebar.locator('a[href="/tasks"]')).toBeVisible();
    const badges = sidebar.locator('span[title]');
    if (administrator) {
      await expect(badges).toHaveCount(0);
    } else {
      await expect(badges).toHaveCount(1);
      await expect(badges).toHaveAttribute('aria-label', '3 assigned tasks and 2 unread feedback updates');
      await expect(badges).toHaveText('5');
    }
    await sidebar.getByRole('button', { name: 'Operating Costs', exact: true }).click();
    await expect(sidebar.getByRole('link', { name: 'Fuel', exact: true })).toBeVisible();
    await expect(badges).toHaveCount(administrator ? 0 : 1);
    await sidebar.getByRole('button', { name: 'Collapse sidebar', exact: true }).click();
    await expect(badges).toHaveCount(administrator ? 0 : 1);
    await expect(sidebar.locator('a[href="/tasks"]')).toBeVisible();
    expect(sidebarRequests).toBe(0);
    await page.goto(`${base}/tasks`);
    await expect(page.locator('thead th')).toHaveText(['Task', 'Status', 'Assignee', 'Age', '']);
    const openRow = page.getByRole('row').filter({ hasText: 'Employee system task' });
    await expect(openRow.locator('td').nth(2)).toHaveText('Employee');
    await expect(openRow.locator('td').nth(3)).toHaveText('3d');
    await expect(openRow.locator('td').nth(3)).toHaveAttribute('title', /Time since creation/);
    const completedRow = page.getByRole('row').filter({ hasText: 'Completed task' });
    await expect(completedRow.locator('td').nth(3)).toHaveText('2h');
    await expect(completedRow.locator('td').nth(3)).toHaveAttribute('title', /Time from creation to completion/);
    expect(errors).toEqual([]);
    await page.close();
  }
  console.log('Task badges and age passed: personal counts, private feedback, admin suppression, expanded/collapsed navigation, and task age after assignee.');
} finally {
  await browser.close();
  server.closeAllConnections();
  await new Promise(done => server.close(done));
}
