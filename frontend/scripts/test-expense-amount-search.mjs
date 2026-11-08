import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname } from 'node:path';

// Fixture-backed UI contract test; exact numeric matching is tested by the API suite.
const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = path === '/' ? 'index.html' : extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] ?? 'application/octet-stream' });
    res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
let browser;
try {
  browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = [], queries = [];
  const expense = { id: 'expense', categoryId: 'other', category: 'Other', expenseType: 'Repair', description: 'Server-selected expense', expenseDate: '2026-10-20', amount: '1234.50', paidAmount: '0.00', remainingAmount: '1234.50', coveredBy: 'Company', payments: [], verifications: [], createdAt: '2026-10-20T10:00:00Z', updatedAt: '2026-10-20T10:00:00Z', paymentFee: '0.00', paymentType: 'Cash', canEdit: false };
  const options = { settings: [], categories: [{ id: 'other', name: 'Other' }], filters: { drivers: [], trucks: [], paymentTypes: [], paidBy: [], coveredBy: [] }, paymentTypes: [], expenseTypes: [], paidBy: [], coveredBy: [] };
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url()), path = url.pathname.slice(4);
    if (path === '/auth/session') return route.fulfill({ json: { user: { id: 'user', username: 'Reviewer', administrator: false, permissions: ['expenses.read'], expenseCategoryAccess: [{ categoryId: 'other', canView: true, canCreate: false, canEdit: false, canDelete: false }] }, csrfToken: 'fixture' } });
    if (path === '/expenses') {
      queries.push(Object.fromEntries(url.searchParams));
      const search = url.searchParams.get('search') || '';
      const empty = search === '9999.99';
      const filtered = !!search || !!url.searchParams.get('categoryId');
      // Deliberately return a nonmatching row for text search: the browser must
      // trust the server page rather than filter only locally available rows.
      return route.fulfill({ json: { items: empty ? [] : [expense], page: Number(url.searchParams.get('page') || 1), pageSize: 25, total: empty ? 0 : filtered ? 1 : 26, totalPages: filtered ? 1 : 2, summary: { amount: empty ? '0' : filtered ? '1234.50' : '32100.00', incompleteCount: 0 }, options } });
    }
    return route.fulfill({ json: [] });
  });
  await page.goto(`http://127.0.0.1:${server.address().port}/expenses`);
  const search = page.getByRole('textbox', { name: 'Search expenses by text or amount', exact: true });
  const row = page.getByRole('row', { name: 'Open Server-selected expense', exact: true });
  await expect(row).toBeVisible();
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect.poll(() => queries.at(-1)?.page).toBe('2');
  for (const value of ['1234', '1234.50', '1,234.50', '$1,234.50']) {
    await search.fill(value);
    await expect.poll(() => queries.at(-1)?.search).toBe(value);
    expect(queries.at(-1).page).toBe('1');
    await expect(row).toBeVisible();
    await expect(page.getByText('Filtered total', { exact: true }).locator('..')).toContainText('$1,234.50');
  }
  await page.getByRole('combobox', { name: 'Expense category', exact: true }).selectOption('other');
  await expect.poll(() => queries.at(-1)?.categoryId).toBe('other');
  expect(queries.at(-1).search).toBe('$1,234.50');
  await search.fill('server-side text match');
  await expect.poll(() => queries.at(-1)?.search).toBe('server-side text match');
  await expect(row).toBeVisible();
  await search.fill('9999.99');
  await expect(page.getByText('No entries match these filters.', { exact: true })).toBeVisible();
  await expect(search).toBeVisible();
  await expect(page.getByRole('combobox', { name: 'Expense category', exact: true })).toHaveValue('other');
  await search.fill('');
  await expect.poll(() => queries.at(-1)?.search || '').toBe('');
  expect(queries.at(-1).categoryId).toBe('other');
  await expect(row).toBeVisible();
  await page.getByRole('combobox', { name: 'Expense category', exact: true }).selectOption('');
  await expect.poll(() => queries.at(-1)?.categoryId || '').toBe('');
  await expect(page.getByRole('button', { name: 'Next page', exact: true })).toBeEnabled();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(search).toBeInViewport();
  await search.fill('$1,234.50');
  await expect.poll(() => queries.at(-1)?.search).toBe('$1,234.50');
  await expect(row).toBeVisible();
  await page.getByRole('combobox', { name: 'Expense category', exact: true }).selectOption('other');
  await expect.poll(() => queries.at(-1)?.categoryId).toBe('other');
  await page.getByRole('button', { name: 'Clear', exact: true }).click();
  await expect(search).toHaveValue('');
  await expect.poll(() => queries.at(-1)?.search || '').toBe('');
  expect(queries.at(-1).categoryId || '').toBe('');
  expect(queries.at(-1).page).toBe('1');
  await expect(page.getByRole('button', { name: 'Next page', exact: true })).toBeEnabled();
  expect(errors).toEqual([]);
  console.log('Expense amount search browser checks passed: formatted transport, page reset, server totals, category intersection, empty/clear, server-owned matching and narrow layout.');
} finally {
  await browser?.close();
  await new Promise(resolve => server.close(resolve));
}
