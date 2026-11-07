import { expect } from '@playwright/test';

export async function runDriverPayEscrowE2E({ page, base, sql, schema }) {
  const driver = '20100000-0000-0000-0000-000000000001';
  const escrow = '20100000-0000-0000-0000-000000000002';
  const expense = '20100000-0000-0000-0000-000000000003';
  const week = '2026-10-19';
  const driverName = 'Former Payroll Driver';
  sql(`SET search_path TO ${schema},public;
    INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date,active,termination_date)
    VALUES('${driver}','${driverName}','former payroll driver','cpm',0.75,'2026-09-01',false,'2026-10-04');
    INSERT INTO driver_escrows(id,driver_id,driver_name,start_date,amount,opening_paid)
    VALUES('${escrow}','${driver}','${driverName}','2026-09-01',2000,2000);
    INSERT INTO expenses(id,category_id,category,expense_type,expense_date,amount,driver_id,charge_driver_id,covered_by,payment_type)
    SELECT '${expense}',id,name,'Settlement charge','2026-10-21',500,'${driver}','${driver}','Driver',''
    FROM expense_settings WHERE kind='category' AND name='Other';`);
  const report = async () => {
    const response = await page.request.get(`${base}/api/driver-pay?weekStart=${week}&statementId=${driver}&page=1&pageSize=25&summary=1`);
    expect(response.ok(), await response.text()).toBeTruthy();
    return response.json();
  };
  const inspect = async (query) => {
    const response = await page.request.get(`${base}/api/escrows?driverId=${driver}&includeInactive=true`);
    expect(response.ok(), await response.text()).toBeTruthy();
    return query((await response.json()).items[0]);
  };
  await page.goto(`${base}/accounting/driver-pay?weekStart=${week}`);
  await expect(page.getByRole('heading', { name: 'Driver pay', exact: true })).toBeVisible();
  const add = page.getByRole('button', { name: 'Add driver', exact: true });
  await add.click({ timeout: 30000 });
  await page.getByLabel('Search drivers').fill(driverName);
  await page.getByRole('radio', { name: new RegExp(driverName) }).check();
  await page.getByRole('button', { name: 'Add to payroll' }).click();
  const manage = page.getByRole('button', { name: 'Manage escrow', exact: true });
  await manage.click({ timeout: 30000 });
  await expect(page.getByLabel('Escrow collection')).toHaveValue('stop');
  await page.getByLabel('Escrow for Settlement charge').fill('300');
  await page.getByLabel('Return to driver').fill('1800');
  const save = page.getByRole('button', { name: 'Save escrow draft' });
  await expect(save).toBeDisabled();
  await page.getByLabel('Return to driver').fill('1700');
  await save.click();
  await expect(page.getByRole('dialog', { name: 'Manage escrow' })).toHaveCount(0);
  await expect(page.getByLabel(`${driverName}, expense Settlement charge, deduction`)).toHaveValue('-200.00');
  expect(await inspect(account => account.heldAmount)).toBe('2000.00');
  const row = page.getByRole('row').filter({ has: page.getByRole('link', { name: driverName, exact: true }) }).first();
  await expect(row).toContainText('$1,500.00');
  await page.getByRole('button', { name: `Finalize ${driverName}`, exact: true }).click();
  await expect(row.getByLabel('Finalized', { exact: true })).toBeVisible({ timeout: 30000 });
  expect(await inspect(account => [account.heldAmount, account.collectionStopped, account.releases[0].payrollReturn])).toEqual(['0.00', true, '1700.00']);
  const exportResponse = await page.request.get(`${base}/api/driver-pay/export?weekStart=${week}&statementId=${driver}`);
  expect(exportResponse.ok(), await exportResponse.text()).toBeTruthy();
  expect(exportResponse.headers()['content-type']).toContain('spreadsheetml');
  await row.click({ button: 'right' });
  await page.getByRole('menuitem', { name: /Reopen/i }).click();
  await page.getByRole('button', { name: 'Reopen settlement', exact: true }).click();
  await expect(page.getByRole('button', { name: `Finalize ${driverName}`, exact: true })).toBeVisible({ timeout: 30000 });
  expect(await inspect(account => [account.heldAmount, account.releases[0].cancelled])).toEqual(['2000.00', true]);
  await page.getByRole('button', { name: `Finalize ${driverName}`, exact: true }).click();
  await expect(row.getByLabel('Finalized', { exact: true })).toBeVisible({ timeout: 30000 });
  expect((await report()).drivers[0].summary?.payable).toBe('1500.00');
  expect(await inspect(account => account.heldAmount)).toBe('0.00');
  console.log('Driver Pay escrow browser flow passed');
}
