import { expect } from '@playwright/test';
import { join } from 'node:path';

export async function runFleetLocationE2E({ page, base, sql, schema, temp }) {
  const driver = 'e4500000-0000-0000-0000-000000000002';
  const truck = 'e4500000-0000-0000-0000-000000000004';
  const otherTruck = 'e4500000-0000-0000-0000-000000000005';
  // Keep map tests independent of external tile availability.
  const tileReferrers = [];
  await page.route('https://tile.openstreetmap.org/**', async route => {
    tileReferrers.push((await route.request().allHeaders()).referer);
    await route.fulfill({ status: 200, contentType: 'image/png', body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jG1sAAAAASUVORK5CYII=', 'base64') });
  });
  await page.getByRole('button', { name: 'Board Cpm · Loads', exact: true }).click();
  await expect(page.getByRole('dialog').getByLabel('Truck location map')).toHaveCount(0);
  await expect(page.getByRole('dialog').getByRole('region', { name: 'Truck location', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Close loads', exact: true }).click();
  await page.getByRole('link', { name: 'ELD-17', exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/trucks/detail\\?id=${truck}`));
  await expect(page.getByRole('heading', { name: 'Truck ELD-17', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Vehicle details', exact: true })).toBeVisible();
  const panel = page.getByRole('region', { name: 'Latest truck location' });
  await expect(panel.getByRole('img', { name: 'Reported heading 90 degrees' })).toBeVisible();
  await expect.poll(() => tileReferrers.length).toBeGreaterThan(0);
  expect(tileReferrers.every(value => value === `${new URL(base).origin}/`)).toBe(true);
  await expect(panel.locator('img.leaflet-tile').first()).toHaveAttribute('referrerpolicy', 'strict-origin');
  await expect(panel.locator('[data-heading="90"]')).toHaveCSS('transform', 'matrix(0, 1, -1, 0, 0, 0)');
  await panel.getByRole('button', { name: 'Truck ELD-17 · Latest location', exact: true }).click();
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe('41.88100, -87.62300');
  await page.locator('main').evaluate(el => el.scrollTo(0, 0));
  await page.screenshot({ path: join(temp, 'truck-details-location.png'), fullPage: true, animations: 'disabled' });
  await page.getByRole('button', { name: 'Edit truck', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Edit truck' })).toBeVisible();
  await page.getByRole('button', { name: 'Save truck', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await page.getByRole('tab', { name: 'Expenses', exact: true }).click();
  await expect(page.getByRole('tab', { name: 'Expenses', exact: true })).toHaveAttribute('aria-selected', 'true');
  await page.reload();
  await expect(page.getByRole('tab', { name: 'Expenses', exact: true })).toHaveAttribute('aria-selected', 'true');
  await page.getByRole('tab', { name: 'Overview', exact: true }).click();
  await page.getByRole('link', { name: 'Board Cpm', exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/drivers/detail\\?id=${driver}`));
  await expect(panel.getByRole('img', { name: 'Reported heading 90 degrees' })).toBeVisible();
  await expect(panel.getByText('Unit ELD-17', { exact: true })).toHaveClass(/text-zinc-400/);
  await expect(panel.getByRole('link', { name: /ELD-17/ })).toHaveCount(0);
  await expect(page.locator('main header').getByRole('link', { name: 'ELD-17', exact: true })).toHaveAttribute('href', `/trucks/detail?id=${truck}`);
  await page.screenshot({ path: join(temp, 'driver-details-location.png'), fullPage: true, animations: 'disabled' });
  // Reads resolve the current assignment, not a saved driver/truck association.
  sql(`SET search_path TO ${schema},public; UPDATE truck_driver_assignments SET unassigned_at=now() WHERE truck_id='${truck}' AND unassigned_at IS NULL;`);
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(panel.getByText('No assigned truck.', { exact: true })).toBeVisible();
  await expect(panel.getByLabel('Truck location map')).toHaveCount(0);
  sql(`SET search_path TO ${schema},public; INSERT INTO truck_driver_assignments(truck_id,driver_id) VALUES('${truck}','${driver}');`);
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(panel.getByRole('img', { name: 'Reported heading 90 degrees' })).toBeVisible();
  await page.goto(`${base}/trucks/detail?id=${otherTruck}`);
  await expect(panel.getByRole('img', { name: 'Truck location; heading unavailable' })).toBeVisible();
  await expect(panel.getByRole('button', { name: 'Truck ELD-18 · Latest location' })).toHaveClass(/(?:^| )text-zinc-400(?: |$)/);
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: join(temp, 'truck-details-mobile.png'), fullPage: true, animations: 'disabled' });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.unroute('https://tile.openstreetmap.org/**');
  await page.goto(`${base}/driver-board`);
  await page.getByLabel('Dispatcher filter').selectOption('e4500000-0000-0000-0000-000000000001');
}
