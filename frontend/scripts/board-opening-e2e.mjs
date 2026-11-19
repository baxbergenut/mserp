import { expect } from '@playwright/test';

// Same signed-in browser, API and PostgreSQL fixture. Only the negotiated opening
// capability changes between the two paths.
export async function runBoardOpeningE2E({ page, base, setNetworkLatency }) {
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
}
