// Profile map lifecycle against isolated API/tile fixtures and the built UI.
import { chromium, expect } from '@playwright/test';
import { startStaticFixtureServer } from './static-fixture-server.mjs';

const driver = { id: 'demo-driver', fullName: 'Fixture Driver', active: true, status: 'active', employmentType: 'company', truckId: 'demo-truck', truckUnit: 'DEMO-101', updatedAt: '2026-11-10T08:00:00Z' };
const truck = { id: 'demo-truck', unitNumber: 'DEMO-101', driverId: driver.id, driverName: driver.fullName, status: 'active', operatingStatus: 'in_use', mileage: 125000, nextServiceMiles: null, year: 2025, make: 'Freightliner', model: 'Cascadia', updatedAt: driver.updatedAt };
const server = await startStaticFixtureServer();
let browser;
const errors = [], unexpected = [];
async function setup(reducedMotion = 'no-preference') {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, reducedMotion });
  const page = await context.newPage();
  page.on('pageerror', error => errors.push(error.message));
  const zooms = [];
  const state = { heading: 90, latitude: 41.881, longitude: -87.623 };
  await page.route('https://tile.openstreetmap.org/**', async route => {
    zooms.push(Number(new URL(route.request().url()).pathname.split('/')[1]));
    await route.fulfill({ contentType: 'image/png', body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jG1sAAAAASUVORK5CYII=', 'base64') });
  });
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname.slice(4);
    let data;
    if (route.request().method() !== 'GET' && !(path === '/auth/birthday' && route.request().method() === 'POST')) { unexpected.push(`${route.request().method()} ${path}`); await route.fulfill({ status: 403, json: { error: 'Read-only fixture' } }); return; }
    if (path === '/auth/session') data = { user: { id: 'fixture', username: 'Fixture', permissions: ['trucks.read', 'drivers.read'], expenseCategoryAccess: [] }, csrfToken: 'fixture' };
    else if (path === '/auth/birthday') data = { eligible: false, serverTime: new Date().toISOString() };
    else if (path.endsWith('/location')) data = { truckId: truck.id, truckUnit: truck.unitNumber, location: { ...state, reportedAt: new Date().toISOString(), location: 'Chicago, IL', speedMph: 0 } };
    else if (path === '/drivers/demo-driver') data = driver;
    else if (path === '/trucks/demo-truck') data = truck;
    else if (path === '/trucks/demo-truck/datatruck') data = { state: 'not_requested', version: 0 };
    else if (path === '/driver-services/demo-driver') data = { driverId: driver.id, items: [], policies: [] };
    else if (path.endsWith('/notes') || path.endsWith('/assignments')) data = [];
    else if (path === '/sidebar-counts') data = { fuel: 0, weighMyTruck: 0, tolls: 0 };
    else if (path === '/tasks/count' || path === '/feedback/messages/count') data = { count: 0 };
    else if (path === '/maintenance/access') data = {};
    else { unexpected.push(path); await route.fulfill({ status: 404, json: { error: 'Missing fixture' } }); return; }
    await route.fulfill({ json: data });
  });
  return { context, page, zooms, state };
}
async function openMap(page, kind = 'trucks', id = 'demo-truck') {
  await page.goto(`${server.base}/${kind}/detail?id=${id}`);
  const map = page.getByLabel('Truck location map', { exact: true });
  await expect(map).toBeVisible();
  // Entrance intentionally waits for the map card to enter the viewport.
  await map.scrollIntoViewIfNeeded();
  return map;
}
try {
  browser = await chromium.launch();
  for (const [kind, id] of [['trucks', truck.id], ['drivers', driver.id]]) {
    const { context, page, zooms, state } = await setup();
    const map = await openMap(page, kind, id);
    await expect.poll(() => zooms.includes(0)).toBe(true);
    await expect.poll(() => zooms.includes(10), { timeout: 12000 }).toBe(true);
    const instance = await map.evaluate(element => element._leaflet_id);
    expect(instance).toBeTruthy();
    const worldRequests = zooms.filter(zoom => zoom === 0).length;
    Object.assign(state, { heading: 180, latitude: 41.882, longitude: -87.622 });
    await page.evaluate(() => window.dispatchEvent(new Event('focus')));
    await expect(page.getByRole('img', { name: 'Reported heading 180 degrees' })).toBeVisible();
    expect(await map.evaluate(element => element._leaflet_id)).toBe(instance);
    expect(zooms.filter(zoom => zoom === 0)).toHaveLength(worldRequests);
    await page.getByRole('tab', { name: 'Assignments', exact: true }).click();
    await expect(map).toHaveCount(0);
    zooms.length = 0;
    await page.getByRole('tab', { name: 'Overview', exact: true }).click();
    await expect(map).toBeVisible();
    await map.scrollIntoViewIfNeeded();
    // Chromium can reuse decoded tile images without issuing another request.
    const tileZooms = () => map.locator('img.leaflet-tile').evaluateAll(images => images.map(image => Number(new URL(image.src).pathname.split('/')[1])));
    await expect.poll(async () => (await tileZooms()).includes(0)).toBe(true);
    await expect.poll(async () => (await tileZooms()).includes(10), { timeout: 12000 }).toBe(true);
    await page.setViewportSize({ width: 390, height: 844 });
    await map.scrollIntoViewIfNeeded();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await context.close();
  }
  const reduced = await setup('reduce');
  await openMap(reduced.page);
  await expect.poll(() => reduced.zooms.includes(10)).toBe(true);
  expect(reduced.zooms).not.toContain(0);
  await reduced.context.close();

  const interrupted = await setup();
  const map = await openMap(interrupted.page);
  await expect.poll(() => interrupted.zooms.includes(0)).toBe(true);
  await interrupted.page.getByRole('tab', { name: 'Assignments', exact: true }).click();
  await expect(map).toHaveCount(0);
  // Wait beyond the entrance delay and flight to catch callbacks after unmount.
  await interrupted.page.waitForTimeout(4200);
  expect(interrupted.zooms).not.toContain(10);
  await interrupted.context.close();
  expect(errors).toEqual([]);
  expect(unexpected).toEqual([]);
  console.log('Location entrance passed: truck/driver flight, stable live updates, reopen, reduced motion, navigation cancellation and mobile layout.');
} finally {
  await browser?.close();
  await server.close();
}
