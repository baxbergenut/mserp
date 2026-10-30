// Browser coverage for independent navigation scrolling in a short viewport.
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
try {
  for (const width of [1280, 390]) {
    const page = await browser.newPage({ viewport: { width, height: 360 } });
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.route('**/api/**', route => {
      const path = new URL(route.request().url()).pathname;
      if (path === '/api/auth/session') return route.fulfill({ json: { user: {
        id: 'sidebar-scroll', username: 'Scroll fixture', theme: 'default',
        permissions: ['fuel.read', 'tolls.read', 'weighmytruck.read', 'trucks.read', 'drivers.read', 'driver_pay.read', 'investor_pay.read', 'dispatcher_pay.read', 'escrow.read', 'charges.read'],
      }, csrfToken: 'fixture' } });
      if (path.endsWith('/count')) return route.fulfill({ json: { count: 0 } });
      return route.fulfill({ json: [] });
    });
    await page.goto(`http://127.0.0.1:${server.address().port}/settings`);
    const sidebar = page.locator('aside');
    const nav = sidebar.locator('nav');
    await expect(nav).toBeVisible();
    if (width < 640) await sidebar.getByRole('button', { name: 'Expand sidebar', exact: true }).click();
    for (const name of ['Operating Costs', 'Fleet', 'Accounting', 'HR']) {
      await sidebar.getByRole('button', { name, exact: true }).click();
    }
    const logo = sidebar.getByRole('img', { name: 'Clustie', exact: true });
    const collapse = sidebar.getByRole('button', { name: 'Collapse sidebar', exact: true });
    const logoTop = (await logo.boundingBox()).y;
    const footerTop = (await collapse.boundingBox()).y;
    await expect.poll(() => nav.evaluate(el => el.scrollHeight > el.clientHeight)).toBe(true);
    await nav.evaluate(el => { el.scrollTop = 0; });
    await nav.hover();
    await page.mouse.wheel(0, 2000);
    await expect.poll(() => nav.evaluate(el => el.scrollTop)).toBeGreaterThan(0);
    await sidebar.getByRole('link', { name: 'Settings', exact: true }).scrollIntoViewIfNeeded();
    await expect(sidebar.getByRole('link', { name: 'Settings', exact: true })).toBeInViewport();
    expect((await logo.boundingBox()).y).toBe(logoTop);
    expect((await collapse.boundingBox()).y).toBe(footerTop);
    await expect(collapse).toBeInViewport();
    expect(await page.evaluate(() => document.scrollingElement.scrollTop)).toBe(0);
    expect(await page.locator('main').evaluate(el => el.scrollTop)).toBe(0);
    // The navigation must remain usable after collapsing into an icon rail.
    await collapse.click();
    await expect.poll(() => nav.evaluate(el => el.scrollHeight > el.clientHeight)).toBe(true);
    await nav.evaluate(el => { el.scrollTop = el.scrollHeight; });
    await expect(sidebar.getByRole('link', { name: 'Settings', exact: true })).toBeInViewport();
    await expect(sidebar.getByRole('button', { name: 'Expand sidebar', exact: true })).toBeInViewport();
    expect(errors).toEqual([]);
    await page.close();
  }
  console.log('Sidebar scroll passed: desktop/mobile, expanded groups, collapsed rail, fixed logo/footer, and independent page scroll.');
} finally {
  await browser.close();
  server.closeAllConnections();
  await new Promise(done => server.close(done));
}
