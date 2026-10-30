import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname } from 'node:path';

const server=createServer(async(req,res)=>{
 try { const path=new URL(req.url,'http://localhost').pathname;
  const file=path==='/'?'index.html':extname(path)?path.slice(1):`${path.slice(1)}.html`;
  const body=await readFile(new URL(`../out/${file}`,import.meta.url));
  res.writeHead(200,{'Content-Type':{'.html':'text/html','.js':'application/javascript','.css':'text/css','.txt':'text/plain'}[extname(file)]??'application/octet-stream'});res.end(body);
 }catch{res.writeHead(404);res.end();}
});
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
const base=`http://127.0.0.1:${server.address().port}`;
const pickColor = (input, value) => input.evaluate((el, next) => { Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(el, next); el.dispatchEvent(new Event('input', {bubbles:true})); el.dispatchEvent(new Event('change', {bubbles:true})); }, value);
let browser;
try {
 browser=await chromium.launch();
 let settings={colors:{},version:1}, theme='default', manager=true, fail=false;
 const errors=[];
 async function setup(page){
  page.on('pageerror',e=>errors.push(e.message));
  await page.route('**/api/**',async route=>{
   const path=new URL(route.request().url()).pathname.slice(4),method=route.request().method();
   if(path==='/auth/session')return route.fulfill({json:{user:{id:manager?'admin':'viewer',username:manager?'Administrator':'Viewer',theme,permissions:['trucks.read','drivers.read',...(manager?['access.manage']:[])]},csrfToken:'test'}});
   if(path==='/settings/status-colors'){
    if(method==='PUT'){
     if(!manager)return route.fulfill({status:403,json:{error:'Forbidden'}});
     if(fail)return route.fulfill({status:500,json:{error:'Save failed'}});
     const input=route.request().postDataJSON();
     if(input.version!==settings.version)return route.fulfill({status:409,json:{error:'Colors changed'}});
     settings={colors:input.colors,version:settings.version+1};
    }
    return route.fulfill({json:settings});
   }
   if(path==='/auth/theme'){theme=route.request().postDataJSON().theme;return route.fulfill({status:204});}
   if(path==='/auth/birthday')return route.fulfill({json:{celebrate:false,serverNow:new Date().toISOString()}});
   if(path.startsWith('/trucks'))return route.fulfill({json:{items:[{id:'00000000-0000-0000-0000-000000000001',unitNumber:'TEST-100',status:'available',operatingStatus:'ready',ownerName:'Company',active:true}],total:1,page:1,pageSize:25,totalPages:1,statusCounts:{ready:1,in_use:0,shop:0,out_of_service:0,terminated:0},gpsAlertCounts:{overdue:0,missing:0}}});
   if(path.includes('count'))return route.fulfill({json:{count:0}});
   return route.fulfill({json:[]});
  });
 }
 const page=await browser.newPage({viewport:{width:1440,height:1000}});await setup(page);
 await page.goto(`${base}/settings`);await page.getByRole('tab',{name:'Status colors',exact:true}).click();
 const input=page.getByLabel('Truck ready color',{exact:true});
 await expect(input).toHaveValue('#10b981');
 if(process.env.MSERP_STATUS_SCREENSHOT_DIR) await page.screenshot({path:process.env.MSERP_STATUS_SCREENSHOT_DIR+'/status-colors-settings.png'});
 await pickColor(input,'#8844cc');await page.getByRole('button',{name:'Save colors',exact:true}).click();
 await expect(page.getByRole('status').filter({hasText:/^Saved$/})).toBeVisible();
 await page.goto(`${base}/trucks`);
 const badge=page.getByRole('row',{name:'Open truck TEST-100 details'}).locator('.status-truck_ready');
 await expect(badge).toHaveText('Ready');
 await expect.poll(()=>badge.evaluate(el=>getComputedStyle(el).getPropertyValue('--status-base').trim())).toBe('#8844cc');
 const darkFill=await badge.evaluate(el=>getComputedStyle(el).backgroundColor);
 // A second user gets the company-wide saved hue without editing permissions.
 manager=false;
 const viewer=await browser.newPage();await setup(viewer);await viewer.goto(`${base}/trucks`);
 await expect.poll(()=>viewer.locator('tbody .status-truck_ready').evaluate(el=>getComputedStyle(el).getPropertyValue('--status-base').trim())).toBe('#8844cc');
 await viewer.goto(`${base}/settings`);await expect(viewer.getByRole('tab',{name:'Status colors',exact:true})).toHaveCount(0);
 manager=true;await page.goto(`${base}/settings`);
 await page.getByRole('tab',{name:'Appearance',exact:true}).click();
 await page.getByRole('button',{name:/Default Light/}).click();
 await expect(page.locator('html')).toHaveAttribute('data-theme','default-light');
 await page.goto(`${base}/trucks`);
 await expect.poll(()=>badge.evaluate(el=>getComputedStyle(el).backgroundColor)).not.toBe(darkFill);
 await expect.poll(()=>badge.evaluate(el=>getComputedStyle(el).getPropertyValue('--status-base').trim())).toBe('#8844cc');
 await page.goto(`${base}/settings`);await page.getByRole('tab',{name:'Status colors',exact:true}).click();
 await pickColor(input,'#ff0000');fail=true;await page.getByRole('button',{name:'Save colors',exact:true}).click();
 await expect(page.locator('section[aria-label="Status colors"] [role="alert"]')).toContainText('Save failed');await expect(input).toHaveValue('#ff0000');
 fail=false;await page.getByRole('button',{name:'Discard changes',exact:true}).click();await expect(input).toHaveValue('#8844cc');
 await pickColor(input,'#ffffff');settings={colors:{truck_ready:'#112233'},version:settings.version+1};
 await page.evaluate(()=>window.dispatchEvent(new Event('focus')));
 await expect(page.locator('section[aria-label="Status colors"] [role="alert"]')).toContainText('Colors changed in another window');
 await expect(page.getByRole('button',{name:'Save colors',exact:true})).toBeDisabled();
 await page.getByRole('button',{name:'Discard changes',exact:true}).click();await expect(input).toHaveValue('#112233');
 await page.getByRole('button',{name:'Restore defaults',exact:true}).click();await page.getByRole('button',{name:'Save colors',exact:true}).click();await expect(input).toHaveValue('#10b981');
 await page.setViewportSize({width:390,height:844});await expect(page.getByRole('button',{name:'Save colors',exact:true})).toBeVisible();
 if(errors.length)throw Error(errors.join('\n'));
 console.log('Status color browser checks passed: persistence, cross-user sharing, truck badges, theme changes, failed saves, stale drafts, restore and mobile controls.');
}finally{await browser?.close();await new Promise(resolve=>server.close(resolve));}
