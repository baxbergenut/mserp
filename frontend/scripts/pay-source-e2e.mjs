import { expect } from '@playwright/test';
import { join } from 'node:path';

export async function runPaySourceE2E({ page, base, temp, sql, schema }) {
  const ids = Array.from({length: 7}, (_,i) => `93000000-0000-0000-0000-${String(i+1).padStart(12,'0')}`);
  const [driver, owner, truck, type, investor, investorTruck, expense] = ids;
  sql(`SET search_path TO ${schema},public;
    INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES('${driver}','Carry Driver','carry driver','gross_percentage',88);
    INSERT INTO investors(id,full_name,driver_id) VALUES('${owner}','Carry Driver','${driver}');
    INSERT INTO investors(id,full_name) VALUES('${investor}','Carry Investor');
    INSERT INTO trucks(id,unit_number,owner_id) VALUES('${truck}','CARRY-DRIVER','${owner}'),('${investorTruck}','CARRY-INVESTOR','${investor}');
    UPDATE truck_ownership_history SET start_known=false WHERE truck_id IN ('${truck}','${investorTruck}');
    INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES('${truck}','${driver}','2026-01-01');
    INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) VALUES('${investorTruck}','${investor}','2026-09-28',88);
    INSERT INTO driver_charge_types(id,name,direction,amount,amounts,eligibility) VALUES('${type}','Carry insurance','charge',100,ARRAY[100]::numeric[],'calendar');
    INSERT INTO truck_charge_phases(truck_id,type_id,week_start,amount) VALUES('${truck}','${type}','2026-09-28',100),('${investorTruck}','${type}','2026-09-28',100);
    WITH f AS (INSERT INTO fuel_transactions(relay_environment,relay_transaction_id,driver_id,relay_driver_id,purchased_at,total_amount_paid,total_retail_price,total_amount_saved,is_direct_bill,currency_code,merchant_id,merchant_name,merchant_number,location_id,location_name,merchant_location_id,address,city,state,postal_code,latitude,longitude,timezone,prompts,raw_payload)
    VALUES('production','carry-e2e','${driver}','relay-test','2026-09-28 12:00+00',100,100,0,false,'USD','','','','','','','','','','',0,0,'America/New_York','[{"label":"Truck #","value":"CARRY-INVESTOR"}]','{}') RETURNING id)
    INSERT INTO fuel_transaction_items(fuel_transaction_id,line_number,item_kind,category,total_amount_paid) SELECT id,0,'fuel','diesel',100 FROM f;
    INSERT INTO tolls(truck_id,posting_date,invoice_date,customer_id,source,read_type,transponder_or_plate,equipment_unit,agency,exit_plaza,exit_date,exit_time,toll_class,amount,row_fingerprint)
    VALUES('${investorTruck}','2026-09-29','2026-09-29','','','','','CARRY-INVESTOR','','','2026-09-29','12:00','',40,repeat('e',64));
    INSERT INTO expenses(id,category_id,category,expense_type,truck_id,owner_id,covered_by,expense_date,amount)
    SELECT '${expense}',id,name,'Carry repair','${investorTruck}','${investor}','Truck Owner','2026-09-28',100 FROM expense_settings WHERE kind='category' AND name='Maintenance';
  `);
  const session = await (await page.request.get(base+'/api/auth/session')).json();
  const headers = {'x-csrf-token': session.csrfToken};
  for (const investorMode of [false,true]) {
    const kind=investorMode?'investor':'driver', name=investorMode?'Carry Investor':'Carry Driver', id=investorMode?investorTruck:driver;
    const target=investorMode?'truckId':'driverId';
    const read=async week => (await (await page.request.get(`${base}/api/${kind}-pay?weekStart=${week}&${target}=${id}`)).json()).drivers.find(d=>d.id===id);
    const open=async week => {await page.goto(`${base}/accounting/${kind}-pay?weekStart=${week}&${target}=${id}`);await expect(page.getByLabel(`${name}, Carry insurance, amount`,{exact:true})).toBeVisible();};
    await open('2026-09-28');
    const fee=page.getByLabel(`${name}, Carry insurance, amount`,{exact:true});
    await expect(fee).toHaveValue('-100.00');await expect(fee).toBeEditable();
    await fee.fill('25');await fee.press('Tab');
    await expect.poll(async()=>(await read('2026-09-28')).edits.sourceCharges.find(r=>r.source.startsWith('truck_charge:')).amount).toBe('-25.00');
    await expect(page.getByText('All changes saved',{exact:true})).toBeVisible();
    // Undo a saved collection preserves the newest cross-week revision.
    await fee.locator('..').click();await page.keyboard.press('Control+z');
    await expect.poll(async()=>(await read('2026-09-28')).edits.sourceCharges.find(r=>r.source.startsWith('truck_charge:')).amount).toBe('-100.00');
    await fee.fill('25');await fee.press('Tab');
    await expect.poll(async()=>(await read('2026-09-28')).edits.sourceCharges.find(r=>r.source.startsWith('truck_charge:')).amount).toBe('-25.00');
    if(investorMode){
      for(const [label,value,key] of [['Fuel','60','fuelOverride'],['Toll','10','tollOverride']]){
        const input=page.getByLabel(`${name}, ${label}, amount`,{exact:true});await input.fill(value);await input.press('Tab');
        await expect.poll(async()=>(await read('2026-09-28')).edits[key]).toBe(`-${value}.00`);
      }
      const input=page.getByLabel(`${name}, expense Carry repair, deduction`,{exact:true});await input.fill('25');await input.press('Tab');
      await expect.poll(async()=>(await read('2026-09-28')).edits.expenseDeductions.find(r=>r.expenseId===expense).amount).toBe('25.00');
    }
    await open('2026-10-05');await expect(fee).toHaveValue('-175.00');
    if(investorMode){
      await expect(page.getByLabel(`${name}, Fuel, amount`,{exact:true})).toHaveValue('-40.00');
      await expect(page.getByLabel(`${name}, Toll, amount`,{exact:true})).toHaveValue('-30.00');
      await expect(page.getByLabel(`${name}, expense Carry repair, deduction`,{exact:true})).toHaveValue('-75.00');
    }
    await fee.fill('176');await fee.press('Tab');await expect(fee).toHaveAttribute('aria-invalid','true');
    await expect(page.getByRole('alert').filter({hasText:'Carry insurance:'})).toContainText('This edit has not been saved');
    await fee.fill('0');await fee.press('Tab');
    await expect.poll(async()=>(await read('2026-10-05')).edits.sourceCharges.find(r=>r.source.startsWith('truck_charge:')).amount).toBe('0.00');
    await page.screenshot({path:join(temp,`${kind}-source-carry.png`),fullPage:true});
    await open('2026-10-12');await expect(fee).toHaveValue('-275.00');
    if(investorMode){
      // Other displayed balances were collected with the preceding autosave.
      await expect(page.getByLabel(`${name}, Fuel, amount`,{exact:true})).toHaveValue('0.00');
      await expect(page.getByLabel(`${name}, Toll, amount`,{exact:true})).toHaveValue('0.00');
      await expect(page.getByLabel(`${name}, expense Carry repair, deduction`,{exact:true})).toHaveCount(0);
    }
    for(const invalid of ['1.00','-275.01']){
      const edits=(await read('2026-10-12')).edits;edits.sourceCharges.find(r=>r.source.startsWith('truck_charge:')).amount=invalid;
      const rejected=await page.request.put(`${base}/api/${kind}-pay`,{headers,data:edits});expect(rejected.status(),await rejected.text()).toBe(400);
    }
  }
  console.log('Shared payroll carry E2E passed: editable truck fees, saved undo, zero/partial carry, investor Fuel/Toll/expenses, UI/API bounds and no duplicate collection.');
}
