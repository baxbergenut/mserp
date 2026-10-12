import { expect } from '@playwright/test';
import { join } from 'node:path';

export async function runDriverTeamsE2E({ page, base, sql, schema, temp }) {
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'America/New_York', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date());
  const day = new Date(`${today}T12:00:00Z`);
  day.setUTCDate(day.getUTCDate() - (day.getUTCDay() + 6) % 7);
  const week = day.toISOString().slice(0, 10);
  const main = 'e7800000-0000-4000-8000-000000000001';
  const team = 'e7800000-0000-4000-8000-000000000002';
  sql(`SET search_path TO ${schema},public;
    INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,is_owner_operator,hire_date) VALUES
    ('${main}','Team Main Example','team main example','gross_percentage',90,true,'${week}'),
    ('${team}','Team Member Example','team member example','cpm',0.7,false,'${week}');
    INSERT INTO driver_escrows(driver_id,driver_name,start_date,amount) SELECT id,full_name,'${week}',CASE WHEN id='${main}' THEN 100 ELSE 2500 END FROM drivers WHERE id IN('${main}','${team}');
    WITH f AS (
      INSERT INTO fuel_transactions(relay_environment,relay_transaction_id,driver_id,relay_driver_id,purchased_at,total_amount_paid,total_retail_price,total_amount_saved,is_direct_bill,currency_code,merchant_id,merchant_name,merchant_number,location_id,location_name,merchant_location_id,address,city,state,postal_code,latitude,longitude,timezone,raw_payload)
      VALUES('production','team-browser-fuel','${team}','team-relay','${week} 12:00+00',123.45,123.45,0,false,'USD','','','','','','','','','','',0,0,'America/New_York','{}') RETURNING id
    ) INSERT INTO fuel_transaction_items(fuel_transaction_id,line_number,item_kind,category,total_amount_paid) SELECT id,0,'fuel','diesel',123.45 FROM f;
  `);
  const read = async path => { const response = await page.request.get(`${base}/api${path}`); expect(response.status(), await response.text()).toBe(200); return response.json(); };
  const session = await read('/auth/session');
  const headers = { 'X-CSRF-Token': session.csrfToken };
  await page.goto(`${base}/drivers/detail?id=${main}`);
  await page.getByRole('button', { name: 'Set up team', exact: true }).click();
  await page.getByLabel('Team driver', { exact: true }).selectOption(team);
  await page.getByLabel('Team effective Monday').fill(week);
  // Date changes select the effective existing partner. Set the reviewed selection last.
  await page.getByLabel('Team driver', { exact: true }).selectOption(team);
  await page.getByRole('button', { name: 'Save team', exact: true }).click();
  await expect(page.getByText('Main driver · Team driver:', { exact: false })).toBeVisible();
  const configured = await read(`/drivers/${main}/team`);
  expect(configured.periods).toHaveLength(1);
  expect(configured.periods[0]).toMatchObject({ mainDriverId: main, teamDriverId: team, startWeek: week });
  await page.goto(`${base}/drivers/detail?id=${team}`);
  await expect(page.getByText('Team driver · Main driver:', { exact: false })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Set up team' })).toHaveCount(0);
  await expect(page.getByText('Main driver’s statement', { exact: true })).toBeVisible();
  const board = await read(`/gross-board?weekStart=${week}`);
  expect(board.drivers.some(d => d.id === main)).toBe(true);
  expect(board.drivers.some(d => d.id === team)).toBe(false);
  const report = await read(`/driver-pay?weekStart=${week}`);
  expect(report.drivers.filter(d => [main, team].includes(d.id))).toHaveLength(1);
  expect(report.drivers.find(d => d.id === main)).toMatchObject({ fuelTotal: '123.45', teamDrivers: [{ id: team, name: 'Team Member Example' }] });
  expect(report.drivers.find(d => d.id === main).edits.expenseDeductions.filter(d => d.source === 'escrow')).toHaveLength(1);
  expect(report.drivers.find(d => d.id === main).edits.expenseDeductions.find(d => d.source === 'escrow').total).toBe('100.00');
  await page.goto(`${base}/accounting/driver-pay?weekStart=${week}`);
  await page.getByPlaceholder('Driver, truck, or load…').fill('Team Member Example');
  await page.getByRole('button', { name: 'Team Main Example', exact: true }).click();
  await expect(page.getByLabel('Team Main Example, Fuel, amount', { exact: true })).toHaveValue('-123.45');
  await expect(page.getByRole('button', { name: 'Team Member Example', exact: true })).toHaveCount(0);
  await page.getByLabel('Team Main Example, Fuel, amount', { exact: true }).fill('-23.45');
  await page.getByLabel('Team Main Example, Fuel, amount', { exact: true }).press('Enter');
  await expect.poll(async () => (await read(`/driver-pay?weekStart=${week}`)).drivers.find(d => d.id === main).edits.fuelOverride).toBe('-23.45');
  await page.screenshot({ path: join(temp, 'driver-team-payroll.png'), fullPage: true });
  await page.setViewportSize({ width: 900, height: 800 });
  await expect(page.getByLabel('Team Main Example, Fuel, amount', { exact: true })).toBeVisible();
  await page.screenshot({ path: join(temp, 'driver-team-payroll-narrow.png'), fullPage: true });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(`${base}/accounting/driver-charges?driverId=${team}`);
  await expect(page.getByText('Recurring charges use', { exact: false })).toBeVisible();
  await expect(page.locator('table').getByRole('row').filter({ hasText: 'Team Member Example' })).toHaveCount(0);
  const stale = await page.request.put(`${base}/api/drivers/${main}/team`, { headers, data: { teamDriverId: '', weekStart: week, version: 0 } });
  expect(stale.status()).toBe(409);
  const separate = await page.request.put(`${base}/api/driver-pay`, { headers, data: { driverId: team, weekStart: week, version: 0, notes: '', comments: {}, adjustments: [], fuelOverride: null, tollOverride: null } });
  expect(separate.status()).toBe(400);
  console.log('Driver team E2E passed: profile setup, dated boards, one searchable statement, Relay costs, single escrow, autosave and narrow layout.');
}
