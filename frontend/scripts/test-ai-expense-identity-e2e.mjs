// Synthetic AI results, real UI/API/database saves in the isolated review environment.
import { readFile } from 'node:fs/promises';
import { chromium, expect } from '@playwright/test';

const access = JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE, 'utf8'));
if (!['localhost', '127.0.0.1'].includes(new URL(access.base).hostname) || !/^review_[a-f0-9]+$/.test(access.schema)) throw new Error('Isolated local review required');
const browser = await chromium.launch();
try {
  const page = await browser.newPage();
  await page.goto(access.base + '/login');
  await page.getByLabel('Email or existing username').fill(access.email);
  await page.getByLabel('Password', { exact: true }).fill(access.password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.waitForURL(url => !url.pathname.startsWith('/login'));
  const api = async path => (await page.request.get(access.base + '/api' + path)).json();
  const listing = await api('/expenses');
  const category = listing.options.settings.find(s => s.kind === 'category' && s.name === 'HR');
  const options = await api('/maintenance/options');
  const truck = options.trucks.find(t => t.driverId);
  expect(truck).toBeTruthy();
  for (const mode of ['expense', 'work-order', 'category-switch']) {
    const name = 'Unmatched Applicant ' + mode;
    const description = 'AI identity regression ' + mode;
    const draft = { categoryId:category.id, expenseType:'Transportation', expenseDate:'2026-10-21', truckId:truck.id, unitNumber:truck.unitNumber, driverId:null, driverName:name, amount:'85.00', paymentType:'', referenceNumber:'', description, coveredBy:'Company', paidBy:'', costBreakdown:[] };
    await page.route('**/api/expenses/extract', route => route.fulfill({ json:{expenses:[draft]} }));
    await page.route('**/api/maintenance/extract', route => route.fulfill({ json:{drafts:[{...draft,expenseType:'Electrical',maintenance:{hasInvoice:true,serviceDate:'2026-10-21',location:'shop',services:[{typeCode:'electrical',name:'Electrical',amount:'85.00',trackingCode:''}]}}]} }));
    let savedId;
    page.on('response', async response => {
      if (response.request().method() === 'POST' && new URL(response.url()).pathname === (mode === 'expense' ? '/api/expenses' : '/api/maintenance') && response.ok()) savedId = (await response.json()).id;
    });
    await page.goto(access.base + (mode === 'work-order' ? '/maintenance' : '/expenses'));
    await page.getByRole('button', {name:mode === 'work-order' ? 'Add work order' : 'Add entry',exact:true}).click();
    await page.getByLabel(mode === 'work-order' ? 'Maintenance details for AI' : 'Expense details for AI').fill(description);
    await page.getByRole('button', {name:'Analyze',exact:true}).click();
    if (mode === 'category-switch') await page.getByLabel('Category', {exact:true}).selectOption(options.categoryId);
    if (mode === 'expense') {
      await expect(page.getByRole('combobox', {name:'Driver',exact:true})).toHaveValue(name);
      await page.getByRole('button', {name:'Create entry',exact:true}).click();
    } else {
      await expect(page.getByLabel('Maintenance driver', {exact:true})).toHaveValue('');
      await expect(page.getByLabel('Maintenance driver', {exact:true}).locator('option:checked')).toHaveText(name + ' (unlinked)');
      await page.getByLabel('Service type', {exact:true}).selectOption('electrical');
      await page.getByRole('button', {name:'Save work order',exact:true}).click();
    }
    await expect(page.getByRole('dialog')).toHaveCount(0).catch(async error => {
      console.error(mode,await page.getByRole('dialog').innerText());
      console.error(await page.getByRole('dialog').locator(':invalid').evaluateAll(nodes=>nodes.map(n=>({label:n.getAttribute('aria-label'),message:n.validationMessage}))));
      throw error;
    });
    await expect.poll(() => savedId).toBeTruthy();
    const saved = await api((mode === 'expense' ? '/expenses/' : '/maintenance/') + savedId);
    expect(saved).toMatchObject({driverId:null,driverName:name,truckId:truck.id});
    if (mode !== 'expense') expect(saved.expense).toMatchObject({driverId:null,driverName:name});
    await page.unroute('**/api/expenses/extract');
    await page.unroute('**/api/maintenance/extract');
  }
  console.log('AI identity browser checks passed: expense, work order and category switch retain unmatched names with a linked truck.');
} finally { await browser.close(); }
