import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { chromium } from '@playwright/test';
import { installPayrollBrowserTiming } from './payroll-browser-timing.mjs';

const server = createServer((req, res) => {
  if (req.url === '/api/driver-pay') return setTimeout(() => { res.setHeader('content-type', 'application/json'); res.end('{}'); }, 120);
  res.setHeader('content-type', 'text/html');
  res.end('<button id="action">Act</button><span role="status">Saved</span><div id="details"></div>');
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const browser = await chromium.launch();
try {
  const context = await browser.newContext();
  await context.addInitScript(installPayrollBrowserTiming);
  const page = await context.newPage();
  await page.goto(`http://127.0.0.1:${server.address().port}`);
  await page.evaluate(() => {
    window.__payrollTiming.begin('save');
    document.querySelector('#action').onclick = async () => {
      document.querySelector('[role="status"]').textContent = 'Saving…';
      await fetch('/api/driver-pay', { method: 'PUT' });
      document.querySelector('[role="status"]').textContent = 'Saved';
    };
  });
  // Setup time is deliberately outside the actual user dispatch.
  await page.waitForTimeout(200);
  assert.equal(await page.evaluate(() => window.__payrollTiming.active.started), null);
  await page.click('#action');
  assert.equal(await page.evaluate(() => window.__payrollTiming.active.visible), null);
  await page.waitForFunction(() => window.__payrollTiming.active.visible !== null);
  const saved = await page.evaluate(() => window.__payrollTiming.active);
  assert.ok(saved.committedResponse && saved.visible - saved.started >= 120);
  await page.evaluate(() => {
    window.__payrollTiming.begin('cachedExpansion');
    document.querySelector('#action').onclick = () => {
      document.querySelector('#details').innerHTML = '<div role="region" aria-label="Synthetic weekly loads">Loaded</div>';
    };
  });
  await page.click('#action');
  await page.waitForFunction(() => window.__payrollTiming.active.visible !== null);
  const expanded = await page.evaluate(() => window.__payrollTiming.active);
  assert.ok(!expanded.committedResponse && expanded.visible > expanded.started);
  await page.evaluate(() => {
    document.body.insertAdjacentHTML('beforeend', '<table><tbody><tr><td><button aria-expanded="false">Synthetic driver</button></td></tr></tbody></table>');
  });
  await page.waitForFunction(() => window.__payrollTiming.navigationVisible !== null);
  console.log('Payroll browser timing: setup excluded, committed response required, painted expansion and navigation observed.');
} finally {
  await browser.close();
  await new Promise(resolve => server.close(resolve));
}
