// Read-only preparation. Inputs and generated SQL contain private fleet data; keep outside Git.
// Usage: node prepare-weekly-pti-import.mjs source.json trucks.json output-prefix
import {readFileSync,writeFileSync} from 'node:fs';
import {randomUUID,createHash} from 'node:crypto';
const [sourceFile,trucksFile,out]=process.argv.slice(2);
if(!out)throw Error('Supply source JSON, verified truck JSON, and output prefix');
const read=p=>JSON.parse(readFileSync(p,'utf8').replace(/^\uFEFF/,''));
const source=read(sourceFile),trucks=read(trucksFile);
const mapping=source.weekMapping??{'27-Sep':'2026-09-28','05-Oct':'2026-10-05','12-Oct':'2026-10-12','19-Oct':'2026-10-19'};
const weeks=Object.values(mapping);
if(!weeks.length||new Set(weeks).size!==weeks.length||weeks.some(w=>!/^\d{4}-\d{2}-\d{2}$/.test(w)||new Date(w+'T12:00:00Z').getUTCDay()!==1)||JSON.stringify(source.values[0])!==JSON.stringify(Object.keys(mapping)))throw Error('Unexpected source headers; review week mapping');
const sheetTrucks=source.truckList.slice(1).filter(r=>r[1]&&r[4]);
const report={source:source.range??'Fleet board / Weekly Report / HD:HG',weekMapping:mapping,appTrucks:trucks.length,sheetRows:source.units.length,matches:[],unmatched:[],conflicts:[],invalid:[],entries:[],appOnly:[]};
const grouped=new Map();
for(const row of source.units){
 const exact=trucks.filter(t=>t.unit===row.unit),identity=sheetTrucks.filter(t=>t[1]===row.unit),vins=[...new Set(identity.map(t=>t[4]))];
 let match=exact.length===1?exact[0]:null,evidence='Exact unit';
 if(source.resolutions?.aliases?.[row.unit]){match=trucks.find(t=>t.unit===source.resolutions.aliases[row.unit]);evidence='User-confirmed unit alias';}
 if(match&&vins.length===1&&match.vin&&match.vin!==vins[0]){report.unmatched.push({...row,reason:'Source VIN differs from current exact-unit truck'});continue;}
 if(!match&&vins.length===1){const candidates=trucks.filter(t=>t.vin===vins[0]);if(candidates.length===1){match=candidates[0];evidence='Exact VIN from Truck list';}}
 if(!match){report.unmatched.push({...row,reason:'No unique exact unit or VIN match'});continue;}
 report.matches.push({...row,truckId:match.id,appUnit:match.unit,evidence});
 weeks.forEach((week,i)=>{const value=String(source.values[row.row-1]?.[i]??'').trim();if(!value)return;const key=match.id+':'+week;const list=grouped.get(key)??[];list.push({truckId:match.id,unit:match.unit,week,mark:value,sourceRow:row.row});grouped.set(key,list);});
}
for(const entries of grouped.values()){
 const resolved=source.resolutions?.cells?.[entries[0].unit+':'+entries[0].week];
 if(resolved){if(!['/','1','0'].includes(resolved))throw Error('Invalid reviewed mark');report.entries.push({...entries[0],mark:resolved,sourceRows:entries.map(e=>e.sourceRow),resolution:'User-confirmed'});continue;}
 if(entries.some(e=>!['/','1','0'].includes(e.mark))){report.invalid.push(entries);continue;}
 if(new Set(entries.map(e=>e.mark)).size!==1){report.conflicts.push(entries);continue;}
 report.entries.push({...entries[0],sourceRows:entries.map(e=>e.sourceRow)});
}
report.appOnly=trucks.filter(t=>!report.matches.some(m=>m.truckId===t.id)).map(t=>t.unit);
const digest=createHash('sha256').update(JSON.stringify(source)).digest('hex');
const quote=value=>"'"+String(value).replaceAll("'","''")+"'";
const entries=report.entries.map(e=>({...e,eventId:randomUUID(),hash:createHash('sha256').update(digest+JSON.stringify(e)).digest('hex')}));
const sql=`BEGIN;
SET LOCAL lock_timeout='10s';
LOCK TABLE weekly_pti_rows IN SHARE ROW EXCLUSIVE MODE;
CREATE TEMP TABLE pti_import_input ON COMMIT DROP AS SELECT * FROM jsonb_to_recordset(${quote(JSON.stringify(entries))}::jsonb) AS x("truckId" uuid,unit text,week date,mark text,"sourceRows" jsonb,"eventId" uuid,hash text);
DO $$ BEGIN IF EXISTS(SELECT 1 FROM pti_import_input x LEFT JOIN trucks t ON t.id=x."truckId" WHERE t.id IS NULL OR t.unit_number<>x.unit) THEN RAISE EXCEPTION 'Truck identity changed; prepare again'; END IF; END $$;
INSERT INTO weekly_pti_weeks(week_start,created_by,late_snapshot) SELECT DISTINCT week,'Google Sheets import',true FROM pti_import_input ON CONFLICT DO NOTHING;
INSERT INTO weekly_pti_rows(week_start,truck_id,data)
 SELECT w.week_start,t.id,jsonb_build_object('weekStart',w.week_start::text,'truckId',t.id::text,'truckUnit',t.unit_number,'crew','[]'::jsonb,'eligibility','unknown','snapshotEligibility','unknown','review','pending','received',false,'notes','')
 FROM weekly_pti_weeks w CROSS JOIN trucks t WHERE w.week_start IN (${weeks.map(quote).join(',')}) ON CONFLICT DO NOTHING;
CREATE TEMP TABLE pti_import_changes ON COMMIT DROP AS
 SELECT p.week_start,p.truck_id,p.version,p.data AS before_data,x."eventId",x.hash,
 p.data || jsonb_build_object('version',p.version+1,'mark',x.mark,'received',x.mark='1','review',CASE WHEN x.mark='1' THEN 'checked' ELSE 'pending' END,
 'updatedBy','Google Sheets import','updatedAt',now(),'importSource',jsonb_build_object('spreadsheetId','1RcRmMoM4Jyj1OTAMqDFxawHDiPG8oRVEPzbVOZHmJDk','sheetId',2022857842,'rows',x."sourceRows",'sourceDigest',${quote(digest)})) AS after_data
 FROM pti_import_input x JOIN weekly_pti_rows p ON p.week_start=x.week AND p.truck_id=x."truckId"
 WHERE NOT(p.data ? 'mark') AND p.data->>'updatedAt' IS NULL AND NOT coalesce((p.data->>'received')::boolean,false) AND coalesce(p.data->>'notes','')='';
INSERT INTO weekly_pti_events(id,request_hash,week_start,truck_id,before_data,after_data,actor_id,actor_name)
 SELECT "eventId",hash,week_start,truck_id,before_data,after_data,(SELECT id FROM app_users WHERE username='Ben Simpson' AND active),'Google Sheets import' FROM pti_import_changes;
UPDATE weekly_pti_rows p SET data=c.after_data,version=p.version+1 FROM pti_import_changes c WHERE p.week_start=c.week_start AND p.truck_id=c.truck_id;
SELECT (SELECT count(*) FROM pti_import_input) AS source_cells,count(*) AS imported_cells,(SELECT count(*) FROM pti_import_input)-count(*) AS preserved_native_cells FROM pti_import_changes;
COMMIT;`;
writeFileSync(out+'.json',JSON.stringify(report,null,2));writeFileSync(out+'.sql',sql);
console.log(JSON.stringify({sourceDigest:digest,appTrucks:trucks.length,sheetRows:source.units.length,matchedRows:report.matches.length,preparedCells:entries.length,unmatched:report.unmatched,conflicts:report.conflicts,invalid:report.invalid,appOnly:report.appOnly},null,2));
