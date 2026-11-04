// HTTP timings against the isolated synthetic fixture. Never uses production.
import assert from 'node:assert/strict';
import { readFile, writeFile } from 'node:fs/promises';
import { randomUUID, createHash } from 'node:crypto';
import { request } from '@playwright/test';

const access = JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE, 'utf8'));
assert(['127.0.0.1', 'localhost'].includes(new URL(access.base).hostname));
assert(/^payroll_perf_[0-9]+$/.test(access.schema));
const runs = Number(process.env.MSERP_PERF_RUNS || 20);
assert(Number.isInteger(runs) && runs > 0);
const kinds = (process.env.MSERP_PERF_KINDS || 'driver,investor').split(',');
assert(kinds.every(k => ['driver', 'investor'].includes(k)));
const groups = (process.env.MSERP_PERF_GROUPS || '1,171').split(',').map(Number);
assert(groups.every(n => Number.isInteger(n) && n >= 1 && n <= 176));
const weekStart = '2026-10-19', samples = [];
let phase = 'first';
const client = await request.newContext({ baseURL: access.base, timeout: 120000 });
const login = await client.post('/api/auth/login', { data: { email: access.email, password: access.password } });
assert(login.ok(), `login ${login.status()}`);
const session = await login.json(), headers = { 'x-csrf-token': session.csrfToken };
const metric = (header, name) => Number(header.match(new RegExp(`${name};(?:dur=|desc=")([0-9.]+)`))?.[1] || 0);
async function call(kind, action, path, method = 'GET', data, record = true) {
  const start = performance.now();
  const response = await client.fetch(path, { method, headers, data });
  const body = await response.json(), elapsed = performance.now() - start;
  const timing = response.headers()['server-timing'] || '';
  if (record) samples.push({ kind, action, phase, status: response.status(), ms: elapsed,
    appMS: metric(timing, 'app'), queries: metric(timing, 'queries'), calculations: metric(timing, 'payroll_calculations') });
  assert(response.ok(), `${kind} ${action}: ${response.status()} ${JSON.stringify(body).slice(0, 300)}`);
  return body;
}
const idFor = value => createHash('md5').update(value).digest('hex').replace(/(.{8})(.{4})(.{4})(.{4})(.{12})/, '$1-$2-$3-$4-$5');
const summary = kind => `/api/${kind}-pay?prepared=1&summary=1&page=1&pageSize=25&weekStart=${weekStart}`;
async function detail(kind, id, record = true) {
  const path = `/api/${kind}-pay?prepared=1&statementId=${id}&weekStart=${weekStart}`;
  for (let attempt = 0; attempt < 600; attempt++) {
    const report = await call(kind, 'detail', path, 'GET', undefined, record && attempt === 0);
    if (report.preparation?.state === 'ready' && report.drivers.length === 1) return report;
    await new Promise(resolve => setTimeout(resolve, 1000));
  }
  throw Error(`${kind} preparation did not finish`);
}
try {
  for (const kind of kinds) {
    // Group 1 has a large historical assignment component; 171 is independent.
    for (const group of groups) {
      // Every fourth synthetic owner is paid through the driver statement.
      // There is deliberately no separate investor statement for that truck.
      if (kind === 'investor' && group % 4 === 0) continue;
      phase = 'first';
      const id = idFor(`${kind === 'driver' ? 'driver' : 'truck'}${group}`);
      const initial = await detail(kind, id, false);
      const invoice = initial.drivers[0].edits.expenseDeductions?.find(row => row.liveBalance && !row.source && !row.saved);
      if (kind === 'driver' && invoice) {
        let expense = await call('expense', 'read', `/api/expenses/${invoice.expenseId}`, 'GET', undefined, false);
        const originalAmount = expense.amount;
        const fields = ['categoryId', 'expenseDate', 'truckId', 'driverId', 'applicantId', 'ownerId', 'unitNumber', 'driverName', 'amount', 'baseAmount', 'paymentType', 'expenseType', 'referenceNumber', 'description', 'coveredBy', 'brokerLoadNumber', 'paidBy', 'accountingVerified', 'updatedAt', 'costBreakdown'];
        for (let n = 0; n < runs; n++) {
          const input = Object.fromEntries(fields.filter(key => expense[key] !== undefined).map(key => [key, expense[key]]));
          input.amount = (Number(originalAmount) + (n % 2 ? 0 : 1)).toFixed(2);
          delete input.baseAmount; delete input.costBreakdown;
          expense = await call('expense', 'save', `/api/expenses/${invoice.expenseId}`, 'PUT', input);
        }
      }
      for (let n = 0; n < runs; n++) {
        phase = n === 0 ? 'first' : 'repeat';
        await call(kind, 'summary', summary(kind));
        let report = await detail(kind, id), card = report.drivers[0];
        assert(!card.settlement?.finalized, 'fixture must begin open');
        const edits = { ...card.edits, notes: `HTTP benchmark ${group}/${n}`, requestId: randomUUID(), expectedStatementRevision: card.statementRevision };
        for (const key of ['inputRevision', 'statementRevision', 'committedStatement', 'affectedStatements']) delete edits[key];
        await call(kind, 'save', `/api/${kind}-pay?receipt=1`, 'PUT', edits);
        report = await detail(kind, id, false); card = report.drivers[0];
        await call(kind, 'finalize', `/api/${kind}-pay/finalize?receipt=1`, 'POST', {
          weekStart, driverId: id, revision: report.revision, expectedStatementRevision: card.statementRevision,
          expenseRevision: card.expenseRevision, requestId: randomUUID(), override: true,
        });
        report = await detail(kind, id, false); card = report.drivers[0];
        assert(card.settlement?.finalized, 'finalization must persist');
        await call(kind, 'reopen', `/api/${kind}-pay/reopen?receipt=1`, 'POST', {
          weekStart, driverId: id, revision: report.revision, expectedStatementRevision: card.statementRevision,
          requestId: randomUUID(), reason: 'Synthetic performance fixture',
        });
      }
      console.log(`${kind} group ${group}: ${runs} complete save/finalize/reopen cycles`);
    }
  }
} finally {
  const stats = [];
  for (const kind of ['expense', ...kinds]) for (const action of ['summary', 'detail', 'save', 'finalize', 'reopen']) for (const phase of ['first', 'repeat']) {
    const rows = samples.filter(s => s.kind === kind && s.action === action && s.phase === phase);
    if (!rows.length) continue;
    const percentile = (key, p) => rows.map(s => s[key]).sort((a, b) => a - b)[Math.ceil(rows.length * p) - 1];
    stats.push({ kind, action, phase, n: rows.length, failures: rows.filter(s => s.status >= 400).length,
      medianMS: percentile('ms', .5), p95MS: percentile('ms', .95), appP95MS: percentile('appMS', .95),
      medianQueries: percentile('queries', .5), maxCalculations: percentile('calculations', 1) });
  }
  await writeFile(process.env.MSERP_PERF_OUTPUT || 'payroll-command-timings.json', JSON.stringify({ fixture: access.schema, runs, stats, samples }, null, 2));
  console.table(stats);
  await client.dispose();
}
