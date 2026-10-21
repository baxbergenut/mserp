import {execFileSync} from 'node:child_process';
import {readFile} from 'node:fs/promises';
import {chromium,expect} from '@playwright/test';
import {profileFixture as fixture} from './profile-fixture.mjs';
const access=JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE,'utf8')),dsn=process.env.MSERP_REVIEW_TEST_DATABASE_URL;
if(!['127.0.0.1','localhost'].includes(new URL(access.base).hostname)||!/^review_[a-f0-9]+$/.test(access.schema)||!['127.0.0.1','localhost'].includes(new URL(dsn).hostname)||!new URL(dsn).pathname.endsWith('_test'))throw Error('Isolated local review required');
const sql=query=>execFileSync(process.env.PSQL_PATH||'psql',['-X','-Atq','-v','ON_ERROR_STOP=1','-d',dsn],{input:`SET search_path TO ${access.schema},public;\n${query}`,encoding:'utf8',windowsHide:true}).trim();
const vin='1FUJGLDR0NL000101',oil='00000000-0000-4000-8000-000000000020';
sql(`INSERT INTO five_eld_locations(vin,latitude,longitude,reported_at,fetched_at,provider_truck_number) VALUES('${vin}',41,-76,now()-interval '1 minute',now(),'101');
 INSERT INTO maintenance_telemetry(truck_id,vin,mileage,reported_at) VALUES('${fixture.ownTruck}','${vin}',600000,now()-interval '1 minute');
 INSERT INTO maintenance_odometer_readings(truck_id,reading_date,mileage,source,actor_name) VALUES('${fixture.ownTruck}',(now() AT TIME ZONE 'America/New_York')::date,700000,'manual','Fixture');
 INSERT INTO samsara_telemetry(vin,vehicle_id,unit_number,latitude,longitude,heading,gps_at,mileage,odometer_at,fetched_at) VALUES('${vin}','fixture-samsara','101',40,-75,90,now()-interval '5 minutes',500000,now()-interval '5 minutes',now());`);
const browser=await chromium.launch();
try{
 const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.request.post(access.base+'/api/auth/login',{headers:{Origin:access.base},data:{email:access.email,password:access.password}});
 const session=await(await page.request.get(access.base+'/api/auth/session')).json(),headers={Origin:access.base,'X-CSRF-Token':session.csrfToken};
 const api=async path=>{const r=await page.request.get(access.base+'/api'+path);expect(r.ok()).toBe(true);return r.json();};
 const location=await api(`/trucks/${fixture.ownTruck}/location`);expect(location.location.source).toBe('samsara');expect(location.location.latitude).toBe(40);
 expect((await api(`/drivers/${fixture.owner}/location`)).location.source).toBe('samsara');
 await page.request.post(access.base+`/api/maintenance/programs/${oil}/trucks`,{headers,data:{items:[{truckId:fixture.ownTruck,version:0,active:true,overrideInterval:false,intervalMiles:null,intervalDays:null,intervalMonths:null}]}});
 await page.goto(access.base+'/fleet/service-programs');await page.getByPlaceholder('Search trucks or programs…').fill('101');
 const row=page.locator('tbody tr').filter({has:page.getByRole('link',{name:'101',exact:true})});
 await expect(row.getByRole('button',{name:'Edit current odometer for 101',exact:true})).toHaveText('500,000');
 await expect(row.locator('[data-odometer-fresh]')).toHaveAttribute('data-odometer-fresh','true');
 await row.getByRole('button',{name:'Record service',exact:true}).click();await expect(page.getByLabel('Program service odometer',{exact:true})).toHaveValue('500000');await page.getByRole('button',{name:'Cancel',exact:true}).click();
 await page.goto(access.base+'/maintenance');await page.getByRole('button',{name:'Add work order',exact:true}).click();await page.getByLabel('Maintenance truck',{exact:true}).selectOption(fixture.ownTruck);
 await expect(page.getByLabel('Maintenance odometer',{exact:true})).toHaveValue('500000');await expect(page.getByLabel('Maintenance odometer',{exact:true})).toHaveAttribute('title',/Samsara/);await page.getByRole('button',{name:'Cancel',exact:true}).click();
 await page.goto(access.base+'/driver-board');const marker=page.getByRole('button',{name:'Morgan Hayes · Copy current location',exact:true});await expect(marker).toHaveAttribute('title',/Samsara updated/);
 await page.goto(access.base+'/trucks/detail?id='+fixture.ownTruck);await expect(page.getByRole('button',{name:'Truck 101 · Copy current location',exact:true})).toHaveAttribute('title',/Samsara updated/);
 // Availability is independent for each field: missing GPS falls back while
 // Samsara remains authoritative for the odometer.
 sql(`UPDATE samsara_telemetry SET latitude=NULL,longitude=NULL,gps_at=NULL,heading=NULL WHERE vin='${vin}';`);
 expect((await api(`/trucks/${fixture.ownTruck}/location`)).location.source).toBe('five_eld');
 expect((await api('/maintenance/odometer?truckId='+fixture.ownTruck)).reading.source).toBe('samsara');
 sql(`UPDATE samsara_telemetry SET available=false WHERE vin='${vin}';`);
 expect((await api('/maintenance/odometer?truckId='+fixture.ownTruck)).reading.source).toBe('five_eld');
 expect(errors).toEqual([]);
 console.log('Samsara views passed: priority over newer Five ELD/manual mileage, program and work-order prefill, truck/driver maps, Status Board provider labels, independent GPS/odometer fallback.');
}finally{await browser.close();}
