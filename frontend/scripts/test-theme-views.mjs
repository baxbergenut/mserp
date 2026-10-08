// Run against the /api static build. Real persistence and authorization are
// covered by TestAccessDatabase; these fixtures exercise browser lifecycle/UI.
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
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  let user = 'viewer', failSave = false, accessReads = 0;
  const preferences = { viewer: 'default', teammate: 'default', admin: 'default' };
  const errors = []; page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname.slice(4);
    if (path === '/auth/session') return route.fulfill({ json: { user: { id: user, username: user, theme: preferences[user], permissions: user === 'admin' ? ['access.manage'] : [] }, csrfToken: 'test' } });
    if (path === '/auth/theme') {
      if (failSave) return route.fulfill({ status: 500, json: { error: 'Could not save theme' } });
      preferences[user] = route.request().postDataJSON().theme;
      return route.fulfill({ status: 204 });
    }
    if (path === '/settings/access') {
      accessReads++;
      return route.fulfill({ json: { users: [], roles: [], permissions: [], expenseCategories: [] } });
    }
    await route.fulfill({ json: [] });
  });
  await page.goto(`${base}/settings`);
  await expect(page.getByRole('heading', { name: 'Color theme', exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Settings', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Users', exact: true })).toHaveCount(0);
  expect(accessReads).toBe(0);
  const choices = [
    ['Solarized Light', 'solarized-light', 'rgb(253, 246, 227)', 'light'],
    ['Solarized Dark', 'solarized-dark', 'rgb(0, 43, 54)', 'dark'],
    ['Monokai Charcoal', 'monokai', 'rgb(39, 40, 34)', 'dark'],
    ['Monokai Dimmed', 'monokai-dimmed', 'rgb(30, 30, 30)', 'dark'],
    ['Dark Modern', 'dark-modern', 'rgb(31, 31, 31)', 'dark'],
    ['Default Light', 'default-light', 'rgb(255, 255, 255)', 'light'],
  ];
  for (const [label, id, background, scheme] of choices) {
    const button = page.getByRole('button', { name: new RegExp(`^${label}`) });
    await button.click();
    await expect(button).toBeEnabled();
    await expect(button).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('html')).toHaveAttribute('data-theme', id);
    expect(await page.evaluate(() => getComputedStyle(document.body).backgroundColor)).toBe(background);
    expect(await page.evaluate(() => getComputedStyle(document.documentElement).colorScheme)).toBe(scheme);
    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('data-theme', id);
  }
  // A failed save restores the last persisted appearance and offers a retry.
  failSave = true;
  await page.getByRole('button', { name: /^Monokai Charcoal/ }).click();
  await expect(page.getByRole('alert').filter({ hasText: 'Could not save theme' })).toBeVisible();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'default-light');
  failSave = false;
  user = 'teammate'; await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'default');
  user = 'viewer'; await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'default-light');
  await page.goto(`${base}/login`);
  await expect(page.locator('html')).not.toHaveAttribute('data-theme');
  // Dialog portals inherit light surfaces as well as the app shell.
  user = 'admin'; preferences.admin = 'default-light';
  await page.goto(`${base}/settings`);
  await page.getByRole('button', { name: 'Users', exact: true }).click();
  await page.getByRole('button', { name: 'Add user', exact: true }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
  expect(await page.getByRole('dialog').locator('form').evaluate(el => getComputedStyle(el).backgroundColor)).toBe('rgb(255, 255, 255)');
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.getByRole('button', { name: 'Appearance', exact: true }).click();
  await page.setViewportSize({ width: 640, height: 900 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.getByRole('button', { name: /^MSERP \(default\)/ }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'default');
  expect(errors).toEqual([]);
  console.log('Theme views passed: all palettes, persistence, per-user isolation, save failure, non-admin settings, logout reset, dialog portals and narrow layout.');
} finally {
  await browser?.close();
  await new Promise(resolve => server.close(resolve));
}
