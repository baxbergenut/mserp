// Real API/browser regression against the disposable review-local environment.
import { readFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { execFileSync } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { chromium, expect } from '@playwright/test';

const access = JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE, 'utf8'));
const dsn = process.env.MSERP_REVIEW_TEST_DATABASE_URL;
const database = new URL(dsn);
if (!['127.0.0.1', 'localhost'].includes(database.hostname) || !database.pathname.endsWith('_test') ||
    !['127.0.0.1', 'localhost'].includes(new URL(access.base).hostname) || !/^review_[a-f0-9]+$/.test(access.schema)) {
  throw Error('A disposable local review is required');
}
const sql = query => execFileSync(process.env.PSQL_PATH || 'psql', ['-X', '-q', '-v', 'ON_ERROR_STOP=1', '-d', dsn], {
  input: `SET search_path TO ${access.schema},public; ${query}`, encoding: 'utf8', windowsHide: true,
});
const driver = randomUUID(), truck = randomUUID(), dispatcher = randomUUID();
sql(`DELETE FROM drivers WHERE full_name='Departure Browser'; DELETE FROM trucks WHERE unit_number='DEPART-075';
INSERT INTO dispatchers(id,full_name,normalized_name) VALUES('${dispatcher}','Departure Dispatch','departure-${dispatcher}');
INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,dispatcher_id) VALUES('${driver}','Departure Browser','departure-${driver}','cpm',0.5,'${dispatcher}');
UPDATE driver_dispatcher_assignments SET assigned_at=date_trunc('week',now() AT TIME ZONE 'America/New_York') AT TIME ZONE 'America/New_York'-interval '7 days' WHERE driver_id='${driver}';
INSERT INTO trucks(id,unit_number,status) VALUES('${truck}','DEPART-075','assigned');
INSERT INTO truck_driver_assignments(driver_id,truck_id,assigned_at) VALUES('${driver}','${truck}',date_trunc('week',now() AT TIME ZONE 'America/New_York') AT TIME ZONE 'America/New_York'-interval '7 days');
INSERT INTO gross_board_entries(driver_id,service_date,load_number,original_rate,driver_rate,entered_original_rate) VALUES('${driver}',date_trunc('week',now() AT TIME ZONE 'America/New_York')::date,'DEPART-LOAD',2600,2600,2600);`);
const browser = await chromium.launch();
try {
  const context = await browser.newContext({ viewport: { width: 1800, height: 1000 } });
  const page = await context.newPage();
  const login = await page.request.post(access.base + '/api/auth/login', { data: { email: access.email, password: access.password }, headers: { Origin: access.base } });
  expect(login.ok()).toBe(true);
  const session = await (await page.request.get(access.base + '/api/auth/session')).json();
  const headers = { Origin: access.base, 'X-CSRF-Token': session.csrfToken };
  await page.goto(access.base + '/driver-board');
  await expect(page.getByLabel('Departure Browser · Status', { exact: true })).toBeEnabled();
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'America/New_York' }).format(new Date());
  const monday = new Date(today + 'T12:00:00Z'); monday.setUTCDate(monday.getUTCDate() - (monday.getUTCDay() + 6) % 7);
  const boardPath = '/api/driver-board?weekStart=' + monday.toISOString().slice(0, 10);
  const boardBefore = await (await page.request.get(access.base + boardPath)).json();
  const profile = await (await page.request.get(access.base + '/api/drivers/' + driver)).json();
  const terminated = await page.request.patch(access.base + '/api/drivers/' + driver + '/status', {
    headers, data: { status: 'terminated', terminationDate: today, assignmentWeek: boardBefore.weekStart, chargePauseWeek: boardBefore.weekStart, updatedAt: profile.updatedAt },
  });
  expect(terminated.ok(), await terminated.text()).toBe(true);
  const status = page.getByLabel('Departure Browser · Status', { exact: true });
  await expect(status).toHaveValue('Left');
  await expect(status).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Move Departure Browser', exact: true })).toBeDisabled();
  const row = page.locator(`[data-driver-id="${driver}"]`);
  await expect(row).toContainText('DEPART-075');
  await expect(row.locator('[data-board-column="originalGross"]')).toContainText('2,600.00');
  await expect(row.locator('[data-board-column="driverGross"]')).toContainText('2,600.00');
  const board = await (await page.request.get(access.base + boardPath)).json();
  expect(board.drivers.find(d => d.id === driver)).toMatchObject({ dispatcherId: dispatcher, departed: true, truckUnit: 'DEPART-075' });
  await page.screenshot({ path: join(dirname(process.env.MSERP_REVIEW_ACCESS_FILE), 'departure-board.png') });
  const gross = await context.newPage();
  await gross.goto(access.base + '/gross-board');
  await gross.getByPlaceholder('Search drivers or load numbers…').fill('Departure Browser');
  await expect(gross.locator(`[data-gross-driver="${driver}"][data-gross-field="driver"]`)).toHaveCount(1);
  await expect(gross.getByRole('row', { name: 'Departure Dispatch totals' })).toContainText('2,600.00');
  await expect(gross.getByText('Changed dispatcher', { exact: true })).toHaveCount(0);
  await gross.screenshot({ path: join(dirname(process.env.MSERP_REVIEW_ACCESS_FILE), 'departure-gross-board.png') });
  await gross.close();
  // Model the following week's eligibility while preserving the saved gross.
  sql(`UPDATE drivers SET termination_date=date_trunc('week',now() AT TIME ZONE 'America/New_York')::date-1 WHERE id='${driver}';`);
  await expect(status).toHaveCount(0);
  console.log('Departure E2E passed: native offboarding, live Left row, one Gross Board dispatcher row, preserved gross, read-only controls and removal.');
} finally {
  await browser.close();
}
