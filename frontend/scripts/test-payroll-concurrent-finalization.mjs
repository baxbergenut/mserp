// Held HTTP responses prove independent finalizations overlap and receipts merge
// in reverse order without replacing another statement's unsaved comment.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname } from 'node:path';
const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html':'text/html', '.js':'application/javascript', '.css':'text/css' }[extname(file)] ?? 'application/octet-stream' }); res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const browser = await chromium.launch();
const week = '2026-10-26';
try {
  for (const kind of ['driver', 'investor']) {
    const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } });
    const errors = [], requests = [], release = new Map();
    page.on('pageerror', e => errors.push(e.message));
    let blockFinalization = false, preparationState = 'ready';
    let savedComment = '';
    const rows = ['Alpha', 'Bravo', 'Charlie'].map((name, i) => ({
      id: `18200000-0000-4000-8000-00000000000${i+1}`, fullName: name, truckUnit: String(i+101),
      ...(kind === 'investor' ? { investorId: `owner-${i}` } : {}),
      payType:'cpm', payRate:'0.75', fuelTotal:'0.00', tollTotal:'0.00', issues:[], loads:[],
      statementRevision: `18200000-0000-4000-8000-00000000100${i+1}`,
      expenseRevision: `reviewed-expenses-${i+1}`,
      edits: { driverId: `18200000-0000-4000-8000-00000000000${i+1}`, weekStart:week, version:1, notes:'', comments:{}, adjustments:[], fuelOverride:null, tollOverride:null }
    }));
    const snapshot = () => ({ weekStart:week, revision:'inputs:fixture', drivers:structuredClone(rows), preparation:{ state:preparationState, generation:'generation-1' } });
    await page.route('**/api/**', async route => {
      const req=route.request(), url=new URL(req.url()), path=url.pathname.slice(4);
      if (path === '/auth/session') return route.fulfill({json:{ user:{id:'user',username:'Test',permissions:[`${kind}_pay.read`,`${kind}_pay.edit`,`${kind}_pay.finalize`,`${kind}_pay.reopen`],theme:'default'},csrfToken:'fixture'}});
      if (path.endsWith('/availability')) return route.fulfill({json:{firstWeek:week,currentWeek:week}});
      if (path === `/${kind}-pay` && req.method() === 'PUT') {
        const input=req.postDataJSON(), row=rows.find(row=>row.id===input.driverId);
        expect(row.preparationState).toBe('ready'); savedComment=input.notes;
        row.edits={...input,version:row.edits.version+1};
        return route.fulfill({json:{...row.edits,statementRevision:row.statementRevision,committedStatement:structuredClone(row)}});
      }
      if (path === `/${kind}-pay/finalize`) {
        const input=req.postDataJSON(); requests.push(input);
        const row=rows.find(row=>row.id===input.driverId);
        expect(input.expectedStatementRevision).toBe(row.statementRevision);
        expect(input.expenseRevision).toBe(row.expenseRevision);
        await new Promise(resolve=>release.set(input.driverId, resolve));
        if (blockFinalization) return route.fulfill({status:400,json:{error:`Review ${row.fullName}`,code:'finalization_blocked'}});
        row.settlement={finalized:true,version:1,finalizedBy:'Reviewer',finalizedAt:new Date().toISOString()};
        row.statementRevision += '-final';
        return route.fulfill({json:{weekStart:week,revision:'inputs:fixture',receipt:true,drivers:[structuredClone(row)]}});
      }
      if (path === `/${kind}-pay` || path.startsWith(`/${kind}-pay/statements/`)) {
        const value=snapshot();
        if (path.includes('/statements/')) value.drivers=value.drivers.filter(row=>path.endsWith(row.id));
        return route.fulfill({json:value});
      }
      return route.fulfill({json:path==='/tasks/count'?{count:0}:[]});
    });
    const base = process.env.MSERP_PAYROLL_BROWSER_URL ?? `http://127.0.0.1:${server.address().port}`;
    await page.goto(`${base}/accounting/${kind}-pay?weekStart=${week}`);
    const alpha=page.getByRole('button',{name:new RegExp('^Finalize Alpha')}), bravo=page.getByRole('button',{name:new RegExp('^Finalize Bravo')});
    await alpha.click();
    await expect(alpha).toBeDisabled(); await expect(alpha).toHaveText('Finalizing…');
    await expect(bravo).toBeEnabled(); await bravo.click();
    await expect.poll(()=>requests.length).toBe(2);
    expect(requests[0].driverId).not.toBe(requests[1].driverId);
    await alpha.dispatchEvent('click'); // Disabled/stale click cannot enqueue a duplicate.
    expect(requests).toHaveLength(2);
    const charlie=page.getByRole('button',{name:kind==='investor'?'Charlie 103':'Charlie',exact:true});
    if (await charlie.getAttribute('aria-expanded') === 'false') await charlie.click();
    await page.getByRole('button',{name:'Charlie, weekly comment',exact:true}).click();
    await page.getByLabel('Comment',{exact:true}).fill('Keep this draft while the other two finalize');
    release.get(rows[1].id)();
    await expect(page.getByRole('row').filter({hasText:'Bravo'}).locator('td').last()).toHaveText('Reviewer');
    await expect(alpha).toBeDisabled();
    await expect(page.getByLabel('Comment',{exact:true})).toHaveValue('Keep this draft while the other two finalize');
    release.get(rows[0].id)();
    await expect(page.getByRole('row').filter({hasText:'Alpha'}).locator('td').last()).toHaveText('Reviewer');
    await expect(page.getByRole('progressbar',{name:'Finalization'})).toHaveAttribute('aria-valuetext','2 of 3 statements finalized');
    await expect(page.getByLabel('Comment',{exact:true})).toHaveValue('Keep this draft while the other two finalize');
    expect(requests).toHaveLength(2);
    // Two blocked responses must each retain their own review instead of
    // replacing one another while the first dialog is open.
    for (const row of rows) delete row.settlement;
    blockFinalization = true; release.clear();
    page.once('dialog', dialog => dialog.accept());
    await page.reload();
    await alpha.click(); await bravo.click();
    await expect.poll(()=>requests.length).toBe(4);
    release.get(rows[0].id)();
    const review=page.getByRole('dialog',{name:'Finalization issues',exact:true});
    await expect(review).toContainText('Review Alpha');
    release.get(rows[1].id)();
    await expect(bravo).toBeEnabled();
    await expect(review).toContainText('Review Alpha');
    await review.getByRole('button',{name:'Cancel',exact:true}).click();
    await expect(review).toContainText('Review Bravo');
    await review.getByRole('button',{name:'Cancel',exact:true}).click();
    await expect(review).toHaveCount(0);
    // A board edit makes Alpha stale; independent payrolls remain actionable.
    preparationState='updating'; blockFinalization=false; release.clear();
    rows.forEach((row,i)=>{delete row.settlement; row.preparationState=i===0?'updating':'ready';});
    await page.reload();
    await expect(alpha).toBeDisabled(); await expect(bravo).toBeEnabled();
    await bravo.click(); await expect.poll(()=>requests.length).toBe(5);
    release.get(rows[1].id)();
    await expect(page.getByRole('row').filter({hasText:'Bravo'}).locator('td').last()).toHaveText('Reviewer');
    if (await charlie.getAttribute('aria-expanded') === 'false') await charlie.click();
    await page.getByRole('button',{name:'Charlie, weekly comment',exact:true}).click();
    await page.getByLabel('Comment',{exact:true}).fill('Independent edit while another payroll updates');
    await page.getByRole('button',{name:'Save comment',exact:true}).click();
    await expect.poll(()=>savedComment).toBe('Independent edit while another payroll updates');
    await expect(alpha).toBeDisabled();
    expect(errors).toEqual([]);
    await page.close();
  }
  console.log('Concurrent finalization passed on Driver and Investor Pay: overlapping requests, per-row busy state, duplicate suppression, reverse receipts, preserved drafts and progress.');
} finally { await browser.close(); server.closeAllConnections(); await new Promise(resolve=>server.close(resolve)); }
