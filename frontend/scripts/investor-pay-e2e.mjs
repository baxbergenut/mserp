import { expect } from '@playwright/test';
import { join } from 'node:path';

export async function runInvestorPayE2E({ page, base, sql, schema, temp }) {
  const owner = '10000000-0000-0000-0000-000000000001';
  const hired = '10000000-0000-0000-0000-000000000002';
  const investor = '10000000-0000-0000-0000-000000000003';
  const ownTruck = '10000000-0000-0000-0000-000000000004';
  const extraTruck = '10000000-0000-0000-0000-000000000005';
  sql(`SET search_path TO ${schema},public;
    INSERT INTO drivers(id,full_name,normalized_name,is_owner_operator,pay_type,pay_rate) VALUES
      ('${owner}','Payroll Hector','payroll hector',true,'gross_percentage',88),
      ('${hired}','Payroll Employee','payroll employee',false,'cpm',0.50);
    INSERT INTO investors(id,full_name,driver_id) VALUES('${investor}','Payroll Hector','${owner}');
    INSERT INTO trucks(id,unit_number,owner_id) VALUES('${ownTruck}','PAY-OWN','${investor}');
    INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES('${ownTruck}','${owner}','2026-01-01');
  `);
  let directory = await (await page.request.get(`${base}/api/investors?page=1&pageSize=100`)).json();
  expect(directory.items.some(i => i.id === investor)).toBe(true);
  expect((await (await page.request.get(`${base}/api/investors`)).json()).some(i => i.id === investor)).toBe(true);
  sql(`SET search_path TO ${schema},public;
    INSERT INTO trucks(id,unit_number,owner_id) VALUES('${extraTruck}','PAY-EXTRA','${investor}');
    INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES('${extraTruck}','${hired}','2026-01-01');
    INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,truck_unit,pickup_time,raw_payload) VALUES
    (8891,'PAY-OWN-LOAD','delivered',2000,2000,500,'PAY-OWN','2026-09-28 00:01+00','{"trip":{"mile":450,"empty_mile":50},"stops":[{"stop_type":"pickup","ordering":1,"location":{"city":"Dallas","state":"TX"}},{"stop_type":"delivery","ordering":2,"location":{"city":"Columbus","state":"OH"}}]}'),
    (8892,'PAY-EXTRA-LOAD','delivered',10000,10000,1000,'PAY-EXTRA','2026-09-28 00:01+00','{"trip":{"mile":900,"empty_mile":100},"stops":[{"stop_type":"pickup","ordering":1,"location":{"city":"Dallas","state":"TX"}},{"stop_type":"delivery","ordering":2,"location":{"city":"Columbus","state":"OH"}}]}');
    INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate) VALUES
      ('${owner}','2026-09-28','PAY-OWN-LOAD',8891,2000),('${hired}','2026-09-28','PAY-EXTRA-LOAD',8892,10000);
  `);
  directory = await (await page.request.get(`${base}/api/investors?page=1&pageSize=100`)).json();
  expect(directory.items.some(i => i.id === investor)).toBe(true);
  const session = await (await page.request.get(`${base}/api/auth/session`)).json();
  const headers = { 'x-csrf-token': session.csrfToken };
  for (const truckId of [extraTruck]) {
    const response = await page.request.put(`${base}/api/truck-charges/terms`, { headers, data: { truckId, ownerId: investor, weekStart: '2026-01-05', sharePercent: '88', version: 0 } });
    expect(response.status(), await response.text()).toBe(204);
  }
  const truckConfig = await (await page.request.get(`${base}/api/truck-charges`)).json();
  expect(truckConfig.eligibleTruckIds).toContain(extraTruck);
  expect(truckConfig.eligibleTruckIds).not.toContain(ownTruck);
  const rejected = await page.request.put(`${base}/api/truck-charges/terms`, { headers, data: { truckId: ownTruck, ownerId: investor, weekStart: '2026-01-05', sharePercent: '88', version: 0 } });
  expect(rejected.status()).toBe(400);
  const created = await page.request.post(`${base}/api/driver-charges/types`, { headers, data: { id: '', name: 'Truck admin', direction: 'charge', amount: '100.00', amounts: ['100.00'], eligibility: 'calendar', archived: false, version: 0 } });
  expect(created.ok(), await created.text()).toBe(true);
  const type = await created.json();
  const fee = await page.request.put(`${base}/api/truck-charges/recurring`, { headers, data: { truckId: extraTruck, typeId: type.id, weekStart: '2026-01-05', amount: '100.00', included: true, version: 0, typeVersion: type.version } });
  expect(fee.status(), await fee.text()).toBe(204);
  await page.goto(`${base}/accounting/driver-charges?tab=trucks`);
  await expect(page.getByRole('tab', { name: 'Truck charges', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('row').filter({ hasText: 'PAY-EXTRA' })).toContainText('Payroll Employee');
  await expect(page.getByRole('checkbox', { name: 'PAY-EXTRA, Truck admin', exact: true })).toBeChecked();
  await page.goto(`${base}/accounting/investor-pay?weekStart=2026-09-28`);
  await expect(page.getByRole('heading', { name: 'Investor pay', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Payroll Hector PAY-EXTRA', exact: true }).click();
  await expect(page.getByRole('link', { name: 'PAY-EXTRA-LOAD', exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'PAY-OWN-LOAD', exact: true })).toHaveCount(0);
  await expect(page.getByText('Driver earnings · Payroll Employee', { exact: true })).toBeVisible();
  await expect(page.getByRole('table', { name: 'Weekly investor pay' })).toContainText('8,200.00');
  await expect(page.getByText('Truck admin', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Payroll Hector, weekly comment', exact: true }).click();
  await page.getByLabel('Comment', { exact: true }).fill('Investor settlement verified');
  await page.getByRole('button', { name: 'Save comment', exact: true }).click();
  await expect.poll(async () => (await (await page.request.get(`${base}/api/investor-pay?weekStart=2026-09-28`)).json()).drivers[0].edits.notes, { timeout: 15000 }).toBe('Investor settlement verified');
  await page.getByRole('button', { name: 'Payroll Hector PAY-EXTRA', exact: true }).click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Finalize truck', exact: true }).click();
  await page.getByRole('button', { name: 'Finalize settlement', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await page.getByRole('button', { name: 'Payroll Hector PAY-EXTRA', exact: true }).click({ button: 'right' });
  await expect(page.getByRole('menuitem', { name: 'Reopen truck', exact: true })).toBeVisible();
  await page.keyboard.press('Escape');
  await page.screenshot({ path: join(temp, 'investor-pay-desktop.png'), fullPage: true });
  await page.getByRole('button', { name: 'Payroll Hector PAY-EXTRA', exact: true }).click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Reopen truck', exact: true }).click();
  await page.getByLabel('Reason for reopening').fill('E2E correction');
  await page.getByRole('button', { name: 'Reopen settlement', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await page.getByRole('button', { name: 'Payroll Hector PAY-EXTRA', exact: true }).click({ button: 'right' });
  await expect(page.getByRole('menuitem', { name: 'Finalize truck', exact: true })).toBeVisible();
  await page.keyboard.press('Escape');
  await page.goto(`${base}/accounting/driver-pay?weekStart=2026-09-28`);
  await page.getByRole('button', { name: 'Payroll Hector', exact: true }).click();
  await expect(page.getByRole('link', { name: 'PAY-OWN-LOAD', exact: true })).toBeVisible();
  await expect(page.getByRole('table', { name: 'Weekly driver pay' })).toContainText('1,760.00');
  console.log('Investor payroll E2E passed: single-truck owner discovery, additional-truck inclusion, truck charges, shared load table, CPM earnings deduction, autosave, finalize/reopen, separate owner-operator pay.');
}
