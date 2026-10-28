// Real UI timings on review-local only; this never accepts a production origin.
import assert from 'node:assert/strict';
import { readFile, writeFile } from 'node:fs/promises';
import { execFileSync, execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { dirname, join } from 'node:path';
import { chromium, expect } from '@playwright/test';
const access=JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE,'utf8')),dsn=new URL(process.env.MSERP_REVIEW_TEST_DATABASE_URL);
if(!['127.0.0.1','localhost'].includes(new URL(access.base).hostname)||!['127.0.0.1','localhost'].includes(dsn.hostname)||!dsn.pathname.endsWith('_test')||! /^(review_[a-f0-9]+|payroll_perf_[0-9]+)$/.test(access.schema))throw Error('Local synthetic review required');
const week=process.env.MSERP_PERF_WEEK_START||'2026-10-26';if(!/^\d{4}-\d{2}-\d{2}$/.test(week))throw Error('Invalid week');
const args=['-X','-q','-At','-v','ON_ERROR_STOP=1','-d',dsn.toString()],psql=process.env.PSQL_PATH||'psql';
const sql=q=>execFileSync(psql,args,{input:`SET search_path TO ${access.schema},public;${q}`,encoding:'utf8',windowsHide:true}).trim();
const browser=await chromium.launch(),contexts=[],samples={firstLoad:[],cachedLoad:[],expansion:[],filtering:[],save:[],fiveUsersWithSync:[]};
const latency=Number(process.env.MSERP_PERF_BROWSER_LATENCY_MS??175);
const paint=p=>p.evaluate(()=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))));
async function measured(name,fn){const start=performance.now();await fn();samples[name].push(performance.now()-start)}
async function login(index){const context=await browser.newContext({viewport:{width:1440,height:1000}});contexts.push(context);await context.route('**/api/**',async route=>{if(route.request().url().includes('/events'))return route.continue();const response=await route.fetch();await new Promise(r=>setTimeout(r,latency));await route.fulfill({response})});const page=await context.newPage();await page.goto(access.base+'/login');await page.getByLabel('Email or existing username').fill(index==null?access.email:`perf-session-${index}@example.test`);await page.getByLabel('Password',{exact:true}).fill(access.password);await page.getByRole('button',{name:'Sign in',exact:true}).click();await page.waitForURL(u=>u.pathname!='/login');return page}
try{
 assert.equal(sql('SELECT active FROM accounting_cutover'),'t');
 const page=await login(),report=await(await page.request.get(access.base+'/api/driver-pay?weekStart='+week)).json(),drivers=report.drivers.slice(0,5);assert.equal(drivers.length,5);
 const url=access.base+'/accounting/driver-pay?weekStart='+week;
 await measured('firstLoad',async()=>{await page.goto(url);await expect(page.getByText(drivers[0].fullName,{exact:true}).first()).toBeVisible();await paint(page)});
 for(let n=0;n<20;n++){
  await measured('cachedLoad',async()=>{await page.goto(url);await expect(page.getByText(drivers[0].fullName,{exact:true}).first()).toBeVisible();await paint(page)});
  const row=page.locator('tr').filter({has:page.getByText(drivers[0].fullName,{exact:true})}).first();
  // Collapse if view-memory restored an open statement, then measure expansion.
  if(await page.getByRole('button',{name:'Export statement',exact:true}).count())await row.click();
  await measured('expansion',async()=>{await row.click();await expect(page.getByRole('button',{name:'Export statement',exact:true}).first()).toBeVisible();await paint(page)});
  const search=page.getByPlaceholder('Driver, truck, or load…');
  await measured('filtering',async()=>{const response=page.waitForResponse(r=>r.url().includes('/api/driver-pay?')&&r.request().method()==='GET');await search.fill(drivers[0].fullName);await response;await paint(page)});await search.fill('');await page.waitForResponse(r=>r.url().includes('/api/driver-pay?'));
 }
 async function save(p,d,n,category){await p.getByRole('button',{name:d.fullName+', weekly comment',exact:true}).click();await p.getByLabel('Comment',{exact:true}).fill('Synthetic timing '+category+' '+n);await measured(category,async()=>{const response=p.waitForResponse(r=>r.url().endsWith('/api/driver-pay')&&r.request().method()==='PUT');await p.getByRole('button',{name:'Save comment',exact:true}).click();assert.equal((await response).status(),200);await expect(p.getByText('Saved',{exact:true}).first()).toBeVisible();await paint(p)})}
 await page.goto(url+'&driverId='+drivers[0].id);await expect(page.getByRole('button',{name:'Export statement'})).toBeVisible();
 for(let n=0;n<20;n++)await save(page,drivers[0],n,'save');
 sql(`INSERT INTO app_users(username,email,password_hash,role_id) SELECT 'Perf Session '||n,'perf-session-'||n||'@example.test',u.password_hash,u.role_id FROM app_users u CROSS JOIN generate_series(0,4)n WHERE u.email='review@example.test' ON CONFLICT DO NOTHING;`);
 const pages=await Promise.all(drivers.map(async (d,i)=>{const p=await login(i);await p.goto(url+'&driverId='+d.id);await expect(p.getByRole('button',{name:'Export statement'})).toBeVisible();return p}));
 // Simulate a source sync's committed load changes while five browser sessions save.
 const sync=(async()=>{for(let n=0;n<20;n++){await promisify(execFile)(psql,[...args,'-c',`SET search_path TO ${access.schema},public;UPDATE loads SET total_pay=total_pay+0.01 WHERE id=(SELECT min(id) FROM loads);`],{windowsHide:true});await new Promise(r=>setTimeout(r,250))}})();
 await Promise.all(pages.map((p,i)=>(async()=>{for(let n=0;n<20;n++)await save(p,drivers[i],n,'fiveUsersWithSync')})()));await sync;
 const results=Object.fromEntries(Object.entries(samples).map(([name,values])=>{values.sort((a,b)=>a-b);return[name,{samples:values.length,p50:values[Math.ceil(values.length*.5)-1],p95:values[Math.ceil(values.length*.95)-1],max:values.at(-1),targetMet:values[Math.ceil(values.length*.95)-1]<300}]}));
 const output={synthetic:true,week,distinctConcurrentUsers:5,driverCount:report.drivers.length,addedAPIRoundTripMs:latency,results};await writeFile(join(dirname(process.env.MSERP_REVIEW_ACCESS_FILE),'cutover-browser-performance.json'),JSON.stringify(output,null,2));console.log(JSON.stringify(output));
}finally{await Promise.all(contexts.map(c=>c.close()));await browser.close()}
