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
  const listenerPids = new Map();
  const results = [];
  const measuredBoards = new Set();
  const checkTiming = req => {
    const samples = req.headers()['x-clustie-board-timing'] || '';
    for (const sample of samples.split(',')) {
      const paired = /^(status|gross)-open=\d+(?:\/\d+){4}~(\d+)\/(\d+)\/(\d+)\/(\d+)\/(\d+)\/(\d+)$/.exec(sample);
      if (paired && Number(paired[4]) > Number(paired[3]) && Number(paired[7]) > 0) measuredBoards.add(paired[1]);
    }
  };
  page.on('request', checkTiming);
  for (const enabled of [false, true]) {
    parallel = enabled;
    await page.goto(base + '/investors');
    await expect(page.getByRole('heading', {name:'Investors',exact:true})).toBeVisible();
    const opened = new Set();
    for (const [name, route] of [['Status Board','driver-board'], ['Gross Board','gross-board'], ['Status Board','driver-board'], ['Gross Board','gross-board']]) {
      const returning = opened.has(route);
      if (returning && route === 'driver-board') {
        const note = enabled ? 'Parallel return edit' : 'Return edit';
        sql(`SET search_path TO ${schema},public; INSERT INTO driver_board(driver_id,notes)
          SELECT id,'${note}' FROM drivers WHERE full_name='E2e Driver' ON CONFLICT(driver_id) DO UPDATE SET notes=excluded.notes,version=driver_board.version+1;`);
      }
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
      expect(board._sections.drivers).toMatch(/^[A-Za-z0-9_-]{43}$/);
      if (returning) {
        expect(board).not.toHaveProperty('drivers');
        if (route === 'driver-board') {
          const note = enabled ? 'Parallel return edit' : 'Return edit';
          expect(board.entries.some(e => e.notes === note)).toBe(true);
          await expect(page.getByRole('textbox', {name:'E2e Driver · Notes',exact:true})).toHaveValue(note);
        }
      } else {
        expect(board.drivers.length).toBeGreaterThan(0);
      }
      opened.add(route);
      await expect(page.locator(grid).first()).toBeVisible();
      const clickToPaintMS = await page.evaluate(() => window.boardClickPaint);
      const channel = route === 'driver-board' ? 'mserp_driver_board' : 'mserp_gross_board';
      const listener = () => sql(`COPY (SELECT pid FROM pg_stat_activity WHERE application_name='${schema}' AND query='LISTEN "${channel}"') TO STDOUT`).trim();
      await expect.poll(listener).toMatch(/^\d+$/);
      const pid = listener();
      if (listenerPids.has(channel)) expect(pid).toBe(listenerPids.get(channel));
      else listenerPids.set(channel, pid);
      results.push({ route, parallel: enabled, streamToGetMS: Math.round(sentAt - subscription.at), bytes:(await response.body()).length, clickToPaintMS });
    }
  }
  console.log('Actual browser/API/PostgreSQL opening at 178 ms RTT', JSON.stringify(results));
  page.off('request', checkTiming);
  expect([...measuredBoards].sort()).toEqual(['gross', 'status']);
  setNetworkLatency(0);
  await page.unrouteAll({behavior: 'wait'});
  await checkTruckProfileRequests(page, base, sql, schema);
  await checkFleetReturnPayloads(page, base, sql, schema);
}

async function checkTruckProfileRequests(page, base, sql, schema) {
  sql(`SET search_path TO ${schema},public;
    INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate)
    SELECT 'Prefetch Driver '||i,'prefetch driver '||i,'cpm',0 FROM generate_series(1,60) i;
    INSERT INTO trucks(unit_number,status) SELECT 'PREFETCH-'||i,'assigned' FROM generate_series(1,60) i;
    INSERT INTO truck_driver_assignments(truck_id,driver_id)
    SELECT t.id,d.id FROM generate_series(1,60) i JOIN trucks t ON t.unit_number='PREFETCH-'||i
    JOIN drivers d ON d.normalized_name='prefetch driver '||i;
    INSERT INTO loads(id,load_id,status,load_pay,total_pay,raw_payload)
    VALUES(987693,'BOARD-DONE-93','Delivered',500,500,'{"stops":[{"ordering":1,"stop_type":"pickup","location":{"city":"Dallas","state":"TX"}}]}');
    INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id)
    SELECT id,date_trunc('week',now() AT TIME ZONE 'America/New_York')::date,'BOARD-DONE-93',987693
    FROM drivers WHERE normalized_name='prefetch driver 1';`);
  await page.mouse.move(1,1);
  let requests = 0;
  const count = req => { if (new URL(req.url()).pathname.startsWith('/trucks/detail')) requests++; };
  page.on('request', count);
  try {
    const snapshot = page.waitForResponse(res => {
      const url = new URL(res.url());
      return url.pathname === '/api/driver-board' && url.searchParams.get('weekPreview') === '1' && res.status() === 200;
    });
    await page.goto(base+'/driver-board');
    const payload = await (await snapshot).json();
    const board = payload.board ?? payload;
    const driver = board.drivers.find(row => row.fullName === 'Prefetch Driver 1');
    const weekly = board.loads[driver.id].week.map(i => board.plans[i]);
    expect(weekly.find(load => load.number === 'BOARD-DONE-93')).toMatchObject({completed:true,stops:[]});
    const full = await (await page.request.get(base+'/api/driver-board/loads/'+driver.id)).json();
    expect(full.week.find(load => load.number === 'BOARD-DONE-93').stops[0].location).toBe('Dallas, TX');
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


async function checkFleetReturnPayloads(page, base, sql, schema) {
  sql(`SET search_path TO ${schema},public;
    INSERT INTO gross_board_entries(driver_id,service_date,load_number,driver_rate)
    SELECT d.id,date_trunc('week',now() AT TIME ZONE 'America/New_York')::date+i,'RETURN-'||d.id||'-'||i,1000
    FROM drivers d CROSS JOIN generate_series(1,4) i WHERE d.normalized_name LIKE 'prefetch driver %';`);
  await page.goto(base + '/investors');
  const firstBytes = new Map();
  for (let i = 0; i < 2; i++) {
    for (const [name, path, grid] of [['Status Board','driver-board','tr[data-driver-id]'],['Gross Board','gross-board','[data-gross-field="truck"]']]) {
      const read = page.waitForResponse(r => r.status() === 200 && r.request().method() === 'GET' && new URL(r.url()).pathname === '/api/'+path);
      await page.getByRole('link',{name,exact:true}).click();
      const response = await read;
      const payload = await response.json();
      const board = payload.board ?? payload;
      const bytes = (await response.body()).length;
      await expect(page.locator(grid).first()).toBeVisible();
      if (!i) {
        expect(board.drivers.length).toBeGreaterThanOrEqual(60);
        firstBytes.set(path, bytes);
      } else {
        expect(board).not.toHaveProperty('drivers');
        expect(bytes).toBeLessThan(firstBytes.get(path) / 5);
        console.log(name, '60-driver opening response bytes', firstBytes.get(path), 'return', bytes);
      }
    }
  }
}
