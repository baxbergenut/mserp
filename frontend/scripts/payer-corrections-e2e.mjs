import { expect } from '@playwright/test';
import { join } from 'node:path';

export async function runPayerCorrectionsE2E({ page, base, temp, sql, schema }) {
  const [driver, oldOwner, owner, truck, type, expense] = Array.from({length:6}, (_,i)=>`98000000-0000-0000-0000-${String(i+1).padStart(12,'0')}`);
  sql(`SET search_path TO ${schema},public;
    INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES('${driver}','Correction Driver','correction driver','cpm',0.75);
    UPDATE driver_dispatcher_assignments SET assigned_at='2026-09-21 04:00+00' WHERE driver_id='${driver}';
    INSERT INTO investors(id,full_name,driver_id) VALUES('${oldOwner}','Correction Driver','${driver}');
    INSERT INTO investors(id,full_name) VALUES('${owner}','Correction Investor');
    INSERT INTO trucks(id,unit_number,owner_id) VALUES('${truck}','CORRECT-PAYER','${oldOwner}');
    UPDATE truck_ownership_history SET assigned_at='2026-09-21 04:00+00',start_known=true WHERE truck_id='${truck}';
    INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES('${truck}','${driver}','2026-09-21 04:00+00');
    INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) VALUES('${truck}','${owner}','2026-10-26',88);
    INSERT INTO driver_charge_types(id,name,direction,amount,amounts,eligibility) VALUES('${type}','Correction admin fee','charge',120,ARRAY[120]::numeric[],'calendar');
    INSERT INTO truck_charge_phases(truck_id,type_id,week_start,amount) VALUES('${truck}','${type}','2026-09-28',120);
    INSERT INTO pay_source_collections(source,owner_id,week_start,kind,statement_id,truck_id,name,base,amount,statement)
      VALUES('truck_charge:${truck}:${type}','${oldOwner}','2026-10-19','driver','${driver}','${truck}','Correction admin fee',-120,-120,'{}');
    INSERT INTO expenses(id,category_id,category,expense_type,truck_id,driver_id,covered_by,expense_date,amount,payment_type)
      SELECT '${expense}',id,name,'Brake correction','${truck}','${driver}','Driver','2026-10-28',836.01,'Cash' FROM expense_settings WHERE kind='category' AND name='Maintenance';
    INSERT INTO expense_payments(expense_id,week_start,amount) VALUES('${expense}','2026-10-19',836.01);
  `);
  const session=await (await page.request.get(`${base}/api/auth/session`)).json();
  const headers={'x-csrf-token':session.csrfToken};
  const read=async(kind,week='2026-10-19')=>{
    const response=await page.request.get(`${base}/api/${kind}-pay?weekStart=${week}&${kind==='driver'?'driverId='+driver:'truckId='+truck}`);
    expect(response.ok(),await response.text()).toBe(true);
    return (await response.json()).drivers;
  };
  const before=(await read('driver')).find(d=>d.id===driver);
  expect(before.autoCharges.some(c=>c.name==='Correction admin fee')).toBe(true);
  // Correct ownership through the same dated form used in production.
  await page.goto(`${base}/trucks/detail?id=${truck}`);
  await page.getByRole('button',{name:'Edit truck',exact:true}).click();
  await page.getByLabel('Truck owner',{exact:true}).selectOption(owner);
  await page.getByLabel('Ownership effective date',{exact:true}).fill('2026-09-21');
  await expect(page.getByText(/Incorrect fees saved for the previous owner are cleared/)).toBeVisible();
  await page.getByRole('button',{name:'Save truck',exact:true}).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(((await read('driver')).find(d=>d.id===driver).autoCharges??[]).some(c=>c.name==='Correction admin fee')).toBe(false);
  // Correct the expense from its original Driver Pay statement.
  await page.goto(`${base}/accounting/driver-pay?weekStart=2026-10-19&driverId=${driver}`);
  await page.getByText('Brake correction',{exact:true}).click();
  const dialog=page.getByRole('dialog');
  await dialog.getByLabel('Who will cover',{exact:true}).selectOption('Truck Owner');
  await expect(dialog.getByText(/Saving transfers the saved deductions/)).toBeVisible();
  await page.screenshot({path:join(temp,'payer-correction-preview.png'),fullPage:true});
  await dialog.getByRole('button',{name:'Save changes',exact:true}).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByText('Brake correction',{exact:true})).toHaveCount(0);
  const updated=await (await page.request.get(`${base}/api/expenses/${expense}`)).json();
  expect(updated.coveredBy).toBe('Truck Owner');
  expect(updated.ownerId).toBe(owner);
  expect(updated.payments).toEqual([{weekStart:'2026-10-19',amount:'836.01'}]);
  const target=(await read('investor')).find(d=>d.id===truck);
  expect(target.edits.expenseDeductions.find(d=>d.expenseId===expense)).toMatchObject({amount:'836.01',saved:true});
  // The original week predates the agreement. Its expense remains visible,
  // while earnings remain uncalculated until an explicit agreement is entered.
  await page.goto(`${base}/accounting/investor-pay?weekStart=2026-10-19&truckId=${truck}`);
  await expect(page.getByText('Brake correction',{exact:true})).toBeVisible();
  await expect(page.getByLabel('Correction Investor, expense Brake correction, deduction',{exact:true})).toHaveValue('-836.01');
  await expect(page.getByText(/reassigned expense deductions only/)).toBeVisible();
  await page.screenshot({path:join(temp,'payer-correction-investor.png'),fullPage:true});
  const next=(await read('investor','2026-10-26')).find(d=>d.id===truck);
  expect((next.edits.expenseDeductions??[]).some(d=>d.expenseId===expense)).toBe(false);
  // A stale original invoice cannot put the deduction back on the driver.
  const rejected=await page.request.put(`${base}/api/expenses/${expense}`,{headers,data:{categoryId:updated.categoryId,expenseDate:'2026-10-28',expenseType:'Brake correction',truckId:truck,driverId:driver,coveredBy:'Driver',amount:'836.01',paymentType:'Cash',updatedAt:'2026-01-01T00:00:00Z'}});
  expect(rejected.status()).toBe(409);
  console.log('Payer corrections E2E passed: dated ownership clears saved fees, expense editor transfers the original deduction, investor history stays visible, no duplicate collection, and stale edits fail.');
}
