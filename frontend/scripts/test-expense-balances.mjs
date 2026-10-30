import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';

const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = path === '/' ? 'index.html' : extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] ?? 'application/octet-stream' }); res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const base = `http://127.0.0.1:${server.address().port}`;
let browser;
try {
  browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = [], queries = [], writes = [], history = [];
  let readOnly = false, conflict = false;
  const expense = { id:'expense', categoryId:'other', category:'Other', expenseType:'Repair charge', description:'Mirror replacement', expenseDate:'2026-10-20', amount:'100.00', paidAmount:'20.00', remainingAmount:'80.00', balanceVersion:2, chargeDriverId:'driver', driverId:'driver', coveredBy:'Driver', driverName:'Test Driver', payments:[{weekStart:'2026-10-19',amount:'20.00'}], verifications:[], createdAt:'2026-10-20T10:00:00Z', updatedAt:'2026-10-20T10:00:00Z', paymentFee:'0.00', paymentType:'Cash', canEdit:false };
  const options = { settings:[], categories:[{id:'other',name:'Other'}], filters:{drivers:[],trucks:[],paymentTypes:[],paidBy:[],coveredBy:['Driver']}, paymentTypes:[],expenseTypes:[],paidBy:[],coveredBy:['Driver'] };
  page.on('pageerror', e => errors.push(e.message));
  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url()), path = url.pathname.slice(4);
    if (path === '/auth/session') return route.fulfill({json:{user:{id:'user',username:'Reviewer',administrator:false,permissions:['drivers.read','expenses.read',...(readOnly?[]:['expenses.settle'])],expenseCategoryAccess:[{categoryId:'other',canView:true,canCreate:false,canEdit:false,canDelete:false}]},csrfToken:'fixture'}});
    if (path === '/expenses/expense/balance-corrections') {
      if (route.request().method() === 'POST') {
        const input = route.request().postDataJSON(); writes.push(input);
        if (conflict) return route.fulfill({status:409,json:{error:'The expense changed; reload and review'}});
        expect(input.version).toBe(expense.balanceVersion);
        history.unshift({...input,previous:expense.remainingAmount,actor:'Reviewer',createdAt:'2026-10-30T12:00:00Z'});
        expense.remainingAmount = Number(input.remaining).toFixed(2); expense.balanceVersion++;
      }
      return route.fulfill({json:history});
    }
    if (path === '/expenses/expense') return route.fulfill({json:expense});
    if (path === '/expenses') {
      queries.push(Object.fromEntries(url.searchParams));
      const empty = url.searchParams.get('search') === 'missing';
      return route.fulfill({json:{items:empty?[]:[expense],page:1,pageSize:10,total:empty?0:1,totalPages:1,summary:{amount:empty?'0':'100',incompleteCount:0},options}});
    }
    if (path === '/drivers/driver') return route.fulfill({json:{id:'driver',fullName:'Test Driver',active:true,availabilityStatus:'active',phone:'',updatedAt:'2026-10-30T12:00:00Z'}});
    if (path === '/driver-services/driver') return route.fulfill({json:{driverId:'driver',items:[]}});
    return route.fulfill({json:[]});
  });
  await page.goto(`${base}/drivers/detail?id=driver`);
  try { await page.getByRole('tab',{name:'Personal charges',exact:true}).click(); }
  catch (e) { console.error(errors, await page.locator('body').innerText()); throw e; }
  await expect(page.getByRole('button',{name:'Edit balance: Repair charge'})).toBeVisible();
  await page.getByPlaceholder('Search charges…').fill('missing');
  await expect(page.getByText('No expenses match these filters.')).toBeVisible();
  await expect(page.getByRole('button',{name:'Filter Category',exact:true})).toBeVisible();
  await page.getByRole('button',{name:'Clear filters'}).click();
  await page.getByRole('button',{name:'Filter Type',exact:true}).click();
  await page.getByLabel('Filter type',{exact:true}).fill('repair');
  await page.getByRole('group',{name:'Type filter controls'}).getByRole('button',{name:'Done'}).click();
  await page.getByRole('button',{name:'Filter Remaining',exact:true}).click();
  await page.getByLabel('Remaining minimum').fill('10');
  await page.getByRole('group',{name:'Remaining filter controls'}).getByRole('button',{name:'Done'}).click();
  await expect.poll(() => queries.at(-1)?.remainingMin).toBe('10');
  expect(queries.at(-1).expenseType).toBe('repair'); expect(queries.at(-1).chargeDriverId).toBe('driver');
  await page.getByRole('button',{name:'Edit balance: Repair charge'}).click();
  const modal=page.getByRole('dialog',{name:'Edit charge balance'});
  await expect(modal.getByLabel('Remaining balance',{exact:true})).toHaveValue('80.00');
  await modal.getByLabel('Remaining balance',{exact:true}).fill('35.25');
  await modal.getByLabel('Balance correction reason').fill('Reviewed receipt');
  conflict=true;
  await modal.getByRole('button',{name:'Save balance',exact:true}).click();
  await expect(modal.getByRole('alert')).toContainText('expense changed');
  await expect(modal.getByLabel('Balance correction reason')).toHaveValue('Reviewed receipt');
  conflict=false;
  await modal.getByRole('button',{name:'Save balance',exact:true}).click();
  await expect(modal).toHaveCount(0);
  expect(writes[0].id).toBe(writes[1].id);
  await expect(page.getByRole('cell',{name:'$35.25 Edit balance: Repair charge'})).toBeVisible();
  await page.screenshot({path:join(tmpdir(),'mserp-personal-charge-balances.png'),fullPage:true,animations:'disabled'});
  await page.goto(`${base}/expenses`);
  await page.getByRole('button',{name:'Edit balance: Repair charge'}).click();
  await expect(modal.getByText('Reviewed receipt',{exact:true})).toBeVisible();
  await modal.getByLabel('Remaining balance',{exact:true}).fill('0');
  await modal.getByLabel('Balance correction reason').fill('Balance waived');
  await modal.getByRole('button',{name:'Save balance',exact:true}).click();
  await expect(modal).toHaveCount(0);
  await expect(page.getByRole('cell').filter({hasText:'Remaining: $0.00'})).toBeVisible();
  await page.setViewportSize({width:390,height:844});
  await page.getByRole('button',{name:'Edit balance: Repair charge'}).click();
  await expect(modal.getByLabel('Remaining balance',{exact:true})).toBeVisible();
  await page.screenshot({path:join(tmpdir(),'mserp-expense-balance-mobile.png'),fullPage:true,animations:'disabled'});
  await modal.getByRole('button',{name:'Cancel',exact:true}).click();
  readOnly=true;await page.reload();
  await expect(page.getByRole('cell').filter({hasText:'Remaining: $0.00'})).toBeVisible();
  await expect(page.getByRole('button',{name:'Edit balance: Repair charge'})).toHaveCount(0);
  expect(errors).toEqual([]);
  console.log('Expense balance browser checks passed: profile/search/header filters, retries, shared expense editor, history, zero balance, mobile and read-only permissions.');
} finally { await browser?.close(); await new Promise(resolve=>server.close(resolve)); }
