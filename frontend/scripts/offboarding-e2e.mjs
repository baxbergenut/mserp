import { expect } from '@playwright/test';
import { createHmac, randomUUID } from 'node:crypto';

export async function runOffboardingE2E({ page, base, apiBase, webhookCompany, webhookSecret, sql, schema }) {
  const driverId = randomUUID(), truckId = randomUUID(), dispatcherId = randomUUID();
  sql(`SET search_path TO ${schema},public;
    INSERT INTO dispatchers(id,full_name,normalized_name) VALUES('${dispatcherId}','Offboarding Dispatch','offboarding dispatch');
    INSERT INTO trucks(id,unit_number,status) VALUES('${truckId}','OFFBOARD-E2E','assigned');
    INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,dispatcher_id) VALUES('${driverId}','Offboarding Driver','offboarding driver','cpm',0.75,'${dispatcherId}');
    INSERT INTO truck_driver_assignments(driver_id,truck_id) VALUES('${driverId}','${truckId}');`);
  const session = await (await page.request.get(`${base}/api/auth/session`)).json();
  const write = async (path, data) => {
    const response = await page.request.put(`${base}/api${path}`, { data, headers: { 'X-CSRF-Token': session.csrfToken } });
    expect(response.status(), await response.text()).toBe(200);
  };
  const getDriver = async () => (await page.request.get(`${base}/api/drivers/${driverId}`)).json();
  const getTruck = async () => (await page.request.get(`${base}/api/trucks/${truckId}`)).json();
  await page.goto(`${base}/drivers`);
  await page.getByPlaceholder('Search drivers…').fill('Offboarding Driver');
  await page.getByRole('row').filter({ hasText: 'Offboarding Driver' }).getByRole('button', { name: 'Edit', exact: true }).click();
  await page.getByLabel('Driver status', { exact: true }).selectOption('terminated');
  await expect(page.getByLabel(/^Dispatcher/)).toHaveValue('');
  await expect(page.getByLabel(/^Truck/)).toBeDisabled();
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(await getDriver()).toMatchObject({ active: false, truckId: null, dispatcherId: null });
  expect(await getTruck()).toMatchObject({ driverId: null, status: 'available' });
  await write(`/drivers/${driverId}`, { fullName: 'Offboarding Driver', payType: 'cpm', payRate: 0.75, active: true, truckId, dispatcherId });
  await page.goto(`${base}/trucks`);
  await page.getByPlaceholder('Search trucks…').fill('OFFBOARD-E2E');
  await page.getByRole('row').filter({ hasText: 'OFFBOARD-E2E' }).getByRole('button', { name: 'Edit', exact: true }).click();
  await page.getByLabel(/^Active truck/).uncheck();
  await expect(page.getByLabel(/^Assigned driver/)).toHaveValue('');
  await expect(page.getByLabel(/^Assigned driver/)).toBeDisabled();
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(await getTruck()).toMatchObject({ active: false, driverId: null });
  expect(await getDriver()).toMatchObject({ active: true, truckId: null, dispatcherId });
  await write(`/trucks/${truckId}`, { unitNumber: 'OFFBOARD-E2E', status: 'available', active: true, driverId });
  const sourceId = randomUUID();
  const send = async (event, path) => {
    const body = JSON.stringify(event), stamp = String(Math.floor(Date.now() / 1000));
    const signature = createHmac('sha256', webhookSecret).update(`${stamp}.${body}`).digest('hex');
    const response = await fetch(`${apiBase}/integrations/fleetscope/${path}`, { method: 'POST', body, headers: {
      'Content-Type': 'application/json', 'X-FleetScope-Timestamp': stamp, 'X-FleetScope-Signature': `v1=${signature}`,
    } });
    expect(response.status).toBe(200);
    return response.json();
  };
  const hired = await send({ version: 1, eventId: randomUUID(), companyId: webhookCompany, type: 'driver.hired', occurredAt: new Date().toISOString(),
    driver: { id: sourceId, fullName: 'Offboarding Driver', driverType: 'company', hireDate: '2026-01-01' } }, 'driver-hired');
  const linked = await page.request.post(`${base}/api/driver-intake/${hired.intakeId}/complete`, { data: { linkDriverId: driverId }, headers: { 'X-CSRF-Token': session.csrfToken } });
  expect(linked.status()).toBe(200);
  await page.goto(`${base}/tasks`);
  await expect(page.getByRole('heading', { name: 'Tasks', exact: true })).toBeVisible();
  // Exercise an idle SSE heartbeat before the webhook. A per-frame write
  // deadline must not expire while the stream waits for the next task.
  await page.waitForTimeout(27000);
  const term = { version: 1, eventId: randomUUID(), companyId: webhookCompany, type: 'driver.terminated', occurredAt: new Date().toISOString(),
    terminationDate: '2026-01-01', driver: { id: sourceId, fullName: 'Offboarding Driver' } };
  expect(await send(term, 'driver-terminated')).toMatchObject({ status: 'accepted' });
  const task = page.getByRole('row').filter({ hasText: 'Offboard Offboarding Driver' });
  await expect(task).toBeVisible({ timeout: 25000 }); // The open task list discovers webhook arrivals.
  await task.getByRole('button', { name: 'Offboard Offboarding Driver', exact: true }).click();
  await expect(page.getByRole('dialog')).toContainText('current truck and dispatcher assignments disconnected');
  expect(await getDriver()).toMatchObject({ active: false, truckId: null, dispatcherId: null, payRate: 0.75 });
  expect(await getTruck()).toMatchObject({ active: true, driverId: null, status: 'available' });
  await page.getByRole('checkbox', { name: 'Equipment and documents collected' }).check();
  await page.getByRole('checkbox', { name: 'Fuel/toll cards and external access closed' }).check();
  await page.getByRole('checkbox', { name: 'Driver status, charges and final settlement reviewed' }).check();
  await page.getByRole('button', { name: 'Confirm offboarding', exact: true }).click();
  await expect(task).toHaveCount(0);
  expect(await send(term, 'driver-terminated')).toMatchObject({ status: 'duplicate' });
  await page.getByLabel('Task status').selectOption('all');
  await expect(task).toHaveCount(1);
  await expect(task).toContainText('Completed');
  console.log('Offboarding E2E passed: driver/truck inactive forms, signed hire/termination, idle task arrival, completion and retry deduplication.');
}
