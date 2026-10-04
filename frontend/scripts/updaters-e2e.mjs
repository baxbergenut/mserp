import { expect } from '@playwright/test';

export async function runUpdatersE2E({ page, base, sql, schema, temp }) {
  await page.goto(`${base}/dispatchers`);
  await expect(page.getByRole('heading', { name: 'Dispatchers and updaters', exact: true })).toBeVisible();
  for (const [name, shift, extension] of [['E2e Main Updater', 'main', '106'], ['E2e Night Updater', 'after_hours', '']]) {
    await page.getByRole('button', { name: 'Add updater', exact: true }).click();
    await page.getByLabel('Full name', { exact: true }).fill(name);
    await page.getByLabel('Shift', { exact: true }).selectOption(shift);
    await page.getByLabel('Phone extension', { exact: true }).fill(extension);
    await page.getByRole('button', { name: 'Create updater', exact: true }).click();
    await expect(page.getByRole('cell', { name, exact: true })).toBeVisible();
  }
  for (const name of ['E2e Updater Dispatcher One', 'E2e Updater Dispatcher Two']) {
    await page.getByRole('button', { name: 'Add dispatcher', exact: true }).click();
    await page.getByLabel('Full name', { exact: true }).fill(name);
    await page.getByLabel('Phone extension', { exact: true }).fill('111');
    await page.getByLabel('Main updater', { exact: true }).selectOption({ label: 'E2e Main Updater (106)' });
    await page.getByLabel('After hours updater', { exact: true }).selectOption({ label: 'E2e Night Updater' });
    await expect(page.getByLabel('Main updater', { exact: true }).locator('option')).toHaveCount(2);
    await page.getByRole('button', { name: 'Create dispatcher', exact: true }).click();
    await expect(page.getByRole('cell', { name: `${name} (111)`, exact: true })).toBeVisible();
  }
  await page.reload();
  const row = page.getByRole('row').filter({ has: page.getByRole('cell', { name: 'E2e Main Updater', exact: true }) });
  await expect(row).toContainText('E2e Updater Dispatcher One, E2e Updater Dispatcher Two');
  await row.getByRole('button', { name: 'Edit', exact: true }).click();
  await expect(page.getByLabel('Shift', { exact: true })).toBeDisabled();
  await page.getByLabel('Phone extension', { exact: true }).fill('107');
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(row).toContainText('107');
  sql(`SET search_path TO ${schema},public; INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,dispatcher_id) SELECT 'E2e Updater Board Driver','e2e updater board driver','cpm',0,id FROM dispatchers WHERE full_name='E2e Updater Dispatcher One';`);
  await page.goto(`${base}/driver-board`);
  const heading = page.getByRole('row', { name: 'E2e Updater Dispatcher One totals', exact: true });
  await expect(heading).toContainText('E2e Updater Dispatcher One (111)');
  await expect(heading).toContainText('Main: E2e Main Updater (107)');
  await expect(heading).toContainText('After hours: E2e Night Updater');
  await page.screenshot({ path: `${temp}/updaters-status-board.png`, fullPage: true });
  await page.goto(`${base}/dispatchers`);
  await expect(page.getByRole("cell", { name: "E2e Main Updater", exact: true })).toBeVisible();
  await page.screenshot({ path: `${temp}/dispatchers-updaters.png`, fullPage: true });
}
