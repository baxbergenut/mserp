import { expect } from '@playwright/test';
import { join } from 'node:path';

export async function runPayrollLoadPlacementsE2E({page,base,sql,schema,temp}) {
 const driver='b1000000-0000-0000-0000-000000000001',truck='b2000000-0000-0000-0000-000000000001',owner='b3000000-0000-0000-0000-000000000001';
 const week='2026-10-19',old='2026-09-28';
 sql(`SET search_path TO ${schema},public;
 INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES('${driver}','Placement Driver','placement driver','gross_percentage',25);
 INSERT INTO investors(id,full_name) VALUES('${owner}','Placement Investor');
 INSERT INTO trucks(id,unit_number) VALUES('${truck}','PLACEMENT');
 UPDATE trucks SET owner_id='${owner}' WHERE id='${truck}';UPDATE truck_ownership_history SET assigned_at='2026-01-01' WHERE truck_id='${truck}';
 INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES('${truck}','${driver}','2026-01-01');
 INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) VALUES('${truck}','${owner}','2026-01-05',90);
 INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,pickup_time,delivery_time,raw_payload) VALUES
 (8801,'MISSED-LOAD','delivered',3000,3000,600,'2026-09-28','2026-09-29','{"trip":{"mile":550,"empty_mile":50},"stops":[{"stop_type":"pickup","ordering":1,"location":{"city":"First","state":"OH"}},{"stop_type":"delivery","ordering":2,"location":{"city":"Last","state":"PA"}}]}'),
 (8802,'FLAGGED-LOAD','dispatched',1000,1000,100,'2026-10-20','2026-10-21','{}');
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate) VALUES('${driver}','${old}','MISSED-LOAD',8801,2800),('${driver}','2026-10-20','FLAGGED-LOAD',8802,900);`);
 const session=await (await page.request.get(`${base}/api/auth/session`)).json();
 const headers={'X-CSRF-Token':session.csrfToken};
 const get=async path=>{const response=await page.request.get(`${base}/api${path}`);expect(response.ok()).toBe(true);return response.json();};
 const post=async(kind,input)=>page.request.post(`${base}/api/${kind}-pay/statement-load`,{headers,data:input});
 const boardBefore=await get(`/gross-board?weekStart=${old}`);
 const go=async kind=>{
  await page.goto(`${base}/accounting/${kind}-pay?weekStart=${week}&${kind==='driver'?'driverId':'truckId'}=${kind==='driver'?driver:truck}`);
  await expect(page.getByRole('region',{name:`Placement ${kind==='driver'?'Driver':'Investor'} weekly loads`})).toBeVisible();
 };
 await go('driver');
 await expect(page.getByLabel(/Load is not delivered or invoiced/)).toBeVisible();
 await page.getByRole('button',{name:'Remove FLAGGED-LOAD from statement',exact:true}).click();
 await expect(page.getByRole('link',{name:'FLAGGED-LOAD',exact:true})).toHaveCount(0);
 let source=await get(`/gross-board?weekStart=${week}`);expect(source.entries.find(e=>e.loadNumber==='FLAGGED-LOAD')).toBeTruthy();
 // The extra row is below Sunday, fills all source details, and persists on reload.
 const input=page.getByLabel('Placement Driver, extra load number');
 await input.fill('UNKNOWN-LOAD');await input.press('Enter');
 await expect(page.getByRole('alert').filter({hasText:'Load not found'})).toBeVisible();
 await input.fill('MISSED-LOAD');await input.press('Enter');
 await expect(page.getByLabel('MISSED-LOAD, driver gross')).toHaveValue('2800.00');
 await expect(page.getByText('First, OH',{exact:true})).toBeVisible();
 await expect(page.getByText('Last, PA',{exact:true})).toBeVisible();
 await expect(page.getByText('Extra',{exact:true}).locator('..')).toContainText('MISSED-LOAD');
 const labels=await page.locator('tbody th[scope=row],tbody th[scope=rowgroup]').allTextContents();
 expect(labels.findIndex(s=>s==='Extra')).toBeGreaterThan(labels.findIndex(s=>s.startsWith('Sun')));
 await page.reload();await expect(page.getByLabel('MISSED-LOAD, driver gross')).toHaveValue('2800.00');
 expect(await get(`/gross-board?weekStart=${old}`)).toEqual(boardBefore);
 await go('investor');
 await expect(page.getByLabel('MISSED-LOAD, driver gross')).toHaveValue('2800.00');
 sql(`SET search_path TO ${schema},public;
 INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,raw_payload) VALUES(8803,'SPLIT-CATCHUP','delivered',1000,1000,200,
 '{"trips":[{"id":901,"trip_id":"LEG-A","driver__full_name":"Source label","total_load_pay":600,"mile":100,"empty_mile":10,"trip_begin_date":"2026-10-05","pickup":{"city":"First","state":"OH"},"delivery":{"city":"Middle","state":"PA"}},{"id":902,"trip_id":"LEG-B","driver__full_name":"Source label","total_load_pay":400,"mile":80,"empty_mile":10,"trip_begin_date":"2026-10-06","pickup":{"city":"Middle","state":"PA"},"delivery":{"city":"Last","state":"NJ"}}]}');
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,load_trip_id,driver_rate) VALUES('${driver}','2026-10-05','SPLIT-CATCHUP',8803,901,550),('${driver}','2026-10-06','SPLIT-CATCHUP',8803,902,350);`);
 await page.getByLabel('Placement Investor, extra load number').fill('SPLIT-CATCHUP');
 await page.getByRole('button',{name:'Add to statement',exact:true}).click();
 await expect(page.getByLabel('Load trip allocation')).toBeVisible();
 await page.getByLabel('Load trip allocation').selectOption({label:'LEG-A · Placement Driver · 2026-10-05'});
 await page.getByRole('button',{name:'Add to statement',exact:true}).click();
 await expect(page.getByLabel('SPLIT-CATCHUP, driver gross')).toHaveValue('550.00');
 let splitReport=await get(`/investor-pay?weekStart=${week}&truckId=${truck}`);
 let split=splitReport.drivers.find(d=>d.id===truck).loads.find(l=>l.loadNumber==='SPLIT-CATCHUP');
 expect(split.loadTripId).toBe(901);expect(Number(split.originalRate)).toBe(600);expect(split.fee).toBe('495.00');
 await page.getByRole('button',{name:'Remove SPLIT-CATCHUP from statement',exact:true}).click();
 await expect(page.getByLabel('SPLIT-CATCHUP, driver gross')).toHaveCount(0);
 const splitBoard=await get('/gross-board?weekStart=2026-10-05');expect(splitBoard.entries.filter(e=>e.loadNumber==='SPLIT-CATCHUP')).toHaveLength(2);
 await page.getByRole('button',{name:'Remove MISSED-LOAD from statement',exact:true}).click();
 await expect(page.getByLabel('MISSED-LOAD, driver gross')).toHaveCount(0);
 await page.getByLabel('Placement Investor, extra load number').fill('MISSED-LOAD');
 await page.getByRole('button',{name:'Add to statement',exact:true}).click();
 await expect(page.getByLabel('MISSED-LOAD, driver gross')).toHaveValue('2800.00');
 const gross=page.getByLabel('MISSED-LOAD, driver gross');
 await gross.fill('2600');await gross.press('Enter');
 await expect(page.getByLabel('MISSED-LOAD, driver gross')).toHaveValue('2600.00');
 let report=await get(`/driver-pay?weekStart=${week}`);
 expect(report.drivers.find(d=>d.id===driver).loads[0].fee).toBe('650.00');
 const failed=await post('driver',{driverId:driver,weekStart:week,action:'add',loadNumber:'MISSED-LOAD'});expect(failed.status()).toBe(400);
 // Real finalization freezes the supplemental row; the open counterpart cannot remove it.
 let response=await page.request.post(`${base}/api/driver-pay/finalize`,{headers,data:{driverId:driver,weekStart:week,revision:report.revision}});
 expect(response.ok(),await response.text()).toBe(true);
 await page.getByRole('button',{name:'Remove MISSED-LOAD from statement',exact:true}).click();
 await expect(page.getByRole('alert').filter({hasText:/Reopen affected|already included in a finalized/})).toBeVisible();
 await expect(page.getByLabel('MISSED-LOAD, driver gross')).toHaveValue('2600.00');
 await go('driver');await expect(page.getByLabel('Placement Driver, extra load number')).toBeDisabled();
 await expect(page.getByRole('button',{name:'Remove MISSED-LOAD from statement',exact:true})).toBeDisabled();
 await page.screenshot({path:join(temp,'payroll-extra-load-finalized.png'),fullPage:true});
 // View-only access has no editor, and the server rejects a forged mutation.
 sql(`SET search_path TO ${schema},public;
 INSERT INTO app_roles(name,permissions) VALUES('Statement Load Viewer',ARRAY['driver_pay.read','investor_pay.read']);
 INSERT INTO app_users(username,email,password_hash,role_id) SELECT 'Statement Load Viewer','placement-viewer@example.test',crypt('synthetic-viewer-password',gen_salt('bf')),id FROM app_roles WHERE name='Statement Load Viewer';`);
 const viewer=await page.context().browser().newPage();
 try {
  await viewer.goto(`${base}/login`);await viewer.getByLabel('Email',{exact:true}).fill('placement-viewer@example.test');await viewer.getByLabel('Password').fill('synthetic-viewer-password');await viewer.getByRole('button',{name:'Sign in',exact:true}).click();
  await expect.poll(async()=>new URL(viewer.url()).pathname).not.toBe('/login');
  const auth=await (await viewer.request.get(`${base}/api/auth/session`)).json();
  for(const kind of ['driver','investor']){
   await viewer.goto(`${base}/accounting/${kind}-pay?weekStart=${week}&${kind==='driver'?'driverId':'truckId'}=${kind==='driver'?driver:truck}`);
   await expect(viewer.getByRole('region',{name:`Placement ${kind==='driver'?'Driver':'Investor'} weekly loads`})).toBeVisible();
   await expect(viewer.getByPlaceholder('Load ID',{exact:true})).toHaveCount(0);
   const denied=await viewer.request.post(`${base}/api/${kind}-pay/statement-load`,{headers:{'X-CSRF-Token':auth.csrfToken},data:{driverId:kind==='driver'?driver:truck,weekStart:week,action:'add',loadNumber:'MISSED-LOAD'}});expect(denied.status()).toBe(403);
  }
 }finally{await viewer.close();}
 expect(await get(`/gross-board?weekStart=${old}`)).toEqual(boardBefore);
 console.log('Payroll load placement E2E passed: both pages, source details, errors, persistence, gross edit, payroll-only removal, finalized counterparts and read-only permissions.');
}
