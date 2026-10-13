// Isolated extension/browser tests: every HTTP request is fulfilled locally.
// Never uses the dispatcher's Chrome profile, J.B. Hunt session, or live ERP.
import { chromium, expect } from '@playwright/test';
import assert from 'node:assert/strict';
import { readFile, mkdtemp } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import vm from 'node:vm';

const extension = resolve('../extensions/dispatcher');
const sandbox = vm.createContext({});
vm.runInContext(await readFile(join(extension, 'core.js'), 'utf8'), sandbox);
const C = sandbox.DispatchWatch;
const job = (id, price = 3102) => ({ jobIdentifier: id, jobType: 'LOAD', stopCount: 2,
  equipment: { type: 'DRY VAN', subType: null }, loadedMiles: 874, totalMiles: 874, deadheadOrigin: 69,
  loadedWeight: 40161, firstPickup: { appointmentBegin: '2026-10-13 07:00:00', location: { city: 'LA PORTE', state: 'IN', zipcode: '46350' } },
  lastDelivery: { location: { city: 'BLACK CREEK', state: 'GA', zipcode: '31308' } },
  bookNowEligibility: true, bookNowPrice: price, pricePerMile: price / 874, isJobInDeferredStatus: false, loadboardStatus: 'AVAILABLE' });
assert.equal(C.defaults.interval, 5);
assert.equal(C.normalize({ interval: 1 }).interval, 4);
const load = C.fromJob(job('TEST001'));
assert.equal(load.deadhead, 69);
assert.equal(load.pickupDate, '2026-10-13');
assert.equal(C.matches(load, C.normalize({ minPrice: 3102, maxDeadhead: 69, destination: ', TX; , GA' })), true);
assert.equal(C.matches(load, C.normalize({ maxDeadhead: 68 })), false);
assert.equal(C.matches(C.fromJob({ ...job('TEST002'), loadedWeight: null }), C.normalize({ maxWeight: 50000 })), false);
assert.equal(C.matches(C.fromJob({ ...job('TEST002'), loadboardStatus: 'BOOKED' }), C.normalize()), false);
assert.equal(C.fromJob({ ...job('TEST002'), bookNowPrice: 0 }).price, null);
assert.equal(C.matches(C.fromJob({ ...job('TEST002'), isJobInDeferredStatus: true }), C.normalize({ bookableOnly: true })), false);
assert.throws(() => C.fromResponse({ jobs: [], page: 0, totalPages: -1, totalElements: 0, pageSize: 100 }));

const profile = await mkdtemp(join(tmpdir(), 'mserp-extension-test-'));
const context = await chromium.launchPersistentContext(profile, { headless: true, channel: 'chromium',
  args: [`--disable-extensions-except=${extension}`, `--load-extension=${extension}`] });
const errors = [], requests = [], apiChecks = [];
let revoked = false, rateLimited = false;
try {
  await context.route('**/*', async route => {
    const url = new URL(route.request().url());
    if (url.hostname === 'erp.msexpressinc.net') {
      assert.equal(route.request().headers()['cookie'], undefined);
      if (url.pathname.endsWith('/activate')) return route.fulfill({ json: { token: 'synthetic-token', dispatcherName: 'Test Dispatcher' } });
      if (url.pathname.endsWith('/check')) {
        apiChecks.push(Date.now());
        return route.fulfill({ status: revoked ? 401 : 200, json: revoked ? { error: 'Access revoked' } : { dispatcherName: 'Test Dispatcher', minimumIntervalSeconds: 4 } });
      }
      throw new Error(`Unexpected ERP endpoint: ${url.pathname}`);
    }
    if (url.pathname === '/carrier/secure/loadboard/graphql/native') {
      requests.push(Date.now());
      const n = route.request().postDataJSON().page;
      if (rateLimited) return route.fulfill({ status: 429, json: { error: 'rate limited' } });
      return route.fulfill({ json: { data: { searchJobsElastic: { success: true, status: 'OK', data: {
        page: n, pageSize: 100, totalPages: 2, totalElements: 3,
        jobs: n === 0 ? [job('TESTLOW', 500), job('TEST001')] : [job('TEST002', 4200)]
      } } } } });
    }
    if (url.pathname === '/carrier/ngx/load-board/grid') return route.fulfill({ contentType: 'text/html', body: `<!doctype html><html><head><title>Carrier 360 synthetic fixture</title></head><body><main>
      <h1>Find Loads — synthetic test</h1><button data-cy="lb-grid-refresh-button" aria-label="Refresh">Refresh</button><button aria-label="Go to first page" disabled>First</button><button aria-label="Go to next page">Next</button>
      <table><thead><tr><th>Load</th><th>Price</th></tr></thead><tbody></tbody></table></main>
      <script>let page=0;async function search(n){page=n;const endpoint='/carrier/secure/loadboard/graphql/native';const payload=JSON.stringify({page:n});let body;
      if(n===1){body=await new Promise(resolve=>{const x=new XMLHttpRequest();x.open('POST',endpoint);x.onload=()=>resolve(x.status===200?JSON.parse(x.responseText):null);x.send(payload);});}else{const r=await fetch(endpoint,{method:'POST',body:payload});if(!r.ok)return;body=await r.json();}if(!body)return;const d=body.data.searchJobsElastic.data;
      document.querySelector('tbody').innerHTML=d.jobs.map(j=>'<tr data-cy="lb-table-row"><td id="jobIdentifier">'+j.jobIdentifier+'</td><td>'+j.bookNowPrice+'</td></tr>').join('');
      document.querySelector('[aria-label="Go to first page"]').disabled=n===0;document.querySelector('[aria-label="Go to next page"]').disabled=n===1;}
      document.querySelector('[aria-label="Refresh"]').onclick=()=>search(page);document.querySelector('[aria-label="Go to first page"]').onclick=()=>search(0);document.querySelector('[aria-label="Go to next page"]').onclick=()=>search(1);search(0);</script></body></html>` });
    if (url.protocol === 'chrome-extension:') return route.continue();
    return route.abort();
  });
  let worker = context.serviceWorkers()[0];
  if (!worker) worker = await context.waitForEvent('serviceworker');
  const id = new URL(worker.url()).host;
  const settings = await context.newPage();
  await settings.goto(`chrome-extension://${id}/settings.html`);
  await expect(settings.locator('#identity')).toHaveText('Not activated');
  await settings.locator('#code').fill('synthetic-code');
  await settings.getByRole('button', { name: 'Activate', exact: true }).click();
  await expect(settings.locator('#identity')).toHaveText('Activated for Test Dispatcher');
  await settings.locator('[name="minPrice"]').fill('2000');
  await settings.locator('[name="interval"]').fill('4');
  await settings.locator('[name="sound"]').selectOption('silent');
  await settings.getByRole('button', { name: 'Save settings', exact: true }).click();
  const page = await context.newPage(); page.on('pageerror', e => errors.push(e.message));
  await page.goto('https://scm.jbhunt.com/carrier/ngx/load-board/grid');
  await expect(page.locator('#mserp-dispatch-watch')).toBeVisible();
  await page.getByRole('button', { name: '▶ Play', exact: true }).click();
  await expect(page.locator('#mserp-dispatch-watch #status')).toContainText('Running for', { timeout: 15000 });
  await expect(page.locator('tbody tr').first()).toContainText('TEST001');
  await expect(page.locator('.mserp-watch-match')).toHaveCount(1);
  await expect(page.locator('#mserp-dispatch-watch #count')).toHaveText('2 matching loads', { timeout: 12000 });
  for (let i = 2; i < requests.length; i++) assert.ok(requests[i] - requests[i - 1] >= 3900, 'automatic requests must be spaced at least 4s');
  assert.ok(apiChecks.length >= 2);
  revoked = true;
  await expect(page.locator('#mserp-dispatch-watch #status')).toContainText('Access revoked', { timeout: 12000 });
  await expect(page.getByRole('button', { name: '▶ Play', exact: true })).toBeVisible();
  const before = requests.length;
  await page.waitForTimeout(4500);
  assert.equal(requests.length, before, 'revocation stops vendor requests');
  revoked = false; rateLimited = true;
  await page.getByRole('button', { name: '▶ Play', exact: true }).click();
  await expect(page.locator('#mserp-dispatch-watch #status')).toContainText('rate limited', { timeout: 12000 });
  assert.deepEqual(errors, []);
  await settings.screenshot({ path: join(profile, 'settings.png'), fullPage: true });
  console.log('Dispatch extension: structured parsing, settings, activation, page scanning, promotion, spacing, revocation and 429 checks passed.');
  console.log(`Preview: ${join(profile, 'settings.png')}`);
} finally { await context.close(); }
