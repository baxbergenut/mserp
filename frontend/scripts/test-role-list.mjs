// Run against the /api static build. Real persistence and authorization are
// covered by TestAccessDatabase; these fixtures exercise browser lifecycle/UI.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname } from 'node:path';

const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = path === '/' ? 'index.html' : extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] ?? 'application/octet-stream' }); res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const base = `http://127.0.0.1:${server.address().port}`;

let browser;
try {
  browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  const errors=[]; page.on('pageerror', error=>errors.push(error.message));
  const permission=(key,label)=>({key,label,group:'Loads',action:key.split('.')[1],requires:[],sensitive:false});
  const data={ users:[{id:'u1',roleId:'viewer'}], permissions:[permission('loads.read','View imported loads'),permission('loads.edit','Edit loads'),permission('dispatcher_compensation.read','Retired permission')], expenseCategories:[{id:'other',name:'Other',active:true}], roles:[
    {id:'admin',name:'Administrator',system:true,permissions:[],expenseCategoryAccess:[],version:1},
    {id:'viewer',name:'Loads viewer',system:false,permissions:['loads.read','unknown.key'],expenseCategoryAccess:[{categoryId:'other',canView:true,canCreate:true,canEdit:false,canDelete:false}],version:1},
    {id:'empty',name:'Empty role',system:false,permissions:[],expenseCategoryAccess:[],version:1},
    {id:'dispatcher',name:'Dispatcher',system:false,permanent:true,permissions:['loads.read'],expenseCategoryAccess:[],version:1}
  ]};
  let saved,failSave=false,canManage=true;
  await page.route('**/api/**',async route=>{
    const path=new URL(route.request().url()).pathname;
    if(path==='/api/auth/session')return route.fulfill({json:{user:{id:'admin',username:'Admin',permissions:canManage?['access.manage']:[]},csrfToken:'test'}});
    if(path==='/api/settings/access')return route.fulfill({json:data});
    if(path==='/api/settings/roles/viewer' && route.request().method()==='PUT'){
      if(failSave)return route.fulfill({status:409,json:{error:'Role changed. Reload and retry.'}});
      saved=route.request().postDataJSON(); data.roles[1]={...saved,version:2}; return route.fulfill({json:data.roles[1]});
    }
    return route.fulfill({json:[]});
  });
  await page.goto(base+'/settings');
  await page.getByRole('tab',{name:'Roles & permissions',exact:true}).click();
  const viewer=()=>page.getByRole('button',{name:/^Loads viewer,/});
  await expect(viewer()).toHaveAccessibleName('Loads viewer, 50% access');
  await expect(viewer()).toContainText('1 user');
  await expect(page.getByRole('button',{name:'Empty role, 0% access'})).toBeVisible();
  await expect(page.getByText('View imported loads',{exact:true})).toHaveCount(0);
  await viewer().focus(); await page.keyboard.press('Enter');
  const dialog=page.getByRole('dialog',{name:'Edit role',exact:true});
  await expect(dialog).toBeVisible();
  await expect(dialog.getByLabel('Role name',{exact:true})).toHaveValue('Loads viewer');
  await dialog.getByRole('button',{name:'Cancel',exact:true}).click();
  expect(saved).toBeUndefined();
  await viewer().click();
  await dialog.getByLabel('Other Edit',{exact:true}).check();
  failSave=true;
  await dialog.getByRole('button',{name:'Save role',exact:true}).click();
  await expect(dialog.getByRole('alert')).toContainText('Role changed');
  failSave=false;
  await dialog.getByRole('button',{name:'Save role',exact:true}).click();
  await expect(dialog).toHaveCount(0);
  await expect(viewer()).toHaveAccessibleName('Loads viewer, 67% access');
  expect(saved.expenseCategoryAccess[0].canEdit).toBe(true);
  expect(saved.permissions).toEqual(['loads.read','unknown.key']);
  await page.reload(); await expect(viewer()).toContainText('67%');
  await page.getByRole('button',{name:'Dispatcher, 17% access'}).click();
  await expect(dialog.getByLabel('Role name',{exact:true})).toHaveAttribute('readonly','');
  await dialog.getByRole('button',{name:'Cancel',exact:true}).click();
  await page.getByRole('button',{name:'Administrator, 100% access'}).click();
  await expect(page.getByRole('dialog',{name:'Administrator',exact:true})).toContainText('cannot be edited');
  await expect(page.getByRole('button',{name:'Save role',exact:true})).toHaveCount(0);
  await page.getByRole('dialog').getByRole('button',{name:'Close',exact:true}).first().click();
  for(const width of [1440,390]){
    await page.setViewportSize({width,height:900});
    await expect(viewer()).toBeVisible();
    await expect.poll(async()=> (await page.locator('aside').boundingBox()).width).toBe(width<640?64:240);
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
    const box=await viewer().boundingBox(); expect(box.height).toBeLessThan(80);
    await page.screenshot({path:process.env.TEMP+'/clustie-roles-'+width+'.png',fullPage:true});
  }
  canManage=false; await page.reload();
  await expect(page.getByRole('tab',{name:'Roles & permissions',exact:true})).toHaveCount(0);
  expect(errors).toEqual([]);
  console.log('Role list passed: percentages, compact desktop/mobile rows, keyboard/modal, cancel/save/conflict/reload, permanent/system protection and restricted access.');
} finally { await browser?.close(); await new Promise(resolve=>server.close(resolve)); }
