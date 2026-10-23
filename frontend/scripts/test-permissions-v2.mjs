// Run against review-local.mjs: synthetic database, real API, restricted users.
import { chromium, expect } from '@playwright/test';
import { readFile } from 'node:fs/promises';
import { randomBytes } from 'node:crypto';

const access = JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE, 'utf8'));
const { base } = access;
if (!['localhost','127.0.0.1'].includes(new URL(base).hostname)) throw new Error('Local review only');
const browser = await chromium.launch();
const contexts = [];
const token = randomBytes(5).toString('hex');
async function login(email,password) {
 const context=await browser.newContext();contexts.push(context);
 const response=await context.request.post(`${base}/api/auth/login`,{data:{email,password}});
 expect(response.status(),await response.text()).toBe(200);
 const session=await response.json();
 return {context,session,page:await context.newPage()};
}
try {
 const admin=await login(access.email,access.password);
 const change=async(method,path,data)=>{
  const response=await admin.context.request.fetch(`${base}/api${path}`,{method,data,headers:{'X-CSRF-Token':admin.session.csrfToken}});
  expect(response.ok(),`${method} ${path}: ${await response.text()}`).toBe(true);return response;
 };
 const roles=async()=> (await (await admin.context.request.get(`${base}/api/settings/access`)).json()).roles;
 async function user(name,permissions){
  const roleName=`${name} ${token}`;
  await change('POST','/settings/roles',{name:roleName,permissions,expenseCategoryAccess:[]});
  const role=(await roles()).find(r=>r.name===roleName);
  const email=`${name}-${token}@example.test`,password=randomBytes(18).toString('hex');
  await change('POST','/settings/users',{username:roleName,email,password,roleId:role.id,active:true});
  return {...await login(email,password),role};
 }
 const drivers=await (await admin.context.request.get(`${base}/api/drivers`)).json();
 const trucks=await (await admin.context.request.get(`${base}/api/trucks`)).json();
 const driver=drivers.find(d=>d.fullName==='Alex Morgan')??drivers[0];
 const viewer=await user('driver-viewer',['drivers.read']);
 const failures=[];
 viewer.page.on('pageerror',e=>failures.push(e.message));
 viewer.page.on('response',r=>{if(r.url().includes('/api/')&&r.status()>=400)failures.push(`${r.status()} ${r.url()}`)});
 await viewer.page.goto(`${base}/drivers`);
 await expect(viewer.page.getByRole('heading',{name:'Drivers',exact:true})).toBeVisible();
 await expect(viewer.page.getByRole('button',{name:'Add driver',exact:true})).toHaveCount(0);
 await viewer.page.goto(`${base}/drivers/detail?id=${driver.id}`);
 await expect(viewer.page.getByRole('heading',{name:driver.fullName,exact:true})).toBeVisible();
 for(const tab of ['Pay history','Escrow','Ownership','Personal charges','Company & other expenses']) await expect(viewer.page.getByRole('tab',{name:tab,exact:true})).toHaveCount(0);
 const raw=await (await viewer.context.request.get(`${base}/api/drivers/${driver.id}`)).json();
 for(const field of ['payType','payRate','address','city','state','postalCode','emergencyContact','licenseNumber','licenseState','licenseExpires','cdlFileId','cdlFileName'])expect(raw).not.toHaveProperty(field);
 expect((await viewer.context.request.get(`${base}/api/trucks`)).status()).toBe(403);
 expect(failures).toEqual([]);
 const truckViewer=await user('truck-viewer',['trucks.read']);
 await truckViewer.page.goto(`${base}/trucks`);
 await expect(truckViewer.page.getByRole('heading',{name:'Trucks',exact:true})).toBeVisible();
 if(trucks.length){await truckViewer.page.goto(`${base}/trucks/detail?id=${trucks[0].id}`);await expect(truckViewer.page.getByRole('heading',{name:`Truck ${trucks[0].unitNumber}`,exact:true})).toBeVisible();}
 expect((await truckViewer.context.request.get(`${base}/api/drivers`)).status()).toBe(403);
 const updater=await user('updater-viewer',['updaters.read']);
 await updater.page.goto(`${base}/updaters`);
 await expect(updater.page.getByRole('heading',{name:'Updaters',exact:true})).toBeVisible();
 await updater.page.goto(`${base}/dispatchers`);
 await expect(updater.page.getByRole('heading',{name:'Access restricted',exact:true})).toBeVisible();
 expect((await updater.context.request.get(`${base}/api/dispatchers`)).status()).toBe(403);
 expect((await updater.context.request.get(`${base}/api/lookups/investors`)).status()).toBe(403);
 const dispatcher=await user('dispatcher-viewer',['dispatchers.read']);
 const dispatcherFailures=[];
 dispatcher.page.on('response',r=>{if(r.url().includes('/api/')&&r.status()>=400)dispatcherFailures.push(r.url())});
 await dispatcher.page.goto(`${base}/dispatchers`);
 await expect(dispatcher.page.getByRole('heading',{name:'Dispatchers',exact:true})).toBeVisible();
 expect((await dispatcher.context.request.get(`${base}/api/lookups/updaters`)).status()).toBe(200);
 expect((await dispatcher.context.request.get(`${base}/api/updaters`)).status()).toBe(403);
 expect(dispatcherFailures).toEqual([]);
 const board=await user('board-viewer',['board.read','drivers.read']);
 const mutations=[],dialogs=[];
 board.page.on('request',r=>{if(r.url().includes('/api/')&&!['GET','HEAD'].includes(r.method()))mutations.push(r.url())});
 board.page.on('dialog',async d=>{dialogs.push(d.type());await d.dismiss()});
 await board.page.goto(`${base}/gross-board`);
 await expect(board.page.getByText('Read only',{exact:true})).toBeVisible();
 const cell=board.page.locator('[data-grid-cell]').first();
 await expect(cell).toBeVisible();await cell.click();
 for(const key of ['F2','9','Delete','Control+v','Control+z','ArrowRight'])await board.page.keyboard.press(key);
 await cell.dblclick();
 await expect(board.page.locator('[data-grid-cell] input:not([disabled]):not([readonly])')).toHaveCount(0);
 await board.page.locator('aside').getByRole('link',{name:'Drivers',exact:true}).click();
 await expect(board.page).toHaveURL(`${base}/drivers`);
 expect(mutations).toEqual([]);expect(dialogs).toEqual([]);
 const editor=await user('profile-editor',['drivers.read','drivers.edit']);
 const profileInput={fullName:driver.fullName,firstName:driver.firstName,lastName:driver.lastName,active:driver.active,status:driver.status,phone:'555-222-3333',email:driver.email??'',hireDate:driver.hireDate?.slice(0,10)??'',notes:driver.notes??'',isOwnerOperator:driver.isOwnerOperator,dispatcherId:driver.dispatcherId,truckId:driver.truckId,isTeamDriver:false};
 const editHeaders={'X-CSRF-Token':editor.session.csrfToken};
 for(const forged of [{PayRate:0},{PAYRATE:0},{payRate:0},{address:null},{cdlFileId:null}]){
  const response=await editor.context.request.put(`${base}/api/drivers/${driver.id}`,{data:{...profileInput,...forged},headers:editHeaders});expect(response.status(),await response.text()).toBe(403);
 }
 const saved=await editor.context.request.put(`${base}/api/drivers/${driver.id}`,{data:profileInput,headers:editHeaders});
 expect(saved.status(),await saved.text()).toBe(200);expect(await saved.json()).not.toHaveProperty('payRate');
 const fullAfter=await (await admin.context.request.get(`${base}/api/drivers/${driver.id}`)).json();
 expect(fullAfter.payRate).toBe(driver.payRate);expect(fullAfter.address).toBe(driver.address);expect(fullAfter.cdlFileId).toBe(driver.cdlFileId);
 const adder=await user('driver-add-only',['drivers.read','drivers.create']);
 expect((await adder.context.request.post(`${base}/api/drivers`,{data:profileInput,headers:{'X-CSRF-Token':adder.session.csrfToken}})).status()).toBe(403);
 for(const area of ['driver_pay','investor_pay','dispatcher_pay']){
  const finance=await user(area,[`${area}.read`]);
  for(const endpoint of ['driver-pay','investor-pay','dispatcher-pay']){
   const response=await finance.context.request.get(`${base}/api/${endpoint}?weekStart=2026-10-12`);
   expect(response.status(),`${area} -> ${endpoint}: ${await response.text()}`).toBe(endpoint===area.replace('_','-')?200:403);
   if(area==='dispatcher_pay'&&endpoint==='dispatcher-pay'){const data=await response.json();expect(Object.keys(data).sort()).toEqual(['availableWeeks','dispatchers','period']);}
  }
 }
 // Revoke access while a profile editor is open. The capability refresh unmounts it.
 await editor.page.goto(`${base}/drivers/detail?id=${driver.id}`);
 await editor.page.getByRole('button',{name:'Edit driver',exact:true}).click();
 await expect(editor.page.getByRole('dialog')).toBeVisible();
 await change('PUT',`/settings/roles/${editor.role.id}`,{...editor.role,permissions:['drivers.read']});
 await editor.page.evaluate(()=>window.dispatchEvent(new Event('focus')));
 await expect(editor.page.getByRole('dialog')).toHaveCount(0);
 await expect(editor.page.getByText('Your access changed.',{exact:false})).toBeVisible();
 await expect(editor.page.getByRole('button',{name:'Edit driver',exact:true})).toHaveCount(0);
 const liveBoard=await user('live-board',['board.read','board.write','drivers.read']);
 let releaseSave, pendingSave=false, saveRequests=0;
 const gate=new Promise(resolve=>{releaseSave=resolve});
 const liveDialogs=[];
 liveBoard.page.on('dialog',async d=>{liveDialogs.push(d.type());await d.dismiss()});
 await liveBoard.page.route('**/api/gross-board',async route=>{
  if(route.request().method()!=='PUT')return route.continue();
  pendingSave=true;saveRequests++;await gate;
  await route.continue().catch(()=>{});
 });
 await liveBoard.page.goto(`${base}/gross-board`);
 await liveBoard.page.getByLabel(/, miles$/).first().fill('123');
 await expect.poll(()=>pendingSave).toBe(true);
 await change('PUT',`/settings/roles/${liveBoard.role.id}`,{...liveBoard.role,permissions:['board.read','drivers.read']});
 await liveBoard.page.evaluate(()=>window.dispatchEvent(new Event('focus')));
 await expect(liveBoard.page.getByText('Read only',{exact:true})).toBeVisible();
 releaseSave();
 await liveBoard.page.locator('aside').getByRole('link',{name:'Drivers',exact:true}).click();
 await expect(liveBoard.page).toHaveURL(`${base}/drivers`);
 expect(saveRequests).toBe(1);expect(liveDialogs).toEqual([]);
 const tabViewer=await user('financial-tab',['drivers.read','driver_pay.read']);
 await tabViewer.page.goto(`${base}/drivers/detail?id=${driver.id}`);
 await tabViewer.page.getByRole('tab',{name:'Pay history',exact:true}).click();
 await expect(tabViewer.page.getByRole('tab',{name:'Pay history',exact:true})).toHaveAttribute('aria-selected','true');
 await change('PUT',`/settings/roles/${tabViewer.role.id}`,{...tabViewer.role,permissions:['drivers.read']});
 await tabViewer.page.evaluate(()=>window.dispatchEvent(new Event('focus')));
 await expect(tabViewer.page.getByRole('tab',{name:'Pay history',exact:true})).toHaveCount(0);
 await expect(tabViewer.page.getByRole('tab',{name:'Overview',exact:true})).toHaveAttribute('aria-selected','true');
 await admin.page.goto(`${base}/settings`);
 await admin.page.getByRole('button',{name:'Roles & permissions',exact:true}).click();
 await admin.page.getByRole('button',{name:'Create role',exact:true}).click();
 for(const label of ['View Drivers','View Trucks','View Driver private details','View Driver Pay','View Investor Pay'])await expect(admin.page.getByLabel(label,{exact:true})).toBeVisible();
 await expect(admin.page.getByLabel('Edit Drivers',{exact:true})).toBeDisabled();
 await admin.page.getByLabel('View Drivers',{exact:true}).check();
 await expect(admin.page.getByLabel('Edit Drivers',{exact:true})).toBeEnabled();
 await expect(admin.page.getByLabel('View Driver private details',{exact:true})).not.toBeChecked();
 console.log('Permissions v2 passed: independent profiles, raw JSON privacy, read-only board, safe edits, onboarding boundary, separate payroll, live access loss and Settings dependencies.');
} finally {for(const context of contexts)await context.close();await browser.close();}
