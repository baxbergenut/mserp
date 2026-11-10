import { expect, request } from '@playwright/test';
import { randomUUID, randomBytes } from 'node:crypto';
import { join } from 'node:path';

export async function runEscrowTasksE2E({ page, base, sql, schema, temp }) {
 const ids = Object.fromEntries(['full','partial','kept','early','home','vacation'].map(key => [key, randomUUID()]));
 const employee = randomUUID(), outsider = randomUUID(), role = randomUUID(), password = randomBytes(24).toString('hex');
 const run = statement => sql(`SET search_path TO ${schema},public; ${statement}`);
 run(`INSERT INTO app_roles(id,name,permissions) VALUES('${role}','Escrow team',ARRAY['tasks.read','tasks.create','tasks.edit','tasks.delete','tasks.review','escrow.read','escrow.create','escrow.edit','escrow.delete']);
 INSERT INTO app_users(id,username,email,password_hash,role_id) VALUES
 ('${employee}','Nate Test','nate-test@example.com',crypt('${password}',gen_salt('bf')),'${role}'),
 ('${outsider}','Outside Test','outside-test@example.com',crypt('${password}',gen_salt('bf')),'${role}');`);
 for (const [kind,id] of Object.entries(ids)) {
  const status = ['home','vacation'].includes(kind) ? kind : 'terminated';
  const opening = kind === 'vacation' ? 0 : ['full','home'].includes(kind) ? 2000 : 2500;
  run(`INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate,status,termination_date)
   VALUES('${id}','Escrow ${kind}','escrow ${kind}','cpm',0.75,'${status}',(now() AT TIME ZONE 'America/New_York')::date-${kind==='early'?29:30});
   INSERT INTO driver_escrows(driver_id,driver_name,start_date,amount,opening_paid) VALUES('${id}','Escrow ${kind}',(now() AT TIME ZONE 'America/New_York')::date-40,2500,${opening});`);
 }
 run(`INSERT INTO driver_escrow_payments(escrow_id,week_start,amount)
 SELECT id,date_trunc('week',now() AT TIME ZONE 'America/New_York')::date-7,400
 FROM driver_escrows WHERE driver_id='${ids.vacation}';`);
 await page.goto(`${base}/settings`);
 await page.getByRole('tab', { name: 'System tasks', exact: true }).click();
 const assignees=page.getByRole('group',{name:'Escrow release (30 days after termination) assignees'});
 await assignees.locator('select').first().selectOption({label:'Nate Test'});
 await assignees.locator('select').first().selectOption({label:'investor-e2e'});
 await page.getByRole('button',{name:'Save Escrow release (30 days after termination) assignment',exact:true}).click();
 await expect(page.getByRole('status')).toContainText('assignment saved');
 await page.reload();
 await expect(assignees.getByRole('button',{name:'Remove Nate Test from Escrow release (30 days after termination)',exact:true})).toBeVisible();
 await expect(assignees.getByRole('button',{name:'Remove investor-e2e from Escrow release (30 days after termination)',exact:true})).toBeVisible();
 const session=await (await page.request.get(`${base}/api/auth/session`)).json();
 const headers={'X-CSRF-Token':session.csrfToken};
 await page.goto(`${base}/accounting/escrow`);
 await expect(page.getByRole('tab',{name:'Active',exact:true})).toHaveAttribute('aria-selected','true');
 await expect(page.getByRole('row').filter({hasText:'Escrow home'})).toContainText('Partially collected');
 const vacationRow=page.getByRole('row').filter({hasText:'Escrow vacation'});
 await expect(vacationRow.getByRole('cell',{name:'$400.00',exact:true})).toBeVisible();
 await vacationRow.getByRole('button',{name:'Reduce balance',exact:true}).click();
 await page.getByLabel('New balance ($)',{exact:true}).fill('125.25');
 await page.getByLabel('Correction reason',{exact:true}).fill('Reconciled escrow ledger');
 await page.getByRole('button',{name:'Save correction',exact:true}).click();
 await expect(page.getByRole('dialog')).toHaveCount(0);
 await expect(vacationRow.getByRole('cell',{name:'$125.25',exact:true})).toBeVisible();
 await expect(vacationRow.getByRole('cell',{name:'$2,374.75',exact:true})).toBeVisible();
 await vacationRow.getByRole('button',{name:'Payment history'}).click();
 await expect(page.getByRole('heading',{name:'Balance reductions'})).toBeVisible();
 await expect(page.getByText('Reconciled escrow ledger',{exact:true})).toBeVisible();
 await expect(page.getByText('-$274.75',{exact:true})).toBeVisible();
 await expect(page.getByText('Total collected $400.00 · Total released $0.00',{exact:true})).toBeVisible();
 await page.getByRole('button',{name:'Close payment history'}).click();
 await expect(page.getByRole('row').filter({hasText:'Escrow full'})).toHaveCount(0);
 await page.getByRole('tab',{name:'Terminated',exact:true}).click();
 const fullRow=page.getByRole('row').filter({hasText:'Escrow full'});
 await expect(fullRow).toContainText('Partially released');
 await fullRow.getByRole('button',{name:'Payment history'}).click();
 await page.getByRole('button',{name:'Edit previously paid',exact:true}).click();
 await page.getByLabel('Previously paid ($)',{exact:true}).fill('2500');
 await page.getByLabel('Correction reason',{exact:true}).fill('Verified imported balance');
 await page.getByRole('button',{name:'Save correction',exact:true}).click();
 await expect(page.getByRole('dialog')).toHaveCount(0);
 await expect(fullRow).toContainText('2,500.00');
 // Keep the Tasks page open while the real one-minute worker makes tasks due.
 await page.goto(`${base}/tasks`);
 const fullTask=page.getByRole('button',{name:'Release escrow: Escrow full',exact:true});
 await expect(fullTask).toBeVisible({timeout:75000});
 await expect(page.getByRole('button',{name:'Release escrow: Escrow early',exact:true})).toHaveCount(0);
 const tasks=await (await page.request.get(`${base}/api/tasks?search=Release%20escrow&status=open`)).json();
 expect(tasks.total).toBe(3);
 const taskFor=kind=>tasks.items.find(item=>item.title===`Release escrow: Escrow ${kind}`);
 for(const email of ['nate-test@example.com','outside-test@example.com']) {
  const client=await request.newContext({baseURL:base});
  const login=await client.post('/api/auth/login',{data:{email,password}});expect(login.status()).toBe(200);
  const list=await (await client.get('/api/tasks?search=Release%20escrow&status=open')).json();
  expect(list.total).toBe(email.startsWith('nate')?3:0);
  const detail=await client.get(`/api/tasks/escrow/${taskFor('full').id}`);expect(detail.status()).toBe(email.startsWith('nate')?200:404);
  await client.dispose();
 }
 const blocked=await page.request.patch(`${base}/api/tasks/custom/${taskFor('full').id}`,{headers,data:{completed:true}});expect([400,404,409]).toContain(blocked.status());
 await fullTask.click();
 await expect(page.getByLabel('Reason',{exact:true})).toBeVisible();
 await expect(page.getByLabel('Outcome',{exact:true})).toHaveCount(0);
 await expect(page.getByRole('button',{name:'Refresh balances',exact:true})).toHaveCount(0);
 await page.getByRole('dialog').getByRole('button',{name:'Release',exact:true}).click();
 await page.getByLabel('Release amount',{exact:true}).fill('2500');
 await page.getByRole('dialog',{name:'Release escrow',exact:true}).getByRole('button',{name:'Release',exact:true}).click();
 await expect(page.getByRole('dialog',{name:'Release escrow',exact:true})).toHaveCount(0);
 await expect(page.getByRole('dialog')).toContainText('Released $2,500.00');
 await expect(page.getByLabel('Reason',{exact:true})).toHaveCount(0);
 await page.getByRole('button',{name:'Complete escrow review',exact:true}).click();
 await expect(fullTask).toHaveCount(0);
 await page.getByRole('button',{name:'Release escrow: Escrow partial',exact:true}).click();
 await page.getByRole('dialog').getByRole('button',{name:'Release',exact:true}).click();
 await page.getByLabel('Release amount',{exact:true}).fill('500');
 await page.getByRole('dialog',{name:'Release escrow',exact:true}).getByRole('button',{name:'Release',exact:true}).click();
 await expect(page.getByRole('dialog',{name:'Release escrow',exact:true})).toHaveCount(0);
 await expect(page.getByLabel('Reason',{exact:true})).toBeVisible();
 await page.getByLabel('Reason',{exact:true}).fill('Retain balance for documented repair');
 await page.getByRole('button',{name:'Complete escrow review',exact:true}).click();
 await expect(page.getByRole('dialog')).toHaveCount(0);
 await page.getByRole('button',{name:'Release escrow: Escrow kept',exact:true}).click();
 await page.getByLabel('Reason',{exact:true}).fill('Keep funds for documented final charges');
 await page.getByRole('button',{name:'Complete escrow review',exact:true}).click();
 await expect(page.getByRole('dialog')).toHaveCount(0);
 await page.getByLabel('Task status',{exact:true}).selectOption('completed');
 await expect(page.getByRole('button',{name:'Release escrow: Escrow full',exact:true})).toBeVisible();
 await page.screenshot({path:join(temp,'escrow-tasks-completed.png'),fullPage:true});
 await page.goto(`${base}/accounting/escrow`);
 await page.getByRole('tab',{name:'Terminated',exact:true}).click();
 await expect(page.getByRole('row').filter({hasText:'Escrow full'})).toContainText('Released');
 await expect(page.getByRole('row').filter({hasText:'Escrow partial'})).toContainText('Partially released');
 await expect(page.getByRole('row').filter({hasText:'Escrow kept'})).toContainText('Not released');
 await page.screenshot({path:join(temp,'escrow-terminated.png'),fullPage:true});
 console.log('Escrow E2E passed: real timed worker/SSE, day-29 exclusion, two assignees and privacy, opening correction, full/partial/kept decisions, protected completion and both tabs.');
}
