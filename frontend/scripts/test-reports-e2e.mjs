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
  await page.getByRole('button',{name:/COMPANY-RPT/}).click();
  await expect(page.locator('caption').filter({hasText:'Source records'})).toBeVisible();
  await expect(page.getByText('MANUAL',{exact:true})).toBeVisible();
  await page.getByRole('button',{name:'Owner-Operator Truck P&L',exact:true}).click();
  await expect(page.getByRole('heading',{name:'Operator settlement'})).toBeVisible();
  await expect(page.getByText('$340.00',{exact:true})).toBeVisible();
  await page.getByRole('button',{name:'Comparison',exact:true}).click();
  await expect(page.locator('[data-report-ready="true"]')).toBeVisible();
  await page.getByRole('button',{name:'Change dates'}).click();
  await page.getByLabel('From',{exact:true}).fill('2026-11-09');
  await page.getByLabel('Through',{exact:true}).fill('2026-11-15');
  await page.getByRole('button',{name:'Apply dates'}).click();
  await page.getByRole('button',{name:'Custom period',exact:true}).click();
  await expect(page.locator('[data-report-ready="true"]')).toBeVisible();
  const downloadEvent=page.waitForEvent('download');
  await page.getByRole('button',{name:'Export CSV'}).click();
  const download=await downloadEvent;
  await download.saveAs(join(output,'operator.csv'));
  const csv=await readFile(join(output,'operator.csv'),'utf8');
  expect(csv).toContain('carrierFees (USD)'); expect(csv).toContain('REPORT-2');
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
