import { expect } from '@playwright/test';

export async function runAssignmentWeekE2E({ page, base, sql, schema }) {
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'America/New_York', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date());
  const date = new Date(`${today}T12:00:00Z`);
  date.setUTCDate(date.getUTCDate() - (date.getUTCDay() + 6) % 7);
  const week = date.toISOString().slice(0, 10);
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
  await page.getByLabel(/^Truck/).selectOption(truck2);
  const start = page.getByLabel('Assignment starts week (Monday)', { exact: true });
  await expect(start).toHaveValue('');
  await expect(start).toHaveAttribute('required', '');
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(start).toBeVisible(); // Native validation prevents a date-free save.
  await page.getByRole('button', { name: 'This week', exact: true }).click();
  await expect(start).toHaveValue(week);
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(start).toHaveCount(0);
  await expect(row).toContainText('Week Mark');
  const getDriver = async selected => {
    const response = await page.request.get(`${base}/api/gross-board?weekStart=${selected}`);
    expect(response.status()).toBe(200);
    return (await response.json()).drivers.find(item => item.id === driver);
  };
  expect(await getDriver(previous)).toMatchObject({ dispatcherName: 'Week Cameron', truckUnit: 'WEEK-ONE' });
  expect(await getDriver(week)).toMatchObject({ dispatcherName: 'Week Mark', truckUnit: 'WEEK-TWO' });
  await page.goto(`${base}/driver-board`);
  await expect(page.getByRole('heading', { name: 'Status Board', exact: true })).toBeVisible();
  const nav = page.locator('aside a');
  await expect(nav.nth(0)).toHaveAttribute('href', '/driver-board');
  await expect(nav.nth(1)).toHaveAttribute('href', '/gross-board');
  await expect(nav.nth(2)).toHaveAttribute('href', '/loads');
}
