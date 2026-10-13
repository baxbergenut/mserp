// Isolated browser fixtures exercise shared payroll editing and table geometry.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname } from 'node:path';

const week = '2026-09-28';
const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = path === '/' ? 'index.html' : extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] ?? 'application/octet-stream' });
    res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const base = `http://127.0.0.1:${server.address().port}`;
const browser = await chromium.launch();
try {
  for (const investor of [false, true]) {
    const driver = {
      id: 'statement', fullName: 'Grid Test', truckUnit: 'T1', payType: 'gross_percentage', payRate: '80', isOwnerOperator: true,
      dispatcherId: '', dispatcherName: '', fuelTotal: '40.00', tollTotal: '10.00',
      ...(investor ? { investorId: 'owner', truckId: 'statement', autoCharges: [{ source: 'driver:operator', name: 'Driver earnings · Operator', amount: '-80.00' }] } : {}),
      loads: Array.from({ length: 4 }, (_, slot) => ({ date: week, slot, loadNumber: `L${slot}`, loadRecordId: slot + 1, commentKey: `load${slot}`, boardVersion: 1,
        originalRate: '400.00', systemOriginalRate: '401.00', driverGross: '350.00', totalMiles: '90.00', systemMiles: '91.00', loadedMiles: '80.00', deadheadMiles: '10.00', fee: '280.00', pickupDate: week, pickupLocation: 'Columbus, OH', deliveryLocation: 'Pittsburgh, PA', issues: [] })),
      edits: { driverId: 'statement', weekStart: week, version: 0, notes: '', comments: {}, adjustments: [], fuelOverride: null, tollOverride: null,
        expenseDeductions: [{ expenseId: 'expense', name: 'Equipment', expenseDate: week, amount: '25.00', total: '100.00', available: '100.00', openingBalance: '100.00', remaining: '75.00', version: 1, saved: true }],
        generatedCharges: [{ scheduleId: 'schedule', kind: 'recurring', name: 'Insurance', amount: '-50.00', scheduledAmount: '-50.00', overridden: false, version: 1, scheduleVersion: 1, typeVersion: 1 }],
      },
    };
    let detailReads=0, summaryReads=0;
    const errors = [];
    const page = await browser.newPage({ viewport: { width: 1800, height: 1000 } });
    page.on('pageerror', e => errors.push(e.message));
    await page.route('**/api/**', async route => {
      const request = route.request(), path = new URL(request.url()).pathname.slice(4);
      if (path === '/driver-pay' || path === '/investor-pay') {
        const summary=new URL(request.url()).searchParams.get('summary')==='1';
        let row=structuredClone(driver);
        if(summary){summaryReads++;row.summary={loads:4,missingFees:0,review:0,fee:'1120.00',payable:investor?'915.00':'995.00'};row.loads=[];row.autoCharges=[];row.edits={driverId:'statement',weekStart:week,version:0,notes:'',comments:{},adjustments:[]};
          expect(JSON.stringify(row).length).toBeLessThan(JSON.stringify(driver).length/2);
        } else {detailReads++;await new Promise(resolve=>setTimeout(resolve,150));}
        return route.fulfill({json:{weekStart:week,revision:'fixture',drivers:[row]}});
      }
      const fixtures = {
        '/auth/session': { user: { id: 'user', username: 'Test', expenseCategoryAccess:[{categoryId:'category',canView:true,canEdit:true,canCreate:true}], permissions: ['payroll.read', 'payroll.write', 'payroll.finalize', 'fleet.read'], theme: 'solarized-dark' }, csrfToken: 'fixture' },
        '/driver-pay': { weekStart: week, revision: 'fixture', drivers: [driver] },
        '/investor-pay': { weekStart: week, revision: 'fixture', drivers: [driver] },
        '/tasks/count': { count: 0 },
      };
      return route.fulfill({ json: fixtures[path] ?? [] });
    });

    await page.goto(base+'/accounting/'+(investor?'investor':'driver')+'-pay?weekStart='+week);
    const expand=page.getByRole('button',{name:investor?'Grid Test T1':'Grid Test',exact:true});
    await expect(expand).toBeVisible(); expect(summaryReads).toBe(1);expect(detailReads).toBe(0);
    await expect(expand).toHaveAttribute('aria-expanded','false');
    const row=expand.locator('xpath=ancestor::tr');
    await expect(row).toContainText(investor?'$915.00':'$995.00');
    await expect(page.getByRole('region',{name:'Grid Test weekly loads'})).toHaveCount(0);
    await expand.click();
    await expect(page.getByText('Loading statement…',{exact:true})).toBeVisible();
    await expect(page.getByRole('region',{name:'Grid Test weekly loads'})).toBeVisible();expect(detailReads).toBe(1);
    await expand.click();await expand.click();expect(detailReads).toBe(1);
    await expect(page.getByLabel('Grid Test, Fuel, amount',{exact:true})).toHaveValue('-40.00');
    await expand.click();
    await page.getByRole('button',{name:'Reload',exact:true}).click();
    await expect.poll(()=>summaryReads).toBe(2);expect(detailReads).toBe(1);
    await page.getByRole('button',{name:investor?'Grid Test T1':'Grid Test',exact:true}).click();
    await expect(page.getByRole('region',{name:'Grid Test weekly loads'})).toBeVisible();expect(detailReads).toBe(2);
    expect(errors).toEqual([]);await page.close();
  }
  console.log('Both payroll pages: compact initial summaries, expansion-only details, cached reopen, and refresh invalidation passed.');
}finally{await browser.close();await new Promise(resolve=>server.close(resolve));}
