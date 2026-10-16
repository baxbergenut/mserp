import { expect } from '@playwright/test';
import { join } from 'node:path';

// Two separate authenticated browser contexts and committed database notifications.
export async function runStatusBoardLiveE2E({ page, viewer, base, id, week, headers, sql, schema, temp }) {
  await viewer.getByRole('button', { name: 'Close loads', exact: true }).click();
  await expect(page.getByText('Live', { exact: true })).toBeVisible();
  await expect(viewer.getByText('Live', { exact: true })).toBeVisible();
  const notes = p => p.getByLabel('Board Cpm · Notes', { exact: true });
  const read = async driver => (await (await page.request.get(`${base}/api/driver-board?weekStart=${week}`)).json()).entries.find(e => e.driverId === driver);
  const cursor = page.locator('[data-board-presence="Board Viewer"]');
  // A viewer may select a cell without having permission to change its value.
  await viewer.locator(`tr[data-driver-id="${id}"]`).locator('td').nth(8).click();
  await expect(cursor).toHaveAttribute('aria-label', /Board Cpm.*Notes/, { timeout: 2500 });
  await viewer.locator(`tr[data-driver-id="${id}"]`).locator('td').nth(2).click();
  await page.locator('[data-scroll-key="driver-board"]').evaluate(element => { element.scrollLeft = 0; });
  await expect(cursor).toHaveAttribute('aria-label', /Board Cpm.*Trailer/, { timeout: 2500 });
  await page.screenshot({ path: join(temp, 'status-board-live-presence.png'), fullPage: true, animations: 'disabled' });
  await notes(page).fill('Live change from dispatcher');
  await expect(notes(viewer)).toHaveValue('Live change from dispatcher', { timeout: 2500 });
  await expect(page.getByText('All changes saved', { exact: true })).toBeVisible();
  await page.getByRole('heading', { name: 'Status Board', exact: true }).click();
  await page.keyboard.press('Control+z');
  await expect(notes(viewer)).not.toHaveValue('Live change from dispatcher', { timeout: 2500 });
  // Freeze the response after commit, then type again while another row changes.
  let release, started;
  const gate = new Promise(resolve => { release = resolve; });
  const inFlight = new Promise(resolve => { started = resolve; });
  await page.route('**/api/driver-board', async route => {
    if (route.request().method() !== 'PUT') return route.continue();
    const response = await route.fetch(); started(); await gate; await route.fulfill({ response });
  }, { times: 1 });
  await notes(page).fill('First saved value');
  await inFlight;
  await notes(page).fill('Still typing locally');
  const other = 'e4500000-0000-0000-0000-000000000003';
  const baseline = await read(other);
  const response = await page.request.put(`${base}/api/driver-board`, { headers, data: { entries: [{ ...baseline, notes: 'Other row changed live' }] } });
  expect(response.status()).toBe(200);
  await expect(page.getByLabel('Board Percent · Notes', { exact: true })).toHaveValue('Other row changed live', { timeout: 2500 });
  await expect(notes(page)).toHaveValue('Still typing locally');
  release();
  await expect(page.getByText('All changes saved', { exact: true })).toBeVisible();
  await expect(notes(viewer)).toHaveValue('Still typing locally', { timeout: 2500 });
  expect((await read(id)).notes).toBe('Still typing locally');
  // Reconnect reconciles data missed while offline, without waiting for polling.
  await viewer.context().setOffline(true);
  await expect(viewer.getByText('Connecting…', { exact: true })).toBeVisible();
  await notes(page).fill('Saved while viewer offline');
  await expect(page.getByText('All changes saved', { exact: true })).toBeVisible();
  await viewer.context().setOffline(false);
  await expect(viewer.getByText('Live', { exact: true })).toBeVisible({ timeout: 10000 });
  await expect(notes(viewer)).toHaveValue('Saved while viewer offline', { timeout: 2500 });
  // Expiry removes crashed/offline tabs; navigation actively clears presence.
  await viewer.keyboard.press('Escape');
  await viewer.locator(`tr[data-driver-id="${id}"]`).locator('td').nth(8).click();
  await expect(cursor).toHaveAttribute('aria-label', /Notes/, { timeout: 2500 });
  sql(`SET search_path TO ${schema},public; UPDATE driver_board_presence SET expires_at=now()-interval '1 second' WHERE user_id=(SELECT id FROM app_users WHERE username='Board Viewer'); SELECT pg_notify('mserp_board_presence','');`);
  await expect(cursor).toHaveCount(0, { timeout: 2500 });
  await viewer.locator(`tr[data-driver-id="${id}"]`).locator('td').nth(7).click();
  await expect(cursor).toHaveCount(1, { timeout: 2500 });
  await viewer.goto(`${base}/account`);
  await expect(cursor).toHaveCount(0, { timeout: 2500 });
  const rows = await (await page.request.get(`${base}/api/driver-board/presence`)).json();
  expect(rows.every(p => !('sessionHash' in p) && !('session_hash' in p))).toBe(true);
  console.log('Status Board live E2E passed: two-user updates under 2.5s, named cell movement, read-only presence, personal undo, protected in-flight typing, reconnect, expiry and navigation cleanup.');
}
