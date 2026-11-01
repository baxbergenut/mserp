// Real browser actions against a disposable, production-sized local fixture only.
import assert from 'node:assert/strict';
import { readFile, writeFile } from 'node:fs/promises';
import { execFileSync, execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { dirname, join } from 'node:path';
import { chromium, expect } from '@playwright/test';
import { installPayrollBrowserTiming } from './payroll-browser-timing.mjs';

const accessFile = process.env.MSERP_REVIEW_ACCESS_FILE;
const access = JSON.parse(await readFile(accessFile, 'utf8'));
const dsn = new URL(process.env.MSERP_REVIEW_TEST_DATABASE_URL);
if (!['127.0.0.1', 'localhost'].includes(new URL(access.base).hostname) || !['127.0.0.1', 'localhost'].includes(dsn.hostname)
  || !dsn.pathname.endsWith('_test') || !/^payroll_perf_[0-9]+$/.test(access.schema)) throw Error('Local synthetic payroll fixture required');
// Measure old to new on a fresh fixture: reopening intentionally retains payment
// reservations, so writing a later week first must block earlier corrections.
const weeks = (process.env.MSERP_PERF_WEEKS || '2026-10-26,2026-10-19,2026-09-28').split(',').sort();
if (weeks.some(w => !/^\d{4}-\d{2}-\d{2}$/.test(w))) throw Error('Invalid weeks');
const kinds = (process.env.MSERP_PERF_KINDS || 'driver,investor').split(',');
if (kinds.some(k => !['driver', 'investor'].includes(k))) throw Error('Invalid payroll kinds');
const runs = Number(process.env.MSERP_PERF_RUNS || 200), latency = Number(process.env.MSERP_PERF_BROWSER_LATENCY_MS ?? 175);
const prepared = process.env.MSERP_PERF_PREPARED === '1';
if (![runs].every(n => Number.isInteger(n) && n > 0) || !Number.isFinite(latency) || latency < 0) throw Error('Invalid timing sample settings');
const concurrencyOnly = process.env.MSERP_PERF_CONCURRENCY_ONLY === '1';
const savesOnly = process.env.MSERP_PERF_SAVES_ONLY === '1';
const sourcesOnly = process.env.MSERP_PERF_SOURCES_ONLY === '1';
if ([concurrencyOnly, savesOnly, sourcesOnly].filter(Boolean).length > 1) throw Error('Choose only one isolated benchmark action');
const runTag = Date.now().toString(36);
let preparationSetupMS = 0, runFailure;
const psql = process.env.PSQL_PATH || 'psql', args = ['-X', '-q', '-At', '-v', 'ON_ERROR_STOP=1', '-d', dsn.toString()];
const sql = q => execFileSync(psql, args, { input: `SET search_path TO ${access.schema},public;${q}`, encoding: 'utf8', windowsHide: true }).trim();
const browser = await chromium.launch(), contexts = [], samples = [], telemetry = new WeakMap();
const concurrentPages = new Map();
const output = process.env.MSERP_PERF_OUTPUT || join(dirname(accessFile), 'payroll-browser-performance.json');
const paint = p => p.evaluate(() => new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r))));
const timing = (header, field) => Number(header?.match(new RegExp(field + ';(?:dur=|desc=")([0-9.]+)'))?.[1] || 0);
const pageURL = (kind, week, id = '') => `${access.base}/accounting/${kind}-pay?weekStart=${week}&${kind === 'driver' ? 'driverId' : 'truckId'}=${id}`;
const row = (p, d) => p.getByRole('button', { name: d.investorId ? `${d.fullName} ${d.truckUnit}` : d.fullName, exact: true });
const detail = (p, d) => p.getByRole('region', { name: `${d.fullName} weekly loads`, exact: true });
async function login(index) {
  if (index != null && concurrentPages.has(index)) return concurrentPages.get(index);
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } }); contexts.push(context);
  await context.addInitScript(installPayrollBrowserTiming);
  await context.route('**/api/**', async route => {
    if (route.request().url().includes('/events')) return route.continue();
    try {
      // A slow command is a failed timing sample, not a proxy crash or retry.
      // Keep the target unchanged and let the action record the actual outcome.
      const response = await route.fetch({ timeout: 120000, maxRetries: 0 });
      await new Promise(r => setTimeout(r, latency)); await route.fulfill({ response });
    } catch {
      await route.abort('failed').catch(() => {});
    } finally {
      // A navigation or AbortController can cancel a routed browser request
      // while its independent route.fetch is finishing. Playwright may omit
      // the terminal page event in that case. The proxy is done only after
      // fulfillment/abort, so release that exact tracked request here too.
      const request = route.request(), state = telemetry.get(request.frame().page());
      if (state?.pending.delete(request)) state.changed = performance.now();
    }
  });
  const p = await context.newPage(), state = { pending: new Set(), records: [], changed: performance.now() }; telemetry.set(p, state);
  const relevant = request => /^\/api\/(driver|investor)-pay(?:$|\/(?!events$))/.test(new URL(request.url()).pathname);
  p.on('request', request => { if (relevant(request)) { state.pending.add(request); state.changed = performance.now(); } });
  p.on('requestfinished', request => { if (relevant(request)) { state.pending.delete(request); state.changed = performance.now(); } });
  p.on('requestfailed', request => { if (relevant(request)) { state.pending.delete(request); state.changed = performance.now(); } });
  p.on('response', response => {
    if (!relevant(response.request())) return;
    void (async () => {
      const url = new URL(response.url()), header = response.headers()['server-timing'];
      let bytes = 0; try { bytes = (await response.body()).length; } catch {}
      state.records.push({ finished: performance.now(), status: response.status(), method: response.request().method(),
        path: url.pathname.replace(/\/statements\/[^/]+/, '/statements/:id'), bytes,
        queries: timing(header, 'queries'), calculations: timing(header, 'payroll_calculations'), appMS: timing(header, 'app'), lockMS: timing(header, 'lock') });
      state.pending.delete(response.request()); state.changed = performance.now();
    })();
  });
  await p.goto(access.base + '/login');
  await p.getByLabel('Email', { exact: true }).fill(index == null ? access.email : `perf-session-${index}@example.test`);
  await p.getByLabel('Password', { exact: true }).fill(access.password);
  const [authenticated] = await Promise.all([
    p.waitForResponse(r => new URL(r.url()).pathname === '/api/auth/login' && r.request().method() === 'POST'),
    p.getByRole('button', { name: 'Sign in', exact: true }).click(),
  ]);
  assert.equal(authenticated.status(), 200, 'Benchmark login setup failed; payroll action has not started');
  await p.waitForURL(u => u.pathname !== '/login');
  if (index != null) concurrentPages.set(index, p);
  return p;
}
async function settled(p) {
  const state = telemetry.get(p), start = performance.now();
  while (state.pending.size || performance.now() - state.changed < 100) {
    if (performance.now() - start > 60000) throw Error(`Payroll requests did not finish: ${[...state.pending].map(r => r.method() + ' ' + new URL(r.url()).pathname).join(', ')}`);
    await p.waitForTimeout(25);
  }
}
async function preparedReport(p, kind, week) {
  // Initial preparation is setup, measured separately from a prepared read.
  // Never hide Updating inside an individual measured financial action.
  const start = performance.now();
  for (;;) {
    const response = await p.request.get(`${access.base}/api/${kind}-pay?weekStart=${week}${prepared ? '&prepared=1' : ''}`);
    assert.equal(response.status(), 200, await response.text());
    const report = await response.json();
    if (!prepared || report.preparation?.state === 'ready') return report;
    if (report.preparation?.state === 'failed' || performance.now() - start > 180000) throw Error(`Preparation failed or timed out: ${kind}/${week}`);
    await p.waitForTimeout(250);
  }
}
async function measure(p, kind, week, action, fn, target = 300) {
  console.log(`Measuring ${kind}/${week}/${action}`);
  try { await settled(p); } catch (error) {
    samples.push({ kind, week, action, target, visibleMS: null, automationMS: null, failure: error.message, aggregateRefreshMS: null, requests: [] });
    throw error;
  }
  const browserStart = await p.evaluate(action => { window.__payrollTiming.begin(action); return performance.now(); }, action);
  const start = performance.now(); let failure;
  try { await fn(); await paint(p); } catch (e) { failure = e.message; }
  const automationMS = performance.now() - start;
  const observed = await p.evaluate(() => ({ active: window.__payrollTiming.active, navigation: window.__payrollTiming.navigationVisible }));
  const visibleMS = ['coldPage', 'warmPage'].includes(action) ? observed.navigation
    : observed.active?.visible == null || observed.active?.started == null ? null : observed.active.visible - observed.active.started;
  if (!failure && (!Number.isFinite(visibleMS) || visibleMS <= 0 || (!['coldPage', 'warmPage'].includes(action) && observed.active?.visible === null))) failure = 'Browser did not observe the completed visible action';
  const actionStart = start + (observed.active?.started == null ? 0 : observed.active.started - browserStart);
  try { await settled(p); } catch (e) { failure ||= e.message; }
  const requests = telemetry.get(p).records.filter(r => r.finished >= start).map(r => ({ ...r, completedMS: r.finished - actionStart, finished: undefined }));
  const refreshes = requests.filter(r => r.method === 'GET' && r.path === `/api/${kind}-pay`);
  samples.push({ kind, week, action, target, visibleMS, automationMS, failure, aggregateRefreshMS: refreshes.length ? Math.max(...refreshes.map(r=>r.completedMS)) : null, requests });
  console.log(JSON.stringify({ kind, week, action, visibleMS: visibleMS == null ? null : Math.round(visibleMS), requests: requests.length,
    serverMS: requests.map(r => ({ method: r.method, ms: r.appMS, queries: r.queries })), failure }));
  if (failure) throw Error(`${kind}/${week}/${action}: ${failure}`);
}
async function expand(p, d) {
  if (await row(p, d).getAttribute('aria-expanded') !== 'true') await row(p, d).click();
  await expect(detail(p, d)).toBeVisible();
}
async function save(p, kind, week, d, n, action = 'save') {
  await expand(p, d);
  await p.getByRole('button', { name: `${d.fullName}, weekly comment`, exact: true }).click();
  await p.getByLabel('Comment', { exact: true }).fill(`Synthetic timing ${runTag} ${kind} ${action} ${n}`);
  await measure(p, kind, week, action, async () => {
    const [response] = await Promise.all([
      p.waitForResponse(r => new URL(r.url()).pathname === `/api/${kind}-pay` && r.request().method() === 'PUT'),
      p.getByRole('button', { name: 'Save comment', exact: true }).click(),
    ]);
    assert.equal(response.status(), 200);
    await expect(p.getByText('Saved', { exact: true }).first()).toBeVisible();
  });
}
async function finalize(p, kind, week, d) {
  await measure(p, kind, week, 'finalize', async () => {
    const [result] = await Promise.all([
      p.waitForResponse(r => new URL(r.url()).pathname === `/api/${kind}-pay/finalize`),
      row(p, d).locator('xpath=ancestor::tr').getByRole('button', { name: /^Finalize / }).click(),
    ]);
    assert.equal(result.status(), 200, await result.text());
    await expect(p.getByText('Finalized', { exact: true }).first()).toBeVisible();
  });
}
async function reopen(p, kind, week, d) {
  await measure(p, kind, week, 'reopenDialog', async () => {
    await row(p, d).click({ button: 'right' }); await p.getByRole('menuitem', { name: kind === 'driver' ? 'Reopen driver' : 'Reopen truck', exact: true }).click();
    await expect(p.getByLabel('Reason for reopening')).toBeVisible();
  }, 300);
  await p.getByLabel('Reason for reopening').fill('Synthetic performance verification');
  await measure(p, kind, week, 'reopen', async () => {
    const [result] = await Promise.all([
      p.waitForResponse(r => new URL(r.url()).pathname === `/api/${kind}-pay/reopen`),
      p.getByRole('button', { name: 'Reopen settlement', exact: true }).click(),
    ]);
    assert.equal(result.status(), 200, await result.text());
    await expect(p.getByRole('dialog')).toHaveCount(0);
    await expect(p.getByText('Reopened', { exact: true }).first()).toBeVisible();
  }, d ? 300 : 2000);
}

async function sourceVisibility(p, kind, week, statement) {
  const report = await preparedReport(p, kind, week);
  const card = report.drivers.find(d => d.id === statement.id);
  const load = card.loads.find(l => !l.payrollSupplement && l.loadTripId == null);
  assert.ok(load, 'Synthetic statement must have a native unsplit source load');
  const driver = load.sourceDriverId || card.id, date = load.sourceDate || load.date, slot = load.sourceSlot ?? load.slot;
  assert.match(driver, /^[0-9a-f-]{36}$/i); assert.match(date, /^\d{4}-\d{2}-\d{2}$/); assert.ok(Number.isInteger(slot));
  const next = Number(load.totalMiles) + 1;
  assert.ok(Number.isFinite(next));
  await expand(p, card); await settled(p);
  const start = performance.now();
  await p.evaluate(({ name, number, expected }) => {
    const previous = window.__payrollSourceTiming;
    previous?.observer?.disconnect();
    const state = window.__payrollSourceTiming = { visibleAt: null, pending: false, observer: null };
    const matches = () => {
      const region = [...document.querySelectorAll('[role="region"]')].find(e => e.getAttribute('aria-label') === `${name} weekly loads`);
      const link = [...(region?.querySelectorAll('a') || [])].find(e => e.textContent.trim() === number);
      const table = link?.closest('table');
      const column = [...(table?.tHead?.rows[0]?.cells || [])].findIndex(cell => cell.textContent.trim() === 'Total miles');
      // Day is a row-group header, so the load's td list starts one column later.
      const cell = column > 0 ? link?.closest('tr')?.querySelectorAll('td')[column - 1] : null;
      return cell && cell.getClientRects().length && cell.textContent.trim().startsWith(expected);
    };
    const check = () => {
      if (state.visibleAt !== null || state.pending || !matches()) return;
      state.pending = true;
      requestAnimationFrame(() => requestAnimationFrame(() => {
        if (matches()) { state.visibleAt = performance.timeOrigin + performance.now(); state.observer.disconnect(); }
        state.pending = false;
      }));
    };
    state.observer = new MutationObserver(check);
    state.observer.observe(document, { childList: true, subtree: true, attributes: true, characterData: true });
  }, { name: card.fullName, number: load.loadNumber, expected: new Intl.NumberFormat('en-US', { maximumFractionDigits: 2 }).format(next) });
  let visibleMS = null, failure;
  try {
    // Use the canonical direct-SQL capture path. The timestamp is taken just
    // after COMMIT, excluding process startup and database lock wait. Both the
    // local database and browser use the same host clock; API delay still applies.
    const sourceTable = slot === 0 ? 'gross_board_entries' : 'gross_board_extra_entries';
    const sourceSlot = slot === 0 ? '' : ` AND slot=${slot} AND NOT deleted`;
    const query = `SET search_path TO ${access.schema},public; BEGIN;
 UPDATE ${sourceTable} SET entered_miles=coalesce(entered_miles,miles,400)+1,version=version+1
 WHERE driver_id='${driver}'::uuid AND service_date='${date}'::date${sourceSlot} RETURNING entered_miles;
 COMMIT; SELECT extract(epoch FROM clock_timestamp())*1000;`;
    const { stdout } = await promisify(execFile)(psql, [...args, '-c', query], { windowsHide: true });
    const values = stdout.trim().split(/\r?\n/).map(Number);
    assert.equal(values.length, 2); assert.equal(values[0], next);
    await p.waitForFunction(() => window.__payrollSourceTiming.visibleAt !== null, null, { timeout: 30000 });
    visibleMS = await p.evaluate(() => window.__payrollSourceTiming.visibleAt) - values[1];
    assert.ok(visibleMS >= 0, 'Browser/database clocks disagree');
  } catch (e) { failure = e.message; }
  finally { await p.evaluate(() => window.__payrollSourceTiming?.observer?.disconnect()); }
  try { await settled(p); } catch (e) { failure ||= e.message; }
  const requests = telemetry.get(p).records.filter(r => r.finished >= start).map(r => ({ ...r, completedMS: r.finished - start, finished: undefined }));
  samples.push({ kind, week, action: 'externalSourceVisibility', target: 2000, visibleMS, automationMS: performance.now() - start, failure, aggregateRefreshMS: null, requests });
  console.log(JSON.stringify({ kind, week, action: 'externalSourceVisibility', visibleMS, failure }));
  if (failure) throw Error(`${kind}/${week}/externalSourceVisibility: ${failure}`);
}
try {
  assert.equal(sql('SELECT active FROM accounting_cutover'), 'f');
  assert.equal(sql("SELECT count(*) FROM drivers WHERE full_name LIKE 'Perf Driver %'"), '176');
  if (prepared) assert.equal(sql('SELECT capture_enabled FROM payroll_projection_control WHERE singleton'), 't');
  // Supply complete evidence for ordinary finalization in this disposable fixture.
  sql(`UPDATE loads SET raw_payload='{"trip":{"mile":400,"empty_mile":0},"stops":[{"stop_type":"pickup","ordering":1,"location":{"city":"Dallas","state":"TX"}},{"stop_type":"delivery","ordering":2,"location":{"city":"Houston","state":"TX"}}]}'::jsonb
    WHERE raw_payload IS DISTINCT FROM '{"trip":{"mile":400,"empty_mile":0},"stops":[{"stop_type":"pickup","ordering":1,"location":{"city":"Dallas","state":"TX"}},{"stop_type":"delivery","ordering":2,"location":{"city":"Houston","state":"TX"}}]}'::jsonb;
    INSERT INTO app_users(username,email,password_hash,role_id) SELECT 'Perf Session '||n,'perf-session-'||n||'@example.test',u.password_hash,u.role_id FROM app_users u CROSS JOIN generate_series(0,4)n WHERE u.email='review@example.test' ON CONFLICT DO NOTHING;`);
  if (prepared) {
    const started = performance.now();
    for (;;) {
      const pending = Number(sql(`SELECT (SELECT count(*) FROM payroll_projection_jobs WHERE requested_generation>completed_generation)
        +(SELECT count(*) FROM payroll_projection_changes WHERE dispatched_at IS NULL)`));
      if (!pending) break;
      if (performance.now() - started > 300000) throw Error('Fixture backfill did not finish; no browser action was timed');
      await new Promise(resolve => setTimeout(resolve, 1000));
    }
    preparationSetupMS = performance.now() - started;
    console.log(`Initial preparation finished in ${Math.round(preparationSetupMS)} ms; excluded from prepared-read timing`);
  }
  const p = await login();
  const session = await (await p.request.get(access.base + '/api/auth/session')).json();
  for (const week of weeks) for (const kind of kinds) {
    let report = await preparedReport(p, kind, week);
    if (report.drivers.some(d => d.settlement?.finalized)) {
      const reset = await p.request.post(`${access.base}/api/${kind}-pay/reopen?receipt=1`, { headers: { 'x-csrf-token': session.csrfToken }, data: { weekStart: week, revision: report.revision, reason: 'Reset interrupted synthetic benchmark' } });
      assert.equal(reset.status(), 200, await reset.text());
      report = await preparedReport(p, kind, week);
    }
    const drivers = report.drivers.filter(d => !d.truckInactive).slice(0, 5); assert.equal(drivers.length, 5);
    await p.goto(pageURL(kind, week)); await expect(row(p, drivers[0])).toBeVisible();
    for (let n = 0; n < (concurrencyOnly || sourcesOnly ? 0 : runs); n++) {
      if (savesOnly) {
        await p.goto(pageURL(kind, week, drivers[0].id)); await expand(p, drivers[0]);
        await save(p, kind, week, drivers[0], n); continue;
      }
      // Clear remembered expansions before invalidating so the measured first
      // expansion cannot silently reuse details fetched during page loading.
      for (const d of drivers.slice(0,3)) if (await row(p,d).getAttribute('aria-expanded') === 'true') await row(p,d).click();
      // Prepared cold reads retain durable versions. Artificially advancing a
      // source counter without a captured source transaction would instead test
      // unrecoverable stale input, not application/browser cache eviction.
      if (!prepared) sql("UPDATE payroll_cache_revisions SET revision=revision+1 WHERE table_name='loads'");
      await measure(p, kind, week, 'coldPage', async () => { await p.goto(pageURL(kind, week)); await expect(row(p, drivers[0])).toBeVisible(); }, 1000);
      await measure(p, kind, week, 'firstExpansion', () => expand(p, drivers[0]));
      await row(p, drivers[0]).click();
      await measure(p, kind, week, 'cachedExpansion', () => expand(p, drivers[0]));
      for (const d of drivers.slice(1, 3)) await measure(p, kind, week, 'multipleOpen', () => expand(p, d));
      await p.goto(pageURL(kind, week, drivers[0].id)); await expand(p, drivers[0]); await settled(p);
      await save(p, kind, week, drivers[0], n);
      await finalize(p, kind, week, drivers[0]); await reopen(p, kind, week, drivers[0]);
      await measure(p, kind, week, 'warmPage', async () => { await p.goto(pageURL(kind, week)); await expect(row(p, drivers[0])).toBeVisible(); }, 1000);
    }
    if (prepared && !concurrencyOnly && !savesOnly) {
      await p.goto(pageURL(kind, week, drivers[0].id)); await expand(p, drivers[0]);
      for (let n = 0; n < runs; n++) await sourceVisibility(p, kind, week, drivers[0]);
    }
    if (savesOnly || sourcesOnly) continue;
    const pages = await Promise.all(drivers.map(async (d, i) => {
      const page = await login(i); await page.goto(pageURL(kind, week, d.id)); await expand(page, d); return page;
    }));
    const sync = (async () => { for (let n = 0; n < runs; n++) {
      await promisify(execFile)(psql, [...args, '-c', `SET search_path TO ${access.schema},public; UPDATE loads SET total_pay=total_pay+0.01 WHERE id=(SELECT max(id) FROM loads);`], { windowsHide: true });
      await new Promise(r => setTimeout(r, 250));
    } })();
    const concurrentResults = await Promise.allSettled([sync, ...pages.map((page, i) => (async () => { for (let n = 0; n < runs; n++) await save(page, kind, week, drivers[i], n, 'fiveUsersWithSync'); })())]);
    const failures = concurrentResults.filter(result => result.status === 'rejected').map(result => result.reason);
    if (failures.length) throw new AggregateError(failures, 'Concurrent payroll benchmark failed');
    // Reuse these five authenticated browser sessions between weeks. Repeated
    // login setup otherwise measures the IP login limit instead of payroll.
  }
} catch (error) {
  runFailure = error.message;
  throw error;
} finally {
  const groups = Map.groupBy(samples, s => `${s.kind}/${s.week}/${s.action}`);
  const percentile = (values, q) => {
    const measured = values.filter(Number.isFinite).toSorted((a,b) => a-b);
    return measured[Math.max(0, Math.ceil(measured.length*q)-1)] ?? null;
  };
  const results = Object.fromEntries([...groups].map(([key, rows]) => [key, {
    samples: rows.length, failures: rows.filter(s => s.failure).length,
    p50: percentile(rows.map(s => s.visibleMS), .50), p95: percentile(rows.map(s => s.visibleMS), .95), p99: percentile(rows.map(s => s.visibleMS), .99),
    targetMS: rows[0].target, targetMet: rows.every(s => !s.failure) && percentile(rows.map(s => s.visibleMS), .95) < rows[0].target,
    requestCount: rows.reduce((s,r) => s+r.requests.length,0), queryCount: rows.flatMap(r=>r.requests).reduce((s,r)=>s+r.queries,0),
    calculationCount: rows.flatMap(r=>r.requests).reduce((s,r)=>s+r.calculations,0),
    payloadBytes: rows.flatMap(r=>r.requests).reduce((s,r)=>s+r.bytes,0), lockWaitP95: percentile(rows.flatMap(r=>r.requests).filter(r=>r.method !== 'GET').map(r=>r.lockMS), .95),
    aggregateRefreshP95: percentile(rows.map(r=>r.aggregateRefreshMS).filter(v=>v!==null),.95),
  }]));
  await writeFile(output, JSON.stringify({ synthetic: true, timing: 'actual input dispatch to committed visible state plus two animation frames; automation overhead recorded separately', addedRoundTripMS: latency, runs, prepared, preparationSetupMS, kinds, weeks, concurrencyOnly, savesOnly, sourcesOnly, runFailure, results, samples }, null, 2));
  console.log(JSON.stringify({ output, runFailure, results }));
  await Promise.all(contexts.map(c => c.close())); await browser.close();
}
