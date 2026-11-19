// Run matched local board-open bursts. Repeat with the same authenticated clients.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { performance } from 'node:perf_hooks';
import { request } from '@playwright/test';

const count = Number(process.env.MSERP_REVIEW_CLIENTS || 5);
const rounds = Number(process.env.MSERP_REVIEW_ROUNDS || 1);
assert.ok(Number.isInteger(count) && count > 0 && count <= 30);
assert.ok(Number.isInteger(rounds) && rounds > 0 && rounds <= 20);
const monday = process.env.MSERP_REVIEW_WEEK || '2026-10-05';
assert.match(monday, /^\d{4}-\d{2}-\d{2}$/);
const week = '?weekStart=' + monday;
const clients = [];

async function fixture(path, combined) {
  const access = JSON.parse(await readFile(path, 'utf8'));
  assert.ok(['127.0.0.1','localhost'].includes(new URL(access.base).hostname));
  const sessions = [];
  if (access.accounts) assert.ok(access.accounts.length >= count, 'one local account per client required');
  for (let i = 0; i < count; i++) {
    const client = await request.newContext({ baseURL: access.base });
    clients.push(client);
    const user = access.accounts?.[i] ?? access;
    const response = await client.post('/api/auth/login', { data: { email: user.email, password: user.password } });
    assert.equal(response.status(), 200, 'local fixture login');
    sessions.push(client);
  }
  const badges = combined ? ['/tasks/count?includeFeedback=1'] : ['/tasks/count', '/feedback/messages/count'];
  return { sessions, routes: ['/auth/session', ...badges, '/driver-board/presence', '/gross-board/presence'+week,
    '/driver-board'+week, '/gross-board'+week] };
}
async function wave({ sessions, routes }) {
  const start = performance.now();
  const samples = await Promise.all(sessions.flatMap(client => routes.map(async route => {
    const requestStart = performance.now();
    const response = await client.get('/api' + route);
    try {
      assert.equal(response.status(), 200, route);
      await response.body();
      if (route.includes('includeFeedback')) {
        const counts = await response.json();
        assert.ok(Number.isInteger(counts.count) && Number.isInteger(counts.feedbackCount));
      }
      const timing = response.headers()['server-timing'] || '';
      const calls = timing.match(/queries;desc="(\d+)"/), pool = timing.match(/pool;dur=([\d.]+)/), db = timing.match(/db;dur=([\d.]+)/);
      assert.ok(calls && pool && db, 'Missing Server-Timing on '+route);
      return { route, ms: performance.now()-requestStart, calls: +calls[1], pool: +pool[1], db: +db[1] };
    } finally { await response.dispose(); }
  })));
  return { elapsed: Math.round(performance.now()-start), samples };
}
function summary(samples) {
  const times = samples.map(x=>x.ms).sort((a,b)=>a-b);
  const percentile = p => Math.round(times[Math.ceil(times.length*p)-1]);
  const sum = name => Math.round(samples.reduce((n,s)=>n+s[name],0));
  return { requests:samples.length, calls:sum('calls'), poolMs:sum('pool'), dbMs:sum('db'),
    p50ms:percentile(.5), p95ms:percentile(.95), p99ms:percentile(.99),
    over1s:times.filter(ms=>ms>=1000).length, over5s:times.filter(ms=>ms>=5000).length };
}
try {
  const workloads = new Map();
  if (process.env.MSERP_REVIEW_BASELINE_FILE) workloads.set('before', await fixture(process.env.MSERP_REVIEW_BASELINE_FILE, false));
  workloads.set('after', await fixture(process.env.MSERP_REVIEW_ACCESS_FILE, process.env.MSERP_REVIEW_COMBINED_BADGES === '1'));
  const results = Object.fromEntries([...workloads.keys()].map(k=>[k, { samples:[], waves:[] }]));
  for (const workload of workloads.values()) await wave(workload);
  for (let i=0; i<rounds*5; i++) {
    const order = [...workloads];
    if (i%2) order.reverse();
    for (const [name,workload] of order) {
      const {elapsed,samples}=await wave(workload);
      results[name].samples.push(...samples);
      results[name].waves.push(elapsed);
    }
  }
  const report = {};
  for (const [name,{ samples,waves }] of Object.entries(results)) {
    const routes=workloads.get(name).routes;
    report[name]={all:summary(samples), routes:Object.fromEntries(routes.map(route=>[route,summary(samples.filter(s=>s.route===route))])),
      waveMs: waves };
  }
  console.log(JSON.stringify({count,rounds,week:monday,...report},null,2));
} finally { await Promise.all(clients.map(client=>client.dispose())); }
