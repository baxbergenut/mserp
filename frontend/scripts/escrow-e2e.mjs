import { expect } from '@playwright/test';
import { join } from 'node:path';

export async function verifyEscrow(page, base, temp, driver) {
  const read = async () => (await (await page.request.get(`${base}/api/escrows?driverId=${driver.id}`)).json()).items[0];
  expect(await read()).toMatchObject({ amount: '2650.00', paidAmount: '0.00', remainingAmount: '2650.00', status: 'unpaid' });
  await page.goto(`${base}/accounting/driver-pay?weekStart=2026-09-21`);
  await expect(page.getByRole('button', { name: driver.fullName, exact: true })).toHaveCount(0);
  const openPay = async week => {
    await page.goto(`${base}/accounting/driver-pay?weekStart=${week}`);
    const toggle = page.getByRole('button', { name: driver.fullName, exact: true });
    await expect(toggle).toBeVisible();
    if (await toggle.getAttribute('aria-expanded') !== 'true') await toggle.click();
    return page.getByLabel(`${driver.fullName}, expense Escrow, deduction`, { exact: true });
  };
  const deduction = await openPay('2026-09-28');
  await expect(deduction).toHaveValue('-2650.00');
  await deduction.fill('-100.25');
  await expect.poll(async () => (await read()).paidAmount).toBe('100.25');
  await page.goto(`${base}/accounting/escrow`);
  await page.getByPlaceholder('Search drivers…').fill(driver.fullName);
  await page.getByLabel('Escrow payment status').selectOption('partial');
  const row = page.getByRole('row').filter({ hasText: driver.fullName });
  await expect(row).toContainText('$2,549.75');
  await expect(row).toContainText('Partially paid');
  await page.screenshot({ path: join(temp, 'escrow-partial.png'), fullPage: true });
  await page.getByLabel('Escrow payment status').selectOption('unpaid');
  await expect(row).toHaveCount(0);
  await expect(page.getByText('No escrow records match these filters.')).toBeVisible();
  const next = await openPay('2026-10-05');
  await expect(next).toHaveValue('-2549.75');
  await next.fill('-2549.75');
  // A new draft must be created to collect an unchanged suggested amount.
  await next.fill('-2549.74');
  await next.fill('-2549.75');
  await expect.poll(async () => (await read()).status).toBe('paid');
  await page.goto(`${base}/accounting/escrow`);
  await page.getByLabel('Escrow payment status').selectOption('paid');
  await expect(row).toContainText('Fully paid');
  const rowBefore = await row.boundingBox();
  const history = row.getByRole('button', { name: 'Payment history', exact: true });
  await history.click();
  const panel = page.getByRole('dialog', { name: 'Payment history' });
  await expect(panel).toContainText('Week of 2026-09-28');
  await expect(panel).toContainText('$100.25');
  expect(await row.boundingBox()).toEqual(rowBefore);
  await page.keyboard.press('Escape');
  await expect(panel).toHaveCount(0);
  await expect(history).toBeFocused();
  await page.goto(`${base}/drivers/detail?id=${driver.id}`);
  await page.getByRole('tab', { name: 'Escrow', exact: true }).click();
  await expect(page.getByRole('table', { name: 'Driver escrow balances' })).toContainText('Fully paid');
  await page.screenshot({ path: join(temp, 'driver-profile-escrow.png'), fullPage: true });
  await page.goto(`${base}/accounting/escrow`);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: 'Collapse sidebar' }).click();
  await expect(page.locator('aside')).toHaveCSS('width', '64px');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: join(temp, 'escrow-mobile.png'), fullPage: true, animations: 'disabled' });
  await page.getByRole('button', { name: 'Expand sidebar' }).click();
  await page.setViewportSize({ width: 1440, height: 1000 });
  await verifyReleases(page, base, temp, driver);
}

async function verifyReleases(page, base, temp, driver) {
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'America/New_York', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date());
  const monday = new Date(`${today}T12:00:00Z`); monday.setUTCDate(monday.getUTCDate() - (monday.getUTCDay() + 6) % 7);
  const week = monday.toISOString().slice(0, 10); monday.setUTCDate(monday.getUTCDate() + 7); const nextWeek = monday.toISOString().slice(0, 10);
  const read = async () => (await (await page.request.get(`${base}/api/escrows?driverId=${driver.id}`)).json()).items[0];
  const row = page.getByRole('row').filter({ hasText: driver.fullName });
  await row.getByRole('button', { name: 'Release', exact: true }).click();
  const form = page.getByRole('dialog', { name: 'Release escrow', exact: true });
  await expect(form).toBeVisible();
  await expect(form.getByLabel('Release week')).toHaveAttribute('min', week);
  await form.getByLabel('Release amount').fill('100.25');
  await page.screenshot({ path: join(temp, 'escrow-release-form.png'), fullPage: true });
  await form.getByRole('button', { name: 'Release', exact: true }).click();
  await expect(form).toHaveCount(0);
  await expect.poll(async () => (await read()).heldAmount).toBe('2549.75');
  await page.goto(`${base}/accounting/driver-pay?weekStart=${week}`);
  const toggle = page.getByRole('button', { name: driver.fullName, exact: true });
  await expect(toggle).toBeVisible(); if (await toggle.getAttribute('aria-expanded') !== 'true') await toggle.click();
  const credit = page.getByLabel(`${driver.fullName}, escrow release`, { exact: true });
  await expect(credit).toHaveText('+$100.25');
  await expect(credit.locator('input')).toHaveCount(0);
  await page.screenshot({ path: join(temp, 'escrow-release-pay.png'), fullPage: true });
  // Edit from the driver profile. The old week loses the credit; the new week gets it once.
  await page.goto(`${base}/drivers/detail?id=${driver.id}`);
  await page.getByRole('tab', { name: 'Escrow', exact: true }).click();
  await page.getByRole('button', { name: 'Payment history', exact: true }).click();
  await page.getByRole('button', { name: `Edit release for ${week}`, exact: true }).click();
  const editor = page.getByRole('dialog', { name: 'Edit escrow release', exact: true });
  await editor.getByLabel('Release amount').fill('150.50'); await editor.getByLabel('Release week').fill(nextWeek);
  await editor.getByRole('button', { name: 'Save release', exact: true }).click();
  await expect(editor).toHaveCount(0);
  await expect.poll(async () => (await read()).heldAmount).toBe('2499.50');
  const credits = async selectedWeek => {
    const report = await (await page.request.get(`${base}/api/driver-pay?weekStart=${selectedWeek}`)).json();
    return (report.drivers.find(d => d.id === driver.id)?.autoCharges ?? []).filter(r => r.source.startsWith('escrow-release:'));
  };
  expect(await credits(week)).toHaveLength(0); expect((await credits(nextWeek)).map(r => r.amount)).toEqual(['150.50']);
  await page.getByRole('button', { name: 'Payment history', exact: true }).click();
  await page.getByRole('button', { name: `Edit release for ${nextWeek}`, exact: true }).click();
  await editor.getByRole('button', { name: 'Cancel release', exact: true }).click(); await expect(editor).toHaveCount(0);
  await expect.poll(async () => (await read()).heldAmount).toBe('2650.00'); expect(await credits(nextWeek)).toHaveLength(0);
  // Create directly from the same profile tab.
  await page.getByRole('button', { name: 'Release', exact: true }).click();
  await form.getByLabel('Release amount').fill('25'); await form.getByRole('button', { name: 'Release', exact: true }).click(); await expect(form).toHaveCount(0);
  await expect.poll(async () => (await read()).heldAmount).toBe('2625.00');
  expect((await credits(week)).map(r => r.amount)).toEqual(['25.00']); expect(await credits(nextWeek)).toHaveLength(0);
  const account = await read(); expect(account.paidAmount).toBe('2650.00'); expect(account.remainingAmount).toBe('0.00');
  expect(account.releases.some(r => r.cancelled)).toBe(true);
}
