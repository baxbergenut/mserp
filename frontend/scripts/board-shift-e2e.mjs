import { expect } from '@playwright/test';

// Use distinct browser sessions and independent drivers. Time includes the
// browser's actual grid paint and confirmation of the durable write.
export async function runBoardShift({ page, base, sql, schema, password }) {
  const week = sql(`COPY (SELECT date_trunc('week',now() AT TIME ZONE 'America/New_York')::date) TO STDOUT`).trim();
  sql(`SET search_path TO ${schema},public;
    INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate)
    SELECT 'Shift Driver '||n,'shift driver '||n,'cpm',0 FROM generate_series(1,60) n;
    INSERT INTO gross_board_entries(driver_id,service_date,load_number,original_rate,driver_rate,miles)
    SELECT id,'${week}','SHIFT-'||full_name,800,500,300 FROM drivers WHERE normalized_name LIKE 'shift driver %';
    INSERT INTO app_users(username,email,password_hash,role_id)
    SELECT 'Shift '||n,'shift-'||n||'@example.test',password_hash,role_id FROM app_users
    CROSS JOIN generate_series(1,6) n WHERE email='investor-e2e@example.com';`);
  const browser = page.context().browser();
  const contexts = await Promise.all(Array.from({length:6}, () => browser.newContext({baseURL:base,viewport:{width:1440,height:1000}})));
  try {
    const clients = await Promise.all(contexts.map(async (context, i) => {
      const login = await context.request.post('/api/auth/login',{data:{email:`shift-${i+1}@example.test`,password}});
      expect(login.status()).toBe(200);
      const tab = await context.newPage();
      const status = i % 2 === 0;
      const path = status ? 'driver-board' : 'gross-board';
      const name = 'Shift Driver '+(i+1);
      const label = status ? name+' · Notes' : name+', '+week+', driver rate';
      const speculative = [];
      tab.on('request', request => {
        if (request.headers()['next-router-prefetch'] || request.headers()['next-router-segment-prefetch'] || request.url().includes('_rsc=')) speculative.push(new URL(request.url()).pathname);
      });
      await tab.addInitScript(selector => {
        new MutationObserver((_, observer) => {
          if (window.gridPaint || !document.querySelector(selector)) return;
          observer.disconnect();
          requestAnimationFrame(() => requestAnimationFrame(() => {window.gridPaint = Math.round(performance.now());}));
        }).observe(document,{subtree:true,childList:true});
      }, status ? 'tr[data-driver-id]' : '[data-gross-field="truck"]');
      return {tab, path, status, name, input:tab.getByLabel(label,{exact:true}), speculative};
    }));
    await Promise.all(clients.map(async c => {
      await c.tab.goto(base+'/'+c.path,{waitUntil:'domcontentloaded'});
      await c.tab.waitForFunction(() => window.gridPaint > 0);
      await expect(c.input).toBeVisible();
    }));
    const results = await Promise.all(clients.map(async (c,i) => {
      const value = c.status ? 'shift edit '+i : String(570+i);
      const receipt = c.tab.waitForResponse(r => r.request().method()==='PUT' && new URL(r.url()).pathname==='/api/'+c.path);
      await c.tab.evaluate(() => document.addEventListener('input',()=> {window.editStart=performance.now();},{capture:true,once:true}));
      await c.input.focus();
      await c.input.fill(value);
      const response = await receipt;
      expect(response.ok()).toBe(true);
      await response.finished();
      await expect(c.tab.getByText('Saved',{exact:true}).first()).toBeVisible();
      const timing = await c.tab.evaluate(() => ({openMS:window.gridPaint, confirmedByMS:Math.round(performance.now()-window.editStart)}));
      const saved = sql(`SET search_path TO ${schema},public; COPY (SELECT ${c.status ? 'b.notes' : 'b.driver_rate::integer'} FROM ${c.status ? 'driver_board' : 'gross_board_entries'} b JOIN drivers d ON d.id=b.driver_id WHERE d.full_name='${c.name}' ${c.status ? '' : "AND b.service_date='"+week+"'"}) TO STDOUT`).trim();
      expect(saved).toBe(value);
      return {board:c.path,...timing,prefetch:c.speculative.length};
    }));
    console.table(results);
    for (const result of results) expect(result.prefetch).toBe(0);

    // Keyboard navigation still warms the chosen route and opens it normally.
    const first = clients[0].tab;
    const destination = first.getByRole('link', { name: 'Gross Board', exact: true });
    const warm = first.waitForRequest(request => request.url().includes('/gross-board') &&
      (request.headers()['next-router-prefetch'] || request.headers()['next-router-segment-prefetch'] || request.url().includes('_rsc=')));
    await destination.focus();
    await warm;
    await destination.press('Enter');
    await expect(first.locator('[data-gross-field="truck"]').first()).toBeVisible();
  } finally {
    await Promise.all(contexts.map(context => context.close()));
  }
}
