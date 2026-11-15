// Read-only expense attachments against the built UI; no database or provider.
import { chromium, expect } from '@playwright/test';
import { startStaticFixtureServer } from './static-fixture-server.mjs';

const settings = [{ id: 'other', name: 'Other', kind: 'category', active: true }];
const options = {
  settings, categories: settings, applicants: [],
  filters: { drivers: [], trucks: [], paymentTypes: [], paidBy: [], coveredBy: [] },
  paymentTypes: ['Cash'], expenseTypes: [], paidBy: ['Fixture'], coveredBy: ['Company'],
};
const entries = ['Invoice attached', 'Receipt attached', 'No attachment'].map((description, i) => ({
  id: `demo-${i}`, description, expenseType: 'Supplies', category: 'Other', categoryId: 'other',
  expenseDate: '2026-11-09', amount: ['245.00', '89.50', '32.00'][i], coveredBy: 'Company',
  paidBy: 'Fixture', paymentType: 'Cash', canEdit: false, hasDocuments: i < 2,
  payments: [], verifications: [], accountingVerified: false, updatedAt: '2026-11-09T12:00:00Z',
  costBreakdown: [], balanceVersion: 1,
}));
const documents = [
  { id: 'invoice', fileName: 'Sample invoice.pdf', contentType: 'application/pdf' },
  { id: 'receipt', fileName: 'Sample receipt.svg', contentType: 'image/svg+xml' },
];
function samplePdf() {
  const stream = 'BT /F1 24 Tf 60 730 Td (SYNTHETIC INVOICE) Tj ET';
  const objects = [
    '<< /Type /Catalog /Pages 2 0 R >>', '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
    '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>',
    '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>', `<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`,
  ];
  let data = '%PDF-1.4\n';
  const offsets = [];
  objects.forEach((object, i) => { offsets.push(data.length); data += `${i + 1} 0 obj\n${object}\nendobj\n`; });
  const xref = data.length;
  data += 'xref\n0 6\n0000000000 65535 f \n' + offsets.map(n => `${String(n).padStart(10, '0')} 00000 n \n`).join('');
  return data + `trailer\n<< /Root 1 0 R /Size 6 >>\nstartxref\n${xref}\n%%EOF`;
}

const server = await startStaticFixtureServer();
let browser;
try {
  browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = [], unexpected = [];
  let failList = false, failFile = false;
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname.slice(4);
    if (route.request().method() !== 'GET' && !(path === '/auth/birthday' && route.request().method() === 'POST')) {
      unexpected.push(`${route.request().method()} ${path}`);
      await route.fulfill({ status: 403, json: { error: 'Read-only fixture' } }); return;
    }
    let data;
    if (path === '/auth/session') data = { user: { id: 'fixture', username: 'Fixture', permissions: ['expenses.read'], expenseCategoryAccess: [{ categoryId: 'other', canView: true, canCreate: false, canEdit: false, canDelete: false }] }, csrfToken: 'fixture' };
    else if (path === '/auth/birthday') data = { eligible: false, serverTime: new Date().toISOString() };
    else if (path === '/expenses') data = { items: entries, total: 3, page: 1, pageSize: 25, totalPages: 1, summary: { amount: '366.50', incompleteCount: 0 }, options };
    else if (path === '/expenses/form-choices') data = { options, drivers: [], trucks: [], owners: [] };
    else if (path.startsWith('/expenses/documents/')) {
      if (failFile) { await route.fulfill({ status: 503, body: 'Fixture unavailable' }); return; }
      const pdf = path.endsWith('/invoice');
      await route.fulfill({ contentType: pdf ? 'application/pdf' : 'image/svg+xml', body: pdf ? samplePdf() : '<svg xmlns="http://www.w3.org/2000/svg" width="500" height="800"><rect width="500" height="800" fill="white"/><text x="40" y="80">SYNTHETIC RECEIPT</text></svg>' }); return;
    } else if (/^\/expenses\/demo-\d\/documents$/.test(path)) {
      if (failList) { await route.fulfill({ status: 503, json: { error: 'Fixture attachments unavailable' } }); return; }
      data = path.includes('demo-0') ? documents : path.includes('demo-1') ? [documents[1]] : [];
    } else if (/^\/expenses\/demo-\d$/.test(path)) data = entries.find(entry => path.endsWith(entry.id));
    else if (/^\/expenses\/demo-\d\/external-settlements$/.test(path)) data = [];
    else if (path === '/sidebar-counts') data = { fuel: 0, weighMyTruck: 0, tolls: 0 };
    else if (path === '/tasks/count' || path === '/feedback/messages/count') data = { count: 0 };
    else if (path === '/maintenance/access') data = {};
    else { unexpected.push(path); await route.fulfill({ status: 404, json: { error: 'Missing fixture' } }); return; }
    await route.fulfill({ json: data });
  });
  await page.goto(`${server.base}/expenses`);
  await expect(page.getByRole('columnheader', { name: 'Invoice / receipt' })).toBeVisible();
  const attached = page.getByRole('button', { name: 'View invoice or receipt', exact: true });
  await expect(attached).toHaveCount(2);
  await attached.first().click();
  const preview = page.getByRole('region', { name: 'Invoice and receipt preview' });
  await expect(preview.locator('iframe[title="Sample invoice.pdf"]')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  await preview.getByLabel('Select invoice or receipt').selectOption('receipt');
  const receipt = preview.getByRole('img', { name: 'Sample receipt.svg' });
  await expect(receipt).toBeVisible();
  await expect.poll(() => receipt.evaluate(image => image.naturalWidth)).toBeGreaterThan(0);
  await expect(preview.locator('iframe')).toHaveCount(0);
  await expect(preview.getByRole('link', { name: 'Open file' })).toHaveAttribute('href', '/api/expenses/documents/receipt');
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  await page.getByRole('button', { name: 'No invoice or receipt attached', exact: true }).click();
  await expect(preview.getByText('No invoice or receipt attached', { exact: true })).toBeVisible();
  await expect(preview.locator('iframe, img')).toHaveCount(0);
  await page.getByRole('button', { name: 'Close', exact: true }).click();

  failList = true;
  await attached.first().click();
  await expect(preview.getByRole('alert')).toBeVisible();
  failList = false;
  await preview.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(preview.locator('iframe[title="Sample invoice.pdf"]')).toBeVisible();
  failFile = true;
  await preview.getByLabel('Select invoice or receipt').selectOption('receipt');
  await expect(preview.getByRole('alert')).toContainText('Unable to preview this attachment');
  await expect(preview.getByRole('link', { name: 'Open file' })).toBeVisible();
  failFile = false;
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  await attached.first().click();
  await expect(preview.locator('iframe[title="Sample invoice.pdf"]')).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  await preview.scrollIntoViewIfNeeded();
  await expect(preview).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect(errors).toEqual([]);
  expect(unexpected).toEqual([]);
  console.log('Expense documents passed: read-only PDF/image switching, empty state, list retry, file failure, reopen and mobile layout.');
} finally {
  await browser?.close();
  await server.close();
}
