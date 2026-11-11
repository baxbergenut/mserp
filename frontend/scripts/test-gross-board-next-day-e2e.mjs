// Real API/database check against the isolated review-local.mjs environment.
import { chromium, expect } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { readFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';

const accessFile = process.env.MSERP_REVIEW_ACCESS_FILE;
const dsn = process.env.MSERP_REVIEW_TEST_DATABASE_URL;
if (!accessFile || !dsn) throw new Error('Set MSERP_REVIEW_ACCESS_FILE and MSERP_REVIEW_TEST_DATABASE_URL');
const { base, email, password, schema } = JSON.parse(await readFile(accessFile, 'utf8'));
const database = new URL(dsn);
if (!['127.0.0.1', 'localhost'].includes(database.hostname) || !database.pathname.endsWith('_test') ||
    !/^review_[a-f0-9]+$/.test(schema) || !/^http:\/\/127\.0\.0\.1:\d+$/.test(base)) throw new Error('An isolated local review is required');
const sql = input => execFileSync(process.env.PSQL_PATH || 'psql', ['-X', '-q', '-v', 'ON_ERROR_STOP=1', '-d', dsn], {
  input: `SET search_path TO ${schema},public; ${input}`, encoding: 'utf8', windowsHide: true,
});
const week = '2026-11-09', next = '2026-11-10';
sql(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date)
 VALUES ('e4590000-0000-0000-0000-000000000001','Destination Example','destination example','cpm',0.75,'2026-09-28')
 ON CONFLICT DO NOTHING;
 INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,raw_payload) VALUES
 (989961,'DEMO-101','Delivered',1800,2000,800,'{"stops":[{"ordering":1,"stop_type":"pickup","location":{"city":"Denver","state":"CO"}},
 {"ordering":3,"stop_type":"delivery","location":{"city":"Boise","state":"ID","address1":"123 Delivery Lane","zip_code":"83702"}},
 {"ordering":2,"stop_type":"delivery","location":{"city":"Ogden","state":"UT"}}]}'),
 (989962,'DEMO-102','Dispatched',1500,1700,600,'{"stops":[{"ordering":1,"stop_type":"pickup","location":{"city":"Boise","state":"ID"}},
 {"ordering":2,"stop_type":"delivery","location":{"city":"Portland","state":"OR","zip_code":"97201"}}]}'),
 (989963,'DEMO-103','Delivered',900,1100,400,'{"stops":[{"ordering":2,"stop_type":"delivery","location":{"city":"Omaha","state":"NE"}}]}'),
 (989964,'DEMO-104','Dispatched',1200,1400,500,'{"stops":[{"ordering":2,"stop_type":"delivery","location":{"city":"Seattle","state":"WA"}}]}')
 ON CONFLICT(id) DO UPDATE SET raw_payload=excluded.raw_payload;
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate)
 SELECT id,'${week}','DEMO-101',989961,1800 FROM drivers WHERE full_name IN ('Destination Example','Jordan Lee','Sam Rivera')
 ON CONFLICT(driver_id,service_date) DO NOTHING;
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate)
 SELECT id,'${next}','DEMO-102',989962,1500 FROM drivers WHERE full_name='Jordan Lee' ON CONFLICT DO NOTHING;
 INSERT INTO gross_board_entries(driver_id,service_date,day_status)
 SELECT id,'${next}','HOME' FROM drivers WHERE full_name='Sam Rivera' ON CONFLICT DO NOTHING;
 INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate)
 SELECT id,'2026-11-08','DEMO-103',989963,900 FROM drivers WHERE full_name='Taylor Brooks' ON CONFLICT DO NOTHING;
 DELETE FROM gross_board_entries WHERE driver_id=(SELECT id FROM drivers WHERE full_name='Destination Example') AND service_date='${next}';
 DELETE FROM gross_board_planning_entries WHERE driver_id=(SELECT id FROM drivers WHERE full_name='Destination Example') AND service_date='${next}';
 SELECT pg_notify('mserp_gross_board','');`);

const browser = await chromium.launch();
try {
  const context = await browser.newContext({ viewport: { width: 1920, height: 1080 } });
  const page = await context.newPage();
  await page.clock.setFixedTime(new Date('2026-11-10T12:00:00Z'));
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  expect((await page.request.post(`${base}/api/auth/login`, { data: { email, password }, headers: { origin: base } })).ok()).toBe(true);
  const read = async () => (await (await page.request.get(`${base}/api/gross-board?weekStart=${week}`)).json());
  await expect.poll(async () => (await read()).nextDayDestinations?.some(entry => entry.date === next && entry.text === 'Boise, ID')).toBe(true);
  const board = await read();
  const alex = board.drivers.find(driver => driver.fullName === 'Destination Example').id;
  const jordan = board.drivers.find(driver => driver.fullName === 'Jordan Lee').id;
  const sam = board.drivers.find(driver => driver.fullName === 'Sam Rivera').id;
  const taylor = board.drivers.find(driver => driver.fullName === 'Taylor Brooks').id;
  const suggestions = board.nextDayDestinations ?? [];
  expect(suggestions).toContainEqual({ driverId: alex, date: next, text: 'Boise, ID' });
  expect(suggestions).toContainEqual({ driverId: taylor, date: week, text: 'Omaha, NE' });
  expect(suggestions.some(entry => [jordan, sam].includes(entry.driverId) && entry.date === next)).toBe(false);
  const cell = target => target.locator(`td[data-gross-driver="${alex}"][data-gross-date="${next}"]`);
  await page.goto(`${base}/gross-board`);
  await expect(cell(page).locator('[data-gross-suggested-destination]')).toContainText('Boise, ID');
  expect(await cell(page).innerText()).not.toMatch(/83702|Delivery Lane|Ogden/);
  const statusLabel = `Custom status for Destination Example, ${next}, existing destination`;
  await expect(cell(page).locator('[data-gross-planning-full-day]')).toContainText('Boise, ID');
  await expect(cell(page).getByRole('combobox')).toHaveCount(0);
  await expect(cell(page).getByRole('textbox')).toHaveCount(0);
  await expect(cell(page).getByRole('button')).toHaveCount(1);
  await page.getByRole('button', { name: statusLabel, exact: true }).dblclick();
  await page.getByRole('textbox', { name: statusLabel, exact: true }).fill('Meridian, ID');
  await page.getByRole('textbox', { name: statusLabel, exact: true }).press('Enter');
  await expect(cell(page).getByRole('button', { name: statusLabel, exact: true })).toHaveText('Meridian, ID');
  await expect.poll(async () => (await read()).planningEntries.some(entry => entry.driverId === alex && entry.date === next && entry.text === 'Meridian, ID')).toBe(true);
  const other = await context.newPage();
  await other.clock.setFixedTime(new Date('2026-11-10T12:00:00Z'));
  await other.goto(`${base}/gross-board`);
  await expect(cell(other).locator('[data-gross-planning-full-day]')).toContainText('Meridian, ID');
  await page.getByRole('button', { name: statusLabel, exact: true }).dblclick();
  await page.getByRole('textbox', { name: statusLabel, exact: true }).fill('');
  await page.getByRole('textbox', { name: statusLabel, exact: true }).press('Enter');
  await expect(cell(page).locator('[data-gross-planning-full-day]')).toHaveCount(0);
  await expect(cell(other).locator('[data-gross-planning-full-day]')).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole('combobox', { name: `Destination Example, ${next}, load number or status`, exact: true })).toBeVisible();
  await expect(cell(page).locator('[data-gross-suggested-destination]')).toHaveCount(0);
  expect((await read()).planningEntries).toContainEqual({ driverId: alex, date: next, kind: 'destination', text: '', version: 2 });
  await other.close();
  // Restore the synthetic example for the user's review.
  sql(`DELETE FROM gross_board_planning_entries WHERE driver_id='${alex}' AND service_date='${next}' AND kind='destination'; SELECT pg_notify('mserp_gross_board','');`);
  await page.reload();
  await expect(cell(page).locator('[data-gross-suggested-destination]')).toContainText('Boise, ID');
  await page.getByRole('textbox', { name: 'Search drivers or load numbers…', exact: true }).fill('Destination Example');
  await page.screenshot({ path: join(dirname(accessFile), 'gross-board-next-day.png'), fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await cell(page).getByRole('button', { name: statusLabel, exact: true }).scrollIntoViewIfNeeded();
  await expect(cell(page).locator('[data-gross-suggested-destination]')).toBeInViewport();
  await page.screenshot({ path: join(dirname(accessFile), 'gross-board-next-day-mobile.png'), fullPage: true });
  expect(errors).toEqual([]);
  console.log('Passed: full-day custom status, city/state only, final stop ordering, protected days, Sunday/Monday, double-click editing, clearing to delete, persistent deletion, live second tab, and mobile layout.');
  console.log(`Preview: ${base}/gross-board\nScreenshots: ${dirname(accessFile)}`);
} finally {
  await browser.close();
}
