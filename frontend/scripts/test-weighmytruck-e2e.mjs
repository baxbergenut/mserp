// Called by TestWeighMyTruckDatabase: real Go routes, CSRF/permissions,
// PostgreSQL and server-side HTTP calls to a synthetic OAuth/provider service.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';

const api = process.env.MSERP_WMT_TEST_API;
if (!api || new URL(api).hostname !== '127.0.0.1') throw new Error('Local test API required');
const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    if (path.startsWith('/api/')) {
      const chunks = []; for await (const chunk of req) chunks.push(chunk);
      const response = await fetch(`${api}${req.url.slice(4)}`, { method: req.method,
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': req.headers['x-csrf-token'] ?? '' },
        body: ['GET', 'HEAD'].includes(req.method) ? undefined : Buffer.concat(chunks),
      });
      res.writeHead(response.status, { 'Content-Type': 'application/json' }); res.end(Buffer.from(await response.arrayBuffer())); return;
    }
    const file = path === '/' ? 'index.html' : extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] ?? 'application/octet-stream' }); res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const base = `http://127.0.0.1:${server.address().port}`;
const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 960 } });
  const errors = [], external = [];
  page.on('pageerror', e => errors.push(e.message));
  page.on('request', r => { if (!r.url().startsWith(base)) external.push(r.url()); });
  await page.goto(`${base}/weighmytruck`);
  await expect(page.getByRole('heading', { name: 'WeighMyTruck', exact: true })).toBeVisible();
  await expect(page.getByText('Terminated driver still has, or may have, WeighMyTruck access.')).toBeVisible();
  await expect(page.getByText('Not linked to an MSERP driver.')).toBeVisible();
  await page.getByRole('combobox', { name: 'Membership filter' }).selectOption('available');
  await page.getByRole('button', { name: 'Add Available Driver', exact: true }).dblclick();
  await expect(page.getByRole('status')).toHaveText('Available Driver added to WeighMyTruck.');
  await page.getByRole('combobox', { name: 'Membership filter' }).selectOption('enrolled');
  await page.getByRole('button', { name: 'Remove Available Driver', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText('Available Driver removed from WeighMyTruck.');
  await page.reload();
  await expect(page.getByRole('button', { name: 'Remove Available Driver', exact: true })).toHaveCount(0);
  await page.getByRole('combobox', { name: 'Membership filter' }).selectOption('all');
  await page.getByPlaceholder('Search drivers, email or code').fill('Available Driver');
  await expect(page.getByRole('button', { name: 'Add Available Driver', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Verify', exact: true }).click();
  await page.getByLabel('Website membership').selectOption('false');
  await page.getByLabel('Verification note').fill('Verified absent in synthetic fleet');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText('Membership verification recorded.');
  await page.getByPlaceholder('Search drivers, email or code').fill('');
  await page.getByRole('combobox', { name: 'Membership filter' }).selectOption('enrolled');
  await page.screenshot({ path: join(tmpdir(), 'mserp-weighmytruck-desktop.png'), fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: 'Collapse sidebar' }).click();
  await expect(page.locator('aside')).toHaveCSS('width', '64px');
  await expect(page.getByRole('heading', { name: 'WeighMyTruck', exact: true })).toBeVisible();
  await page.screenshot({ path: join(tmpdir(), 'mserp-weighmytruck-mobile.png'), fullPage: true });
  if (errors.length || external.length) throw new Error(JSON.stringify({ errors, external }));
  console.log('WeighMyTruck browser E2E passed: real API/database add, remove, double-click, reload, search, verification, warnings, narrow layout; no external browser calls.');
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
