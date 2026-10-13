// Read-only browser checks against the isolated review-local.mjs environment.
import { readFile } from 'node:fs/promises';
import { chromium, expect } from '@playwright/test';

if (!process.env.MSERP_REVIEW_ACCESS_FILE) throw new Error('Set MSERP_REVIEW_ACCESS_FILE to review-local.mjs access details');
const access = JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE, 'utf8'));
if (!['localhost', '127.0.0.1'].includes(new URL(access.base).hostname) || !/^review_[a-f0-9]+$/.test(access.schema)) throw new Error('An isolated local review environment is required');
const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = [];
  page.on('pageerror', error => { errors.push(error.message); console.error('Browser error:', error.message); });
  await page.goto(`${access.base}/login`);
  await page.getByLabel('Email or existing username').fill(access.email);
  await page.getByLabel('Password', { exact: true }).fill(access.password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.waitForURL(url => !url.pathname.startsWith('/login'));

  const views = [
    ['/expenses', null, ['Date', 'Expense', 'Verification']],
    ['/drivers', null, ['Status']], ['/investors', null, ['Type']],
    ['/loads', null, ['Customer', 'Driver', 'Status', 'Pickup']],
    ['/fuel', ['button', 'Transactions'], ['Purchased', 'Driver', 'Merchant / location', 'Products']],
    ['/fuel', ['button', 'Drivers'], ['Driver', 'Policy']],
    ['/tolls', ['button', 'Transactions'], ['Post date', 'Truck', 'Agency']],
    ['/tolls', ['button', 'PrePass'], ['Toll linking']],
    ['/weighmytruck', ['tab', 'Transactions'], ['Date']],
    ['/tasks', ['button', 'List'], ['Status']],
    ['/accounting/escrow', null, ['Status']],
    ['/accounting/driver-charges', null, ['Truck']],
    ['/accounting/driver-charges', ['tab', 'Installment plans'], ['Driver', 'Status']],
    ['/accounting/driver-charges', ['tab', 'Charge types'], ['Status']],
    ['/accounting/driver-pay', null, ['Dispatcher']], ['/accounting/investor-pay', null, ['Dispatcher']],
  ];
  for (const [path, tab, columns] of views) {
    await page.goto(access.base + path);
    if (tab) await page.getByRole(tab[0], { name: tab[1], exact: true }).click();
    for (const name of columns) {
      const trigger = page.getByRole('columnheader').getByRole('button', { name: `Filter ${name}`, exact: true });
      await expect(trigger).toBeVisible();
      await trigger.click();
      const panel = page.getByRole('group', { name: `${name} filters`, exact: true });
      await expect(panel).toBeVisible();
      await expect(trigger).toHaveAttribute('aria-expanded', 'true');
      const rect = await panel.boundingBox();
      expect(rect.x >= 0 && rect.x + rect.width <= 1440).toBe(true);
      const column = await trigger.locator('xpath=ancestor::th').boundingBox();
      expect(Math.abs(rect.width - column.width)).toBeLessThan(2);
      expect(Math.abs(rect.y - column.y - column.height)).toBeLessThan(2);
      for (const list of await panel.getByRole('listbox').all()) {
        const box = await list.boundingBox();
        expect(box.height).toBeGreaterThanOrEqual(56);
      }
      await page.keyboard.press('Escape');
      await expect(panel).not.toBeVisible();
    }
  }
  await page.goto(access.base + '/expenses');
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: 'Filter Verification', exact: true }).click();
  const panel = page.getByRole('group', { name: 'Verification filters', exact: true });
  const rect = await panel.boundingBox();
  expect(rect.x >= 0 && rect.x + rect.width <= 390).toBe(true);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.getByLabel('Accountant verification').selectOption('false');
  await page.getByLabel('Admin verification').selectOption('true');
  await page.keyboard.press('Escape');
  await expect(page.getByRole('button', { name: 'Filter Verification', exact: true })).toHaveAttribute('title', 'Verification filters active');
  await page.reload();
  await page.getByRole('button', { name: 'Filter Verification', exact: true }).click();
  await expect(page.getByLabel('Accountant verification')).toHaveValue('false');
  await expect(page.getByLabel('Admin verification')).toHaveValue('true');
  await page.goto(access.base + '/driver-board');
  await expect(page.getByLabel('Dispatcher filter', { exact: true })).toBeVisible();
  await expect(page.getByLabel('Status filter', { exact: true })).toBeVisible();
  await expect(page.getByRole('columnheader').getByRole('button', { name: /^Filter / })).toHaveCount(0);
  await page.goto(access.base + '/gross-board');
  await expect(page.getByLabel('Dispatcher', { exact: true })).toBeVisible();
  await expect(page.getByRole('columnheader').getByRole('button', { name: /^Filter / })).toHaveCount(0);
  expect(errors).toEqual([]);
  console.log('Column filters passed on 16 table views, including empty results, keyboard dismissal, saved filters and mobile placement.');
} finally { await browser.close(); }
