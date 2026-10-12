import { expect } from '@playwright/test';
import { join } from 'node:path';

export async function runDriverSetupE2E({ page, base, sql, schema, temp }) {
 const read=async path=>{const r=await page.request.get(`${base}/api${path}`);expect(r.status(),await r.text()).toBe(200);return r.json();};
 const intake='e8000000-0000-4000-8000-000000000001';
 sql(`SET search_path TO ${schema},public;
 INSERT INTO fleetscope_driver_intake(id,company_id,fleetscope_driver_id,normalized_name,driver_data,occurred_at)
 VALUES('${intake}',gen_random_uuid(),gen_random_uuid(),'wizard intake driver','{"fullName":"Wizard Intake Driver","hireDate":"2026-10-07","driverType":"company"}',now());`);
 for(const mode of ['manual','intake']) {
  const name=mode==='manual'?'Wizard Manual Driver':'Wizard Intake Driver';
  const unit=mode==='manual'?'WIZARD-MANUAL':'WIZARD-INTAKE';
  if(mode==='manual'){await page.goto(`${base}/drivers`);await page.getByRole('button',{name:'Add driver',exact:true}).click();}
  else await page.goto(`${base}/drivers?setup=${intake}`);
  const dialog=page.getByRole('dialog'),next=()=>dialog.getByRole('button',{name:'Continue',exact:true}).click();
  await expect(dialog.getByRole('progressbar')).toHaveAttribute('aria-valuemax','5');
  await dialog.getByLabel('Full name',{exact:true}).fill(name);
  await dialog.getByLabel('City',{exact:true}).fill('Louisville');await dialog.getByLabel('State / province',{exact:true}).fill('KY');
  await expect(dialog.locator('input[type=file]')).toHaveCount(0);
  await next();await expect(dialog.getByLabel('Truck',{exact:true})).toHaveValue('__new__');
  await dialog.getByLabel('Rate per mile ($)',{exact:true}).fill('0.65');
  await next();await dialog.getByLabel('Unit number',{exact:true}).fill(unit);
  if(mode==='manual')await dialog.getByRole('combobox',{name:/^Owner/}).selectOption('__driver__');
  await next();await dialog.getByRole('button',{name:'Remind me later',exact:true}).first().click();
  await next();await dialog.getByLabel('Driver started working',{exact:true}).fill('2026-10-07');
  await page.screenshot({path:join(temp,`driver-setup-${mode}-finalize.png`)});
  await dialog.getByRole('button',{name:mode==='manual'?'Create driver':'Complete setup',exact:true}).click();
  await expect(dialog).toHaveAttribute('aria-label',`${name} saved`);
  await dialog.getByRole('button',{name:'Done',exact:true}).click();
  const driver=(await read('/drivers')).find(d=>d.fullName===name);
  expect(driver).toMatchObject({city:'Louisville',state:'KY',truckUnit:unit});expect(driver).not.toHaveProperty('driverHome');
  const truck=await read(`/trucks/${driver.truckId}`);expect(truck.driverId).toBe(driver.id);expect(truck.ownerName).toBe(mode==='manual'?name:'MS Express Inc.');
  const assignments=await read(`/drivers/${driver.id}/assignments`);expect(new Date(assignments.find(a=>a.kind==='truck').assignedAt).toISOString()).toBe('2026-10-05T04:00:00.000Z');
  await page.goto(`${base}/drivers/detail?id=${driver.id}&services=1`);
  await expect(page.getByLabel('Relay setup')).toContainText('Reminder set');
  await expect(page.getByRole('button',{name:'Remind me later',exact:true})).toHaveCount(0);
 }
 const session=await read('/auth/session');
 const removed=await page.request.post(`${base}/api/cdl-files`,{headers:{'X-CSRF-Token':session.csrfToken}});expect(removed.status()).toBe(403);
 console.log('Driver setup real API/browser passed: manual and intake, atomic new truck, driver/company ownership, dated assignments, canonical address, service reminders, retired CDL upload.');
}
