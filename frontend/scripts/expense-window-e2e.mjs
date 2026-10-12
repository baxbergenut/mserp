import { expect } from '@playwright/test';
import { join } from 'node:path';

export async function runExpenseWindowE2E({ page, base, sql, schema, temp }) {
  const driver = '21000000-0000-0000-0000-000000000001';
  const investor = '21000000-0000-0000-0000-000000000002';
  const truck = '21000000-0000-0000-0000-000000000003';
  sql(`SET search_path TO ${schema},public;
    INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate)
      VALUES('${driver}','Window Driver','window driver','cpm',0.75);
    UPDATE driver_dispatcher_assignments SET assigned_at='2026-09-21'::timestamp AT TIME ZONE 'America/New_York' WHERE driver_id='${driver}';
    DELETE FROM driver_escrows WHERE driver_id='${driver}';
    INSERT INTO investors(id,full_name) VALUES('${investor}','Window Investor');
    INSERT INTO trucks(id,unit_number,owner_id) VALUES('${truck}','WINDOW-TRUCK','${investor}');
    INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent)
      VALUES('${truck}','${investor}','2026-09-21',88);
  `);
  const session = await (await page.request.get(`${base}/api/auth/session`)).json();
  const headers = { 'x-csrf-token': session.csrfToken };
  const settings = await (await page.request.get(`${base}/api/expense-settings`)).json();
  const category = settings.find(row => row.kind === 'category' && row.name === 'Penalties');
  for (const route of ['driver', 'investor']) {
    const name = route === 'driver' ? 'Window Driver' : 'Window Investor';
    const statement = route === 'driver' ? driver : truck;
    const button = route === 'driver' ? name : `${name} WINDOW-TRUCK`;
    const ids = [];
    for (const [day, date] of [['Monday', '2026-10-05'], ['Tuesday', '2026-10-06'], ['Wednesday', '2026-10-07'], ['Thursday', '2026-10-08']]) {
      const response = await page.request.post(`${base}/api/expenses`, { headers, data: {
        company: 'MS Express', categoryId: category.id, expenseType: `${route} ${day}`,
        expenseDate: date, amount: '100.25', driverId: driver,
        ...(route === 'investor' ? { truckId: truck, ownerId: investor, coveredBy: 'Truck Owner' } : { coveredBy: 'Driver' }),
      } });
      expect(response.ok(), await response.text()).toBe(true);
      ids.push((await response.json()).id);
    }
    const read = async week => {
      const response = await page.request.get(`${base}/api/${route}-pay?weekStart=${week}`);
      expect(response.ok(), await response.text()).toBe(true);
      return (await response.json()).drivers.find(row => row.id === statement);
    };
    const open = async week => {
      await page.goto(`${base}/accounting/${route}-pay?weekStart=${week}`);
      const summary = page.getByRole('button', { name: button, exact: true });
      await expect(summary).toBeVisible();
      if (await summary.getAttribute('aria-expanded') !== 'true') await summary.click();
    };
    const input = day => page.getByLabel(`${name}, expense ${route} ${day}, deduction`, { exact: true });
    await open('2026-09-28');
    for (const day of ['Monday', 'Tuesday', 'Wednesday']) await expect(input(day)).toHaveValue('-100.25');
    await expect(input('Thursday')).toHaveCount(0);
    expect((await read('2026-09-28')).edits.expenseDeductions.every(row => !row.saved)).toBe(true);
    // Editing one row also collects the other displayed suggestions through autosave.
    await input('Wednesday').fill('30.10');
    await input('Wednesday').press('Tab');
    await expect.poll(async () => (await read('2026-09-28')).edits.expenseDeductions.find(row => row.expenseId === ids[2])?.amount, { timeout: 15000 }).toBe('30.10');
    await page.reload();
    await expect(input('Wednesday')).toHaveValue('-30.10');
    await open('2026-10-05');
    await expect(input('Monday')).toHaveCount(0);
    await expect(input('Tuesday')).toHaveCount(0);
    await expect(input('Wednesday')).toHaveValue('-70.15');
    await expect(input('Thursday')).toHaveValue('-100.25');
    await page.screenshot({ path: join(temp, `${route}-expense-window.png`), fullPage: true });
    const report = await (await page.request.get(`${base}/api/expenses?page=1&pageSize=100`)).json();
    expect(report.items.find(row => row.id === ids[0]).remainingAmount).toBe('0.00');
    expect(report.items.find(row => row.id === ids[2]).remainingAmount).toBe('70.15');
  }
  console.log('Expense window E2E passed: both payroll UIs include Monday–Wednesday, exclude Thursday, autosave partial payments, and prevent repeat collections.');
}
