// Fixture-only regression for sidebar branding, including actual wordmark contrast.
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
    if (path === '/api/settings/theme-usage') return route.fulfill({ json: { counts: {}, totalUsers: 0 } });
    if (path === '/api/tasks/count') return route.fulfill({ json: { count: 0 } });
    if (path === '/api/sidebar-counts') return route.fulfill({ json: { fuel: 0, tolls: 0, weighMyTruck: 0 } });
    return route.fulfill({ json: [] });
  });
  const logoGate = new Promise(done => { releaseLogo = done; });
  await page.route('**/brand/clustie-logo-dark.png', async route => {
    await logoGate;
    await route.continue();
  });
  await page.goto(`${base}/settings`, { waitUntil: 'domcontentloaded' });
  await expect(page.getByRole('heading', { name: 'Color theme', exact: true })).toBeVisible();
  const logo = page.locator('aside').getByRole('img', { name: 'Clustie', exact: true });
  const badge = page.locator('aside').getByText('beta', { exact: true });
  async function frame(collapsed) {
    const rect = await logo.boundingBox();
    expect(rect.width).toBeCloseTo(collapsed ? 32 : 96, 1);
    expect(rect.height).toBeCloseTo(collapsed ? 32 : 96 * 153 / 640, 1);
    // The actual mark has padding in the square export. Match visible ink,
    // rather than stretching the horizontal wordmark to a 32px height.
    if (!collapsed) expect(Math.abs(rect.height * 152 / 153 - 32 * 137 / 192)).toBeLessThan(1);
    if (collapsed) await expect(badge).toBeHidden();
    else await expect(badge).toBeVisible();
  }
  async function letterPixels(kind) {
    const png = await logo.screenshot();
    return page.evaluate(async ({ png, kind }) => {
      const bitmap = await createImageBitmap(await (await fetch(`data:image/png;base64,${png}`)).blob());
      const canvas = new OffscreenCanvas(bitmap.width, bitmap.height);
      const ctx = canvas.getContext('2d');
      ctx.drawImage(bitmap, 0, 0);
      // Sample the lettering, to the right of the colored symbol.
      const { data } = ctx.getImageData(28, 0, bitmap.width - 28, bitmap.height);
      let count = 0;
      for (let i = 0; i < data.length; i += 4) {
        const [r, g, b] = data.subarray(i, i + 3);
        if (kind === 'white' ? r > 220 && g > 220 && b > 220
          : r >= 14 && r <= 26 && g >= 30 && g <= 46 && b >= 65 && b <= 90) count++;
      }
      return count;
    }, { png: png.toString('base64'), kind });
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
  expect(await logo.evaluate(el => getComputedStyle(el.closest('aside > div')).overflowX)).toBe('hidden');
  expect(await letterPixels('navy')).toBe(0);
  releaseLogo();
  await expect.poll(() => letterPixels('white')).toBeGreaterThan(100);
  for (let toggle = 0; toggle < 12; toggle++) {
    await page.getByRole('button', { name: toggle % 2 ? 'Expand sidebar' : 'Collapse sidebar', exact: true }).click();
    await frame(toggle % 2 === 0);
  }
  await page.screenshot({ animations: 'disabled', path: join(tmpdir(), 'clustie-sidebar-small-dark.png') });
  await page.getByRole('button', { name: /^Default Light/ }).click();
  await frame(false);
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'default-light');
  await expect.poll(() => letterPixels('navy')).toBeGreaterThan(100);
  await page.getByRole('button', { name: /^Solarized Light/ }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'solarized-light');
  await expect.poll(() => letterPixels('navy')).toBeGreaterThan(100);
  await page.getByRole('button', { name: /^Clustie \(default\)/ }).click();
  await expect.poll(() => letterPixels('white')).toBeGreaterThan(100);
  await page.getByRole('button', { name: /^Default Light/ }).click();
  await expect.poll(() => letterPixels('navy')).toBeGreaterThan(100);
  await page.screenshot({ animations: 'disabled', path: join(tmpdir(), 'clustie-sidebar-small-light.png') });
  await page.goto(`${base}/login`);
  const loginLogo = page.getByRole('img', { name: 'Clustie', exact: true });
  await expect(loginLogo).toBeVisible();
  expect((await loginLogo.boundingBox()).width).toBe(224);
  expect(errors).toEqual([]);
  console.log('Sidebar branding passed: fixed dimensions, slow image loading, badge visibility, painted theme contrast, and login size.');
} finally {
  releaseLogo?.();
  await browser.close();
  server.closeAllConnections();
  await new Promise(done => server.close(done));
}
