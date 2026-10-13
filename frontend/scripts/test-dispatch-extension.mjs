// Isolated tests: mocked HTTP and disposable Chromium profile; no live accounts.
import { chromium, expect } from '@playwright/test';
import assert from 'node:assert/strict';
import { readFile, mkdtemp, cp, appendFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import vm from 'node:vm';

const source = resolve('../extensions/dispatcher');
const sandbox = vm.createContext({});
vm.runInContext(await readFile(join(source, 'core.js'), 'utf8'), sandbox);
const C = sandbox.DispatchWatch;
const job = (id, price = 3102) => ({ jobIdentifier: id,
  firstPickup: { location: { city: 'LA PORTE', state: 'IN' } },
  lastDelivery: { location: { city: 'BLACK CREEK', state: 'GA' } },
  bookNowPrice: price, pricePerMile: price / 874, loadboardStatus: 'AVAILABLE' });
const response = (jobs, page = 0, totalPages = 1) => C.fromResponse({jobs, page, totalPages, totalElements: jobs.length, pageSize: 100});
assert.equal(C.defaults.interval, 5);
assert.equal(C.normalize({interval: 1}).interval, 4);
assert.equal(C.normalize({minPrice: 10000, alertExisting: true}).minPrice, undefined);
assert.equal(C.fromJob(job('TEST001', 0)).price, null);
assert.throws(() => response([], 0, -1));
assert.throws(() => response([job('TEST001'), job('TEST001')]));
const tracker = C.createTracker();
assert.equal(tracker.observe(response([job('TEST001')], 0, 2)).fresh.length, 0);
assert.equal(tracker.ready, false);
assert.equal(tracker.observe(response([job('TEST002')], 1, 2)).fresh.length, 0);
assert.equal(tracker.ready, true);
assert.equal(tracker.observe(response([job('TEST002', 9000), job('TEST003')], 0, 2)).fresh.length, 1);
assert.equal(tracker.observe(response([job('TEST001'), job('TEST003')], 1, 2)).fresh.length, 0);
assert.equal(tracker.recent.length, 1);
tracker.observe(response([]));
assert.equal(tracker.recent.length, 1, 'missing discoveries remain in history');
assert.equal(tracker.recent[0].presence, 'missing');
assert.equal(tracker.observe(response([job('TEST003')])).fresh.length, 0, 'returning IDs never re-alert');
assert.equal(tracker.recent[0].presence, 'present', 'returning discoveries regain their highlight');
assert.equal(tracker.seenCount, 3);
assert.equal(C.createTracker().observe(response([job('TEST003')])).fresh.length, 0, 'new run resets baseline');
const empty = C.createTracker(); empty.observe(response([], 0, 0));
assert.equal(empty.observe(response([job('TEST001')])).fresh.length, 1);
empty.observe(response([{...job('TEST001'), loadboardStatus: 'BOOKED'}]));
assert.equal(empty.recent.length, 1);
assert.equal(empty.recent[0].presence, 'unavailable');
assert.throws(() => C.createTracker().observe(response([], 1, 2)), /pages changed/);
const capped = C.createTracker(1); capped.observe(response([job('TEST001')]));
assert.throws(() => capped.observe(response([job('TEST002')])), /history is full/);

const profile = await mkdtemp(join(tmpdir(), 'mserp-extension-test-'));
const extension = join(profile, 'extension');
await cp(source, extension, {recursive: true});
// Observe actual sound calls without shipping a test hook in the extension.
await appendFile(join(extension, 'sound.js'), '\nconst actualPlay = DispatchWatchSound.play; DispatchWatchSound.play = (...args) => { document.documentElement.dataset.testNotifications = String(Number(document.documentElement.dataset.testNotifications || 0) + 1); return actualPlay(...args); };\n');
const context = await chromium.launchPersistentContext(join(profile, 'browser'), {headless: true, channel: 'chromium', viewport: {width: 1280, height: 800},
  args: [`--disable-extensions-except=${extension}`, `--load-extension=${extension}`]});
const errors = [], requests = [], apiChecks = [];
let revoked = false, rateLimited = false;
let pages = [[job('TESTLOW', 500), job('TEST001')], [job('TEST002', 4200)]];
try {
  await context.route('**/*', async route => {
    const url = new URL(route.request().url());
    if (url.hostname === 'erp.msexpressinc.net') {
      assert.equal(route.request().headers()['cookie'], undefined);
      if (url.pathname.endsWith('/activate')) return route.fulfill({json: {token: 'synthetic-token', dispatcherName: 'Test Dispatcher'}});
      if (url.pathname.endsWith('/check')) {
        apiChecks.push(Date.now());
        return route.fulfill({status: revoked ? 401 : 200, json: revoked ? {error: 'Access revoked'} : {dispatcherName: 'Test Dispatcher', minimumIntervalSeconds: 4}});
      }
      throw new Error(`Unexpected ERP endpoint: ${url.pathname}`);
    }
    if (url.pathname === '/carrier/secure/loadboard/graphql/native') {
      requests.push(Date.now());
      const n = route.request().postDataJSON().page;
      if (rateLimited) return route.fulfill({status: 429, json: {error: 'rate limited'}});
      return route.fulfill({json: {data: {searchJobsElastic: {success: true, status: 'OK', data: {
        page: n, pageSize: 100, totalPages: pages.length, totalElements: pages.flat().length, jobs: pages[n]
      }}}}});
    }
    if (url.pathname === '/carrier/ngx/load-board/grid') return route.fulfill({contentType: 'text/html', body: `<!doctype html><html><head><meta charset="utf-8"><title>Carrier 360 synthetic fixture</title><style>
      body{margin:16px;font:14px Arial}main{height:calc(100vh - 32px);display:flex;flex-direction:column}table{width:100%}.table-wrap{flex:1;overflow:auto}td{padding:12px;border-bottom:1px solid #ddd}.p-paginator{display:flex;align-items:center;gap:5px;flex-wrap:wrap;min-height:44px;background:#eee}.p-paginator-right-content{margin-left:auto;max-width:100%}</style></head><body><main>
      <h1>Find Loads — synthetic test</h1><div><label>Native origin <input id="origin" value="Chicago"></label><button data-cy="lb-grid-refresh-button" aria-label="Refresh">Refresh</button><button id="repaint">Repaint rows</button></div>
      <div class="table-wrap"><table><thead><tr><th>Load</th><th>Price</th></tr></thead><tbody></tbody></table></div>
      <div class="p-paginator"><button aria-label="Go to first page" disabled>First</button><button aria-label="Go to next page">Next</button><label>Rows <select><option>100</option></select></label><div class="p-paginator-right-content"></div></div></main>
      <script>let page=0;async function search(n){page=n;const endpoint='/carrier/secure/loadboard/graphql/native';const payload=JSON.stringify({page:n});let body;
      if(n===1){body=await new Promise(resolve=>{const x=new XMLHttpRequest();x.open('POST',endpoint);x.onload=()=>resolve(x.status===200?JSON.parse(x.responseText):null);x.send(payload);});}else{const r=await fetch(endpoint,{method:'POST',body:payload});if(!r.ok)return;body=await r.json();}if(!body)return;const d=body.data.searchJobsElastic.data;
      const markup=d.jobs.map(j=>'<tr data-cy="lb-table-row"><td id="jobIdentifier">'+j.jobIdentifier+'</td><td>'+j.bookNowPrice+'</td></tr>').join('');
      document.querySelector('tbody').innerHTML=markup;document.querySelector('#repaint').onclick=()=>{document.querySelector('tbody').innerHTML=markup;};
      const oldPaginator=document.querySelector('.p-paginator');const paginator=oldPaginator.cloneNode(true);paginator.querySelector('.p-paginator-right-content').replaceChildren();oldPaginator.replaceWith(paginator);
      document.querySelector('[aria-label="Go to first page"]').disabled=n===0;document.querySelector('[aria-label="Go to next page"]').disabled=n===d.totalPages-1;bindPagination();}
      function bindPagination(){document.querySelector('[aria-label="Go to first page"]').onclick=()=>search(0);document.querySelector('[aria-label="Go to next page"]').onclick=()=>search(page+1);}
      document.querySelector('[aria-label="Refresh"]').onclick=()=>search(page);bindPagination();search(0);</script></body></html>`});
    if (url.protocol === 'chrome-extension:') return route.continue();
    return route.abort();
  });
  const worker = context.serviceWorkers()[0] || await context.waitForEvent('serviceworker');
  await worker.evaluate(() => chrome.storage.local.set({settings: {minPrice: 99999, alertExisting: true, interval: 4, sound: 'silent'}}));
  const id = new URL(worker.url()).host;
  const settings = await context.newPage();
  await settings.goto(`chrome-extension://${id}/settings.html`);
  await expect(settings.locator('#identity')).toHaveText('Not activated');
  await expect(settings.locator('[name="minPrice"]')).toHaveCount(0);
  await expect(settings.locator('[name="interval"]')).toHaveValue('4');
  await settings.locator('#code').fill('synthetic-code');
  await settings.getByRole('button', {name: 'Activate', exact: true}).click();
  await expect(settings.locator('#identity')).toHaveText('Activated for Test Dispatcher');
  await settings.getByRole('button', {name: 'Save settings', exact: true}).click();
  const page = await context.newPage(); page.on('pageerror', e => errors.push(e.message));
  await page.goto('https://scm.jbhunt.com/carrier/ngx/load-board/grid');
  const bar = page.locator('#mserp-dispatch-watch'), status = bar.locator('#status');
  await expect(page.locator('.p-paginator-right-content #mserp-dispatch-watch')).toBeVisible();
  await page.getByRole('button', {name: '▶ Play', exact: true}).click();
  await expect(status).toContainText('Running', {timeout: 15000});
  assert.equal(await page.locator('html').getAttribute('data-test-notifications'), null, 'all pages form a silent baseline');
  await expect(bar.locator('#new')).toHaveText('New loads (0)');
  pages[0].push(job('NEW0001', 800));
  await expect(page.locator('tbody tr').first()).toContainText('NEW0001', {timeout: 12000});
  await expect(page.locator('.mserp-watch-match')).toHaveCount(1);
  await expect(page.locator('html')).toHaveAttribute('data-test-notifications', '1');
  await expect(bar.locator('#new')).toHaveText('New loads (1)');
  const beforePopover = await page.locator('.p-paginator').boundingBox();
  await bar.locator('#new').click();
  await expect(bar.locator('#loads')).toContainText('NEW0001');
  assert.deepEqual(await page.locator('.p-paginator').boundingBox(), beforePopover, 'popover does not displace pagination');
  await page.screenshot({path: join(profile, 'new-load.png')});
  await bar.locator('#new').click();
  pages = [[job('TESTLOW', 500), job('TEST001', 9999)], [job('NEW0001', 1600), job('TEST002', 4200)]];
  await expect(status).toContainText('page 2/2', {timeout: 10000});
  assert.equal(await page.locator('html').getAttribute('data-test-notifications'), '1', 'page moves and price changes do not alert');
  await expect(page.locator('.mserp-watch-match')).toHaveCount(1);
  await page.getByRole('button', {name: 'Ⅱ Pause', exact: true}).click();
  await expect(page.locator('.mserp-watch-match')).toHaveCount(1, {timeout: 3000});
  await page.getByRole('button', {name: 'Repaint rows', exact: true}).click();
  await expect(page.locator('.mserp-watch-match')).toHaveCount(1);
  await expect(page.locator('tbody tr').first()).toContainText('NEW0001');
  await page.locator('#origin').fill('Dallas');
  await expect(page.locator('.mserp-watch-match')).toHaveCount(1);
  await page.getByRole('button', {name: '▶ Play', exact: true}).click();
  await expect(status).toContainText('Running', {timeout: 15000});
  await expect(bar.locator('#new')).toHaveText('New loads (0)');
  assert.equal(await page.locator('html').getAttribute('data-test-notifications'), '1', 'restart silently establishes a fresh baseline');
  pages[0].push(job('NEW0002', 1200));
  await expect(page.locator('tbody tr').first()).toContainText('NEW0002', {timeout: 12000});
  await expect(page.locator('html')).toHaveAttribute('data-test-notifications', '2');
  await expect(status).toContainText('page 2/2', {timeout: 10000});
  await expect(status).toContainText('page 1/2', {timeout: 10000});
  await expect(page.locator('tbody tr').first()).toContainText('NEW0002');
  await expect(page.locator('.mserp-watch-match')).toHaveCount(1);
  assert.equal(await page.locator('html').getAttribute('data-test-notifications'), '2', 'unchanged refresh retains highlight without another sound');
  pages[0] = pages[0].filter(j => j.jobIdentifier !== 'NEW0002');
  await expect(bar.locator('#loads')).toContainText('No longer in results', {timeout: 16000});
  await expect(bar.locator('#new')).toHaveText('New loads (1)');
  await bar.locator('#new').click();
  await expect(bar.locator('#loads')).toContainText('NEW0002');
  await page.screenshot({path: join(profile, 'missing-load.png')});
  await bar.locator('#new').click();
  pages[0].push(job('NEW0002', 1250));
  await expect(page.locator('tbody tr').first()).toContainText('NEW0002', {timeout: 12000});
  await expect(page.locator('.mserp-watch-match')).toHaveCount(1);
  assert.equal(await page.locator('html').getAttribute('data-test-notifications'), '2', 'returning discovery is highlighted without duplicate sound');
  revoked = true;
  await expect(status).toContainText('Access revoked', {timeout: 12000});
  const before = requests.length;
  await page.waitForTimeout(4500);
  assert.equal(requests.length, before, 'revocation stops vendor requests');
  revoked = false; rateLimited = true;
  await page.getByRole('button', {name: '▶ Play', exact: true}).click();
  await expect(status).toContainText('rate limited', {timeout: 12000});
  for (let i = 2; i < requests.length; i++) assert.ok(requests[i] - requests[i - 1] >= 3900, 'automatic requests must be spaced at least 4s');
  assert.ok(apiChecks.length >= 2);
  await page.setViewportSize({width: 650, height: 650});
  await expect(page.getByRole('button', {name: 'Go to next page'})).toBeInViewport();
  await expect(bar.locator('#play')).toBeInViewport();
  await expect(bar.locator('#settings')).toBeInViewport();
  await page.screenshot({path: join(profile, 'compact.png')});
  await settings.screenshot({path: join(profile, 'settings.png'), fullPage: true});
  assert.deepEqual(errors, []);
  console.log('PASS: silent all-page baseline, ID history, only-new sound, promotion, native filters, restart, footer, settings migration, spacing, revocation and 429 stops.');
  console.log(`Previews: ${profile}`);
} finally { await context.close(); }
