// Real browser/API checks against a synthetic review-local.mjs environment.
import { readFile } from 'node:fs/promises';
import { chromium, expect } from '@playwright/test';

const access = JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE, 'utf8'));
if (!['localhost', '127.0.0.1'].includes(new URL(access.base).hostname) || !/^review_[a-f0-9]+$/.test(access.schema)) throw new Error('An isolated local review environment is required');
const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  await page.goto(`${access.base}/login`);
  await page.getByLabel('Email', { exact: true }).fill(access.email);
  await page.getByLabel('Password', { exact: true }).fill(access.password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.waitForURL(url => !url.pathname.startsWith('/login'));
  await page.goto(`${access.base}/expenses`);

  const session = await (await page.request.get(`${access.base}/api/auth/session`)).json();
  const headers = { 'X-CSRF-Token': session.csrfToken, Origin: access.base };
  const settings = await (await page.request.get(`${access.base}/api/expense-settings`)).json();
  const categoryId = settings.find(item => item.kind === 'category' && item.name === 'Other').id;
  const base = { categoryId, expenseDate: '2026-11-10', expenseType: `Synthetic EFS repair ${Date.now()}`, amount: '100', paymentType: 'EFS', referenceNumber: 'INV-629101', coveredBy: 'Company' };
  const reject = async (response) => {
    expect(response.status()).toBe(400);
    expect(await response.text()).toContain('EFS reference number is required');
  };
  await reject(await page.request.post(`${access.base}/api/expenses`, { headers, data: base }));
  await reject(await page.request.post(`${access.base}/api/expenses/bulk`, { headers, data: { expenses: [{ ...base, efsReferenceNumber: 'BULK-901' }, base] } }));

  const dialog = page.getByRole('dialog');
  await page.getByRole('button', { name: 'Add entry', exact: true }).click();
  await dialog.getByLabel('Category', { exact: true }).selectOption(categoryId);
  await dialog.getByLabel('Expense type', { exact: true }).selectOption('__custom');
  await dialog.getByLabel('Expense type custom', { exact: true }).fill(base.expenseType);
  await dialog.getByLabel('Amount', { exact: true }).fill('100');
  await dialog.getByLabel('Invoice reference number', { exact: true }).fill(base.referenceNumber);
  const efs = dialog.getByLabel('EFS reference number', { exact: true });
  await expect(efs).toHaveCount(0);
  const payment = dialog.getByLabel('Payment method', { exact: true });
  await payment.selectOption('EFS');
  await dialog.getByLabel('Paid by', { exact: true }).selectOption('Local Review');
  const create = dialog.getByRole('button', { name: 'Create entry', exact: true });
  await create.click();
  await expect(efs).toBeFocused();
  expect(await efs.evaluate(input => input.validity.valueMissing)).toBe(true);
  await efs.fill(' 00043-Alpha ');
  const creating = page.waitForResponse(response => response.url().endsWith('/api/expenses') && response.request().method() === 'POST');
  await create.click();
  const response = await creating;
  expect(response.status()).toBe(201);
  const saved = await response.json();
  expect(saved).toMatchObject({ referenceNumber: base.referenceNumber, efsReferenceNumber: '00043-Alpha', amount: '103.00' });
  await expect(dialog).toHaveCount(0);

  const search = page.getByPlaceholder('Search expenses by text or amount');
  await search.fill('43-alpha');
  const match = page.getByRole('row', { name: `Open ${base.expenseType}`, exact: true });
  await expect(match.getByRole('cell', { name: 'EFS - 00043-Alpha Paid by Local Review' })).toBeVisible();
  await match.click();
  await expect(dialog.getByLabel('Invoice reference number', { exact: true })).toHaveValue(base.referenceNumber);
  await expect(efs).toHaveValue('00043-Alpha');
  await reject(await page.request.put(`${access.base}/api/expenses/${saved.id}`, { headers, data: { ...base, updatedAt: saved.updatedAt, efsReferenceNumber: '   ' } }));

  await payment.selectOption('Cash');
  await expect(efs).toHaveCount(0);
  const updating = page.waitForResponse(response => response.url().endsWith(`/api/expenses/${saved.id}`) && response.request().method() === 'PUT');
  await dialog.getByRole('button', { name: 'Save changes', exact: true }).click();
  const updated = await updating;
  expect(updated.ok()).toBe(true);
  expect(await updated.json()).toMatchObject({ referenceNumber: base.referenceNumber, efsReferenceNumber: null, paymentType: 'Cash' });
  await expect(dialog).toHaveCount(0);
  await expect(match).toHaveCount(0);
  await search.fill('INV-629101');
  await expect(match).toHaveCount(1);
  console.log('EFS reference browser checks passed: required on create and edit, independent persistence, indexed search, and clearing on method change.');
} finally {
  await browser.close();
}
