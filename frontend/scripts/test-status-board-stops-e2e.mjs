// Real API/database review fixture. Run against review-local.mjs after a /api build.
import { chromium, expect } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { readFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';

const accessFile = process.env.MSERP_REVIEW_ACCESS_FILE;
const dsn = process.env.MSERP_REVIEW_TEST_DATABASE_URL;
if (!accessFile || !dsn) throw new Error('Set MSERP_REVIEW_ACCESS_FILE and MSERP_REVIEW_TEST_DATABASE_URL');
const { base, email, password, schema } = JSON.parse(await readFile(accessFile, 'utf8'));
const database = new URL(dsn);
if (!['127.0.0.1', 'localhost'].includes(database.hostname) || !database.pathname.endsWith('_test') || !/^review_[a-f0-9]+$/.test(schema) || !/^http:\/\/127\.0\.0\.1:\d+$/.test(base)) throw new Error('An isolated local review environment is required');
const sql = input => execFileSync(process.env.PSQL_PATH || 'psql', ['-X', '-q', '-v', 'ON_ERROR_STOP=1', '-d', dsn], { input: `SET search_path TO ${schema},public; ${input}`, encoding: 'utf8', windowsHide: true });
const driver = 'e4520000-0000-0000-0000-000000000001';
const truck = 'e4520000-0000-0000-0000-000000000002';
const number = 'MULTI-DELIVERY-2026-1103';
const today = new Date().toLocaleDateString('en-CA', { timeZone: 'America/New_York' });
const date = new Date(`${today}T12:00:00Z`);
date.setUTCDate(date.getUTCDate() - (date.getUTCDay() + 6) % 7);
const week = date.toISOString().slice(0, 10);
sql(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,phone,dispatcher_id) VALUES('${driver}','Avery Bennett','avery bennett','cpm',0.6,'2025550198','63000000-0000-0000-0000-000000000012') ON CONFLICT DO NOTHING;
UPDATE drivers SET city='Louisville',state='Kentucky' WHERE id='${driver}';
INSERT INTO trucks(id,unit_number) VALUES('${truck}','MS-TRUCK-12345') ON CONFLICT DO NOTHING;
INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at,source) SELECT '${truck}','${driver}','${week}','Synthetic status review' WHERE NOT EXISTS(SELECT 1 FROM truck_driver_assignments WHERE truck_id='${truck}' AND driver_id='${driver}');
INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,raw_payload) VALUES
(989201,'${number}','dispatched',99999.99,99999.99,1200,'{"stops":[{"id":9892011,"ordering":1,"stop_type":"pickup","location":{"city":"Atlanta","state":"GA"}},{"id":9892012,"ordering":2,"stop_type":"delivery","location":{"city":"Dallas","state":"TX"}},{"id":9892013,"ordering":3,"stop_type":"delivery","location":{"city":"Fort Worth","state":"TX"}}]}'),
(989202,'NEXT-LONG-LOAD-123456789','dispatched',1500,1500,450,'{"stops":[{"id":9892021,"ordering":1,"stop_type":"pickup","schedule_type":"appointment","scheduled_date":"${today}","scheduled_time":"15:00:00","location":{"city":"Oklahoma City","state":"OK"}},{"id":9892022,"ordering":2,"stop_type":"delivery","location":{"city":"Kansas City","state":"MO"}}]}'),
(989203,'REVIEW-BOARD-DISPATCH','dispatched',1500,1500,450,'{}') ON CONFLICT(id) DO NOTHING;
INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id,driver_rate) VALUES('${driver}','${week}','${number}',989201,88888.88) ON CONFLICT DO NOTHING;`);

const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1920, height: 1000 } });
  const errors = []; page.on('pageerror', error => errors.push(error.message));
  expect((await page.request.post(`${base}/api/auth/login`, { data: { email, password }, headers: { origin: base } })).ok()).toBe(true);
  const session = await (await page.request.get(`${base}/api/auth/session`)).json();
  const headers = { origin: base, 'x-csrf-token': session.csrfToken };
  const read = async () => (await (await page.request.get(`${base}/api/driver-board/loads/${driver}`)).json());
  const readEntry = async () => (await (await page.request.get(`${base}/api/driver-board?weekStart=${week}`)).json()).entries.find(entry => entry.driverId === driver);
  const action = async data => {
    const entry = await readEntry(), loads = await read();
    const response = await page.request.post(`${base}/api/driver-board/loads/${driver}`, { headers, data: { ...data, version: entry.version, revision: loads.revision, fromDate: loads.fromDate } });
    expect(response.ok(), await response.text()).toBe(true);
    return response.json();
  };
  // Allow rerunning the retained synthetic review.
  await action({ action: 'clear' });
  sql(`UPDATE driver_board_load_state SET payload='{}'::jsonb WHERE driver_id='${driver}';`);
  await page.goto(`${base}/driver-board`);
  const field = name => page.getByLabel(`Avery Bennett · ${name}`, { exact: true });
  const saved = () => expect(page.getByRole('status').filter({ hasText: /^Saved$/ })).toBeVisible();
  await field('Current load').fill(number);
  await field('Trailer').fill('HMKD 12345');
  await saved();
  await expect(field('Destination')).toHaveValue('Atlanta, GA');
  const beforeClick = await read();
  await field('Destination').click();
  expect((await read()).stopKey).toBe(beforeClick.stopKey);
  await field('Destination').fill('4500 Very Long Custom Distribution Center Boulevard, Atlanta, GA 30301');
  await saved();
  await page.reload();
  await expect(field('Destination')).toHaveValue('4500 Very Long Custom Distribution Center Boulevard, Atlanta, GA 30301');
  expect((await read()).destinationSource).toBe(false);
  const destinationWidth = await field('Destination').evaluate(el => ({ width: el.clientWidth, scroll: el.scrollWidth }));
  expect(destinationWidth.width).toBeLessThanOrEqual(280);
  expect(destinationWidth.scroll).toBeGreaterThan(destinationWidth.width);
  await expect(field('Destination')).toHaveAttribute('title', '4500 Very Long Custom Distribution Center Boulevard, Atlanta, GA 30301');
  expect(await field('Destination').evaluate(el => el.closest('td').getBoundingClientRect().width)).toBeLessThanOrEqual(280);
  await field('Destination').fill(''); await saved();
  await expect(field('Destination')).toHaveValue('Atlanta, GA');
  for (const status of ['HOME', 'HOME (YARD)', 'VACATION']) {
    await field('Status').selectOption(status);
    if (status !== 'VACATION') await page.getByLabel('Home start date', { exact: true }).fill(today);
    await page.getByRole('button', { name: 'Apply dates', exact: true }).click();
    await expect.poll(async () => (await readEntry()).status).toBe(status);
    await expect(field('Destination')).toHaveValue('Louisville, KY');
    const custom = `Custom destination for ${status}`;
    await field('Destination').fill(custom);
    await expect.poll(async () => (await readEntry()).destination).toBe(custom);
    await page.reload();
    await expect(field('Destination')).toHaveValue(custom);
    await field('Destination').fill('');
    await expect.poll(async () => (await readEntry()).destination).toBe('');
    await expect(field('Destination')).toHaveValue('Louisville, KY');
  }
  await field('Status').selectOption('DISPATCHED');
  await expect.poll(async () => (await readEntry()).status).toBe('DISPATCHED');
  await expect(field('Destination')).toHaveValue('Atlanta, GA');
  expect((await read()).stopKey).toBe(beforeClick.stopKey);
  await field('Advance load').click();
  const dialog = page.getByRole('dialog', { name: 'Next stop · Avery Bennett' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('radio')).toHaveCount(3);
  await expect(dialog.getByText('Pickup · Current destination', { exact: true })).toBeVisible();
  await dialog.getByLabel('Fort Worth, TX', { exact: true }).check();
  await dialog.getByRole('button', { name: 'Apply destination', exact: true }).click();
  await expect(dialog).toHaveCount(0); await saved();
  await expect(field('Destination')).toHaveValue('Fort Worth, TX');
  await expect(field('Status')).toHaveValue('ENROUTE');
  await field('Advance load').click();
  await expect(dialog.getByRole('radio')).toHaveCount(3);
  await dialog.getByLabel('Atlanta, GA', { exact: true }).check();
  await dialog.getByRole('button', { name: 'Apply destination', exact: true }).click();
  await saved();
  await expect(field('Status')).toHaveValue('DISPATCHED');
  await expect(field('Destination')).toHaveValue('Atlanta, GA');
  expect((await read()).completedStopKeys ?? []).toEqual([]);
  await page.reload();
  await expect(field('Status')).toHaveValue('DISPATCHED');
  await expect(field('Destination')).toHaveValue('Atlanta, GA');
  await field('Advance load').click();
  await dialog.getByLabel('Fort Worth, TX', { exact: true }).check();
  await dialog.getByRole('button', { name: 'Apply destination', exact: true }).click();
  await saved();
  await field('Advance load').click();
  await expect(dialog.getByLabel('Dallas, TX', { exact: true })).toBeChecked();
  // Opening/cancelling does not complete any stop.
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  expect((await read()).completedStopKeys ?? []).toEqual([]);
  await field('Advance load').click();
  await dialog.getByRole('button', { name: 'Apply destination', exact: true }).click();
  await saved(); await expect(field('Destination')).toHaveValue('Dallas, TX');
  await expect(field('Current load')).toHaveValue(number);
  expect((await read()).completedStopKeys).toHaveLength(1);
  await page.reload(); await expect(field('Destination')).toHaveValue('Dallas, TX');
  await field('Advance load').click(); await saved();
  await expect(field('Current load')).toHaveValue('');
  // Undo restores the final delivery and its completed-stop receipt.
  await page.getByRole('heading', { name: 'Status Board', exact: true }).click();
  await page.keyboard.press('Control+z'); await saved();
  await expect(field('Current load')).toHaveValue(number);
  expect((await read()).completedStopKeys).toHaveLength(1);
  // Leave an illustrative multi-stop load and next pickup available for review.
  sql(`UPDATE driver_board_load_state SET payload=payload-'completedStops'-'completed' WHERE driver_id='${driver}';`);
  const loads = await read();
  await action({ action: 'source', stopKey: loads.current.stops[1].key });
  if (!(await read()).next.some(load => load.number === 'NEXT-LONG-LOAD-123456789')) await action({ action: 'add', number: 'NEXT-LONG-LOAD-123456789' });
  await page.reload(); await expect(field('Status')).toHaveValue('RESERVED');
  await expect(field('Advance load')).toHaveText('Reserved');
  await expect(field('Advance load')).toHaveCSS('text-align', 'center');
  expect(await field('Advance load').evaluate(el => getComputedStyle(el).fontSize)).toBe(await field('Trailer').evaluate(el => getComputedStyle(el).fontSize));
  const row = page.locator(`tr[data-driver-id="${driver}"]`);
  await expect(row.locator('td').nth(3)).toHaveText('$99,999.99');
  await expect(row.locator('td').nth(3)).toHaveCSS('text-align', 'center');
  const widths = async () => row.evaluate(el => [4, 5, 6].map(i => el.children[i].getBoundingClientRect().width));
  const wide = await widths();
  await page.setViewportSize({ width: 1280, height: 800 });
  expect(await widths()).toEqual(wide);
  for (const name of ['Phone', 'Current load', 'Destination', 'ETA', 'Next load', 'Next pickup time']) {
    expect(await field(name).evaluate(el => el.scrollWidth <= el.clientWidth + 1), `${name} is clipped`).toBe(true);
  }
  const nextSpacing = await row.evaluate(el => { const number = el.querySelector('button[aria-label$=" · Next load"]'); const pickup = el.querySelector('button[aria-label$=" · Next pickup time"]'); return pickup.getBoundingClientRect().left - number.getBoundingClientRect().right; });
  expect(nextSpacing).toBe(8);
  await expect(field('Next load')).toHaveAttribute('title', /NEXT-LONG-LOAD-123456789/);
  await page.setViewportSize({ width: 3840, height: 1000 });
  expect(await field('Next load').evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
  expect(await field('Destination').evaluate(el => el.closest('td').getBoundingClientRect().width)).toBeLessThanOrEqual(280);
  expect(await page.locator('aside').first().evaluate(el => el.getBoundingClientRect().width)).toBe(208);
  expect(errors).toEqual([]);
  // Fill two more review rows using the normal versioned save API.
  const board = await (await page.request.get(`${base}/api/driver-board?weekStart=${week}`)).json();
  const examples = [
    { id: '63000000-0000-0000-0000-000000000011', status: 'READY', trailerNumber: 'JBHZ 45678', destination: '123 Custom Warehouse Road, Indianapolis, IN', notes: 'Custom address override' },
    { id: '63000000-0000-0000-0000-000000000010', status: 'DISPATCHED', currentLoad: 'REVIEW-BOARD-DISPATCH', trailerNumber: 'TA 20986', eta: '14:30', notes: 'Pickup appointment' },
  ];
  const sampleSave = await page.request.put(`${base}/api/driver-board`, { headers, data: { entries: examples.map(({ id, ...values }) => ({ ...board.entries.find(entry => entry.driverId === id), ...values, statusEdited: true })) } });
  expect(sampleSave.ok(), await sampleSave.text()).toBe(true);
  await page.setViewportSize({ width: 1920, height: 1000 });
  await page.reload();
  await expect(field('Destination')).toHaveValue('Dallas, TX');
  await page.locator('[data-scroll-key="driver-board"]').evaluate(el => { el.scrollLeft = 0; });
  await page.screenshot({ path: join(dirname(accessFile), 'status-board-review.png'), fullPage: true });
  console.log('Passed: saved custom destinations in dispatch and leave statuses, clearing overrides, out-of-order deliveries, cancel, intermediate/final completion, undo, reload, fixed money/phone widths and content sizing. Synthetic review data retained.');
} finally { await browser.close(); }
