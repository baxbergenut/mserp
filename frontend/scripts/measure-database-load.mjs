// Repeat the same signed-in request mix against the local review fixture.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { performance } from 'node:perf_hooks';
import { request } from '@playwright/test';

const fixture = JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE, 'utf8'));
assert.ok(['127.0.0.1', 'localhost'].includes(new URL(fixture.base).hostname));
const week = 'weekStart=2026-10-05';
const routes = ['/auth/session', '/tasks/count', '/feedback/messages/count', '/driver-board/presence',
  '/gross-board/presence?' + week, '/driver-board?' + week, '/gross-board?' + week];
const clients = [];
const results = new Map(routes.map(route => [route, []]));
try {
  for (let i = 0; i < 5; i++) {
    const client = await request.newContext({ baseURL: fixture.base });
    clients.push(client);
    const login = await client.post('/api/auth/login', { data: { email: fixture.email, password: fixture.password } });
    assert.equal(login.status(), 200);
  }
  // Separate warm-up from the five measured bursts.
  for (let wave = 0; wave <= 5; wave++) {
    await Promise.all(clients.flatMap(client => routes.map(async route => {
      const start = performance.now();
      const response = await client.get('/api' + route);
      assert.equal(response.status(), 200, route);
      await response.body();
      const ms = performance.now() - start;
      const timing = response.headers()['server-timing'] ?? '';
      const calls = timing.match(/queries;desc="(\d+)"/);
      const pool = timing.match(/pool;dur=([\d.]+)/);
      assert.ok(calls && pool, 'Server-Timing metrics required for ' + route);
      if (wave) results.get(route).push({ ms, calls: Number(calls[1]), pool: Number(pool[1]) });
      await response.dispose();
    })));
  }
  function summary(samples) {
    const sorted = samples.map(x => x.ms).sort((a,b) => a-b);
    const percentile = p => Math.round(sorted[Math.ceil(sorted.length*p)-1]);
    return { requests: samples.length, calls: samples.reduce((sum,x) => sum+x.calls,0),
      poolMs: Math.round(samples.reduce((sum,x) => sum+x.pool,0)),
      p50ms: percentile(.5), p95ms: percentile(.95), p99ms: percentile(.99),
      over1s: sorted.filter(x => x>=1000).length, over5s: sorted.filter(x => x>=5000).length };
  }
  console.log(JSON.stringify({ all: summary([...results.values()].flat()),
    routes: Object.fromEntries([...results].map(([name,samples]) => [name,summary(samples)])) }, null, 2));
} finally {
  await Promise.all(clients.map(client => client.dispose()));
}
