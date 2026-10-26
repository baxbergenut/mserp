// Read-only toolbar checks against isolated review-local.mjs data.
import { readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { chromium, expect } from '@playwright/test';
if (!process.env.MSERP_REVIEW_ACCESS_FILE) throw new Error('Set MSERP_REVIEW_ACCESS_FILE');
const access = JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE, 'utf8'));
if (!['localhost', '127.0.0.1'].includes(new URL(access.base).hostname) || !/^review_[a-f0-9]+$/.test(access.schema)) throw new Error('An isolated local review environment is required');
const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(`${access.base}/login`);
  await page.getByLabel('Email or existing username').fill(access.email);
  await page.getByLabel('Password', { exact: true }).fill(access.password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.waitForURL(url => !url.pathname.startsWith('/login'));
  const views = [
    ['/expenses', null, ['Expense date from', 'Expense date to', 'Expense truck', 'Expense driver', 'Expense category', 'Expense payment type', 'Expense paid by', 'Expense responsibility', 'Accountant verification', 'Admin verification']],
    ['/drivers', null, ['Show terminated drivers']],
    ['/investors', null, ['Show company']],
    ['/fleet/trailers', null, ['Storage location', 'Trailer availability', 'Trailer condition', 'Record status']],
    ['/loads', null, ['Customer filter', 'Driver filter', 'Dispatcher filter', 'Status filter', 'Pickup from', 'Pickup to']],
    ['/fuel', ['button', 'Transactions'], ['Purchase date from', 'Purchase date to', 'Filter by driver', 'Filter by state', 'Filter by purchase category']],
    ['/fuel', ['button', 'Drivers'], ['Driver status', 'Policy filter']],
    ['/tolls', ['button', 'Transactions'], ['Post date from', 'Post date to', 'Toll truck', 'Toll agency']],
    ['/tolls', ['button', 'Trucks'], ['PrePass status filter']],
    ['/tolls', ['button', 'Transponders'], ['PrePass status filter']],
    ['/tolls', ['button', 'Needs attention'], ['Unlinked tolls posted since']],
    ['/weighmytruck', ['tab', 'Transactions'], ['From', 'To']],
    ['/tasks', ['button', 'List'], ['Task status']],
    ['/accounting/escrow', null, ['Escrow payment status']],
    ['/accounting/driver-charges', ['tab', 'Truck charges'], ['Filter truck charge type', 'Truck inclusion', 'Show archived charge types']],
    ['/accounting/driver-charges', ['tab', 'Installment plans'], ['Filter driver', 'Filter status']],
    ['/accounting/driver-charges', ['tab', 'Charge types'], ['Filter status']],
    ['/accounting/driver-pay', null, ['Dispatcher']],
    ['/accounting/investor-pay', null, ['Dispatcher']],
  ];
  for (const [path, tab, labels] of views) {
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto(access.base + path);
    if (tab) await page.getByRole(tab[0], { name: tab[1], exact: true }).click();
    const filters = page.getByRole('group', { name: 'Table filters', exact: true });
    for (const label of labels) await expect(filters.getByLabel(label, { exact: true })).toBeVisible();
    await expect(page.locator('thead select, thead input[type=date], thead button[aria-label^="Filter "]')).toHaveCount(0);
    for (const width of [1440, 390]) {
      await page.setViewportSize({ width, height: 1000 });
      await page.evaluate(() => Promise.all(document.getAnimations().filter(a => a.effect?.getComputedTiming().iterations !== Infinity).map(a => a.finished.catch(() => {}))));
      for (const label of labels) {
        const box = await filters.getByLabel(label, { exact: true }).boundingBox();
        expect(box.x, `${path}: ${label} left at ${width}`).toBeGreaterThanOrEqual(0);
        expect(box.x + box.width, `${path}: ${label} right at ${width}`).toBeLessThanOrEqual(width + 1);
      }
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${path} wraps at ${width}`).toBe(true);
    }
    console.log(`Visible toolbar filters: ${path} ${tab?.[1] ?? ''}`);
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(access.base + '/expenses');
  await page.getByLabel('Accountant verification').selectOption('false');
  await page.getByLabel('Admin verification').selectOption('true');
  await page.reload();
  await expect(page.getByLabel('Accountant verification')).toHaveValue('false');
  await expect(page.getByLabel('Admin verification')).toHaveValue('true');
  await page.getByPlaceholder('Search expenses…').fill('no matching expense toolbar check');
  await expect(page.getByText('No entries match these filters.', { exact: true })).toBeVisible();
  await expect(page.getByLabel('Expense category')).toBeVisible();
  await page.getByRole('button', { name: 'Clear', exact: true }).click();
  await expect(page.getByLabel('Accountant verification')).toHaveValue('');
  await expect(page.getByPlaceholder('Search expenses…')).toHaveValue('');
  await page.screenshot({ path: join(tmpdir(), 'mserp-toolbar-filters-desktop.png'), fullPage: true });
  await page.setViewportSize({ width: 390, height: 1000 });
  await page.screenshot({ path: join(tmpdir(), 'mserp-toolbar-filters-mobile.png'), fullPage: true });
  await page.goto(access.base + '/loads');
  await page.getByRole('columnheader').getByRole('button', { name: 'Pickup', exact: true }).click();
  await expect(page.getByLabel('Pickup from')).toBeVisible();
  await page.goto(access.base + '/tasks');
  await page.getByRole('button', { name: 'Kanban', exact: true }).click();
  await expect(page.getByLabel('Task status', { exact: true })).toHaveCount(0);
  expect(errors).toEqual([]);
  console.log('Toolbar filters passed on 19 views: desktop/mobile, saved filters, empty results, clearing, sorting and tab scope.');
} finally { await browser.close(); }
