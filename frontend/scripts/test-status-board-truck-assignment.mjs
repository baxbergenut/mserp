// Run against review-local.mjs with a disposable local database and /api build.
import { chromium, expect } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { readFile } from 'node:fs/promises';
import { join, dirname } from 'node:path';

const accessFile = process.env.MSERP_REVIEW_ACCESS_FILE;
const dsn = process.env.MSERP_REVIEW_TEST_DATABASE_URL;
if (!accessFile || !dsn) throw new Error('Set MSERP_REVIEW_ACCESS_FILE and MSERP_REVIEW_TEST_DATABASE_URL');
const { base, email, password, schema } = JSON.parse(await readFile(accessFile, 'utf8'));
const database = new URL(dsn);
if (!['localhost', '127.0.0.1'].includes(database.hostname) || !database.pathname.endsWith('_test') || !/^review_[a-f0-9]+$/.test(schema) || !/^http:\/\/127\.0\.0\.1:\d+$/.test(base)) throw new Error('Use an isolated local review');
const sql = input => execFileSync(process.env.PSQL_PATH || 'psql', ['-X', '-q', '-t', '-A', '-v', 'ON_ERROR_STOP=1', '-d', dsn], { input: `SET search_path TO ${schema},public; ${input}`, encoding: 'utf8', windowsHide: true }).trim();
const ids = Array.from({ length: 8 }, (_, i) => `e4540000-0000-0000-0000-${String(i + 1).padStart(12, '0')}`);
const [driver, other, teammate, first, free, occupied, shop, oos] = ids;
const today = new Date().toLocaleDateString('en-CA', { timeZone: 'America/New_York' });
const weekStart = sql(`SELECT date_trunc('week','${today}'::date)::date`);
sql(`UPDATE fleet_status_control SET enabled=true;
 INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,dispatcher_id) VALUES
 ('${driver}','Avery Bennett','avery bennett','cpm',0.6,'63000000-0000-0000-0000-000000000012'),
 ('${other}','Chris Davis','chris davis','cpm',0.6,'63000000-0000-0000-0000-000000000012'),
 ('${teammate}','Robin Bennett','robin bennett','cpm',0,'63000000-0000-0000-0000-000000000012') ON CONFLICT(id) DO NOTHING;
 INSERT INTO trucks(id,unit_number) VALUES ('${first}','201'),('${free}','202'),('${occupied}','203'),('${shop}','204'),('${oos}','205') ON CONFLICT(id) DO NOTHING;
 INSERT INTO truck_driver_assignments(driver_id,truck_id,assigned_at) VALUES
 ('${driver}','${first}','${today}'::date-7),('${other}','${occupied}','${today}'::date-7) ON CONFLICT DO NOTHING;
 INSERT INTO driver_teams(main_driver_id,team_driver_id,start_week) SELECT '${driver}','${teammate}',date_trunc('week','${today}'::date)::date WHERE NOT EXISTS(SELECT 1 FROM driver_teams WHERE main_driver_id='${driver}');
 UPDATE trucks SET active=false WHERE id='${oos}';`);

const browser = await chromium.launch({ channel: 'chrome' });
try {
 const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } });
 const errors = []; page.on('pageerror', err => errors.push(err.message));
 expect((await page.request.post(`${base}/api/auth/login`, { data: { email, password }, headers: { origin: base } })).ok()).toBe(true);
 const session = await (await page.request.get(`${base}/api/auth/session`)).json();
 const headers = { origin: base, 'x-csrf-token': session.csrfToken };
 const preview = async (id = driver) => {
   const response = await page.request.get(`${base}/api/drivers/${id}/truck-assignment`);
   expect(response.ok(), await response.text()).toBe(true);
   return response.json();
 };
 const save = (view, truckId, confirmReassign = false, id = driver) => page.request.put(`${base}/api/drivers/${id}/truck-assignment`, { headers, data: { truckId, revision: view.revision, confirmReassign } });
 const setCurrentLoad = async (id, currentLoad) => {
   const responseBoard = await page.request.get(`${base}/api/driver-board?weekStart=${weekStart}`);
   expect(responseBoard.ok(), await responseBoard.text()).toBe(true);
   const board = await responseBoard.json();
   const entry = board.entries.find(row => row.driverId === id) || { driverId: id, version: 0, status: '' };
   const fields = { ...entry };
   delete fields.fleetStatus;
   const response = await page.request.put(`${base}/api/driver-board`, { headers, data: { entries: [{ ...fields, currentLoad }] } });
   expect(response.ok(), await response.text()).toBe(true);
 };
 // Restrictions use the real fleet endpoint.
 const truckProfile = await (await page.request.get(`${base}/api/trucks/${shop}`)).json();
 const restricted = await page.request.patch(`${base}/api/trucks/${shop}/status`, { headers, data: { status: 'shop', reason: 'Synthetic repair review', version: truckProfile.statusVersion } });
 expect(restricted.ok(), await restricted.text()).toBe(true);
 const inactiveProfile = await (await page.request.get(`${base}/api/trucks/${oos}`)).json();
 const terminated = await page.request.patch(`${base}/api/trucks/${oos}/status`, { headers, data: { status: 'terminated', reason: 'Synthetic retired truck', version: inactiveProfile.statusVersion } });
 expect(terminated.ok(), await terminated.text()).toBe(true);
 await page.goto(`${base}/driver-board`);
 const cell = page.getByRole('combobox', { name: 'Avery Bennett · Assign truck', exact: true });
 await expect(cell).toBeEnabled();
 await expect(cell).toHaveAttribute('title', 'Assign truck effective today (New York)');
 await expect(cell.locator(`option[value="${shop}"]`)).toBeEnabled();
 await expect(cell.locator(`option[value="${shop}"]`)).toContainText('Shop');
 await expect(cell.locator(`option[value="${oos}"]`)).toBeDisabled();
 await cell.selectOption(free);
 await expect(cell).toHaveValue(free);
 expect(sql(`SELECT count(*) FROM truck_driver_assignments WHERE driver_id='${driver}' AND truck_id='${free}' AND assigned_at='${today}'::date::timestamp AT TIME ZONE 'America/New_York' AND unassigned_at IS NULL`)).toBe('1');
 expect(sql(`SELECT count(*) FROM truck_driver_assignments WHERE driver_id='${driver}' AND truck_id='${first}' AND unassigned_at='${today}'::date::timestamp AT TIME ZONE 'America/New_York'`)).toBe('1');
 const initialCount = sql(`SELECT count(*) FROM truck_driver_assignments WHERE driver_id='${driver}'`);
 expect((await save(await preview(), free)).status()).toBe(204);
 expect(sql(`SELECT count(*) FROM truck_driver_assignments WHERE driver_id='${driver}'`)).toBe(initialCount);
 // A stale selection cannot overwrite a concurrent assignment.
 const stale = await preview();
 expect((await save(stale, first)).status()).toBe(204);
 expect((await save(stale, free)).status()).toBe(409);
 // Occupied trucks require explicit confirmation; cancelling leaves them intact.
 expect((await save(await preview(), occupied)).status()).toBe(409);
 await page.reload();
 // No browser-dialog listener: the user preview dismisses window.confirm automatically.
 await cell.selectOption(occupied);
 const confirmation = page.getByRole('dialog', { name: 'Reassign truck', exact: true });
 await expect(confirmation).toContainText('Chris Davis');
 await confirmation.getByRole('button', { name: 'Cancel', exact: true }).click();
 await expect(cell).toBeEnabled();
 await expect(cell).toHaveValue(first);
 expect((await preview(other)).truckId).toBe(occupied);
 await cell.selectOption(occupied);
 await expect(confirmation).toContainText('Chris Davis');
 // A load assigned while the confirmation is open must still block the save.
 await setCurrentLoad(other, 'ACTIVE-203');
 await confirmation.getByRole('button', { name: 'Reassign truck', exact: true }).click();
 await expect(confirmation.getByRole('alert')).toContainText('active load ACTIVE-203');
 expect((await preview()).truckId).toBe(first);
 expect((await preview(other)).truckId).toBe(occupied);
 expect((await save(await preview(other), occupied, false, other)).status()).toBe(204);
 const blocked = await save(await preview(), occupied, true);
 expect(blocked.status()).toBe(409);
 expect(await blocked.text()).toContain('active load ACTIVE-203');
 await confirmation.getByRole('button', { name: 'Cancel', exact: true }).click();
 await page.reload();
 await expect(cell.locator(`option[value="${occupied}"]`)).toBeDisabled();
 await expect(cell.locator(`option[value="${occupied}"]`)).toContainText('Active load ACTIVE-203');
 await setCurrentLoad(other, '');
 await page.reload();
 await expect(cell.locator(`option[value="${occupied}"]`)).toBeEnabled();
 await cell.selectOption(occupied);
 await expect(confirmation).toContainText('Chris Davis');
 await confirmation.getByRole('button', { name: 'Reassign truck', exact: true }).click();
 await expect(confirmation).toHaveCount(0);
 await expect(cell).toHaveValue(occupied);
 await expect(page.getByRole('combobox', { name: 'Chris Davis · Assign truck', exact: true })).toHaveValue('');
 await page.reload();
 await expect(cell).toHaveValue(occupied);
 expect((await preview(other)).truckId).toBe('');
 // A secondary driver's request changes the main driver's shared assignment.
 const teamPreview = await preview(teammate);
 expect(teamPreview.driverId).toBe(driver);
 expect((await save(teamPreview, free, false, teammate)).status()).toBe(204);
 expect((await preview()).truckId).toBe(free);
 expect(sql(`SELECT count(*) FROM truck_driver_assignments WHERE driver_id='${teammate}'`)).toBe('0');
 // Shop is selectable and assignment must preserve its status and restriction reason.
 await page.reload();
 await cell.selectOption(shop);
 await expect(cell).toHaveValue(shop);
 const assignedShop = await (await page.request.get(`${base}/api/trucks/${shop}`)).json();
 expect(assignedShop.operatingStatus).toBe('shop');
 expect(assignedShop.statusReason).toBe('Synthetic repair review');
 expect((await save(await preview(), free)).status()).toBe(204);
 // The server independently blocks inactive choices.
 expect((await save(await preview(), oos)).status()).toBe(409);
 // Finalized payroll blocks the move without partially releasing the old truck.
 sql(`INSERT INTO payroll_settlements(driver_id,week_start,report) VALUES('${driver}',date_trunc('week','${today}'::date)::date,'{}');`);
 const locked = await save(await preview(), first);
 expect(locked.status()).toBe(409); expect(await locked.text()).toContain('Reopen finalized');
 expect((await preview()).truckId).toBe(free);
 sql(`UPDATE payroll_settlements SET finalized=false WHERE driver_id='${driver}';`);
 // Leave a useful review fixture: occupied, available, Shop, and inactive trucks.
 expect((await save(await preview(other), occupied, false, other)).status()).toBe(204);
 await page.reload();
 await expect(cell).toBeEnabled();
 await expect(cell.locator(`option[value="${occupied}"]`)).toHaveText('203 — Chris Davis');
 await page.goto(`${base}/drivers/detail?id=${driver}`);
 await page.getByRole('button', { name: 'Edit driver', exact: true }).click();
 const profileTruck = page.getByRole('combobox', { name: 'Truck', exact: true });
 await expect(profileTruck.locator(`option[value="${shop}"]`)).toBeEnabled();
 await expect(profileTruck.locator(`option[value="${shop}"]`)).toContainText('Shop');
 await page.goto(`${base}/driver-board`);
 await expect(cell).toBeEnabled();
 await page.screenshot({ path: join(dirname(accessFile), 'truck-dropdown.png') });
 await page.context().storageState({ path: join(dirname(accessFile), 'chrome-storage.json') });
 expect(errors).toEqual([]);
 console.log('Truck assignment browser/API checks passed: dates, old-period closure, no-op, conflicts, confirmation/cancel, active-load block including stale confirmation, teams, Shop assignment preserving restrictions, inactive trucks, and finalized payroll rollback.');
} finally { await browser.close(); }
