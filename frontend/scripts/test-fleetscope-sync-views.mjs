// Isolated static build + fixture API; no FleetScope or production calls.
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname } from 'node:path';
import assert from 'node:assert/strict';
import { chromium, expect } from '@playwright/test';

const stamp = new Date().toISOString();
const owner = { kind: 'investor', mserpOwnerId: 'owner', displayName: 'Independent Owner', mserpOwnerDriverId: null, effectiveDate: null };
const truck = { mserpTruckId: 'truck', unitNumber: '001', vin: '1FUJGLDR9CSBP0001', year: 2022, make: 'Freightliner', model: 'Cascadia', licensePlate: 'ABC123', licenseState: 'IL', operationalActive: true, operationalStatus: 'assigned', ownership: owner };
const data = { pendingCount: 0, canReview: true, canManage: true, configured: false, configuredMode: 'off', companyId: 'company', environment: 'staging', contractHash: 'hash', mode: 'off', version: 1, epoch: 'epoch', lastError: '', lastCheckedAt: null, local: { sourceRevision: 2, week: '2026-10-12', trucks: [{ truck, primaryId: 'driver', secondaryId: '', issues: [] }], drivers: [{ id: 'driver', name: 'Hired Driver', status: 'active', fleetScopeId: '', hireEventId: '' }] }, links: [], operations: [], snapshotId: '', snapshotSourceRevision: 0, snapshotCreatedAt: null, remote: null };
const writes = []; let reject = false;
const server = createServer(async (req, res) => {
 const send = (body, status = 200) => { res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(body)); };
 try {
  const p = new URL(req.url, 'http://localhost').pathname;
  if (p.startsWith('/api/')) {
   if (p === '/api/auth/session') return send({ user: { id: 'user', username: 'Reviewer', theme: 'default', permissions: ['access.manage', 'drivers.read','trucks.read','dispatchers.read','updaters.read','investors.read', 'fleetscope.read', 'fleetscope.manage', 'fleetscope.reconcile'] }, csrfToken: 'fixture' });
   if (p === '/api/settings/access') return send({ users: [], roles: [], permissions: [], expenseCategories: [] });
   if (p === '/api/sidebar-counts') return send({ fuel: 0, tolls: 0, weighMyTruck: 0 });
   if (p === '/api/auth/birthday') return send({});
   if (p === '/api/tasks/count') return send({ count: 0 });
   if (p === '/api/fleetscope-sync' && req.method === 'GET') return send(data);
   if (req.method === 'GET') return send({});
   let raw = ''; for await (const c of req) raw += c;
   const input = raw ? JSON.parse(raw) : {}; writes.push({ path: p, input });
   if (p === '/api/fleetscope-sync/snapshot') {
    data.snapshotId = 'snapshot'; data.snapshotSourceRevision = data.local.sourceRevision; data.snapshotCreatedAt = stamp; data.mode = 'reconcile';
    data.remote = { id: 'remote-snapshot', companyId: 'company', environment: 'staging', epoch: 'epoch', capturedAt: stamp, nextCursor: '', drivers: [{ id: 'remote-driver', name: 'Hired Driver', status: 'active', revision: 'd1', mserpId: '' }], trucks: [{ id: 'remote-truck', unitNumber: '001', vin: truck.vin, revision: 't1', assignmentRevision: 'a1', mserpId: '', primaryId: '', secondaryId: '', safetyHold: false, ownership: { ...owner, kind: 'company', displayName: 'Old Company Label' }, projection: null }] };
   }
   if (p === '/api/fleetscope-sync/review' && reject) { reject = false; return send({ error: 'FleetScope review changed; refresh the comparison' }, 409); }
   return send({ saved: true });
  }
  const file = p === '/' ? 'index.html' : extname(p) ? p.slice(1) : `${p.slice(1)}.html`;
  if (file.includes('..')) return send({}, 404);
  const body = await readFile(new URL(`../out/${file}`, import.meta.url));
  res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] || 'application/octet-stream' }); res.end(body);
 } catch (e) { send({ error: String(e) }, 500); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const browser = await chromium.launch();
try {
 const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } }); const errors = [];
 page.on('pageerror', e => errors.push(e.message));
 // Fail unexpected network destinations; fixtures must not contact real services.
 await page.route('**/*', route => new URL(route.request().url()).hostname === '127.0.0.1' ? route.continue() : route.abort());
 await page.goto(`http://127.0.0.1:${server.address().port}/settings`);
 await page.getByRole('tab', { name: 'FleetScope', exact: true }).click();
 await expect(page.getByRole('button', { name: 'Compare FleetScope' })).toBeDisabled();
 await expect(page.getByRole('cell', { name: 'Independent Owner investor owned', exact: true })).toBeVisible();
 assert.equal(writes.length, 0);
 data.configured = true; data.configuredMode = 'live'; await page.getByRole('button', { name: 'Refresh status' }).click();
 await page.getByRole('button', { name: 'Compare FleetScope' }).click();
 await page.getByRole('button', { name: 'Review', exact: true }).click();
 const dialog = page.getByRole('dialog');
 await dialog.getByLabel(/^FleetScope record/).selectOption('remote-truck');
 await expect(dialog).toContainText('Independent Owner (investor)'); await expect(dialog).toContainText('Old Company Label');
 await dialog.getByLabel('Review reason').fill('Verified VIN and recorded ownership'); reject = true;
 await dialog.getByRole('button', { name: 'Save reviewed decision' }).click();
 await expect(dialog).toContainText('FleetScope review changed');
 await expect(dialog.getByLabel('Review reason')).toHaveValue('Verified VIN and recorded ownership');
 await dialog.getByRole('button', { name: 'Save reviewed decision' }).click(); await expect(dialog).toHaveCount(0);
 assert.equal(writes.at(-1).input.sourceRevision, 2); assert.equal(writes.at(-1).input.remoteId, 'remote-truck');
 data.links = [{ kind: 'truck', localId: 'truck', remoteId: 'remote-truck', lastError: '', excludedReason: '', lastSyncedAt: stamp }, { kind: 'truck', localId: 'deleted', remoteId: 'old-truck', lastError: '', excludedReason: '' }];
 data.remote.trucks.push({ ...data.remote.trucks[0], id: 'old-truck', unitNumber: '002', primaryId: 'remote-driver' });
 await page.getByRole('button', { name: 'Refresh status' }).click();
 await page.getByRole('button', { name: 'Review assignments' }).click();
 await expect(dialog).toContainText('002 (deleted in Clustie)'); await expect(dialog).toContainText('Unassigned; retained for retirement');
 await dialog.getByRole('button', { name: 'Queue reviewed assignments' }).click(); await expect(dialog).toHaveCount(0);
 assert.equal(writes.at(-1).path, '/api/fleetscope-sync/assignments');
 data.local.sourceRevision++; await page.getByRole('button', { name: 'Refresh status' }).click();
 await expect(page.getByRole('button', { name: 'Review assignments' })).toBeDisabled();
 data.canReview = false; data.canManage = false; await page.getByRole('button', { name: 'Refresh status' }).click();
 await expect(page.getByRole('button', { name: 'Compare FleetScope' })).toBeDisabled();
 await expect(page.getByRole('button', { name: 'Pause', exact: true })).toBeDisabled();
 await page.setViewportSize({ width: 390, height: 844 });
 assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
 assert.deepEqual(errors, []);
 console.log('FleetScope review browser checks passed: off/read-only, ownership, stale review, preserved draft, deleted crew preview, mobile layout.');
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
