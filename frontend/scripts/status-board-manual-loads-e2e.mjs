import { expect } from '@playwright/test';
import { join } from 'node:path';

export async function runManualNextLoadsE2E({ page, base, sql, schema, temp, id }) {
  const driver = 'e4500000-0000-0000-0000-000000000099';
  sql(`SET search_path TO ${schema},public;
    INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,dispatcher_id)
    SELECT '${driver}','Manual Queue Driver','manual queue driver','cpm',0.6,dispatcher_id FROM drivers WHERE id='${id}';
    INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES(989011,'MANUAL-KNOWN','Dispatched',0,0,
    '{"stops":[{"ordering":1,"stop_type":"pickup","location":{"city":"Dallas","state":"TX"}},{"ordering":2,"stop_type":"delivery","location":{"city":"Boston","state":"MA"}}]}');
  `);
  await page.getByRole('button', { name: 'Reload', exact: true }).click();
  const search = page.getByLabel('Search Status Board', { exact: true });
  await search.fill('Manual Queue Driver');
  await page.getByRole('button', { name: 'Manual Queue Driver · Next load', exact: true }).click();
  const panel = page.getByRole('dialog', { name: 'Manual Queue Driver · Loads', exact: true });
  const input = panel.getByRole('textbox', { name: 'Add next load', exact: true });
  const add = panel.getByRole('button', { name: 'Add next load', exact: true });
  const card = number => panel.getByRole('article').filter({ hasText: number });
  await expect(add).toBeDisabled();
  await input.fill('  MANUAL-KNOWN  ');
  await input.press('Enter');
  await expect(card('MANUAL-KNOWN')).toContainText('Dallas, TX');
  await expect(card('MANUAL-KNOWN')).toContainText('Boston, MA');
  await expect(input).toHaveValue('');
  await input.fill('MANUAL-LATE');
  await add.click();
  await expect(card('MANUAL-LATE')).toContainText('Waiting for load details');
  const before = await (await page.request.get(`${base}/api/driver-board/loads/${driver}`)).json();
  expect(before.next.map(load => load.number)).toEqual(['MANUAL-KNOWN', 'MANUAL-LATE']);
  const lateID = before.next[1].planId;
  await input.fill('manual-late');
  await add.click();
  await expect(panel.getByRole('alert')).toContainText('already listed');
  await expect(input).toHaveValue('manual-late');
  await panel.getByRole('button', { name: 'Close loads', exact: true }).click();
  await page.reload();
  await page.getByRole('button', { name: 'Manual Queue Driver · Next load', exact: true }).click();
  await card('MANUAL-LATE').getByRole('button', { name: 'MANUAL-LATE', exact: true }).click();
  await expect(card('MANUAL-LATE')).toContainText('Waiting for load details');
  // A real import invalidation hydrates the open panel through the live board.
  sql(`SET search_path TO ${schema},public;
    INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload) VALUES(989012,'MANUAL-LATE','Dispatched',0,0,
    '{"stops":[{"ordering":1,"stop_type":"pickup","location":{"city":"Miami","state":"FL"}}]}');
  `);
  await expect(card('MANUAL-LATE')).toContainText('Miami, FL', { timeout: 15000 });
  const after = await (await page.request.get(`${base}/api/driver-board/loads/${driver}`)).json();
  expect(after.next[1].planId).toBe(lateID);
  expect(after.next[1].loadId).toBe(989012);
  await panel.getByRole('button', { name: 'Move MANUAL-LATE up', exact: true }).click();
  await expect(panel.getByLabel('Load sequence').getByRole('article').first()).toContainText('MANUAL-LATE');
  await card('MANUAL-LATE').getByRole('button', { name: 'Set current', exact: true }).click();
  await panel.getByRole('button', { name: 'Confirm current load', exact: true }).click();
  await expect(panel.getByLabel('Current load destination source')).toHaveValue(/.+/);
  await page.screenshot({ path: join(temp, 'status-board-manual-next-load.png'), fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(input).toBeInViewport({ ratio: 1 });
  await expect(add).toBeInViewport({ ratio: 1 });
  expect(await panel.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
  await page.screenshot({ path: join(temp, 'status-board-manual-next-load-mobile.png'), fullPage: true });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await panel.getByRole('button', { name: 'Close loads', exact: true }).click();
  await expect(page.getByLabel('Manual Queue Driver · Current load', { exact: true })).toHaveValue('MANUAL-LATE');
  await expect(page.getByRole('button', { name: 'Manual Queue Driver · Destination from load', exact: true })).toHaveText('Miami, FLPU');
  expect(sql(`SET search_path TO ${schema},public; SELECT count(*) FROM gross_board_entries WHERE driver_id='${driver}';`).trim()).toMatch(/\b0\b/);
  await search.fill('');
  console.log('Manual next loads E2E passed: existing details, unknown persistence, duplicate protection, live late import, stable identity, ordering and current-load destination.');
}
