import { expect } from '@playwright/test';
import { join } from 'node:path';

export async function runPayrollOverrideE2E({ page, base, sql, schema, temp }) {
  const driver = 'a8600000-0000-0000-0000-000000000001';
  const investor = 'a8600000-0000-0000-0000-000000000002';
  const truck = 'a8600000-0000-0000-0000-000000000003';
  const week = '2026-09-28';
  sql(`SET search_path TO ${schema},public;
    INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES('${driver}','Override Driver','override driver','gross_percentage',25);
    INSERT INTO investors(id,full_name) VALUES('${investor}','Override Investor');
    INSERT INTO trucks(id,unit_number,owner_id) VALUES('${truck}','OVERRIDE-1','${investor}');
    UPDATE truck_ownership_history SET start_known=false WHERE truck_id='${truck}';
    INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES('${truck}','${driver}','2026-01-01');
    INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,truck_unit,pickup_time,raw_payload) VALUES
      (88601,'OVERRIDE-LOAD','delivered',1000,1000,100,'OVERRIDE-1','2026-09-28 12:00+00','{"trip":{"mile":90,"empty_mile":10},"stops":[{"stop_type":"pickup","ordering":1,"location":{"city":"Dallas","state":"TX"}}]}');
    INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate) VALUES('${driver}','${week}','OVERRIDE-LOAD',88601,1000);
  `);
  const { csrfToken } = await (await page.request.get(`${base}/api/auth/session`)).json();
  const headers = { 'x-csrf-token': csrfToken };
  const terms = await page.request.put(`${base}/api/truck-charges/terms`, { headers, data: { truckId: truck, ownerId: investor, weekStart: '2026-01-05', sharePercent: '88', version: 0 } });
  expect(terms.status(), await terms.text()).toBe(204);

  for (const isInvestor of [false, true]) {
    const resource = isInvestor ? 'investor-pay' : 'driver-pay';
    const target = isInvestor ? truck : driver;
    const name = isInvestor ? 'Override Investor OVERRIDE-1' : 'Override Driver';
    const read = async () => (await page.request.get(`${base}/api/${resource}?weekStart=${week}`)).json();
    const card = report => report.drivers.find(d => d.id === target);
    const current = await read();
    expect(card(current).loads[0].deliveryLocation).toBe('');
    expect(card(current).loads[0].fee).toBe(isInvestor ? '880.00' : '250.00');
    const stale = await page.request.post(`${base}/api/${resource}/finalize`, { headers, data: { weekStart: week, driverId: target, revision: 'stale', override: true } });
    expect(stale.status()).toBe(409);

    await page.goto(`${base}/accounting/${resource}?weekStart=${week}`);
    await page.getByRole('button', { name, exact: true }).click({ button: 'right' });
    await page.getByRole('menuitem', { name: isInvestor ? 'Finalize truck' : 'Finalize driver', exact: true }).click();
    let dialog = page.getByRole('dialog');
    const overrideName = /Override all finalization checks/;
    await expect(dialog.getByRole('checkbox', { name: overrideName })).not.toBeChecked();
    await expect(dialog.getByText(/Source details incomplete/)).toBeVisible();
    await dialog.getByRole('button', { name: 'Finalize settlement', exact: true }).click();
    await expect(dialog.getByText(/^Resolve /)).toBeVisible();
    expect(card(await read()).settlement?.finalized ?? false).toBe(false);
    await dialog.getByRole('checkbox', { name: overrideName }).check();
    await dialog.getByRole('button', { name: 'Override and finalize', exact: true }).click();
    await expect(dialog).toHaveCount(0);
    await expect(page.getByTitle('Finalized with override', { exact: true })).toBeVisible();
    const frozen = card(await read());
    expect(frozen.settlement.override).toBe(true);
    expect(frozen.loads[0].issues.length).toBeGreaterThan(0);
    expect(frozen.loads[0].fee).toBe(card(current).loads[0].fee);

    await page.getByRole('button', { name, exact: true }).click({ button: 'right' });
    await page.getByRole('menuitem', { name: isInvestor ? 'Reopen truck' : 'Reopen driver', exact: true }).click();
    dialog = page.getByRole('dialog');
    await expect(dialog.getByRole('checkbox', { name: overrideName })).toHaveCount(0);
    await dialog.getByLabel('Reason for reopening').fill('Review missing source location');
    await dialog.getByRole('button', { name: 'Reopen settlement', exact: true }).click();
    await expect(dialog).toHaveCount(0);

    // Exercise a missing fee and unmatched source as part of a whole-week action.
    if (!isInvestor) sql(`SET search_path TO ${schema},public; INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number) VALUES('${driver}','${week}',1,'UNMATCHED-OVERRIDE');`);
    await page.getByRole('button', { name: 'Reload', exact: true }).click();
    await page.getByRole('button', { name: 'Finalize week', exact: true }).click();
    dialog = page.getByRole('dialog');
    await expect(dialog.getByText(/Pay amount is missing/)).toBeVisible();
    await dialog.getByRole('checkbox', { name: overrideName }).check();
    await dialog.getByRole('button', { name: 'Override and finalize', exact: true }).click();
    await expect(dialog).toHaveCount(0);
    expect(card(await read()).settlement.override).toBe(true);
    await page.screenshot({ path: join(temp, `${resource}-override.png`), fullPage: true });
    await page.getByRole('button', { name: 'Reopen week', exact: true }).click();
    await page.getByLabel('Reason for reopening').fill('Review whole-week override');
    await page.getByRole('button', { name: 'Reopen settlement', exact: true }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
  }
  console.log('Payroll override E2E passed for Driver Pay and Investor Pay: ordinary rejection, stale revision protection, individual and whole-week override, missing fees, audit metadata, and reopening.');
}
