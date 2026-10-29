// Run after NEXT_PUBLIC_API_URL=/api npm run build. API/database authorization
// and concurrent annual claims are covered by TestUserProfilesDatabase.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname } from 'node:path';

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
  const context = await browser.newContext({ timezoneId: 'America/New_York', viewport: { width: 1440, height: 900 } });
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  let administrator = true, saved, birthday = false, claimed = false;
  const checks = [];
  const data = { users: [{ id: 'alex', username: 'Alex Smith', firstName: 'Alex', lastName: 'Smith', email: 'alex@example.com', dateOfBirth: '2000-10-29', phone: '+1 555 123 4567', roleId: 'staff', active: true, version: 1 }], roles: [{ id: 'staff', name: 'Staff', permissions: [], expenseCategoryAccess: [], version: 1 }], permissions: [], expenseCategories: [] };
  await context.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/auth/session') return route.fulfill({ json: { user: { id: 'alex', username: 'Alex Smith', administrator, theme: 'default', permissions: ['access.manage'], expenseCategoryAccess: [] }, csrfToken: 'csrf' } });
    if (path === '/api/auth/birthday') {
      expect(route.request().headers()['x-csrf-token']).toBe('csrf');
      expect(route.request().postData()).toBeNull();
      checks.push(true);
      const celebrate = birthday && !claimed;
      if (celebrate) claimed = true;
      return route.fulfill({ json: { celebrate, ...(celebrate ? { name: 'Alex' } : {}) } });
    }
    if (path === '/api/settings/access') return route.fulfill({ json: { ...data, users: data.users.map(user => { if (administrator) return user; const safe = { ...user }; delete safe.dateOfBirth; delete safe.phone; return safe; }) } });
    if (path.startsWith('/api/settings/users') && ['PUT', 'POST'].includes(route.request().method())) {
      saved = route.request().postDataJSON();
      return route.fulfill({ json: { id: 'alex' } });
    }
    return route.fulfill({ json: [] });
  });
  await page.clock.setFixedTime(new Date('2026-10-28T18:59:59.000Z')); // Tashkent 23:59:59, office PC still on ET.
  await page.goto(base + '/settings');
  await expect.poll(() => checks.length).toBe(1);
  await expect(page.getByText('Happy birthday, Alex!')).toHaveCount(0);
  await page.getByRole('tab', { name: 'Users', exact: true }).click();
  await page.getByRole('button', { name: 'Edit', exact: true }).click();
  let dialog = page.getByRole('dialog', { name: 'Edit user', exact: true });
  await expect(dialog.getByLabel('First name', { exact: true })).toHaveValue('Alex');
  await expect(dialog.getByLabel('Last name', { exact: true })).toHaveValue('Smith');
  await expect(dialog.getByLabel('Date of birth', { exact: true })).toHaveValue('2000-10-29');
  await expect(dialog.getByLabel('Phone number', { exact: true })).toHaveValue('+1 555 123 4567');
  await dialog.getByLabel('First name', { exact: true }).fill('Alexandra');
  await dialog.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect(saved.firstName).toBe('Alexandra');
  expect(saved.dateOfBirth).toBe('2000-10-29');
  expect(saved.phone).toBe('+1 555 123 4567');
  // The first action after local midnight bypasses the normal one-minute throttle.
  birthday = true;
  await page.clock.setFixedTime(new Date('2026-10-28T19:00:01.000Z'));
  await page.keyboard.press('ArrowDown');
  await expect(page.getByText('Happy birthday, Alex! 🎉', { exact: true })).toBeVisible();
  await expect(page.locator('[data-birthday-confetti]')).toHaveCount(1);
  const afterBirthday = checks.length;
  await page.keyboard.press('ArrowDown');
  expect(checks.length).toBe(afterBirthday);
  await page.getByRole('button', { name: 'Dismiss birthday greeting' }).click();
  await expect(page.locator('[data-birthday-confetti]')).toHaveCount(0);
  await page.reload();
  await expect.poll(() => checks.length).toBe(afterBirthday + 1);
  await expect(page.getByText('Happy birthday, Alex! 🎉', { exact: true })).toHaveCount(0);
  administrator = false;
  await page.reload();
  await page.getByRole('tab', { name: 'Users', exact: true }).click();
  await page.getByRole('button', { name: 'Edit', exact: true }).click();
  dialog = page.getByRole('dialog', { name: 'Edit user', exact: true });
  await expect(dialog.getByLabel('Date of birth')).toHaveCount(0);
  await expect(dialog.getByLabel('Phone number')).toHaveCount(0);
  await dialog.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect(saved).not.toHaveProperty('dateOfBirth');
  expect(saved).not.toHaveProperty('phone');
  // A different PC timezone still gets the greeting; reduced motion suppresses animation.
  const reduced = await browser.newContext({ timezoneId: 'America/Los_Angeles', reducedMotion: 'reduce' });
  const reducedPage = await reduced.newPage();
  await reducedPage.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/auth/session') return route.fulfill({ json: { user: { id: 'local', username: 'Alex', permissions: [], theme: 'default' }, csrfToken: 'csrf' } });
    if (path === '/api/auth/birthday') return route.fulfill({ json: { celebrate: true, name: 'Alex' } });
    return route.fulfill({ json: [] });
  });
  await reducedPage.goto(base + '/settings');
  await expect(reducedPage.getByText('Happy birthday, Alex! 🎉', { exact: true })).toBeVisible();
  await expect(reducedPage.locator('[data-birthday-confetti]')).toHaveCount(0);
  // A server-synchronized countdown on a PC configured for Eastern time.
  const midnightContext = await browser.newContext({ timezoneId: 'America/New_York' });
  const midnightPage = await midnightContext.newPage();
  let midnightClaims = 0;
  await midnightPage.clock.install({ time: new Date('2026-10-28T18:59:53Z') });
  await midnightPage.clock.pauseAt(new Date('2026-10-28T18:59:54Z'));
  await midnightPage.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/auth/session') return route.fulfill({ json: { user: { id: 'midnight', username: 'Alex', permissions: [], theme: 'default' }, csrfToken: 'csrf' } });
    if (path === '/api/auth/birthday') {
      midnightClaims++;
      return route.fulfill({ json: midnightClaims === 1
        ? { celebrate: false, countdownAt: '2026-10-28T19:00:00Z', serverNow: '2026-10-28T18:59:54Z' }
        : { celebrate: midnightClaims === 2, name: 'Alex', serverNow: '2026-10-28T19:00:00Z' } });
    }
    return route.fulfill({ json: [] });
  });
  await midnightPage.goto(base + '/settings');
  await expect.poll(async () => { await midnightPage.clock.runFor(50); return midnightClaims; }).toBe(1);
  await midnightPage.clock.runFor(1000);
  const countdown = midnightPage.getByRole('timer', { name: 'Birthday countdown' });
  await expect(countdown).toHaveText('5');
  expect(await countdown.evaluate(el => getComputedStyle(el).backgroundColor)).toBe('rgb(0, 0, 0)');
  for (const number of [4, 3, 2, 1, 0]) { await midnightPage.clock.runFor(1000); await expect(countdown).toHaveText(String(number)); }
  await expect.poll(() => midnightClaims).toBe(2);
  await midnightPage.clock.runFor(400);
  await expect(countdown).toHaveCount(0);
  await expect(midnightPage.getByText('Happy birthday, Alex! 🎉', { exact: true })).toBeVisible();
  await expect(midnightPage.locator('[data-birthday-confetti]')).toHaveCount(1);
  await midnightPage.screenshot({ path: process.env.TEMP + '/clustie-birthday-midnight.png' });
  await midnightPage.clock.runFor(65000);
  await midnightPage.keyboard.press('ArrowDown');
  await expect.poll(() => midnightClaims).toBe(3);
  await expect(countdown).toHaveCount(0);
  await midnightContext.close();
  // An idle user gets no screen takeover or greeting claim at midnight.
  const idleContext = await browser.newContext({ timezoneId: 'America/New_York' });
  const idlePage = await idleContext.newPage();
  let idleClaims = 0;
  await idlePage.clock.install({ time: new Date('2026-10-28T18:57:59Z') });
  await idlePage.clock.pauseAt(new Date('2026-10-28T18:58:00Z'));
  await idlePage.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/auth/session') return route.fulfill({ json: { user: { id: 'idle', username: 'Alex', permissions: [], theme: 'default' }, csrfToken: 'csrf' } });
    if (path === '/api/auth/birthday') {
      idleClaims++;
      return route.fulfill({ json: idleClaims === 1
        ? { celebrate: false, countdownAt: '2026-10-28T19:00:00Z', serverNow: '2026-10-28T18:58:00Z' }
        : { celebrate: idleClaims === 2, name: 'Alex', serverNow: '2026-10-28T19:00:01Z' } });
    }
    return route.fulfill({ json: [] });
  });
  await idlePage.goto(base + '/settings');
  await expect.poll(async () => { await idlePage.clock.runFor(50); return idleClaims; }).toBe(1);
  await idlePage.clock.runFor(121000);
  expect(idleClaims).toBe(1);
  await expect(idlePage.getByRole('timer', { name: 'Birthday countdown' })).toHaveCount(0);
  await expect(idlePage.getByText('Happy birthday, Alex! 🎉', { exact: true })).toHaveCount(0);
  await idlePage.keyboard.press('ArrowDown');
  await expect.poll(() => idleClaims).toBe(2);
  await expect(idlePage.getByText('Happy birthday, Alex! 🎉', { exact: true })).toBeVisible();
  expect(errors).toEqual([]);
  console.log('User profiles/birthdays passed: admin privacy UI, name/private-field saves, Tashkent midnight on an ET computer, 5-to-0 blackout countdown, annual repeat handling and reduced motion.');
} finally { await browser?.close(); await new Promise(resolve => server.close(resolve)); }
