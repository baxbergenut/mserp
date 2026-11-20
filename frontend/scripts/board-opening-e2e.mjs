import { expect } from '@playwright/test';

// Same signed-in browser, API and PostgreSQL fixture. Only the negotiated opening
// capability changes between the two paths.
export async function runBoardOpeningE2E({ page, base, setNetworkLatency, sql, schema }) {
  let parallel = false;
  await page.route('**/api/auth/session', async route => {
    const response = await route.fetch();
    const session = await response.json();
    expect(session.parallelBoardOpening).toBe(true);
    await route.fulfill({ response, json: { ...session, parallelBoardOpening: parallel } });
  });
  setNetworkLatency(178);
  const results = [];
  for (const enabled of [false, true]) {
    parallel = enabled;
    await page.goto(base + '/investors');
    await expect(page.getByRole('heading', {name:'Investors',exact:true})).toBeVisible();
    for (const [name, route] of [['Status Board','driver-board'], ['Gross Board','gross-board'], ['Status Board','driver-board'], ['Gross Board','gross-board']]) {
      const grid = route === 'driver-board' ? 'tr[data-driver-id]' : '[data-gross-field="truck"]';
      await page.evaluate(selector => {
        window.boardClickPaint = new Promise(resolve => {
          document.addEventListener('click', () => {
            const start = performance.now();
            const observer = new MutationObserver(() => {
              if (!document.querySelector(selector)) return;
              observer.disconnect();
              requestAnimationFrame(() => requestAnimationFrame(() => resolve(Math.round(performance.now() - start))));
            });
            observer.observe(document.body, {childList: true, subtree: true});
          }, {capture:true, once:true});
        });
      }, grid);
      const stream = page.waitForRequest(req => new URL(req.url()).pathname === '/api/' + route + '/events')
        .then(req => ({req, at:performance.now()}));
      const sent = page.waitForRequest(req => new URL(req.url()).pathname === '/api/' + route && req.method() === 'GET')
        .then(() => performance.now());
      const payload = page.waitForResponse(res => new URL(res.url()).pathname === '/api/' + route && res.request().method() === 'GET');
      await page.getByRole('link', {name,exact:true}).click();
      const [subscription, response, sentAt] = await Promise.all([stream, payload, sent]);
      expect(response.status()).toBe(200);
      const result = await response.json();
      const opening = new URL(subscription.req.url()).searchParams.get('opening');
      const read = response.request();
      expect(new URL(read.url()).searchParams.get('opening')).toBe(opening);
      if (enabled) {
        expect(opening).toBeTruthy();
        expect(result.opening).toBe(opening);
      } else {
        expect(opening).toBeNull();
      }
      const board = enabled ? result.board : result;
      expect(board.drivers.length).toBeGreaterThan(0);
      await expect(page.locator(grid).first()).toBeVisible();
      const clickToPaintMS = await page.evaluate(() => window.boardClickPaint);
      results.push({ route, parallel: enabled, streamToGetMS: Math.round(sentAt - subscription.at), clickToPaintMS });
    }
  }
  console.log('Actual browser/API/PostgreSQL opening at 178 ms RTT', JSON.stringify(results));
  setNetworkLatency(0);
  await checkTruckProfileRequests(page, base, sql, schema);
}

async function checkTruckProfileRequests(page, base, sql, schema) {
  sql(`SET search_path TO ${schema},public;
    INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate)
    SELECT 'Prefetch Driver '||i,'prefetch driver '||i,'cpm',0 FROM generate_series(1,60) i;
    INSERT INTO trucks(unit_number,status) SELECT 'PREFETCH-'||i,'assigned' FROM generate_series(1,60) i;
    INSERT INTO truck_driver_assignments(truck_id,driver_id)
    SELECT t.id,d.id FROM generate_series(1,60) i JOIN trucks t ON t.unit_number='PREFETCH-'||i
    JOIN drivers d ON d.normalized_name='prefetch driver '||i;`);
  await page.mouse.move(1,1);
  let requests = 0;
  const count = req => { if (new URL(req.url()).pathname.startsWith('/trucks/detail')) requests++; };
  page.on('request', count);
  try {
    await page.goto(base+'/driver-board');
    const links = page.locator('main a[href^="/trucks/detail"]');
    await expect.poll(() => links.count()).toBe(60);
    for (let i = 0; i < 60; i += 8) await links.nth(i).scrollIntoViewIfNeeded();
    // Give Next's idle prefetch queue time to issue any viewport-triggered requests.
    await page.waitForTimeout(800);
    expect(requests).toBe(0);
    const target = new URL(await links.last().getAttribute('href'), base);
    await links.last().click();
    await expect(page).toHaveURL(target.toString());
  } finally {
    page.off('request', count);
  }
}
