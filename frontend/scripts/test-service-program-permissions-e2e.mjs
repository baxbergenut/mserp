import {readFile} from 'node:fs/promises';
import {chromium,expect} from '@playwright/test';
const access=JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE,'utf8'));
if(!['127.0.0.1','localhost'].includes(new URL(access.base).hostname)||!/^review_[a-f0-9]+$/.test(access.schema))throw Error('Isolated review required');
const browser=await chromium.launch(),stamp=Date.now(),permission='service_programs.manage';
try{
 const admin=await browser.newContext(),page=await admin.newPage();
 await admin.request.post(access.base+'/api/auth/login',{headers:{Origin:access.base},data:{email:access.email,password:access.password}});
 const session=await(await admin.request.get(access.base+'/api/auth/session')).json();
 const headers={Origin:access.base,'X-CSRF-Token':session.csrfToken};
 const api=async(method,path,data)=>{const r=await admin.request[method](access.base+'/api'+path,{headers,...(data?{data}:{})});if(!r.ok())throw Error(path+': '+await r.text());return r.status()===204?null:r.json();};
 const options=await api('get','/maintenance/options'),name='Program manager '+stamp;
 await api('post','/settings/roles',{name,permissions:[],expenseCategoryAccess:[{categoryId:options.categoryId,canView:true,canEdit:true,canCreate:false,canDelete:false}]});
 const getRole=async()=>(await api('get','/settings/access')).roles.find(r=>r.name===name);
 let role=await getRole();
 const email=`program-manager-${stamp}@example.test`,password=`synthetic-manager-${stamp}`;
 await api('post','/settings/users',{username:name,email,password,roleId:role.id,active:true});
 const program=await api('post','/maintenance/programs',{name:'Permission check '+stamp,instructions:'',intervalMiles:25000,warningMiles:1000,warningDays:14,active:true,useDefaultAssignees:true,assigneeIds:[],assigneeRoleIds:[],version:0});
 const limited=await browser.newContext(),fleet=await limited.newPage();
 await limited.request.post(access.base+'/api/auth/login',{headers:{Origin:access.base},data:{email,password}});
 const user=await(await limited.request.get(access.base+'/api/auth/session')).json();
 const userHeaders={Origin:access.base,'X-CSRF-Token':user.csrfToken};
 const denied=async()=>{
  expect((await limited.request.put(access.base+'/api/maintenance/programs/'+program.id,{headers:userHeaders,data:{}})).status()).toBe(403);
  expect((await limited.request.post(access.base+'/api/maintenance/programs',{headers:userHeaders,data:{}})).status()).toBe(403);
  expect((await limited.request.get(access.base+'/api/maintenance/program-assignees')).status()).toBe(403);
 };
 await fleet.goto(access.base+'/fleet/service-programs?program='+program.id);
 await expect(fleet.getByLabel('Service program',{exact:true})).toHaveValue(program.id);
 await expect(fleet.getByRole('button',{name:'Edit program',exact:true})).toHaveCount(0);
 await denied();
 // Grant the new capability using the actual Settings permission matrix.
 const configure=async checked=>{
  await page.goto(access.base+'/settings');
  await page.getByRole('button',{name:'Roles & permissions',exact:true}).click();
  await page.locator('section').filter({has:page.getByRole('heading',{name,exact:true})}).getByRole('button',{name:'Edit role',exact:true}).click();
  await page.getByLabel('Search permissions',{exact:true}).fill('Service programs');
  await page.getByRole('checkbox',{name:'Manage Service programs',exact:true}).setChecked(checked);
  await page.getByRole('button',{name:'Save role',exact:true}).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
 };
 await configure(true);
 role=await getRole();expect(role.permissions).toContain(permission);
 await fleet.reload();
 await fleet.getByRole('button',{name:'Edit program',exact:true}).click();
 await expect(fleet.getByRole('dialog')).toBeVisible();
 expect((await limited.request.get(access.base+'/api/maintenance/program-assignees')).status()).toBe(200);
 await fleet.getByLabel('Service instructions',{exact:true}).fill('Saved by non-administrator program manager');
 await fleet.getByRole('button',{name:'Save program',exact:true}).click();
 await expect(fleet.getByRole('dialog')).toHaveCount(0);
 expect((await api('get','/maintenance/programs')).find(p=>p.id===program.id).instructions).toBe('Saved by non-administrator program manager');
 expect((await limited.request.get(access.base+'/api/settings/access')).status()).toBe(403);
 // Revocation takes effect for existing sessions on the next request.
 await configure(false);
 await denied();await fleet.reload();
 await expect(fleet.getByRole('button',{name:'Edit program',exact:true})).toHaveCount(0);
 // The capability alone cannot bypass the existing category Edit gate.
 role=await getRole();role.permissions=[permission];role.expenseCategoryAccess[0].canEdit=false;
 await api('put','/settings/roles/'+role.id,role);
 expect((await limited.request.put(access.base+'/api/maintenance/programs/'+program.id,{headers:userHeaders,data:{}})).status()).toBe(403);
 await fleet.reload();
 await expect(fleet.getByRole('button',{name:'Edit program',exact:true})).toHaveCount(0);
 console.log('Service program permissions passed: Settings grant/revoke, non-admin save and assignee lookup, category Edit requirement, unrelated settings denied, existing session refresh.');
}finally{await browser.close();}
