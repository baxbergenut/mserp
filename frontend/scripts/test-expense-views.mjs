import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';

const drivers = [
  { id: 'hired', fullName: 'Hired Driver', truckId: 'investor-truck', active: true, isOwnerOperator: false },
  { id: 'owner-driver', fullName: 'Owner Driver', truckId: 'own-truck', active: true, isOwnerOperator: true },
  { id: 'company-driver', fullName: 'Company Driver', truckId: 'company-truck', active: true, isOwnerOperator: true },
];
const owners = [
  { id: 'company', fullName: 'MS Express', isCompany: true },
  { id: 'investor', fullName: 'Truck Investor', isCompany: false },
  { id: 'driver-owner', fullName: 'Owner Driver', driverId: 'owner-driver', isCompany: false },
  { id: 'unrelated', fullName: 'Unrelated Investor', isCompany: false },
];
const trucks = [
  { id: 'investor-truck', unitNumber: '101', ownerId: 'investor', driverId: 'hired', isCompanyOwned: false },
  { id: 'own-truck', unitNumber: '102', ownerId: 'driver-owner', driverId: 'owner-driver', isCompanyOwned: false },
  { id: 'company-truck', unitNumber: '103', ownerId: 'company', driverId: 'company-driver', isCompanyOwned: true },
];
const settings = [{ id: 'maintenance', name: 'Maintenance', kind: 'category', active: true }];
const blankDraft = { company: 'Wrong company', categoryId: 'maintenance', expenseDate: '2026-09-28', truckId: 'investor-truck', driverId: 'hired', unitNumber: '101', driverName: 'Hired Driver', amount: '12.50', paymentType: '', expenseType: 'Repair', referenceNumber: '', description: '', coveredBy: 'Company', paidBy: 'Invented payer' };
let drafts = [blankDraft];
const writes = [];
const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = path === '/' ? 'index.html' : extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css' }[extname(file)] ?? 'application/octet-stream' }); res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
let browser;
try {
  browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1280, height: 1000 } });
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname.slice(4);
    if (route.request().method() === 'POST' && ['/expenses', '/expenses/bulk'].includes(path)) {
      writes.push(route.request().postDataJSON()); await route.fulfill({ status: 201, json: {} }); return;
    }
    const fixtures = {
      '/auth/session': { user: { id: 'user', username: 'Entry User', permissions: ['expenses.read', 'fleet.read'], expenseCategoryAccess: [{ categoryId: 'maintenance', canRead: true, canCreate: true, canEdit: true, canDelete: true }] }, csrfToken: 'fixture' },
      '/expenses': { items: [], total: 0, page: 1, pageSize: 25, totalPages: 1, summary: { amount: '0', incompleteCount: 0 }, options: { settings, categories: settings, companies: ['MS Express'], paymentTypes: [], expenseTypes: [], paidBy: ['Entry User', 'Other User'], coveredBy: [] } },
      '/drivers': drivers, '/trucks': trucks, '/investors': owners, '/expenses/extract': { expenses: drafts },
    };
    await route.fulfill({ json: fixtures[path] ?? {} });
  });
  await page.goto(`http://127.0.0.1:${server.address().port}/expenses`);
  await page.getByRole('button', { name: 'Add entry', exact: true }).click();
  await expect(page.getByLabel('Paid by', { exact: true })).toHaveValue('Entry User');
  await expect(page.getByText('Company', { exact: true }).filter({ visible: true })).toHaveCount(0);
  await expect(page.getByLabel('Responsible truck owner')).toHaveCount(0);
  await page.getByLabel('Unit number', { exact: true }).selectOption('investor-truck');
  await expect(page.getByLabel('Who will cover')).toHaveValue('Truck Owner');
  await page.getByLabel('Expense date', { exact: true }).fill('2026-09-28');
  await expect(page.getByLabel('Who will cover')).toHaveValue('Truck Owner');
  await expect(page.getByLabel('Who will cover').locator('option')).toHaveText(['Company', 'Driver', 'Truck Owner · Truck Investor']);
  await page.getByLabel('Unit number', { exact: true }).selectOption('own-truck');
  await expect(page.getByLabel('Who will cover')).toHaveValue('Driver');
  await expect(page.getByLabel('Who will cover').locator('option')).toHaveText(['Company', 'Driver']);
  await page.getByLabel('Driver', { exact: true }).selectOption('company-driver');
  await expect(page.getByLabel('Who will cover')).toHaveValue('Company');
  await page.getByLabel('Unit number', { exact: true }).selectOption('investor-truck');
  await page.getByLabel('Name', { exact: true }).fill('Repair');
  await page.getByLabel('Amount', { exact: true }).fill('12.50');
  const transfer = await page.evaluateHandle(() => { const data = new DataTransfer(); data.items.add(new File(['receipt'], 'receipt.txt', { type: 'text/plain' })); return data; });
  await page.getByLabel('Expense details for AI').dispatchEvent('drop', { dataTransfer: transfer });
  await expect(page.getByText('receipt.txt', { exact: true })).toBeVisible();
  const browse = await page.getByRole('button', { name: 'Browse', exact: true }).boundingBox();
  const analyze = await page.getByRole('button', { name: 'Analyze', exact: true }).boundingBox();
  expect(Math.abs(browse.y - analyze.y)).toBeLessThan(3);
  await page.getByRole('button', { name: 'Analyze', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Analyze', exact: true })).toBeEnabled();
  await expect(page.getByLabel('Paid by', { exact: true })).toHaveValue('Entry User');
  await expect(page.getByLabel('Who will cover')).toHaveValue('Truck Owner');
  await page.screenshot({ path: join(tmpdir(), 'mserp-expense-modal.png'), fullPage: true });
  await page.getByRole('button', { name: 'Create entry', exact: true }).click();
  await expect.poll(() => writes.length).toBe(1);
  expect(writes[0]).toMatchObject({ company: 'MS Express', coveredBy: 'Truck Owner', ownerId: 'investor', paidBy: 'Entry User' });
  await page.getByRole('button', { name: 'Add entry', exact: true }).click();
  drafts = [blankDraft, { ...blankDraft, truckId: 'own-truck', driverId: 'owner-driver' }];
  await page.getByLabel('Expense details for AI').fill('two transactions');
  await page.getByRole('button', { name: 'Analyze', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Analyze', exact: true })).toBeEnabled();
  await expect(page.getByLabel('Transaction 1 responsibility')).toHaveValue('Truck Owner');
  await expect(page.getByLabel('Transaction 2 responsibility')).toHaveValue('Driver');
  await page.getByLabel('Transaction 2 paid by').selectOption('Other User');
  await page.getByRole('button', { name: 'Create 2 entries', exact: true }).click();
  await expect.poll(() => writes.length).toBe(2);
  expect(writes[1].expenses[0]).toMatchObject({ ownerId: 'investor', paidBy: 'Entry User' });
  expect(writes[1].expenses[1]).toMatchObject({ coveredBy: 'Driver', ownerId: null, paidBy: 'Other User' });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: 'Add entry', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Browse', exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  expect(errors).toEqual([]);
  console.log('Expense modal browser checks passed: ownership, users, AI drop, single/batch payloads and mobile layout.');
} finally { await browser?.close(); await new Promise(resolve => server.close(resolve)); }
