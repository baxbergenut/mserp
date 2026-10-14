// Real API/database browser checks against review-local.mjs synthetic data.
import { readFile } from 'node:fs/promises';
import { chromium, expect } from '@playwright/test';

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
  await page.goto(`${access.base}/expenses`);
  await page.getByRole('button', { name: 'Add entry', exact: true }).click();
  await page.getByLabel('Name', { exact: true }).selectOption('Office supplies');
  await page.getByLabel('Amount', { exact: true }).fill('120.30');
  await page.getByRole('button', { name: 'Add cost', exact: true }).click();
  await page.getByLabel('Cost breakdown 1 name', { exact: true }).selectOption('Office supplies');
  await page.getByLabel('Cost breakdown 1 amount', { exact: true }).fill('100.10');
  await page.getByRole('button', { name: 'Add cost', exact: true }).click();
  await page.getByLabel('Cost breakdown 2 name', { exact: true }).selectOption('__custom');
  await page.getByLabel('Cost breakdown 2 name custom', { exact: true }).fill('Synthetic shop service');
  await page.getByLabel('Cost breakdown 2 amount', { exact: true }).fill('20.19');
  await page.getByRole('button', { name: 'Create entry', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(1);
  await page.getByLabel('Cost breakdown 2 amount', { exact: true }).fill('20.20');
  await page.getByLabel('Payment method', { exact: true }).fill('EFS');
  await page.getByRole('button', { name: 'Create entry', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  const settings = await (await page.request.get(`${access.base}/api/expense-settings`)).json();
  expect(settings.some(item => item.name === 'Synthetic shop service')).toBe(false);
  const listing = await (await page.request.get(`${access.base}/api/expenses?pageSize=100`)).json();
  const saved = listing.items.find(item => item.expenseType === 'Office supplies' && item.baseAmount === '120.30');
  expect(saved.amount).toBe('123.30');
  expect(saved.costBreakdown).toEqual([{ name: 'Office supplies', amount: '100.10' }, { name: 'Synthetic shop service', amount: '20.20' }]);
  await page.reload();
  await page.getByRole('row', { name: 'Open Office supplies', exact: true }).click();
  await expect(page.getByLabel('Cost breakdown 2 name custom', { exact: true })).toHaveValue('Synthetic shop service');
  await page.getByLabel('Cost breakdown 2 name', { exact: true }).selectOption('__custom');
  await page.getByLabel('Cost breakdown 2 name custom', {exact:true}).fill('Stationery');
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  const reread = await (await page.request.get(`${access.base}/api/expenses/${saved.id}`)).json();
  expect(reread.costBreakdown[1].name).toBe('Stationery');
  const session = await (await page.request.get(`${access.base}/api/auth/session`)).json();
  const bad = await page.request.post(`${access.base}/api/expenses`, { headers: { 'X-CSRF-Token': session.csrfToken, Origin: access.base }, data: { categoryId: saved.categoryId, expenseType: 'Office supplies', expenseDate: '2026-10-14', amount: '12.50', baseAmount: '12.50', costBreakdown: [{ name: 'Office supplies', amount: '12.49' }] } });
  expect(bad.status()).toBe(400);
  if (process.env.MSERP_OPENAI_LIVE_TEST === '1') {
    await page.getByRole('button', { name: 'Add entry', exact: true }).click();
    await page.getByLabel('Expense details for AI').fill('Office supplies invoice 2026-10-14: Office supplies $100.10; Pens $20.20; Total $120.30. Paid cash.');
    await page.getByRole('button', { name: 'Analyze', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Analyze', exact: true })).toBeEnabled({ timeout: 100000 });
    await expect(page.getByLabel('Amount', { exact: true })).toHaveValue('120.30');
    await expect(page.getByLabel('Cost breakdown 1 name', { exact: true })).toHaveValue('Office supplies');
    await expect(page.getByText('Matches entered amount',{exact:true})).toBeVisible();
    await page.getByRole('button', { name: 'Create entry', exact: true }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: 'Add entry', exact: true }).click();
  await page.getByRole('button', { name: 'Add cost', exact: true }).click();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect(errors).toEqual([]);
  console.log('Real expense entry checks passed: dropdowns, custom costs, exact totals, EFS, persistence, edits and extraction.');
} finally { await browser.close(); }
