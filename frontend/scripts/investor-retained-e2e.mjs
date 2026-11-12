import { expect } from '@playwright/test';
import { join } from 'node:path';

export async function runInvestorRetainedE2E({ page, base, sql, schema, temp }) {
  const truck='18200000-0000-0000-0000-000000006002', owner='18200000-0000-0000-0000-000000006003';
  const weeks=['2026-10-19','2026-10-26','2026-11-02'];
  sql(`SET search_path TO ${schema},public;
    INSERT INTO investors(id,full_name) VALUES('${owner}','Retained Investor');
    INSERT INTO trucks(id,unit_number,owner_id) VALUES('${truck}','KEEP-1','${owner}');
    UPDATE truck_ownership_history SET start_known=false WHERE truck_id='${truck}';
    INSERT INTO truck_settlement_terms(truck_id,owner_id,week_start,share_percent) VALUES('${truck}','${owner}','2026-01-05',88);
    INSERT INTO investor_pay_weeks(truck_id,owner_id,week_start,edits) VALUES('${truck}','${owner}','${weeks[0]}',
    '{"driverId":"${truck}","weekStart":"${weeks[0]}","adjustments":[{"id":"earning","kind":"addition","name":"Reviewed earnings","amount":"1000.00"}]}');`);
  const session=await (await page.request.get(`${base}/api/auth/session`)).json();
  const headers={'x-csrf-token':session.csrfToken};
  const read=async week=>{
    const res=await page.request.get(`${base}/api/investor-pay?weekStart=${week}&driverId=${truck}`);
    expect(res.ok(),await res.text()).toBe(true); return res.json();
  };
  const go=async week=>{
    await page.goto(`${base}/accounting/investor-pay?weekStart=${week}&truckId=${truck}`);
    await expect(page.getByRole('button',{name:'Retained Investor KEEP-1',exact:true})).toBeVisible({timeout:30000});
  };
  const expand=async()=>{const button=page.getByRole('button',{name:'Retained Investor KEEP-1',exact:true});if(await button.getAttribute('aria-expanded')==='false')await button.click();};
  const keep=page.getByRole('button',{name:'Keep money and finalize Retained Investor KEEP-1',exact:true});
  await go(weeks[0]); await expect(keep).toBeEnabled({timeout:30000});
  const commandPromise=page.waitForRequest(req=>req.url().includes('/investor-pay/finalize')&&req.method()==='POST');
  await keep.click(); const command=(await commandPromise).postDataJSON();
  expect(command.keepMoney).toBe(true);
  await expect(page.getByText('Money kept',{exact:true})).toBeVisible();
  await expect.poll(async()=> (await read(weeks[0])).drivers[0]?.retainedPay).toBe('1000.00');
  const replay=await page.request.post(`${base}/api/investor-pay/finalize?receipt=1`,{headers,data:command});
  expect(replay.ok(),await replay.text()).toBe(true);
  const changed=await page.request.post(`${base}/api/investor-pay/finalize?receipt=1`,{headers,data:{...command,keepMoney:false}});
  expect(changed.ok()).toBe(false);
  await page.reload(); await expect(page.getByText('Money kept',{exact:true})).toBeVisible();
  await expand();
  await expect(page.getByText('Money kept for next week',{exact:true})).toBeVisible();
  await page.screenshot({path:join(temp,'investor-money-kept.png'),fullPage:true});
  await go(weeks[1]); await expect(keep).toBeEnabled({timeout:30000});
  await expand();
  await expect(page.getByText('Money kept from prior week',{exact:true})).toBeVisible();
  expect((await read(weeks[1])).drivers[0].investorCarry).toBe('1000.00');
  await keep.click(); await expect(page.getByText('Money kept',{exact:true})).toBeVisible();
  await go(weeks[2]);
  await expect(page.getByRole('button',{name:'Finalize Retained Investor KEEP-1',exact:true})).toBeEnabled({timeout:30000});
  await page.getByRole('button',{name:'Finalize Retained Investor KEEP-1',exact:true}).click();
  await expect.poll(async()=> (await read(weeks[2])).drivers[0]?.settlement?.finalized).toBe(true);
  const paid=(await read(weeks[2])).drivers[0];
  expect(paid.investorCarry).toBe('1000.00'); expect(paid.retainedPay).toBeUndefined();
  const reopen=async week=>{
    const report=await read(week), row=report.drivers[0];
    return page.request.post(`${base}/api/investor-pay/reopen?receipt=1`,{headers,data:{weekStart:week,driverId:truck,revision:report.revision,expectedStatementRevision:row.statementRevision,requestId:crypto.randomUUID(),override:false}});
  };
  const blocked=await reopen(weeks[0]); expect(blocked.ok()).toBe(false); expect(await blocked.text()).toContain('later statement');
  for(const week of [...weeks].reverse()){const res=await reopen(week);expect(res.ok(),await res.text()).toBe(true);}
  await go(weeks[0]); await expect(keep).toBeEnabled({timeout:30000});
  const restored=(await read(weeks[1])).drivers[0];expect(restored.investorCarry).toBeUndefined();
  console.log('Investor retained pay E2E passed: UI retain, reload, carry, repeat retention, payout, exact replay and guarded reopening.');
}
