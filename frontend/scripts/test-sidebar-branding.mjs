// Fixture-only regression for sidebar branding while images load and decode.
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
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.png': 'image/png' }[extname(file)] ?? 'application/octet-stream' });
    res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(done => server.listen(0, '127.0.0.1', done));
const browser = await chromium.launch();
const base = `http://127.0.0.1:${server.address().port}`;
let releaseLogo;
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  let theme = 'default';
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/auth/session') return route.fulfill({ json: { user: { id: 'branding', username: 'Branding', theme, permissions: [] }, csrfToken: 'fixture' } });
    if (path === '/api/auth/theme') { theme = route.request().postDataJSON().theme; return route.fulfill({ status: 204 }); }
    if (path === '/api/tasks/count') return route.fulfill({ json: { count: 0 } });
    if (path === '/api/sidebar-counts') return route.fulfill({ json: { fuel: 0, tolls: 0, weighMyTruck: 0 } });
    return route.fulfill({ json: [] });
  });
  const logoGate = new Promise(done => { releaseLogo = done; });
  await page.route('**/brand/clustie-logo.png', async route => {
    await logoGate;
    await route.continue();
  });
  await page.goto(`${base}/settings`, { waitUntil: 'domcontentloaded' });
  await expect(page.getByRole('heading', { name: 'Color theme', exact: true })).toBeVisible();
  const logo = page.locator('aside').getByRole('img', { name: 'Clustie', exact: true });
  async function frame(collapsed) {
    const rect = await logo.boundingBox();
    expect(rect.width).toBeCloseTo(collapsed ? 32 : 96, 1);
    expect(rect.height).toBeCloseTo(collapsed ? 32 : 96 * 153 / 640, 1);
    // The actual mark has padding in the square export. Match visible ink,
    // rather than stretching the horizontal wordmark to a 32px height.
    if (!collapsed) expect(Math.abs(rect.height * 152 / 153 - 32 * 137 / 192)).toBeLessThan(1);
  }
  await frame(false);
  await page.getByRole('button', { name: 'Collapse sidebar', exact: true }).click();
  await expect.poll(() => logo.evaluate(img => img.complete && img.naturalWidth)).toBe(192);
  await frame(true);
  // Hold the wordmark request while a decoded square icon is on screen. The
  // old shared image could temporarily use that square ratio at the full width.
  await page.getByRole('button', { name: 'Expand sidebar', exact: true }).click();
  for (let sample = 0; sample < 12; sample++) {
    await frame(false);
    await page.evaluate(() => new Promise(requestAnimationFrame));
  }
  expect(await logo.evaluate(img => getComputedStyle(img.closest('aside > div')).overflowX)).toBe('hidden');
  releaseLogo();
  await expect.poll(() => logo.evaluate(img => img.complete && img.naturalWidth)).toBe(640);
  for (let toggle = 0; toggle < 12; toggle++) {
    await page.getByRole('button', { name: toggle % 2 ? 'Expand sidebar' : 'Collapse sidebar', exact: true }).click();
    await frame(toggle % 2 === 0);
  }
  await page.screenshot({ animations: 'disabled', path: join(tmpdir(), 'clustie-sidebar-small-dark.png') });
  await page.getByRole('button', { name: /^Default Light/ }).click();
  await frame(false);
  await page.screenshot({ animations: 'disabled', path: join(tmpdir(), 'clustie-sidebar-small-light.png') });
  await page.goto(`${base}/login`);
  const loginLogo = page.getByRole('img', { name: 'Clustie', exact: true });
  await expect(loginLogo).toBeVisible();
  expect((await loginLogo.boundingBox()).width).toBe(224);
  expect(errors).toEqual([]);
  console.log('Sidebar branding passed: matching visible symbol size, slow image loading, rapid toggles, dark/light themes, and unchanged login size.');
} finally {
  releaseLogo?.();
  await browser.close();
  server.closeAllConnections();
  await new Promise(done => server.close(done));
}
