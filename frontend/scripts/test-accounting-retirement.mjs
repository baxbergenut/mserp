// Real browser/API checks against a disposable review-local schema only.
import assert from 'node:assert/strict';
import { readFile, mkdir, writeFile } from 'node:fs/promises';
import { execFileSync } from 'node:child_process';
import { randomBytes, randomUUID, createHash } from 'node:crypto';
import { dirname, join } from 'node:path';
import { chromium, expect } from '@playwright/test';

const access = JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE, 'utf8'));
const database = new URL(process.env.MSERP_REVIEW_TEST_DATABASE_URL);
if (!['127.0.0.1','localhost'].includes(new URL(access.base).hostname) || !['127.0.0.1','localhost'].includes(database.hostname) || !database.pathname.endsWith('_test') || !/^review_[a-f0-9]+$/.test(access.schema)) throw Error('Disposable local review required');
const sql = input => execFileSync(process.env.PSQL_PATH || 'psql', ['-X','-q','-At','-v','ON_ERROR_STOP=1','-d',database.toString()], {input:`SET search_path TO ${access.schema},public;\n${input}`,encoding:'utf8',windowsHide:true}).trim();
const artifacts = join(dirname(process.env.MSERP_REVIEW_ACCESS_FILE),'accounting-retirement');
await mkdir(artifacts,{recursive:true});
const driver = sql("SELECT id FROM drivers WHERE full_name='Alex Morgan'");
const current = randomUUID(), overlap = randomUUID();
sql(`INSERT INTO expenses(id,category,category_id,expense_type,description,expense_date,amount,driver_id,charge_driver_id,covered_by) VALUES
('${current}','Other',(SELECT id FROM expense_settings WHERE name='Other' AND kind='category'),'Cutover external','Current expense','2026-10-26',200,'${driver}','${driver}','Driver'),
('${overlap}','Other',(SELECT id FROM expense_settings WHERE name='Other' AND kind='category'),'Cutover overlap','Already in final Sheets week','2026-10-28',80,'${driver}','${driver}','Driver');
INSERT INTO gross_board_entries(driver_id,service_date,load_number,entered_original_rate,driver_rate,entered_miles) SELECT id,'2026-10-26','NEW-'||full_name,2500,2400,2000 FROM drivers WHERE active ON CONFLICT DO NOTHING;
INSERT INTO gross_board_entries(driver_id,service_date,load_number,entered_original_rate,driver_rate,entered_miles) SELECT id,'2026-10-19','OLD-'||full_name,1500,1400,1000 FROM drivers WHERE active ON CONFLICT DO NOTHING;
INSERT INTO driver_pay_weeks(driver_id,week_start,notes) VALUES('${driver}','2026-10-26','Keep current note') ON CONFLICT DO NOTHING;`);

const browser = await chromium.launch(); let page;
const errors=[];
try {
 const context=await browser.newContext({viewport:{width:1440,height:1000}});page=await context.newPage();page.setDefaultTimeout(20000);page.on('pageerror',e=>errors.push(e.message));
 await page.goto(access.base+'/login');await page.getByLabel('Email or existing username').fill(access.email);await page.getByLabel('Password',{exact:true}).fill(access.password);await page.getByRole('button',{name:'Sign in',exact:true}).click();await page.waitForURL(u=>u.pathname!='/login');
 const session=await (await context.request.get(access.base+'/api/auth/session')).json();
 const request=async(method,path,data)=>context.request.fetch(access.base+'/api'+path,{method,data,headers:{'X-CSRF-Token':session.csrfToken,'Origin':access.base}});
 const get=async path=>{const r=await request('GET',path);assert.equal(r.status(),200,await r.text());return r.json()};
 await page.goto(access.base+'/settings');
 await expect(page.getByRole('link',{name:'Accounting cutover',exact:true})).toHaveCount(0);
 const removed=await page.goto(access.base+'/settings/accounting-cutover');assert.equal(removed.status(),404);
 // The closed route-permission registry rejects removed API paths with 403.
 for(const [method,path,data] of [['GET','/accounting/cutover'],['PUT','/accounting/cutover',{}],['POST','/accounting/cutover/preview',{}],['POST','/accounting/cutover/activate',{previewId:randomUUID(),reviewed:true}],['POST','/accounting/cutover/openings',{}]]) {const response=await request(method,path,data);assert.equal(response.status(),403,path)}
 const history=await get('/driver-pay?weekStart=2026-10-19');assert.ok(!history.managedInSheets);
 const pay=await get('/driver-pay?weekStart=2026-10-26');assert.ok(pay.drivers.find(d=>d.id===driver).edits.expenseDeductions.some(e=>e.expenseId===overlap));
 // Actual partial external settlement, optimistic retry, and reversal through UI.
 await page.goto(access.base+'/expenses?expenseId='+current);
 await page.getByRole('button',{name:'Record outside Clustie',exact:true}).click();
 await page.getByLabel('External settlement amount').fill('50');await page.getByLabel('External settlement date').fill('2026-10-26');await page.getByLabel('External settlement reference').fill('External receipt TEST-50');
 await page.getByRole('button',{name:'Record settlement',exact:true}).click();await expect(page.getByText(/Recorded externally 50.00/)).toBeVisible();
 assert.equal((await get('/expenses/'+current)).remainingAmount,'150.00');
 await page.getByRole('button',{name:'Reverse',exact:true}).click();await page.getByLabel('External settlement reference').fill('Correction TEST-50');await page.getByRole('button',{name:'Record reversal',exact:true}).click();
 await expect(page.getByText(/Reversed 50.00/)).toBeVisible();assert.equal((await get('/expenses/'+current)).remainingAmount,'200.00');
 await page.screenshot({path:join(artifacts,'external-settlement.png'),fullPage:true});
 // Referenced adjustment uses ordinary autosave and survives CSV export.
 await page.goto(access.base+'/accounting/driver-pay?weekStart=2026-10-26&driverId='+driver);
 await page.getByRole('button',{name:'Sheets adjustment',exact:true}).click();
 const modal=page.getByRole('dialog',{name:'Sheets balance adjustment'});
 await modal.getByLabel('Amount',{exact:true}).fill('150');await modal.getByLabel('Sheets reference',{exact:true}).fill('Legacy sheet / Alex / row 17');await modal.getByRole('button',{name:'Add to statement'}).click();
 await expect.poll(async()=>sql(`SELECT count(*) FROM driver_pay_weeks w,jsonb_array_elements(w.adjustments) a WHERE w.driver_id='${driver}' AND a->>'legacySheets'='true' AND a->>'note'='Legacy sheet / Alex / row 17'`)).toBe('1');
 const download=page.waitForEvent('download');await page.getByRole('button',{name:'Export statement'}).click();const file=await download;const body=await readFile(await file.path(),'utf8');assert.ok(body.includes('Sheets balance collection'));assert.ok(body.includes('Legacy sheet / Alex / row 17'));assert.ok(body.includes('-150.00'));
 await page.screenshot({path:join(artifacts,'statement.png'),fullPage:true});
 // Ordinary signed-in accounts cannot read/activate cutover or settle expenses.
 const role=randomUUID(),user=randomUUID(),token=randomBytes(32).toString('base64url'),csrf=randomBytes(32).toString('base64url');
 sql(`INSERT INTO app_roles(id,name,permissions) VALUES('${role}','Cutover unprivileged','{}');INSERT INTO app_users(id,username,password_hash,role_id) VALUES('${user}','Cutover ordinary','$2test','${role}');INSERT INTO auth_sessions(user_id,token_hash,csrf_token,expires_at) VALUES('${user}','${createHash('sha256').update(token).digest('hex')}','${csrf}',now()+interval '1 hour')`);
 const ordinary=await browser.newContext();await ordinary.addCookies([{name:'mserp_session',value:token,url:access.base,httpOnly:true,sameSite:'Strict'}]);
 for(const [method,path,data] of [['POST','/expenses/'+current+'/external-settlements',{}]]){const r=await ordinary.request.fetch(access.base+'/api'+path,{method,data,headers:{'X-CSRF-Token':csrf,'Origin':access.base}});assert.equal(r.status(),403,path)}
 await ordinary.close();assert.deepEqual(errors,[]);
 await writeFile(join(artifacts,'verification.json'),JSON.stringify({passed:true,checks:['cutover screen removed','all cutover endpoints removed','ordinary history retained','external partial settlement','reversal','referenced adjustment autosave','CSV reference','unauthorized settlement rejected'],artifacts},null,2));
 console.log(JSON.stringify({passed:true,artifacts}));
}catch(e){if(page)await page.screenshot({path:join(artifacts,'failure.png'),fullPage:true});throw e}finally{await browser.close()}
