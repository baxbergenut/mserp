// Real local API/database and production static build; no mocked report data.
import { readFile, mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { chromium, expect } from '@playwright/test';

const access = JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE, 'utf8'));
if (!/^http:\/\/127\.0\.0\.1:\d+$/.test(access.base) || !access.schema.startsWith('review_')) throw new Error('Isolated review environment required');
const output = join(tmpdir(), 'clustie-reports-review');
await mkdir(output, { recursive: true });
const browser = await chromium.launch();
try {
  const context = await browser.newContext({ viewport: { width: 1600, height: 1000 } });
  const login = await context.request.post(access.base+'/api/auth/login', { data: { email: access.email, password: access.password } });
  expect(login.ok()).toBeTruthy();
  const page = await context.newPage();
  await page.addInitScript(() => {
    const observer=new MutationObserver(() => {
      if (document.querySelector('[data-report-ready="true"]')) {
        observer.disconnect();
        requestAnimationFrame(()=>requestAnimationFrame(()=>{window.__reportFirstPaint=performance.now();}));
      }
    });
    observer.observe(document,{subtree:true,attributes:true,childList:true});
  });
  const errors = []; page.on('pageerror', error => errors.push(error.message));
  const requests = []; page.on('request', r => { if (r.url().includes('/api/reports/')) requests.push(r.url()); });
  let firstNavigationMs;
  if (process.env.REPORTS_COLD_DOCUMENT !== '1') {
    await page.goto(access.base+'/tasks');
    await expect(page.getByRole('link',{name:'Reports',exact:true})).toBeVisible();
    firstNavigationMs = await page.evaluate(() => new Promise((resolve,reject) => {
      const start=performance.now();
      const observer=new MutationObserver(() => {
        if (document.querySelector('[data-report-ready="true"]')) {
          observer.disconnect(); clearTimeout(timer);
          requestAnimationFrame(()=>requestAnimationFrame(()=>resolve(performance.now()-start)));
        }
      });
      observer.observe(document.body,{subtree:true,attributes:true,childList:true});
      const timer=setTimeout(()=>{observer.disconnect();reject(new Error('Navigation did not complete'));},30000);
      document.querySelector('nav[aria-label="Main navigation"] a[href="/reports"]').click();
    }));
  } else { await page.goto(access.base+'/reports'); }
  await expect(page.locator('[data-report-ready="true"]')).toBeVisible();
  if (process.env.REPORTS_COLD_DOCUMENT === '1') await page.waitForFunction(()=>window.__reportFirstPaint !== undefined);
  const coldDocument = process.env.REPORTS_COLD_DOCUMENT === '1' ? await page.evaluate(() => window.__reportFirstPaint) : undefined;
  const firstSummary = await context.request.get(access.base+'/api/reports/company');
  const summary = await firstSummary.json();
  expect(summary.current.length).toBeGreaterThan(100);
  expect(summary.current.every(row => !row.sources)).toBeTruthy();
  expect(summary.comparison).toHaveLength(0);
  expect(requests.some(url => url.includes('/sources'))).toBeFalsy();
  const companyTable = page.getByRole('table', {name:'Truck report',exact:true});
  const headers = await companyTable.getByRole('columnheader').allTextContents();
  expect(headers.at(-1)).toBe('Profit');
  expect(headers[headers.indexOf('Load gross')+1]).toBe('Miles');
  expect(headers[headers.indexOf('RPM')+1]).toBe('CPM');
  expect(headers.slice(0,2)).toEqual(['Truck','Assigned driver']);
  const companyRow = companyTable.locator('[data-report-row]').filter({hasText:'COMPANY-RPT'});
  await expect(companyRow.getByRole('link',{name:'COMPANY-RPT',exact:true})).toHaveAttribute('href','/trucks/detail?id=61000000-0000-0000-0000-000000000004');
  await expect(companyRow.getByRole('link',{name:'Company Driver',exact:true})).toHaveAttribute('href','/drivers/detail?id=61000000-0000-0000-0000-000000000001');
  // A value cell expands the row, not just its chevron. Links navigate without expanding.
  await companyRow.getByRole('cell').filter({hasText:'$1,199.00'}).click();
  await expect(page.locator('caption').filter({hasText:'Source records'})).toBeVisible();
  const sourceTable=page.getByRole('table').filter({has:page.locator('caption')});
  await sourceTable.getByRole('button',{name:'Date',exact:true}).click();
  await expect(sourceTable.getByRole('columnheader',{name:'Date',exact:true})).toHaveAttribute('aria-sort','ascending');
  await companyRow.getByRole('cell').filter({hasText:'$1,199.00'}).click();
  const driverNavigation=page.waitForURL('**/drivers/detail?id=61000000-0000-0000-0000-000000000001');
  await companyRow.getByRole('link',{name:'Company Driver',exact:true}).click(); await driverNavigation;
  await page.goBack(); await expect(page.locator('[data-report-ready="true"]')).toBeVisible();
  await companyRow.getByRole('link',{name:'COMPANY-RPT',exact:true}).click();
  await page.waitForURL('**/trucks/detail?id=61000000-0000-0000-0000-000000000004');
  await page.goBack(); await expect(page.locator('[data-report-ready="true"]')).toBeVisible();
  const beforeSort=requests.length;
  await companyTable.getByRole('button',{name:'Load gross',exact:true}).click();
  await expect(companyTable.getByRole('columnheader',{name:'Load gross',exact:true})).toHaveAttribute('aria-sort','ascending');
  const expectedAscending=[...summary.current].sort((a,b)=>(a.metrics.gross??Infinity)-(b.metrics.gross??Infinity)).slice(0,50).map(row=>row.id);
  expect(await companyTable.locator('[data-report-row]').evaluateAll(rows=>rows.map(row=>row.dataset.reportRow))).toEqual(expectedAscending);
  await page.waitForTimeout(350);
  const animationCount = await companyTable.evaluate(table => new Promise(resolve => {
    [...table.querySelectorAll('thead button')].find(button=>button.textContent==='Load gross').click();
    requestAnimationFrame(()=>resolve([...table.querySelectorAll('[data-report-row]')].filter(row=>row.getAnimations().some(animation=>animation.effect.getTiming().duration===300)).length));
  }));
  expect(animationCount).toBeGreaterThan(0);
  const expectedDescending=[...summary.current].sort((a,b)=>(b.metrics.gross??-Infinity)-(a.metrics.gross??-Infinity)).slice(0,50).map(row=>row.id);
  expect(await companyTable.locator('[data-report-row]').evaluateAll(rows=>rows.map(row=>row.dataset.reportRow))).toEqual(expectedDescending);
  await page.emulateMedia({reducedMotion:'reduce'});
  await companyTable.getByRole('button',{name:'Load gross',exact:true}).click();
  expect(await companyTable.locator('[data-report-row]').evaluateAll(rows=>rows.flatMap(row=>row.getAnimations()).length)).toBe(0);
  await page.emulateMedia({reducedMotion:'no-preference'});
  await companyTable.getByRole('button',{name:'Earned driver pay',exact:true}).click();
  await companyTable.getByRole('button',{name:'Earned driver pay',exact:true}).click();
  expect(await companyTable.locator('[data-report-row]').evaluateAll(rows=>rows.map(row=>row.dataset.reportRow))).not.toContain('61000000-0000-0000-0000-000000000004');
  expect(requests.length).toBe(beforeSort);
  // Sort the full cohort, not just the visible page; sorting returns to page one.
  await page.getByRole('button',{name:'Next',exact:true}).click();
  await companyTable.getByRole('button',{name:'Truck',exact:true}).click();
  await expect(companyRow).toBeVisible();
  await expect(page.getByRole('button',{name:'Previous',exact:true})).toBeDisabled();
  for (const header of await companyTable.getByRole('columnheader').all()) {
    await header.getByRole('button').click();
    expect(await header.getAttribute('aria-sort')).not.toBe('none');
  }
  await companyTable.getByRole('button',{name:'Load gross',exact:true}).click();
  await page.screenshot({ path: join(output,'company.png'), fullPage: true });
  const samples = [];
  async function switchView(label) {
    return page.evaluate(label => new Promise((resolve,reject) => {
      const start=performance.now(); let loading=false;
      const observer=new MutationObserver(() => {
        const report=document.querySelector('[data-report-ready]');
        if (report?.getAttribute('data-report-ready')==='false') loading=true;
        if (loading && report?.getAttribute('data-report-ready')==='true') {
          observer.disconnect(); clearTimeout(timer);
          requestAnimationFrame(()=>requestAnimationFrame(()=>resolve(performance.now()-start)));
        }
      });
      observer.observe(document.body,{subtree:true,attributes:true,childList:true});
      const timer=setTimeout(()=>{observer.disconnect();reject(new Error('Report did not render'));},30000);
      [...document.querySelectorAll('nav[aria-label="Report views"] button')].find(b=>b.textContent===label).click();
    }),label);
  }
  const labels=['Owner-Operator Truck P&L','Dispatcher Performance','Driver Performance','Company Truck P&L'];
  const coldViews={};
  for (const label of labels) {
    coldViews[label]=await switchView(label);
    await page.screenshot({path:join(output,label.startsWith('Owner')?'operator.png':label.startsWith('Dispatcher')?'dispatcher.png':label.startsWith('Driver')?'driver.png':'company.png'),fullPage:true});
  }
  for(let n=0;n<3;n++) for(const label of labels) samples.push({view:label,ms:await switchView(label)});
  console.log(JSON.stringify({firstNavigationMs,coldDocumentMs:coldDocument,coldViews,samples}));
  await page.getByRole('button',{name:'Source records for COMPANY-RPT',exact:true}).click();
  await expect(page.locator('caption').filter({hasText:'Source records'})).toBeVisible();
  await expect(page.getByText('MANUAL',{exact:true})).toBeVisible();
  const beforeFocus=requests.length;
  await page.evaluate(()=>{window.dispatchEvent(new Event('blur'));window.dispatchEvent(new Event('focus'));});
  await page.waitForTimeout(250);
  expect(requests.length).toBe(beforeFocus);
  await expect(page.getByRole('button',{name:/COMPANY-RPT/})).toHaveAttribute('aria-expanded','true');
  await expect(page.getByRole('columnheader',{name:'Profit',exact:true})).toBeVisible();
  await expect(page.getByRole('columnheader',{name:'RPM',exact:true})).toBeVisible();
  await expect(page.getByText(/^Review \d+/)).toHaveCount(0);
  await page.getByRole('button',{name:'Owner-Operator Truck P&L',exact:true}).click();
  await expect(page.getByRole('heading',{name:'Operator settlement'})).toBeVisible();
  await expect(page.getByText('$340.00',{exact:true})).toBeVisible();
  await expect(page.getByRole('button',{name:'Comparison',exact:true})).toHaveCount(0);
  await expect(page.getByRole('button',{name:'Change dates',exact:true})).toHaveCount(0);
  const weeks=await page.getByLabel('Week',{exact:true}).locator('option').evaluateAll(options=>options.map(option=>option.value));
  expect(weeks.at(-1)).toBe('2026-10-19'); expect(weeks[0]).toBe('2026-11-09');
  await expect(page.getByRole('button',{name:'Next week',exact:true})).toBeDisabled();
  await page.getByLabel('Week',{exact:true}).selectOption('2026-10-19');
  await expect(page.locator('[data-report-ready="true"]')).toBeVisible();
  await expect(page.getByRole('button',{name:'Previous week',exact:true})).toBeDisabled();
  await page.getByRole('button',{name:'Last completed week',exact:true}).click();
  await expect(page.locator('[data-report-ready="true"]')).toBeVisible();
  await expect(page.getByRole('columnheader',{name:'RC (recurring charges)',exact:true})).toBeVisible();
  for(const label of ['Equipment','Unassigned costs','Carrier-borne costs','Contribution','Revenue per mile']) await expect(page.getByRole('columnheader',{name:label,exact:true})).toHaveCount(0);
  const downloadEvent=page.waitForEvent('download');
  await page.getByRole('button',{name:'Export CSV'}).click();
  const download=await downloadEvent;
  await download.saveAs(join(output,'operator.csv'));
  const csv=await readFile(join(output,'operator.csv'),'utf8');
  expect(csv).toContain('carrierFees (USD)'); expect(csv).toContain('REPORT-2'); expect(csv).toContain('RC (recurring charges) (USD)'); expect(csv).toContain('Profit (USD)'); expect(csv).not.toContain('Comparison'); expect(csv).not.toContain('carrierCosts (USD)');
  await page.getByLabel('Truck',{exact:true}).selectOption('61000000-0000-0000-0000-000000000005');
  await expect(page.locator('[data-report-ready="true"]')).toBeVisible();
  await page.setViewportSize({width:390,height:844});
  await page.screenshot({path:join(output,'mobile.png'),fullPage:true});
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBeTruthy();
  expect(errors).toEqual([]);
  const sorted=samples.map(s=>s.ms).sort((a,b)=>a-b);
  const result={firstNavigationMs,coldDocumentMs:coldDocument,coldViews,warmP50Ms:sorted[Math.floor(sorted.length*.5)],warmP95Ms:sorted[Math.ceil(sorted.length*.95)-1],samples,summaryBytes:Buffer.byteLength(JSON.stringify(summary)),rows:summary.current.length,output};
  await writeFile(join(output,'performance.json'),JSON.stringify(result,null,2));
  console.log(JSON.stringify(result,null,2));
} finally { await browser.close(); }
