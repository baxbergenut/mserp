import { readFile } from 'node:fs/promises';
import { chromium, expect } from '@playwright/test';

const access = JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE, 'utf8'));
if (!['localhost', '127.0.0.1'].includes(new URL(access.base).hostname) || !/^review_[a-f0-9]+$/.test(access.schema)) throw new Error('An isolated local review environment is required');
const browser = await chromium.launch();
try {
  const page = await browser.newPage();
  await page.goto(`${access.base}/login`);
  await page.getByLabel('Email', { exact: true }).fill(access.email);
  await page.getByLabel('Password', { exact: true }).fill(access.password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.waitForURL(url => !url.pathname.startsWith('/login'));
  const session = await (await page.request.get(`${access.base}/api/auth/session`)).json();
  const headers = { 'X-CSRF-Token': session.csrfToken, Origin: access.base };
  const api = async (path, method = 'GET', data) => {
    const response = await page.request.fetch(`${access.base}/api${path}`, { method, data, headers });
    if (!response.ok()) throw new Error(`${method} ${path} ${response.status()}: ${await response.text()}`);
    return response.json();
  };
  const settings = await api('/expense-settings');
  const category = name => settings.find(item => item.kind === 'category' && item.name === name);
  const bonus = category('Bonuses') ?? await api('/expense-settings', 'POST', { kind: 'category', name: 'Bonuses', active: true });
  const { drivers } = await api('/expenses/form-choices');
  const driver = drivers.find(item => item.fullName === 'Jordan Lee');
  const name = `Referral bonus ${Date.now()}`;
  const input = { driverId: driver.id, categoryId: category('Other').id, expenseDate: '2026-11-11', amount: '500', coveredBy: 'Company', expenseType: name, paymentType: 'Reimburse' };
  const expense = await api('/expenses','POST',input);
  const statement = async () => (await api(`/driver-pay?weekStart=2026-11-09&id=${driver.id}`)).drivers.find(item => item.id === driver.id);
  const before = await statement();
  const credit = before.edits.expenseDeductions.find(row => row.expenseId === expense.id);
  expect(credit.amount).toBe('500.00');
  await api('/driver-pay', 'PUT', { ...before.edits, expenseDeductions: [{ ...credit, apply: true }] });

  await page.goto(`${access.base}/expenses`);
  const row = page.getByRole('row', { name: `Open ${name}`, exact: true });
  await page.getByPlaceholder('Search expenses by text or amount').fill(name);
  await row.click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Category', { exact: true }).selectOption(bonus.id);
  await expect(dialog.getByText('Paid through Driver Pay as bonus earnings.')).toBeVisible();
  await expect(dialog.getByLabel('Payment method', { exact: true })).toHaveCount(0);
  await expect(dialog.getByLabel('Paid by', { exact: true })).toHaveCount(0);
  await dialog.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(row).toContainText('Driver Pay');
  const saved = await api(`/expenses/${expense.id}`);
  expect(saved.category).toBe('Bonuses');
  expect(saved.payments).toMatchObject([{ weekStart: '2026-11-09', amount: '500.00' }]);
  const after = await statement();
  expect(after.edits.expenseDeductions.filter(r => r.expenseId === expense.id)).toMatchObject([{ saved: true, driverBonus: true, amount: '500.00' }]);

  const fresh = await api('/expenses','POST',{ ...input, categoryId: bonus.id, paymentType: '', expenseType: name+' new' });
  expect((await statement()).edits.expenseDeductions.find(r => r.expenseId === fresh.id)).toMatchObject({ driverBonus: true, amount: '500.00' });
  const manual = await api('/expenses', 'POST', { ...input, paymentType: '', expenseType: name+' separate addition' });
  await api(`/expenses/${manual.id}`, 'PUT', { ...input, categoryId: bonus.id, paymentType: '', expenseType: manual.expenseType, updatedAt: manual.updatedAt });
  const retained = await api(`/expenses/${manual.id}`);
  await api(`/expenses/${manual.id}`, 'PUT', { ...input, categoryId: bonus.id, expenseType: manual.expenseType, updatedAt: retained.updatedAt });
  expect((await statement()).edits.expenseDeductions.some(r => r.expenseId === manual.id)).toBe(false);
  console.log('Passed expense editor reclassification, weekly payout preservation and new bonus creation.');
} finally {
  await browser.close();
}
