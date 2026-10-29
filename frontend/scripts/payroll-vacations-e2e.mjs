import { expect } from '@playwright/test';
import { randomUUID } from 'node:crypto';

export async function runPayrollVacationsE2E({ page, base, sql, schema }) {
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'America/New_York' }).format(new Date());
  const add = (day, offset) => { const d = new Date(day + 'T12:00:00Z'); d.setUTCDate(d.getUTCDate() + offset); return d.toISOString().slice(0, 10); };
  const current = add(today, -(new Date(today + 'T12:00:00Z').getUTCDay() + 6) % 7);
  const week = add(current, -7), start = add(week, -7);
  const owner = randomUUID(), hired = randomUUID(), investor = randomUUID(), independent = randomUUID(), ownTruck = randomUUID(), hiredTruck = randomUUID();
  sql(`SET search_path TO ${schema},public;
    INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,hire_date) VALUES
      ('${owner}','Vacation Owner','vacation owner','gross_percentage',88,'${start}'),
      ('${hired}','Vacation Employee','vacation employee','cpm',0.7,'${start}');
    INSERT INTO investors(id,full_name,driver_id) VALUES('${investor}','Vacation Owner','${owner}');
    INSERT INTO investors(id,full_name) VALUES('${independent}','Vacation Investor');
    INSERT INTO trucks(id,unit_number,owner_id) VALUES('${ownTruck}','VAC-OWN','${investor}'),('${hiredTruck}','VAC-HIRED','${independent}');
    UPDATE truck_ownership_history SET assigned_at='${start} 00:00-04',start_known=true WHERE truck_id IN ('${ownTruck}','${hiredTruck}');
    INSERT INTO truck_driver_assignments(truck_id,driver_id,assigned_at) VALUES
      ('${ownTruck}','${owner}','${start} 00:00-04'),('${hiredTruck}','${hired}','${start} 00:00-04');
    INSERT INTO fleet_status_history(entity_kind,entity_id,entity_name,track,previous_status,status,effective_at,actor_name,source)
      SELECT 'driver',id,full_name,'availability','','active','${start} 00:00-04','Test','profile' FROM drivers WHERE id IN ('${owner}','${hired}');
    UPDATE fleet_status_control SET enabled=true;
  `);
  const session = await (await page.request.get(base + '/api/auth/session')).json();
  const headers = { 'X-CSRF-Token': session.csrfToken, Origin: base };
  const api = async (method, path, data) => {
    const r = await page.request[method](base + '/api' + path, { headers, ...(data ? { data } : {}) });
    expect(r.ok(), path + ': ' + await r.text()).toBe(true);
    return r.status() === 204 ? null : r.json();
  };
  await api('put', '/truck-charges/terms', { truckId: hiredTruck, ownerId: independent, weekStart: start, sharePercent: '88', version: 0 });
  const type = await api('post', '/driver-charges/types', { name: 'Vacation insurance', direction: 'charge', amounts: ['100.00'], eligibility: 'calendar', version: 0 });
  for (const truckId of [ownTruck, hiredTruck]) {
    await api('put', '/truck-charges/recurring', { truckId, typeId: type.id, weekStart: start, amount: '100.00', included: true, version: 0, typeVersion: type.version });
  }
  const report = (kind, selected = week, suffix = '') => api('get', `/${kind}-pay?weekStart=${selected}${suffix}`);
  const initial = await report('driver');
  expect(initial.drivers.some(d => d.id === owner)).toBe(true);
  expect((await report('investor')).drivers.some(d => d.id === hiredTruck)).toBe(true);
  // Check the real profile status API, including durable status-history capture.
  const availability = async (id, status, date) => {
    const driver = await api('get', '/drivers/' + id);
    return api('patch', `/drivers/${id}/availability`, { status, version: driver.statusVersion, effectiveAt: date + 'T12:00:00Z', reason: 'Vacation payroll regression' });
  };
  for (const id of [owner, hired]) await availability(id, 'vacation', week);
  for (const kind of ['driver', 'investor']) {
    const target = kind === 'driver' ? owner : hiredTruck;
    for (const suffix of ['', '&summary=1&page=1&pageSize=100', '&summary=1&page=1&pageSize=100&detailIds=' + target]) {
      expect((await report(kind, week, suffix)).drivers.some(d => d.id === target)).toBe(false);
    }
    await page.goto(`${base}/accounting/${kind}-pay?weekStart=${week}`);
    await page.getByPlaceholder(kind === 'driver' ? 'Driver, truck, or load…' : 'Investor, truck, or load…').fill('Vacation');
    await expect(page.getByText('No statements match this view.', { exact: true })).toBeVisible();
  }
  await page.goto(`${base}/gross-board?driverId=${owner}&weekStart=${week}`);
  await page.getByPlaceholder('Search drivers…').fill('Vacation');
  await expect(page.locator(`[data-gross-driver="${owner}"]`)).toHaveCount(0);
  await expect(page.locator(`[data-gross-driver="${hired}"]`)).toHaveCount(0);
  const board = await api('get', '/gross-board?weekStart=' + week);
  expect(board.drivers.some(d => [owner, hired].includes(d.id))).toBe(false);
  const stale = initial.drivers.find(d => d.id === owner).edits;
  expect((await page.request.put(base + '/api/driver-pay', { headers, data: stale })).ok()).toBe(false);
  // Returning restores this week's fee, keeps the preceding vacation exempt,
  // and does not charge extra for the missed week.
  for (const id of [owner, hired]) await availability(id, 'active', current);
  expect((await report('driver')).drivers.some(d => d.id === owner)).toBe(false);
  expect((await report('investor')).drivers.some(d => d.id === hiredTruck)).toBe(false);
  const returned = (await report('driver', current)).drivers.find(d => d.id === owner);
  expect(returned.autoCharges.find(c => c.name === 'Vacation insurance').amount).toBe('-100.00');
  const returnedInvestor = (await report('investor', current)).drivers.find(d => d.id === hiredTruck);
  expect(returnedInvestor.autoCharges.find(c => c.name === 'Vacation insurance').amount).toBe('-100.00');
  await page.goto(`${base}/accounting/driver-pay?weekStart=${current}`);
  await expect(page.getByRole('table', { name: 'Weekly driver pay' })).toContainText('Vacation Owner');
  console.log('Vacation E2E passed: real status API/history, all three pages, full/summary/detail parity, stale-save rejection, historical exemption and automatic fee resumption.');
}
