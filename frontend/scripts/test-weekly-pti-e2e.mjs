// Synthetic local API/database/UI coverage. Never run against production.
import {readFile,mkdir} from 'node:fs/promises';
import {dirname,join} from 'node:path';
import {execFileSync} from 'node:child_process';
import {randomUUID} from 'node:crypto';
import {chromium,expect} from '@playwright/test';

const accessFile=process.env.MSERP_REVIEW_ACCESS_FILE;
const access=JSON.parse(await readFile(accessFile,'utf8'));
const database=new URL(process.env.MSERP_REVIEW_TEST_DATABASE_URL);
if(!['127.0.0.1','localhost'].includes(new URL(access.base).hostname)||!['127.0.0.1','localhost'].includes(database.hostname)||!database.pathname.endsWith('_test')||!/^review_[a-f0-9]+$/.test(access.schema))throw Error('Disposable local review required');
const sql=input=>execFileSync(process.env.PSQL_PATH||'psql',['-X','-q','-v','ON_ERROR_STOP=1','-d',process.env.MSERP_REVIEW_TEST_DATABASE_URL],{input:`SET search_path TO ${access.schema},public;SET ROLE mserp_app;\n${input}`,encoding:'utf8',windowsHide:true});
sql(`UPDATE trucks SET created_at=now()-interval '30 days';`);
database.searchParams.set('search_path',access.schema+',public');database.searchParams.set('role','mserp_app');
const binary=join(dirname(accessFile),process.platform==='win32'?'api.exe':'api');
const cli=args=>execFileSync(binary,args,{cwd:dirname(accessFile),env:{...process.env,DATABASE_URL:database.toString(),DATATRUCK_API_KEY:'test-disabled',DATATRUCK_COMPANY_NAME:'test',RELAY_API_KEY:'test-disabled',PREPASS_CLIENT_ID:'test-disabled',PREPASS_CLIENT_SECRET:'test-disabled',FRONTEND_ORIGIN:access.base,AUTH_COOKIE_SECURE:'false',SCHEDULED_SYNCS_ENABLED:'false'},encoding:'utf8',windowsHide:true});
const preview=JSON.parse(cli(['-fleet-status-preview']));if(!preview.enabled)cli(['-fleet-status-activate',preview.revision]);
const artifacts=join(dirname(accessFile),'weekly-pti');await mkdir(artifacts,{recursive:true});
const browser=await chromium.launch();
try{
 const context=await browser.newContext({viewport:{width:1600,height:1000}}),page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(access.base+'/login');await page.getByLabel('Email or existing username').fill(access.email);await page.getByLabel('Password',{exact:true}).fill(access.password);await page.getByRole('button',{name:'Sign in',exact:true}).click();await page.waitForURL(u=>u.pathname!='/login');
 const session=await(await page.request.get(access.base+'/api/auth/session')).json(),headers={'X-CSRF-Token':session.csrfToken,Origin:access.base};
 const api=async(method,path,data)=>{const r=await page.request[method](access.base+'/api'+path,{headers,...(data?{data}:{})});if(!r.ok())throw Error(path+': '+r.status()+' '+await r.text());return r.status()===204?null:r.json();};
 const today=new Intl.DateTimeFormat('en-CA',{timeZone:'America/New_York'}).format(new Date()),monday=new Date(today+'T12:00:00Z');monday.setUTCDate(monday.getUTCDate()-(monday.getUTCDay()+6)%7);const week=monday.toISOString().slice(0,10),path='/weekly-pti?weekStart='+week;

 const initial=await api('post','/weekly-pti/weeks',{weekStart:week});
 const weeks=[3,2,1,0].map(n=>{const d=new Date(monday);d.setUTCDate(d.getUTCDate()-7*n);return d.toISOString().slice(0,10);});
 sql(`INSERT INTO weekly_pti_weeks(week_start,created_by,late_snapshot) SELECT d::date,'Fixture',true FROM unnest(ARRAY[${weeks.map(w=>"'"+w+"'").join(',')}]) d ON CONFLICT DO NOTHING;`);
 let target=initial.rows.find(r=>r.truckUnit==='102');
 const writePath=`/weekly-pti/${week}/${target.truckId}`;
 const body=(row,mark,notes=row.notes)=>({requestId:randomUUID(),version:row.version,mark,received:mark==='1',review:mark==='1'?'checked':'pending',notes});
 await page.goto(access.base+'/fleet/weekly-pti');
 const row=page.getByRole('row',{name:'Truck 102',exact:true}),cell=page.getByLabel('Truck 102 PTI '+week,{exact:true});
 await expect(row).toBeVisible();await expect(page.getByRole('row',{name:'Truck 103',exact:true})).toBeVisible();
 const matrix=await api('get','/weekly-pti/matrix');expect(matrix.trucks.length).toBe(initial.rows.length);
 expect(matrix.weeks).toEqual(weeks);
 const groupNames=await page.locator('tbody').evaluateAll(elements=>elements.map(e=>e.getAttribute('aria-label')));expect(groupNames).toEqual(['Company & investor trucks','Owner-operated trucks']);
 for(const truck of matrix.trucks){await expect(page.getByRole('rowgroup',{name:truck.ownership==='owner_operator'?'Owner-operated trucks':'Company & investor trucks',exact:true}).getByRole('row',{name:'Truck '+truck.unit,exact:true})).toBeVisible();}
 await expect(page.getByRole('button',{name:'Previous week',exact:true})).toHaveCount(0);
 expect(await cell.locator('option').evaluateAll(options=>options.filter(o=>!o.disabled).map(o=>o.value))).toEqual(['/','1','0']);
 for(const [mark,color] of [['/','rgb(255, 255, 0)'],['0','rgb(255, 0, 0)'],['1','rgb(0, 255, 0)']]){
  await cell.selectOption(mark);await expect(cell).toBeEnabled();await expect(cell).toHaveValue(mark);
  await expect.poll(()=>cell.evaluate(e=>getComputedStyle(e.parentElement).backgroundColor)).toBe(color);
 }
 await expect(row.getByRole('cell',{name:'Local Review',exact:true})).toBeVisible();
 await expect(row.getByText('Local Review',{exact:true})).toHaveAttribute('title',/\d{4}.*\d{1,2}:\d{2}.*[AP]M.*E[DS]T/);
 await page.getByLabel('Truck 102 notes',{exact:true}).fill('Marker lamp follow-up');await page.getByLabel('Truck 102 notes',{exact:true}).press('Enter');await expect(cell).toBeEnabled();
 await page.reload();await expect(cell).toHaveValue('1');await expect(page.getByLabel('Truck 102 notes',{exact:true})).toHaveValue('Marker lamp follow-up');
 const oldCell=page.getByLabel('Truck 102 PTI '+weeks[0],{exact:true});await oldCell.selectOption('/');await expect(oldCell).toBeEnabled();await expect(oldCell).toHaveValue('/');await expect(cell).toHaveValue('1');
 target=(await api('get',path)).rows.find(r=>r.truckId===target.truckId);
 expect((await page.request.patch(access.base+'/api'+writePath,{headers,data:body(target,'2')})).status()).toBe(409);
 const receipt=body(target,'0');await api('patch',writePath,receipt);const saved=(await api('get',path)).rows.find(r=>r.truckId===target.truckId);
 expect((await api('patch',writePath,receipt)).version).toBe(saved.version);
 expect((await page.request.patch(access.base+'/api'+writePath,{headers,data:body(target,'/')})).status()).toBe(409);
 await page.getByRole('button',{name:'Refresh',exact:true}).click();await expect(cell).toHaveValue('0');
 await cell.selectOption('1');await expect(cell).toBeEnabled();
 await page.getByLabel('Truck 102 notes',{exact:true}).fill('My unsaved note');
 target=(await api('get',path)).rows.find(r=>r.truckId===target.truckId);
 await api('patch',writePath,body(target,'1','Other reviewer note'));await page.getByRole('heading',{name:'Weekly PTI',exact:true}).click();
 await expect(page.getByText(/PTI changed since/)).toBeVisible();await expect(page.getByLabel('Truck 102 notes',{exact:true})).toHaveValue('My unsaved note');
 await page.getByRole('button',{name:'Save my note on the latest entry',exact:true}).click();
 await expect.poll(async()=>((await api('get',path)).rows.find(r=>r.truckId===target.truckId)).notes).toBe('My unsaved note');
 const roleName='PTI reader '+randomUUID();await api('post','/settings/roles',{name:roleName,permissions:['drivers.read','trucks.read','pti.read']});const role=(await api('get','/settings/access')).roles.find(r=>r.name===roleName),email=randomUUID()+'@example.test',password='Synthetic-'+randomUUID();await api('post','/settings/users',{username:roleName,email,password,roleId:role.id,active:true});
 const reader=await browser.newContext();await reader.request.post(access.base+'/api/auth/login',{headers:{Origin:access.base},data:{email,password}});const readerSession=await(await reader.request.get(access.base+'/api/auth/session')).json();const readPage=await reader.newPage();await readPage.goto(access.base+'/fleet/weekly-pti');await expect(readPage.getByLabel('Truck 102 PTI '+week,{exact:true})).toBeDisabled();await expect(readPage.getByLabel('Truck 102 notes',{exact:true})).toHaveCount(0);
 expect((await reader.request.patch(access.base+'/api'+writePath,{headers:{Origin:access.base,'X-CSRF-Token':readerSession.csrfToken},data:body(saved,'/')})).status()).toBe(403);expect((await reader.request.get(access.base+'/api/weekly-pti/matrix')).status()).toBe(200);await reader.close();
 const anonymous=await browser.newContext();expect((await anonymous.request.get(access.base+'/api/weekly-pti/matrix')).status()).toBe(401);await anonymous.close();
 await page.screenshot({path:join(artifacts,'weekly-pti.png'),fullPage:true});
 await page.getByRole('button',{name:'Collapse sidebar',exact:true}).click();await expect.poll(async()=>page.locator('aside').evaluate(el=>el.getBoundingClientRect().width)).toBe(64);await page.setViewportSize({width:390,height:844});
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBe(true);await page.screenshot({path:join(artifacts,'weekly-pti-mobile.png'),fullPage:true});
 expect(errors).toEqual([]);console.log('PTI matrix E2E passed: all trucks/weeks, ownership groups, restricted marks/colors, notes/reviewer, historical cells, retries/conflicts, read-only and unauthenticated access. Screenshots: '+artifacts);
}finally{await browser.close();}
