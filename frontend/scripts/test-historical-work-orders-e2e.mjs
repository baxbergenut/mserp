// Real cutover and UI checks in a disposable review-local schema only.
import { readFile, mkdir } from 'node:fs/promises';
import { execFileSync } from 'node:child_process';
import { dirname, join } from 'node:path';
import { randomUUID } from 'node:crypto';
import { chromium, expect } from '@playwright/test';

const access=JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE,'utf8'));
const database=new URL(process.env.MSERP_REVIEW_TEST_DATABASE_URL);
if(!['127.0.0.1','localhost'].includes(database.hostname) || !database.pathname.endsWith('_test') || !/^review_[a-f0-9]+$/.test(access.schema) || !['127.0.0.1','localhost'].includes(new URL(access.base).hostname)) throw Error('Isolated local review required');
const browser=await chromium.launch();
try {
 const page=await browser.newPage({viewport:{width:1536,height:1040}});
 await page.goto(access.base+'/login');
 await page.getByLabel('Email or existing username').fill(access.email);
 await page.getByLabel('Password',{exact:true}).fill(access.password);
 await page.getByRole('button',{name:'Sign in',exact:true}).click();
 await page.waitForURL(url=>!url.pathname.startsWith('/login'));
 const session=await(await page.request.get(access.base+'/api/auth/session')).json();
 const headers={'X-CSRF-Token':session.csrfToken,Origin:access.base};
 const api=async(method,path,data)=>{const r=await page.request[method](access.base+'/api'+path,{headers,...(data?{data}:{})});if(!r.ok())throw Error(path+': '+r.status()+' '+await r.text());return r.json();};
 const options=await api('get','/maintenance/options');
 const day=new Date().toISOString().slice(0,10),truck=options.trucks.find(t=>t.active);
 const draft=(stage,linked,notes)=>({id:randomUUID(),version:0,truckId:linked?truck.id:null,status:stage==='completed'?'completed':'needs_review',workStatus:stage,serviceDate:day,location:'shop',notes,services:[{typeCode:linked?'electrical':'preventive',name:linked?'Electrical':'Oil change',amount:null,trackingCode:linked?'':'pm'}],expense:null});
 const oil=await api('post','/maintenance',draft('needs_review',false,'Historical cutover oil import'));
 const invoice=await api('post','/maintenance',{...draft('completed',true,'Historical cutover paid invoice'),expense:{categoryId:options.categoryId,expenseDate:day,amount:'89.50',baseAmount:'89.50',expenseType:'Electrical',paymentType:'Check',paidBy:options.currentUser,coveredBy:'Company',costBreakdown:[],referenceNumber:'HISTORICAL-PAID'}});
 const fresh=await api('post','/maintenance',draft('completed',true,'New cutover unpaid work'));
 const review=await api('post','/maintenance',draft('needs_review',false,'New cutover service review'));
 const migration=(await readFile('../backend/sql/135_historical_work_orders_paid.sql','utf8')).replace(/^BEGIN;\s*/,'').replace(/COMMIT;\s*$/,'');
 // Reconstruct the pre-135 schema and activation ledger atomically; all other
 // review fixtures have explicit workflow stages and stay outside the cutover.
 const sql=`BEGIN; SET search_path TO ${access.schema},public;
 UPDATE maintenance_records SET work_status='',created_at=now()-interval '2 hours' WHERE id IN ('${oil.id}','${invoice.id}');
 UPDATE maintenance_records SET source_unit_number='047' WHERE id='${oil.id}';
 INSERT INTO schema_migrations(version,applied_at) VALUES('132_fleet_workflow.sql',now()-interval '1 hour') ON CONFLICT(version) DO UPDATE SET applied_at=excluded.applied_at;
 SET CONSTRAINTS ALL IMMEDIATE;
 DROP TRIGGER maintenance_historical_paid_guard ON maintenance_records;
 DROP FUNCTION maintenance_historical_paid_guard(); ALTER TABLE maintenance_records DROP COLUMN historical_paid;
 ${migration}
 COMMIT;`;
 try {execFileSync(process.env.PSQL_PATH||'psql',['-X','-q','-v','ON_ERROR_STOP=1','-d',database.toString()],{input:sql,stdio:['pipe','ignore','pipe'],windowsHide:true});}
 catch(error){throw Error(error.stderr?.toString() || 'Historical fixture migration failed');}
 await page.goto(access.base+'/maintenance');
 const row=id=>page.getByRole('row',{name:'Open maintenance '+id,exact:true});
 await expect(row(review.id)).toBeVisible();await expect(row(oil.id)).toHaveCount(0);await expect(row(invoice.id)).toHaveCount(0);
 await page.getByRole('tab',{name:'Completed - pending payment',exact:true}).click();
 await expect(row(fresh.id)).toBeVisible();await expect(row(oil.id)).toHaveCount(0);await expect(row(invoice.id)).toHaveCount(0);
 await page.getByRole('tab',{name:'Completed - paid',exact:true}).click();
 await expect(row(oil.id)).toBeVisible();await expect(row(invoice.id)).toContainText('$89.50');await expect(row(invoice.id)).toContainText('Check');await expect(row(invoice.id)).toContainText(options.currentUser);await expect(row(fresh.id)).toHaveCount(0);
 const savedInvoice=await api('get','/maintenance/'+invoice.id);expect(savedInvoice.expense).toEqual(invoice.expense);expect(savedInvoice.historicalPaid).toBe(true);expect(savedInvoice.paymentConfirmation).toBeNull();
 const savedOil=await api('get','/maintenance/'+oil.id);expect(savedOil.status).toBe('needs_review');expect(savedOil.truckId).toBeNull();expect(savedOil.unitNumber).toBe('047');expect(savedOil.noCharge).toBe(false);expect(savedOil.expenseId).toBeNull();
 const artifacts=join(dirname(process.env.MSERP_REVIEW_ACCESS_FILE),'historical-work-orders');await mkdir(artifacts,{recursive:true});
 await page.screenshot({path:join(artifacts,'historical-paid.png'),fullPage:true,animations:'disabled'});
 await row(oil.id).click();await expect(page.getByText('Historical work · paid',{exact:true})).toBeVisible();await expect(page.getByRole('button',{name:'Confirm paid & finish',exact:true})).toHaveCount(0);await expect(page.getByText('Truck not linked',{exact:true})).toBeVisible();
 console.log('PASS: historical oil and invoices only in paid, recorded payment details preserved, unresolved identity retained, new unpaid/review work unchanged.');
} finally {await browser.close();}
