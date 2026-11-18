// Disposable review schema only: exercise schedule -> frozen -> reopened UI.
import { readFile, mkdir } from 'node:fs/promises';
import { execFileSync } from 'node:child_process';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { chromium, expect } from '@playwright/test';

const access=JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE,'utf8'));
const database=new URL(process.env.MSERP_REVIEW_TEST_DATABASE_URL);
if(!/^http:\/\/127\.0\.0\.1:\d+$/.test(access.base)||!/^review_[a-zA-Z0-9_]+$/.test(access.schema)||database.hostname!=='127.0.0.1') throw new Error('Disposable local review database required');
const sql=body=>execFileSync(process.env.PSQL_PATH||'psql',[database.toString(),'-X','-q','-v','ON_ERROR_STOP=1'],{input:`SET search_path TO ${access.schema},public;\n${body}`,encoding:'utf8',windowsHide:true});
const driver='61000000-0000-0000-0000-000000000002',truck='61000000-0000-0000-0000-000000000005',type='63000000-0000-0000-0000-000000000001';
sql(`UPDATE payroll_settlements SET finalized=false WHERE driver_id='${driver}';
INSERT INTO driver_charge_types(id,name,direction,amount,amounts,eligibility) VALUES('${type}','Weekly insurance','charge',250,ARRAY[250]::numeric[],'calendar') ON CONFLICT(id) DO NOTHING;
INSERT INTO truck_charge_phases(truck_id,type_id,week_start,amount,included)
SELECT '${truck}','${type}',week_start,250,true FROM payroll_settlements WHERE driver_id='${driver}' ON CONFLICT(truck_id,type_id,week_start) DO NOTHING;`);
const browser=await chromium.launch();
try {
 const context=await browser.newContext({viewport:{width:1600,height:1000}});
 expect((await context.request.post(access.base+'/api/auth/login',{data:{email:access.email,password:access.password}})).ok()).toBeTruthy();
 const page=await context.newPage();await page.goto(access.base+'/reports');
 await page.getByRole('button',{name:'Owner-Operator Truck P&L',exact:true}).click();
 const row=page.getByRole('table',{name:'Truck report',exact:true}).locator(`[data-report-row="${truck}"]`);
 await expect(row.getByRole('cell',{name:'$250.00',exact:true}).locator('[data-estimated="true"]')).toBeVisible();
 await expect(row.getByRole('cell',{name:'$540.00',exact:true}).locator('[data-estimated="true"]')).toBeVisible();
 const estimate=row.getByRole('cell',{name:'$250.00',exact:true}).locator('span');
 await page.evaluate(()=>document.documentElement.dataset.theme='default');
 const etaColor=()=>page.evaluate(()=>{const span=document.createElement('span');span.className='text-yellow-400';document.body.append(span);const color=getComputedStyle(span).color;span.remove();return color;});
 expect(await estimate.evaluate(el=>getComputedStyle(el).color)).toBe(await etaColor());
 await expect(page.getByText(/values are estimates and update/)).toHaveCount(0);
 await page.evaluate(()=>document.documentElement.dataset.theme='default-light');
 expect(await estimate.evaluate(el=>getComputedStyle(el).color)).toBe(await etaColor());
 const output=join(tmpdir(),'clustie-reports-review');await mkdir(output,{recursive:true});
 await page.screenshot({path:join(output,'operator-estimated-light.png'),fullPage:true});
 await page.evaluate(()=>document.documentElement.dataset.theme='default');
 await page.screenshot({path:join(output,'operator-estimated.png'),fullPage:true});
 const downloadEvent=page.waitForEvent('download');await page.getByRole('button',{name:'Export CSV',exact:true}).click();
 const download=await downloadEvent;const csvPath=join(output,'operator-estimated.csv');await download.saveAs(csvPath);
 expect(await readFile(csvPath,'utf8')).toContain('Estimated:');
 const sourceReads=[];
 page.on('response',response=>{if(new URL(response.url()).pathname.endsWith('/sources'))sourceReads.push(response.status());});
 sql(`UPDATE payroll_settlements SET finalized=true,report=jsonb_set(report,'{autoCharges}',
 '[{"name":"Weekly insurance","source":"truck_charge:${truck}:${type}","amount":"-125.00"}]') WHERE driver_id='${driver}'`);
 await row.getByRole('button').click();
 const sources=page.getByRole('table',{name:'Source records',exact:true});
 await expect(sources.getByText(/RC \(recurring charges\) · \$125\.00/)).toBeVisible();
 await expect(row.getByRole('cell',{name:'$125.00',exact:true})).toBeVisible();
 await expect(row.getByRole('cell',{name:'$415.00',exact:true})).toBeVisible();
 await expect(row.locator('[data-estimated="true"]')).toHaveCount(0);
 sql(`UPDATE payroll_settlements SET report=jsonb_set(report,'{autoCharges,0,amount}','"0.00"') WHERE driver_id='${driver}'`);
 await page.getByRole('button',{name:'Refresh report',exact:true}).click();
 await expect(row.getByRole('cell',{name:'$290.00',exact:true})).toBeVisible();
 await expect(row.locator('[data-estimated="true"]')).toHaveCount(0);
 sql(`UPDATE payroll_settlements SET finalized=false WHERE driver_id='${driver}'`);
 await page.getByRole('button',{name:'Refresh report',exact:true}).click();
 await expect(row.getByRole('cell',{name:'$250.00',exact:true}).locator('[data-estimated="true"]')).toBeVisible();
 await expect(row.getByRole('cell',{name:'$540.00',exact:true}).locator('[data-estimated="true"]')).toBeVisible();
 await page.getByRole('button',{name:'Investor Truck P&L',exact:true}).click();
 const investor=page.getByRole('row').filter({has:page.getByRole('link',{name:'INV-RPT',exact:true})});
 await expect(investor.getByRole('cell',{name:'$220.00',exact:true})).toBeVisible();
 const investorTruck='64000000-0000-0000-0000-000000000003';
 try {
   sql(`UPDATE truck_charge_phases SET amount=110 WHERE truck_id='${investorTruck}'`);
   await investor.getByRole('button').click();
   await expect(sources.getByText(/RC \(recurring charges\) · \$110\.00/)).toBeVisible();
   await expect(investor.getByRole('cell',{name:'$230.00',exact:true})).toBeVisible();
   await expect(investor.getByRole('button')).toHaveAttribute('aria-expanded','true');
   expect(sourceReads).toEqual([200,200]);
 } finally {sql(`UPDATE truck_charge_phases SET amount=100 WHERE truck_id='${investorTruck}'`);}
 console.log('Reports estimates passed: scheduled RC/profit, finalized partial/zero replacement, reopening, CSV, dark/light stale-ETA color.');
} finally {await browser.close();}
