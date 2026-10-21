// Isolated signed webhook -> real expense UI/API -> hire link -> driver history.
import { readFile } from 'node:fs/promises';
import { createHmac, randomUUID } from 'node:crypto';
import { chromium, expect } from '@playwright/test';

const access=JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE,'utf8'));
const apiBase=`http://127.0.0.1:${Number(process.env.MSERP_REVIEW_API_PORT)}`;
if (!['localhost','127.0.0.1'].includes(new URL(access.base).hostname) || !/^review_[a-f0-9]+$/.test(access.schema) || !process.env.MSERP_REVIEW_FLEETSCOPE_SECRET) throw new Error('Configured isolated review environment required');
const companyId=process.env.MSERP_REVIEW_FLEETSCOPE_COMPANY_ID;
const sourceApplicant=randomUUID(), sourceDriver=randomUUID();
const name='Recruit Expense '+sourceApplicant.slice(0,8);
const occurredAt=new Date(Date.now()-60000).toISOString();
async function deliver(path,event){
 const body=JSON.stringify({version:1,eventId:randomUUID(),companyId,occurredAt,...event});
 const stamp=String(Math.floor(Date.now()/1000));
 const signature='v1='+createHmac('sha256',process.env.MSERP_REVIEW_FLEETSCOPE_SECRET).update(stamp+'.'+body).digest('hex');
 const response=await fetch(apiBase+'/integrations/fleetscope/'+path,{method:'POST',headers:{'Content-Type':'application/json','X-FleetScope-Timestamp':stamp,'X-FleetScope-Signature':signature},body});
 expect(response.status,await response.clone().text()).toBe(200);return response.json();
}
const result=await deliver('applicant',{type:'applicant.updated',applicant:{id:sourceApplicant,fullName:name,status:'applied'}});
const browser=await chromium.launch();
try {
 const page=await browser.newPage();
 await page.goto(access.base+'/login');
 await page.getByLabel('Email or existing username').fill(access.email);
 await page.getByLabel('Password',{exact:true}).fill(access.password);
 await page.getByRole('button',{name:'Sign in',exact:true}).click();
 await page.waitForURL(url=>!url.pathname.startsWith('/login'));
 const api=async path=>(await page.request.get(access.base+'/api'+path)).json();
 const session=await api('/auth/session');
 const post=async(path,data)=>page.request.post(access.base+'/api'+path,{data,headers:{'X-CSRF-Token':session.csrfToken,'Origin':access.base}});
 const directory=await api('/driver-directory?search='+encodeURIComponent(name));expect(directory.total).toBe(0);
 const options=await api('/expenses');const hr=options.options.categories.find(c=>c.name==='HR');
 expect(options.options.applicants.some(a=>a.id===result.applicantId)).toBe(true);
 await page.goto(access.base+'/expenses');
 await page.getByRole('button',{name:'Add entry',exact:true}).click();
 await page.getByLabel('Category',{exact:true}).selectOption(hr.id);
 await page.getByLabel('Expense type',{exact:true}).selectOption('Recruiting');
 await page.getByLabel('Amount',{exact:true}).fill('85.00');
 const field=page.getByRole('combobox',{name:'Driver',exact:true});
 await expect(field).toHaveJSProperty('tagName','SELECT');
 await field.selectOption('applicant:'+result.applicantId);
 await expect(field).toHaveValue('applicant:'+result.applicantId);
 await expect(page.getByLabel('Unit number',{exact:true})).toHaveValue('');
 const savedResponse=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/expenses'&&r.request().method()==='POST');
 await page.getByRole('button',{name:'Create entry',exact:true}).click();
 const response=await savedResponse;expect(response.status()).toBe(201);
 const saved=await response.json();expect(saved).toMatchObject({applicantId:result.applicantId,driverId:null,truckId:null,driverName:name,coveredBy:'Company'});
 const bad=await post('/expenses',{categoryId:hr.id,expenseDate:'2026-10-21',expenseType:'Transportation',amount:'10',driverName:'Unlisted manual person',coveredBy:'Company'});expect(bad.status()).toBe(400);
 // Deliver hire first; a later explicit applicant-to-driver map must attach history.
 const hire=await deliver('driver-hired',{type:'driver.hired',driver:{id:sourceDriver,fullName:name,driverType:'company',hireDate:'2026-10-21'}});
 const completion=await post('/driver-intake/'+hire.intakeId+'/complete',{driver:{firstName:'Recruit',lastName:'Hired '+sourceApplicant.slice(0,8),payType:'cpm',payRate:0.75,active:true,hireDate:'2026-10-21'},separateConfirmed:true});expect(completion.status(),await completion.text()).toBe(200);
 const existing=await completion.json();
 await deliver('applicant',{type:'applicant.updated',occurredAt:new Date().toISOString(),applicant:{id:sourceApplicant,fullName:name,status:'hired',hiredDriverId:sourceDriver}});
 const history=await api('/expenses?driverId='+existing.id+'&responsibility=non_personal');
 expect(history.items.find(e=>e.id===saved.id)).toMatchObject({driverId:existing.id,applicantId:result.applicantId,coveredBy:'Company',chargeDriverId:null});
 await page.goto(access.base+'/drivers/detail?id='+existing.id);
 await page.getByRole('tab',{name:'Company & other expenses',exact:true}).click();
 await expect(page.getByText('Recruiting',{exact:true})).toBeVisible();
 console.log('Passed: signed applicant delivery, hidden roster, explicit dropdown selection, persisted expense, manual-name rejection, distinct hired UUID mapping, and visible prehire history.');
} finally {await browser.close();}
