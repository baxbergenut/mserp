import { expect } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import { join } from 'node:path';

export async function runPayrollWorkflowsE2E({ page, base, sql, schema, temp }) {
  const week = '2026-09-28';
  const dispatcher = randomUUID(), driver = randomUUID(), investor = randomUUID(), truck = randomUUID(), spare = randomUUID();
  const run = statement => sql(`SET search_path TO ${schema},public; ${statement}`);
  run(`INSERT INTO dispatchers(id,full_name,normalized_name) VALUES('${dispatcher}','Workflow Dispatch','workflow dispatch');
    INSERT INTO investors(id,full_name) VALUES('${investor}','Workflow Owner');
    INSERT INTO trucks(id,unit_number,owner_id) VALUES('${truck}','WORKFLOW-1','${investor}');
    INSERT INTO trucks(unit_number,owner_id) SELECT 'WORKFLOW-PAGE-'||lpad(n::text,2,'0'),'${investor}' FROM generate_series(1,27) n;
    INSERT INTO trucks(id,unit_number) VALUES('${spare}','WORKFLOW-SPARE');
    INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,dispatcher_id) VALUES('${driver}','Workflow Operator','workflow operator','cpm',0.75,'${dispatcher}');
    UPDATE driver_dispatcher_assignments SET assigned_at='2026-09-28 04:00:00+00' WHERE driver_id='${driver}';
    INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES('${truck}','${driver}','2026-09-28 04:00:00+00');
    INSERT INTO truck_settlement_terms(truck_id,owner_id,share_percent,week_start) VALUES('${truck}','${investor}',90,'${week}');
    INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,pickup_time,truck_id,truck_unit,raw_payload)
    VALUES(190101,'WORKFLOW-LOAD','delivered',1200,1200,100,'2026-09-28 12:00:00+00','${truck}','WORKFLOW-1',
      '{"trip":{"mile":90,"empty_mile":10},"stops":[{"stop_type":"pickup","ordering":1,"location":{"city":"Columbus","state":"OH"}},{"stop_type":"delivery","ordering":2,"location":{"city":"Pittsburgh","state":"PA"}}]}');
    INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,original_rate,entered_original_rate,driver_rate,miles,entered_miles)
    VALUES('${driver}','${week}','WORKFLOW-LOAD',190101,1200,1000,900,100,90);
    WITH d AS (INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate)
      SELECT 'Workflow Page '||lpad(n::text,2,'0'),'workflow page '||n,'cpm',0.75 FROM generate_series(1,27) n RETURNING id,full_name)
    INSERT INTO gross_board_entries(driver_id,service_date,load_number,entered_original_rate,driver_rate,entered_miles)
    SELECT id,'${week}',full_name,100,100,10 FROM d;`);
  const session = await (await page.request.get(`${base}/api/auth/session`)).json();
  const headers = { 'X-CSRF-Token': session.csrfToken };
  const read = async path => { const response = await page.request.get(`${base}/api${path}`); expect(response.status()).toBe(200); return response.json(); };
  const post = async (path, data, status = 204) => { const response = await page.request.post(`${base}/api${path}`, { headers, data }); expect(response.status(), await response.text()).toBe(status); return response; };
  const entry = async () => (await read(`/gross-board?weekStart=${week}`)).entries.find(e => e.driverId === driver && e.date === week && e.slot === 0);
  const investorPage = await read(`/investor-pay?weekStart=${week}&page=2&pageSize=25&search=Workflow`);
  expect(investorPage.pagination).toMatchObject({ page: 2, total: 28, totalPages: 2 });
  expect(investorPage.drivers.length + (investorPage.setupRequired?.length ?? 0)).toBe(3);
  const investorSearch = await read(`/investor-pay?weekStart=${week}&page=1&pageSize=25&search=WORKFLOW-PAGE-27`);
  expect(investorSearch.pagination.total).toBe(1);
  expect(investorSearch.setupRequired[0].truckUnit).toBe('WORKFLOW-PAGE-27');

  // Paging and search run on the full report, with fleet-wide filter choices.
  await page.goto(`${base}/accounting/driver-pay?weekStart=${week}`);
  await expect(page.getByRole('table', { name: 'Weekly driver pay' })).toBeVisible();
  await page.getByPlaceholder('Driver, truck, or load…').fill('Workflow');
  await expect(page.getByRole('table', { name: 'Weekly driver pay' }).getByRole('link', { name: 'Workflow Operator', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(page.getByText('Page 2 of 2', { exact: true })).toBeVisible();
  await page.getByPlaceholder('Driver, truck, or load…').fill('Workflow Page 27');
  await expect(page.getByRole('table', { name: 'Weekly driver pay' }).getByRole('link', { name: 'Workflow Page 27', exact: true })).toBeVisible();
  await expect(page.getByText('Page 1 of 1', { exact: true })).toBeVisible();
  await page.getByPlaceholder('Driver, truck, or load…').fill('');
  await page.getByRole('combobox', { name: 'Dispatcher', exact: true }).selectOption(dispatcher);
  await expect(page.getByRole('table', { name: 'Weekly driver pay' }).getByRole('link', { name: 'Workflow Operator', exact: true })).toBeVisible();
  await expect(page.getByRole('table', { name: 'Weekly driver pay' }).getByRole('row')).toHaveCount(2);
  await page.getByRole('button', { name: 'Finalize week', exact: true }).click();
  await expect(page.getByRole('dialog').getByText('Workflow Page 01', { exact: true })).toHaveCount(1);
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel', exact: true }).click();

  await page.goto(`${base}/accounting/driver-pay?weekStart=${week}&driverId=${driver}`);
  const gross = page.getByRole('cell', { name: /^Original gross differs from DataTruck/ });
  await expect(gross).toHaveText('$1,000.00');
  await expect(gross).toHaveAttribute('title', 'DataTruck: $1,200.00');
  await expect(page.getByRole('cell', { name: /^Miles differs from DataTruck/ })).toHaveText('90.00');
  await gross.click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Accept system values', exact: true }).click();
  await expect(gross).toHaveCount(0);
  expect(await entry()).toMatchObject({ originalRate: '1200.00', miles: '100.00', driverRate: '900.00' });
  await page.getByLabel('Workflow Operator, adjustment 1, name', { exact: true }).fill('Workflow deduction');
  const driverSave = page.waitForRequest(request => request.method() === 'PUT' && new URL(request.url()).pathname === '/api/driver-pay', { timeout: 650 });
  await page.getByLabel('Workflow Operator, adjustment 1, amount', { exact: true }).fill('-10.01');
  await driverSave;
  const payableCard = page.locator('.ui-metric').filter({ hasText: 'Total payable' });
  await expect(payableCard).toContainText('$64.99');
  await expect(page.getByRole('status').filter({ hasText: 'All changes saved' })).toBeVisible();
  await expect(payableCard).toContainText('$64.99');

  // Investor statements use the same source driver/slot identity and correction.
  run(`UPDATE gross_board_entries SET entered_original_rate=1000,entered_miles=90,version=version+1 WHERE driver_id='${driver}' AND service_date='${week}';`);
  await page.goto(`${base}/accounting/investor-pay?weekStart=${week}&truckId=${truck}`);
  await expect(gross).toHaveText('$1,000.00');
  await gross.focus(); await page.keyboard.press('Shift+F10');
  await page.getByRole('menuitem', { name: 'Accept system values', exact: true }).click();
  await expect(gross).toHaveCount(0);
  expect(await entry()).toMatchObject({ originalRate: '1200.00', miles: '100.00', driverRate: '900.00' });
  await page.getByLabel('Workflow Owner, adjustment 1, name', { exact: true }).fill('Workflow owner deduction');
  const investorSave = page.waitForRequest(request => request.method() === 'PUT' && new URL(request.url()).pathname === '/api/investor-pay', { timeout: 650 });
  await page.getByLabel('Workflow Owner, adjustment 1, amount', { exact: true }).fill('-0.01');
  await investorSave;
  await expect(page.getByRole('status').filter({ hasText: 'All changes saved' })).toBeVisible();

  // An upstream refresh between review and acceptance cannot silently overwrite.
  run(`UPDATE gross_board_entries SET entered_original_rate=1000,version=version+1 WHERE driver_id='${driver}' AND service_date='${week}';`);
  await page.reload(); await expect(gross).toBeVisible();
  run(`UPDATE loads SET total_pay=1300 WHERE id=190101;`);
  await gross.click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Accept system values', exact: true }).click();
  await expect(page.getByRole('alert').filter({ hasText: 'edited elsewhere' })).toBeVisible();
  expect(await entry()).toMatchObject({ originalRate: '1000.00', driverRate: '900.00' });
  await page.getByRole('button', { name: 'Reload', exact: true }).click();
  await expect(gross).toHaveAttribute('title', 'DataTruck: $1,300.00');

  for (const theme of ['default', 'solarized-light', 'solarized-dark', 'monokai', 'monokai-dimmed', 'dark-modern', 'default-light']) {
    expect((await page.request.put(`${base}/api/auth/theme`, { headers, data: { theme } })).status()).toBe(204);
    await page.reload(); await expect(gross).toBeVisible();
    await gross.click({ button: 'right' });
    await expect(page.getByRole('menuitem', { name: 'Accept system values', exact: true })).toBeVisible();
    const colors = await page.getByRole('menu').evaluate(e => ({ background: getComputedStyle(e).backgroundColor, color: getComputedStyle(e.querySelector('button')).color }));
    expect(colors.background).not.toBe('rgba(0, 0, 0, 0)'); expect(colors.color).not.toBe(colors.background);
    if (theme === 'default' || theme === 'default-light') await page.screenshot({ path: join(temp, `payroll-${theme}.png`), fullPage: true });
    await page.keyboard.press('Escape');
    await page.setViewportSize({ width: 390, height: 844 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.setViewportSize({ width: 1440, height: 1000 });
  }
  await gross.click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Accept system values', exact: true }).click();
  await expect(gross).toHaveCount(0);
  const source = (await read(`/driver-pay?weekStart=${week}`)).drivers.find(d => d.id === driver).loads[0];
  const accept = { driverId: driver, date: source.date, slot: source.slot, version: source.boardVersion, loadRecordId: source.loadRecordId, originalRate: source.systemOriginalRate, miles: source.systemMiles };
  await post('/driver-pay/accept-system', { ...accept, version: accept.version - 1 }, 409);
  const investorReport = await read(`/investor-pay?weekStart=${week}`);
  await post('/investor-pay/finalize', { weekStart: week, driverId: truck, revision: investorReport.revision }, 200);
  await post('/driver-pay/accept-system', accept, 400);
  const frozenInvestor = await read(`/investor-pay?weekStart=${week}`);
  await post('/investor-pay/reopen', { weekStart: week, driverId: truck, revision: frozenInvestor.revision, reason: 'Verify correction after reopening' }, 200);
  const report = await read(`/driver-pay?weekStart=${week}`);
  await post('/driver-pay/finalize', { weekStart: week, driverId: driver, revision: report.revision }, 200);
  await post('/driver-pay/accept-system', accept, 400);

  // Working start date drives roster, truck, dispatcher and escrow creation.
  await page.goto(`${base}/drivers`);
  await page.getByRole('button', { name: 'Add driver', exact: true }).click();
  await page.getByLabel('Full name', { exact: true }).fill('Workflow Started');
  await page.getByLabel('Driver started working', { exact: true }).fill('2026-09-30');
  await page.getByLabel('Rate per mile ($)', { exact: false }).fill('0.75');
  await page.getByLabel(/^Dispatcher/).selectOption(dispatcher);
  await page.getByLabel(/^Truck/).selectOption(spare);
  await expect(page.getByLabel('Assignment starts week (Monday)', { exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Create driver', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  const started = (await read('/drivers?search=Workflow%20Started&page=1&pageSize=25')).items[0];
  expect(started.hireDate.slice(0, 10)).toBe('2026-09-30');
  const assignments = await read(`/drivers/${started.id}/assignments`);
  expect(assignments.map(a => [a.kind, new Date(a.assignedAt).toISOString()])).toEqual(expect.arrayContaining([
    ['truck', '2026-09-28T04:00:00.000Z'], ['dispatcher', '2026-09-28T04:00:00.000Z'],
  ]));
  expect((await read(`/escrows?driverId=${started.id}&page=1&pageSize=25`)).items[0].startDate).toBe('2026-09-30');
  await page.getByPlaceholder('Search drivers…').fill('Workflow Started');
  const row = page.getByRole('row').filter({ hasText: 'Workflow Started' });
  await row.click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Change driver status', exact: true }).click();
  await page.getByRole('combobox', { name: 'Driver status', exact: true }).selectOption('home');
  await page.getByRole('button', { name: 'Save status', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(await read(`/drivers/${started.id}`)).toMatchObject({ status: 'home', active: true, truckId: spare, dispatcherId: dispatcher });
  await page.goto(`${base}/drivers/detail?id=${started.id}`);
  await page.getByRole('button', { name: 'Change status', exact: true }).click();
  await page.getByRole('combobox', { name: 'Driver status', exact: true }).selectOption('terminated');
  await page.getByRole('button', { name: 'Save status', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(await read(`/drivers/${started.id}`)).toMatchObject({ status: 'terminated', active: false, truckId: null, dispatcherId: null });
  await page.getByRole('combobox', { name: 'Search records', exact: true }).fill('Workflow Started');
  await expect(page.getByRole('region', { name: 'Global search' }).getByRole('link', { name: /Workflow Started/ })).toBeVisible();
  await expect(page.getByRole('combobox', { name: 'Search records', exact: true })).toHaveValue('Workflow Started');
  await page.keyboard.press('Escape');
  await expect(page.getByRole('region', { name: 'Global search' })).toHaveCount(0);
  const pageDriver = (await read('/drivers?search=Workflow%20Page%2027&page=1&pageSize=25')).items[0];
  await page.goto(`${base}/gross-board?${new URLSearchParams({ driverId: pageDriver.id, date: week, slot: '0', loadNumber: 'Workflow Page 27' })}`);
  await page.getByPlaceholder('Search drivers…').fill('Workflow Page 27');
  await expect(page.getByRole('link', { name: 'Workflow Page 27', exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Workflow Page 26', exact: true })).toHaveCount(0);
  const boardSave = page.waitForRequest(request => request.method() === 'PUT' && new URL(request.url()).pathname === '/api/gross-board', { timeout: 650 });
  await page.getByLabel(`Workflow Page 27, ${week}, miles`, { exact: true }).fill('11');
  await boardSave;
  await expect(page.getByRole('status').filter({ hasText: 'All changes saved' })).toBeVisible();
  expect((await read(`/gross-board?weekStart=${week}`)).entries.find(e => e.driverId === pageDriver.id && e.date === week).miles).toBe('11.00');
  console.log(`Payroll workflow E2E passed: paging/search, shared driver/investor source corrections, source and board conflicts, frozen protection, working start dates, status changes, inline global search, driver search, immediate autosave on all three pages, seven themes and narrow layouts. Screenshots: ${temp}`);
}
