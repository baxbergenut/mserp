import { expect } from '@playwright/test';

export async function runAssignmentWeekE2E({ page, base, sql, schema }) {
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'America/New_York', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date());
  const date = new Date(`${today}T12:00:00Z`);
  date.setUTCDate(date.getUTCDate() - (date.getUTCDay() + 6) % 7);
  const week = date.toISOString().slice(0, 10);
  date.setUTCDate(date.getUTCDate() + 7);
  const next = date.toISOString().slice(0, 10);
  date.setUTCDate(date.getUTCDate() - 7);
  date.setUTCDate(date.getUTCDate() - 7);
  const previous = date.toISOString().slice(0, 10);
  const driver = 'e4700000-0000-0000-0000-000000000001';
  const cameron = 'e4700000-0000-0000-0000-000000000002';
  const mark = 'e4700000-0000-0000-0000-000000000003';
  const truck1 = 'e4700000-0000-0000-0000-000000000004';
  const truck2 = 'e4700000-0000-0000-0000-000000000005';
  sql(`SET search_path TO ${schema},public;
    INSERT INTO dispatchers(id,full_name,normalized_name) VALUES('${cameron}','Week Cameron','week cameron'),('${mark}','Week Mark','week mark');
    INSERT INTO trucks(id,unit_number) VALUES('${truck1}','WEEK-ONE'),('${truck2}','WEEK-TWO');
    INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,dispatcher_id) VALUES('${driver}','Week Didi','week didi','cpm',0.75,'${cameron}');
    UPDATE driver_dispatcher_assignments SET assigned_at='${previous}'::timestamp AT TIME ZONE 'America/New_York' WHERE driver_id='${driver}';
    INSERT INTO truck_driver_assignments(driver_id,truck_id,assigned_at) VALUES('${driver}','${truck1}','${previous}'::timestamp AT TIME ZONE 'America/New_York');
  `);
  await page.goto(`${base}/drivers`);
  await page.getByPlaceholder('Search drivers…').fill('Week Didi');
  const row = page.getByRole('row').filter({ hasText: 'Week Didi' });
  await row.getByRole('button', { name: 'Edit', exact: true }).click();
  await page.getByLabel(/^Dispatcher/).selectOption(mark);
  await page.getByLabel('Truck', { exact: true }).selectOption(truck2);
  const start = page.getByLabel('Assignment effective date', { exact: true });
  await expect(start).toHaveValue('');
  await expect(start).toHaveAttribute('required', '');
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(start).toBeVisible(); // Native validation prevents a date-free save.
  await page.getByRole('button', { name: 'Today', exact: true }).click();
  await expect(start).toHaveValue(today);
  await expect(start).toHaveAttribute('max', '2100-12-31');
  await start.fill(next);
  expect(await start.evaluate(input => input.checkValidity())).toBe(true);
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(start).toHaveCount(0);
  await expect(row).toContainText('Week Mark');
  const getDriver = async selected => {
    const response = await page.request.get(`${base}/api/gross-board?weekStart=${selected}`);
    expect(response.status()).toBe(200);
    return (await response.json()).drivers.find(item => item.id === driver);
  };
  expect(await getDriver(previous)).toMatchObject({ dispatcherName: 'Week Cameron', truckUnit: 'WEEK-ONE' });
  expect(await getDriver(week)).toMatchObject({ dispatcherName: 'Week Cameron', truckUnit: 'WEEK-ONE' });
  expect(await getDriver(next)).toMatchObject({ dispatcherName: 'Week Mark', truckUnit: 'WEEK-TWO' });
  // A current finalized settlement blocks an earlier correction, with an
  // actionable error. Reopening lets the same form save succeed.
  sql(`SET search_path TO ${schema},public;
    INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES('${driver}','${week}','{}');`);
  await row.getByRole('button', { name: 'Edit', exact: true }).click();
  await page.getByLabel(/^Dispatcher/).selectOption(cameron);
  await page.getByLabel('Truck', { exact: true }).selectOption(truck1);
  await start.fill(week);
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByRole('dialog').getByText('Reopen finalized payroll for this driver from the selected assignment date onward before changing assignments', { exact: true })).toBeVisible();
  expect(await getDriver(next)).toMatchObject({ dispatcherName: 'Week Mark', truckUnit: 'WEEK-TWO' });
  sql(`SET search_path TO ${schema},public; UPDATE payroll_settlements SET finalized=false WHERE driver_id='${driver}';`);
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(start).toHaveCount(0);
  expect(await getDriver(week)).toMatchObject({ dispatcherName: 'Week Cameron', truckUnit: 'WEEK-ONE' });
  expect(await getDriver(next)).toMatchObject({ dispatcherName: 'Week Cameron', truckUnit: 'WEEK-ONE' });

  // Editing only Hire date must move the original roster/crew and uncollected
  // setup escrow, without requiring a second assignment edit in another form.
  const hireDriver = 'e4700000-0000-0000-0000-000000000006';
  const hireTruck = 'e4700000-0000-0000-0000-000000000007';
  sql(`BEGIN; SET search_path TO ${schema},public;
    SELECT set_config('mserp.assignment_week','${previous}',true);
    INSERT INTO trucks(id,unit_number) VALUES('${hireTruck}','HIRE-DATE');
    INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date,dispatcher_id)
      VALUES('${hireDriver}','Hire Date Driver','hire date driver','cpm',0.75,'${previous}','${mark}');
    INSERT INTO truck_driver_assignments(driver_id,truck_id,assigned_at)
      VALUES('${hireDriver}','${hireTruck}','${previous}'::timestamp AT TIME ZONE 'America/New_York');
    INSERT INTO driver_escrows(driver_id,driver_name,start_date,amount)
      VALUES('${hireDriver}','Hire Date Driver','${previous}',2500);
    COMMIT;`);
  await page.goto(`${base}/drivers`);
  await page.getByPlaceholder('Search drivers…').fill('Hire Date Driver');
  const hireRow = page.getByRole('row').filter({ hasText: 'Hire Date Driver' });
  await hireRow.getByRole('button', { name: 'Edit', exact: true }).click();
  await page.getByLabel('Hire date', { exact: true }).fill(week);
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  for (const selected of [previous, week]) {
    for (const resource of ['gross-board', 'driver-pay']) {
      const response = await page.request.get(`${base}/api/${resource}?weekStart=${selected}`);
      expect(response.status()).toBe(200);
      const found = (await response.json()).drivers.find(item => item.id === hireDriver);
      if (selected === previous) expect(found).toBeUndefined();
      else {
        expect(found).toMatchObject({ truckUnit: 'HIRE-DATE', dispatcherName: 'Week Mark' });
        if (resource === 'driver-pay') expect(found.edits.expenseDeductions).toContainEqual(expect.objectContaining({ source: 'escrow', expenseDate: week, total: '2500.00' }));
      }
    }
  }
  // A midweek dispatcher/truck change keeps exactly one editable copy of each
  // day, splits dispatcher totals, and leaves a single complete pay statement.
  const wednesdayDate = new Date(`${week}T12:00:00Z`);
  wednesdayDate.setUTCDate(wednesdayDate.getUTCDate() + 2);
  const wednesday = wednesdayDate.toISOString().slice(0, 10);
  sql(`SET search_path TO ${schema},public;
    INSERT INTO gross_board_entries(driver_id,service_date,load_number,original_rate,driver_rate,miles)
    VALUES('${driver}','${week}','DAILY-OLD',1000,900,100),('${driver}','${wednesday}','DAILY-NEW',2000,1800,200);`);
  await page.goto(`${base}/drivers`);
  await page.getByPlaceholder('Search drivers…').fill('Week Didi');
  await row.getByRole('button', { name: 'Edit', exact: true }).click();
  await page.getByLabel(/^Dispatcher/).selectOption(mark);
  await page.getByLabel('Truck', { exact: true }).selectOption(truck2);
  await start.fill(wednesday);
  expect(await start.evaluate(input => input.checkValidity())).toBe(true);
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(start).toHaveCount(0);
  const daily = await getDriver(week);
  expect(daily.assignments.slice(0, 2).every(a => a.dispatcherId === cameron && a.truckId === truck1)).toBe(true);
  expect(daily.assignments.slice(2).every(a => a.dispatcherId === mark && a.truckId === truck2)).toBe(true);
  await page.goto(`${base}/gross-board`);
  await page.getByPlaceholder('Search drivers…').fill('Week Didi');
  const dailyRows = page.locator('tbody > tr').filter({ has: page.locator(`th[data-gross-driver="${driver}"]`) });
  await expect(dailyRows).toHaveCount(2);
  await expect(dailyRows.nth(0).getByText('Changed dispatcher', { exact: true })).toHaveCount(5);
  await expect(dailyRows.nth(1).getByText('Changed dispatcher', { exact: true })).toHaveCount(2);
  await expect(dailyRows.nth(0).locator('[data-gross-field=originalTotal]')).toHaveText('$1,000.00');
  await expect(dailyRows.nth(1).locator('[data-gross-field=originalTotal]')).toHaveText('$2,000.00');
  await expect(page.locator('tfoot td').first()).toHaveText('$3,000.00');
  await expect(page.locator('tfoot td').last()).toHaveText('+$300.00');
  await page.getByRole('button', { name: 'Dispatcher filter', exact: true }).click();
  const choices = page.getByRole('group', { name: 'Dispatcher choices', exact: true });
  await choices.getByRole('button', { name: 'Clear all', exact: true }).click();
  await choices.getByRole('checkbox', { name: 'Week Cameron', exact: true }).check();
  await expect(dailyRows).toHaveCount(1);
  await expect(page.locator('tfoot td').first()).toHaveText('$1,000.00');
  await choices.getByRole('button', { name: 'Select all', exact: true }).click();
  await page.keyboard.press('Escape');
  const payResponse = await page.request.get(`${base}/api/driver-pay?weekStart=${week}&statementId=${driver}`);
  expect(payResponse.status()).toBe(200);
  const payDriver = (await payResponse.json()).drivers.find(d => d.id === driver);
  expect(payDriver.loads).toHaveLength(2);
  expect(payDriver.dispatcherName).toBe('Week Cameron / Week Mark');
  await page.goto(`${base}/accounting/driver-pay?weekStart=${week}&driverId=${driver}`);
  await expect(page.getByText('Week Cameron / Week Mark', { exact: true })).toBeVisible();
  await page.goto(`${base}/driver-board`);
  await expect(page.getByRole('heading', { name: 'Status Board', exact: true })).toBeVisible();
  // Real form saves must expose persistent attribution on driver and truck
  // histories, including a distinct end actor and elapsed assignment duration.
  const historyResponse = await page.request.get(`${base}/api/drivers/${driver}/assignments`);
  expect(historyResponse.status()).toBe(200);
  const history = await historyResponse.json();
  const currentTruck = history.find(entry => entry.kind === 'truck' && !entry.unassignedAt);
  expect(currentTruck).toMatchObject({ name: 'WEEK-TWO', actorName: 'investor-e2e' });
  expect(currentTruck.actorId).toBeTruthy();
  expect(Date.parse(currentTruck.recordedAt)).toBeGreaterThan(Date.parse(currentTruck.assignedAt));
  expect(history.some(entry => entry.unassignedAt && entry.endedByName === 'investor-e2e')).toBe(true);
  expect(history.find(entry => entry.kind === 'dispatcher' && !entry.unassignedAt).actorName).toBe('investor-e2e');
  await page.goto(`${base}/drivers/detail?id=${driver}`);
  await page.getByRole('tab', { name: 'Assignments', exact: true }).click();
  const assignmentHistory = page.getByRole('region', { name: 'Assignment history', exact: true });
  await expect(assignmentHistory.getByRole('columnheader')).toHaveText(['Truck', 'Started', 'Ended', 'Duration', 'Changed by']);
  const currentAssignment = assignmentHistory.getByRole('row').filter({ hasText: 'Current' });
  await expect(currentAssignment).toContainText('WEEK-TWO');
  await expect(currentAssignment).toContainText('investor-e2e');
  await expect(currentAssignment.getByRole('cell').nth(3)).toHaveText(/^\d+[dhm]/);
  await assignmentHistory.getByLabel('Assignment type').selectOption('dispatcher');
  await expect(assignmentHistory.getByRole('row').filter({ hasText: 'Current' })).toContainText('investor-e2e');
  await page.goto(`${base}/trucks/detail?id=${truck2}`);
  await page.getByRole('tab', { name: 'Assignments', exact: true }).click();
  const truckHistory = page.getByRole('region', { name: 'Assignment history', exact: true });
  await expect(truckHistory.getByRole('columnheader')).toHaveText(['Driver', 'Started', 'Ended', 'Duration', 'Changed by']);
  await expect(truckHistory.getByRole('row').filter({ hasText: 'Current' })).toContainText('investor-e2e');
  await page.screenshot({ path: `${process.env.TEMP || '/tmp'}/clustie-assignment-history.png`, fullPage: true });
}
