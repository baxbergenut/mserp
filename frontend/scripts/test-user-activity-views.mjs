// Run against a NEXT_PUBLIC_API_URL=/api static build.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';

const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = path === '/' ? 'index.html' : extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css' }[extname(file)] ?? 'text/plain' });
    res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
let browser;
try {
  browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  await page.clock.install();
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  let online = true, accessReads = 0, savedUser;
  const users = [
    { id: 'online', firstName: 'Alex', lastName: 'Morgan', username: 'Alex Morgan', email: 'alex@example.test', roleId: 'admin', active: true, version: 1, lastLoginAt: '2026-10-10T14:20:00Z', lastActivityAt: '2026-10-18T14:20:00Z' },
    { id: 'offline', username: 'Jordan Lee', email: 'jordan@example.test', roleId: 'admin', active: true, version: 1, lastLoginAt: '2026-10-10T14:20:00Z', lastActivityAt: '2026-10-17T18:45:00Z' },
    { id: 'unknown', username: 'Sam Rivera', email: 'sam@example.test', roleId: 'admin', active: true, version: 1, lastLoginAt: null, lastActivityAt: null },
    { id: 'disabled', username: 'Taylor Brooks', email: 'taylor@example.test', roleId: 'admin', active: false, version: 1, lastLoginAt: '2026-10-10T14:20:00Z', lastActivityAt: '2026-10-16T12:00:00Z' },
  ];
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname.slice(4);
    if (path === '/auth/session') return route.fulfill({ json: { user: { id: 'admin', username: 'Admin', theme: 'default', permissions: ['access.manage'], expenseCategoryAccess: [] }, csrfToken: 'test' } });
    if (path === '/settings/access') {
      accessReads++;
      return route.fulfill({ json: { users: users.map(user => ({ ...user, online: user.id === 'online' && online })), roles: [{ id: 'admin', name: 'Administrator', system: true, version: 1, permissions: [], expenseCategoryAccess: [] }], permissions: [], expenseCategories: [] } });
    }
    if (path === '/settings/users/online') {
      savedUser = route.request().postDataJSON();
      return route.fulfill({ json: { id: 'online' } });
    }
    return route.fulfill({ json: [] });
  });
  await page.goto(`http://127.0.0.1:${server.address().port}/settings`);
  await page.getByRole('tab', { name: 'Users', exact: true }).click();
  const row = name => page.getByRole('row').filter({ hasText: name });
  await expect(page.getByRole('columnheader', { name: 'Last activity', exact: true })).toBeVisible();
  await expect(row('Alex Morgan').getByText('Online', { exact: true })).toBeVisible();
  await expect(row('Jordan Lee').locator('time')).toContainText('Oct 17, 2026');
  await expect(row('Jordan Lee').locator('time')).toContainText('2:45 PM EDT');
  await expect(row('Sam Rivera').getByText('No activity recorded')).toBeVisible();
  await expect(row('Taylor Brooks').getByText('Disabled', { exact: true })).toBeVisible();
  await expect(row('Taylor Brooks').getByText('Online', { exact: true })).toHaveCount(0);
  // Presence changes on the existing visible-tab polling cadence.
  const beforePoll = accessReads;
  online = false;
  await page.clock.fastForward(30000);
  await expect.poll(() => accessReads).toBeGreaterThan(beforePoll);
  await expect(row('Alex Morgan').locator('time')).toContainText('10:20 AM EDT');
  await expect(row('Alex Morgan').locator('time')).toHaveAttribute('datetime', users[0].lastActivityAt);
  await expect(row('Jordan Lee').locator('time')).toHaveAttribute('datetime', users[1].lastActivityAt);
  // Refreshing activity does not reset an open form or send read-only data back.
  await row('Alex Morgan').getByRole('button', { name: 'Edit', exact: true }).click();
  await page.getByRole('dialog').locator('input').first().fill('Alex Edited');
  online = true;
  await page.clock.fastForward(30000);
  await expect(page.getByRole('dialog').locator('input').first()).toHaveValue('Alex Edited');
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect.poll(() => savedUser?.firstName).toBe('Alex Edited');
  expect(savedUser).not.toHaveProperty('online');
  expect(savedUser).not.toHaveProperty('lastLoginAt');
  expect(savedUser).not.toHaveProperty('lastActivityAt');
  await page.setViewportSize({ width: 640, height: 900 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.screenshot({ path: join(tmpdir(), 'mserp-user-activity.png'), fullPage: true });
  expect(errors).toEqual([]);
  console.log('User activity views passed: online/last-activity/unknown/disabled states, activity distinct from login, New York time, automatic refresh, form preservation, safe save payload and narrow layout.');
} finally {
  await browser?.close();
  await new Promise(resolve => server.close(resolve));
}
