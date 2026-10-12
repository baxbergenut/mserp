import { expect } from '@playwright/test';
import { join } from 'node:path';

export async function runDriverTeamsE2E({ page, base, sql, schema, temp }) {
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'America/New_York', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date());
  const day = new Date(`${today}T12:00:00Z`);
  day.setUTCDate(day.getUTCDate() - (day.getUTCDay() + 6) % 7);
  const week = day.toISOString().slice(0, 10);
  const main = 'e7800000-0000-4000-8000-000000000001';
  const team = 'e7800000-0000-4000-8000-000000000002';
  const truck = 'e7800000-0000-4000-8000-000000000003';
  const dispatcher = 'e7800000-0000-4000-8000-000000000004';
  sql(`SET search_path TO ${schema},public;
    INSERT INTO dispatchers(id,full_name,normalized_name) VALUES('${dispatcher}','Team Dispatcher','team dispatcher');
    INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,is_owner_operator,hire_date) VALUES
    ('${main}','Team Main Example','team main example','gross_percentage',90,true,'${week}'),
    ('${team}','Team Member Example','team member example','cpm',0.7,false,'${week}');
    SET mserp.assignment_week='${week}'; UPDATE drivers SET dispatcher_id='${dispatcher}' WHERE id='${main}';
    INSERT INTO trucks(id,unit_number) VALUES('${truck}','TEAM-BROWSER');
    INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES('${truck}','${main}','${week}'::timestamp AT TIME ZONE 'America/New_York');
    INSERT INTO driver_escrows(driver_id,driver_name,start_date,amount) SELECT id,full_name,'${week}',CASE WHEN id='${main}' THEN 100 ELSE 2500 END FROM drivers WHERE id IN('${main}','${team}');
    WITH f AS (
      INSERT INTO fuel_transactions(relay_environment,relay_transaction_id,driver_id,relay_driver_id,purchased_at,total_amount_paid,total_retail_price,total_amount_saved,is_direct_bill,currency_code,merchant_id,merchant_name,merchant_number,location_id,location_name,merchant_location_id,address,city,state,postal_code,latitude,longitude,timezone,raw_payload)
      VALUES('production','team-browser-fuel','${team}','team-relay','${week} 12:00+00',123.45,123.45,0,false,'USD','','','','','','','','','','',0,0,'America/New_York','{}') RETURNING id
    ) INSERT INTO fuel_transaction_items(fuel_transaction_id,line_number,item_kind,category,total_amount_paid) SELECT id,0,'fuel','diesel',123.45 FROM f;
  `);
  const read = async path => { const response = await page.request.get(`${base}/api${path}`); expect(response.status(), await response.text()).toBe(200); return response.json(); };
  const session = await read('/auth/session');
  const headers = { 'X-CSRF-Token': session.csrfToken };
  expect((await read(`/drivers/team-trucks?weekStart=${week}`)).find(row=>row.truckId===truck)).toMatchObject({mainDriverId:main,dispatcherId:dispatcher});
  await page.goto(`${base}/drivers/detail?id=${main}`);
  await expect(page.getByRole('heading', { name: 'Driver team', exact: true })).toHaveCount(0);
  await page.goto(`${base}/drivers/detail?id=${team}`);
  await page.getByRole('button', { name: 'Edit driver', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('switch', { name: 'Team driver', exact: true }).click();
  await expect(dialog.getByLabel('Pay basis')).toHaveCount(0);
  await expect(dialog.getByLabel('Dispatcher', { exact: true })).toHaveCount(0);
  await dialog.getByLabel('Assignment starts week (Monday)', { exact: true }).fill(week);
  await expect(dialog.getByLabel('Truck', { exact: true }).locator(`option[value="${truck}"]`)).toHaveCount(1);
  await dialog.getByLabel('Truck', { exact: true }).selectOption(truck);
  await dialog.getByRole('button', { name: 'Save driver', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect(await read(`/drivers/${team}`)).toMatchObject({ teamRole: 'team', dispatcherId: dispatcher, truckId: null, teamTruckId: truck, payRate: 0 });
  expect(await read(`/trucks/${truck}`)).toMatchObject({ driverId: main });
  await page.goto(`${base}/drivers/detail?id=${main}`);
  await expect(page.getByRole('heading', { name: 'Driver team', exact: true })).toBeVisible();
  const configured = await read(`/drivers/${main}/team`);
  expect(configured.periods).toHaveLength(1);
  expect(configured.periods[0]).toMatchObject({ mainDriverId: main, teamDriverId: team, startWeek: week });
  await page.goto(`${base}/drivers/detail?id=${team}`);
  await expect(page.getByRole('heading', { name: 'Driver team', exact: true })).toHaveCount(0);
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
  await runTeamOnboarding({ page, base, sql, schema, temp, week, read, dispatcher });
  console.log('Driver team E2E passed: secondary edit, add/setup toggle, inherited truck/dispatcher, hidden compensation, main-only card, dated payroll and autosave.');
}

async function runTeamOnboarding({ page, base, sql, schema, temp, week, read, dispatcher }) {
  const main2='e7800000-0000-4000-8000-000000000005', truck2='e7800000-0000-4000-8000-000000000006';
  const main3='e7800000-0000-4000-8000-000000000007', truck3='e7800000-0000-4000-8000-000000000008';
  const intake='e7800000-0000-4000-8000-000000000009';
  sql(`SET search_path TO ${schema},public; SET mserp.assignment_week='${week}';
    INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date,dispatcher_id) VALUES
    ('${main2}','Add Team Primary','add team primary','cpm',0.75,'${week}','${dispatcher}'),
    ('${main3}','Intake Team Primary','intake team primary','cpm',0.75,'${week}','${dispatcher}');
    INSERT INTO trucks(id,unit_number) VALUES('${truck2}','TEAM-ADD'),('${truck3}','TEAM-INTAKE');
    INSERT INTO truck_driver_assignments(driver_id,truck_id,assigned_at) VALUES('${main2}','${truck2}','${week}'::timestamp AT TIME ZONE 'America/New_York'),('${main3}','${truck3}','${week}'::timestamp AT TIME ZONE 'America/New_York');
    INSERT INTO fleetscope_driver_intake(id,company_id,fleetscope_driver_id,normalized_name,driver_data,occurred_at)
    VALUES('${intake}',gen_random_uuid(),gen_random_uuid(),'intake team newcomer','{"fullName":"Intake Team Newcomer","hireDate":"${week}","driverType":"company_driver"}',now());
  `);
  await page.goto(`${base}/drivers`);
  await page.getByRole('button',{name:'Add driver',exact:true}).click();
  const dialog=page.getByRole('dialog');
  await dialog.getByLabel('Full name',{exact:true}).fill('Added Team Newcomer');
  await dialog.getByRole('button',{name:'Continue',exact:true}).click();
  await expect(dialog.getByRole('switch',{name:'Team driver'})).not.toBeChecked();
  await expect(dialog.getByLabel('Pay basis')).toBeVisible();
  await dialog.getByRole('switch',{name:'Team driver'}).click();
  await expect(dialog.getByLabel('Pay basis')).toHaveCount(0);
  await expect(dialog.getByLabel('Escrow amount ($)',{exact:false})).toHaveCount(0);
  await expect(dialog.getByLabel('Dispatcher',{exact:true})).toHaveCount(0);
  await expect(dialog.getByLabel('Truck',{exact:true}).locator(`option[value="${truck2}"]`)).toHaveCount(1);
  await dialog.getByLabel('Truck',{exact:true}).selectOption(truck2);
  await page.screenshot({path:join(temp,'team-driver-add-modal.png'),fullPage:true});
  await dialog.getByRole('button',{name:'Continue',exact:true}).click();
  await dialog.getByRole('button',{name:'Continue',exact:true}).click();
  await dialog.getByLabel('Driver started working',{exact:true}).fill(week);
  await dialog.getByRole('button',{name:'Create driver',exact:true}).click();
  await dialog.getByRole('button',{name:'Done',exact:true}).click();
  await expect(dialog).toHaveCount(0);
  const added=(await read('/drivers')).find(d=>d.fullName==='Added Team Newcomer');
  expect(added).toMatchObject({teamRole:'team',teamPartnerId:main2,teamTruckId:truck2,dispatcherId:dispatcher,payRate:0});
  expect(await read(`/trucks/${truck2}`)).toMatchObject({driverId:main2});
  await page.getByPlaceholder('Search drivers…').fill('Intake Team Newcomer');
  await page.getByRole('button',{name:'Intake Team Newcomer',exact:true}).click();
  await dialog.getByRole('button',{name:'Continue',exact:true}).click();
  await expect(dialog.getByLabel('Pay basis')).toBeVisible();
  await dialog.getByRole('button',{name:'Continue',exact:true}).click();
  await expect(dialog.getByText('Enter a pay rate greater than zero.',{exact:true})).toBeVisible();
  await dialog.getByRole('switch',{name:'Team driver'}).click();
  await expect(dialog.getByLabel('Truck',{exact:true}).locator(`option[value="${truck3}"]`)).toHaveCount(1);
  await dialog.getByLabel('Truck',{exact:true}).selectOption(truck3);
  await expect(dialog.getByLabel('Pay basis')).toHaveCount(0);
  await page.setViewportSize({width:900,height:800});
  await page.screenshot({path:join(temp,'team-driver-setup-modal-narrow.png'),fullPage:true});
  await dialog.getByRole('button',{name:'Continue',exact:true}).click();
  await dialog.getByRole('button',{name:'Continue',exact:true}).click();
  await dialog.getByRole('button',{name:'Complete setup',exact:true}).click();
  await dialog.getByRole('button',{name:'Done',exact:true}).click();
  await expect(dialog).toHaveCount(0);
  const enrolled=(await read('/drivers')).find(d=>d.fullName==='Intake Team Newcomer');
  expect(enrolled).toMatchObject({teamRole:'team',teamPartnerId:main3,teamTruckId:truck3,dispatcherId:dispatcher,payRate:0});
  expect(await read(`/trucks/${truck3}`)).toMatchObject({driverId:main3});
  await page.setViewportSize({width:1440,height:1000});
}
