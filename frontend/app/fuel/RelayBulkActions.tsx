"use client";
import { useRef, useState } from "react";
import type { RelayDriverList, RelayDriverChange, RelayRosterEntry, RelayBulkResponse } from "../lib/types";
import { changeRelayDrivers } from "../lib/api";
import { controlClass, ErrorBanner, Field, Modal } from "../components/management/ManagementUI";

import { usePermissions } from "../lib/access";

type Action = "disable"|"enable"|"remove"|"unassign"|"assign";
const labels:Record<Action,string>={disable:"Disable policy",enable:"Enable policy",remove:"Remove from Relay",unassign:"Unassign policy",assign:"Assign policy"};
export function RelayBulkActions({data,entries,onBusy,onComplete,onClear}:{data:RelayDriverList;entries:RelayRosterEntry[];onBusy:(value:boolean)=>void;onComplete:(ids:string[])=>Promise<void>;onClear:()=>void}){
 const permissions = usePermissions();
 const allowed = (action: Action) => permissions.includes(action === "remove" ? "fuel.drivers.delete" : "fuel.drivers.edit");
 const [dialog,setDialog]=useState<{action:Action;entries:RelayRosterEntry[];ids:string[]}|null>(null);
 const [policy,setPolicy]=useState("");const [busy,setBusy]=useState(false);const saving=useRef(false);
 const [error,setError]=useState("");const [result,setResult]=useState<RelayBulkResponse|null>(null);
 function open(action:Action){if(!allowed(action))return;setError("");setResult(null);setPolicy("");setDialog({action,entries:[...entries],ids:entries.map(()=>crypto.randomUUID())})}
 async function submit(){
  if(!dialog||!allowed(dialog.action)||saving.current||result)return;saving.current=true;setBusy(true);onBusy(true);setError("");
  try{const changes:RelayDriverChange[]=dialog.entries.map((e,i)=>({requestId:dialog.ids[i],action:dialog.action,relayDriverId:e.driver.id,revision:e.revision,...(dialog.action==="assign"?{policyId:policy}:{})}));const response=await changeRelayDrivers(changes);setResult(response);await onComplete(response.results.filter(r=>!r.error).map(r=>r.relayDriverId))}
  catch(e){setError(e instanceof Error?e.message:"Could not confirm the bulk action. Refresh Relay and review pending requests.")}
  finally{saving.current=false;setBusy(false);onBusy(false)}
 }
 return <>
 {!!entries.length&&<div className="flex flex-wrap items-center gap-2 text-[13px]"><span>{entries.length} selected</span><select aria-label="Action for selected Relay drivers" disabled={busy||entries.length>100} className={controlClass.replace("w-full","w-auto")} value="" onChange={e=>{if(e.target.value)open(e.target.value as Action)}}><option value="">Apply action…</option>{Object.entries(labels).filter(([key])=>allowed(key as Action)).map(([key,label])=><option key={key} value={key}>{label}</option>)}</select><button className="ui-button" disabled={busy} onClick={onClear}>Clear selection</button>{entries.length>100&&<span>Select up to 100 drivers at a time.</span>}</div>}
 {dialog&&<Modal title={`${labels[dialog.action]} · ${dialog.entries.length} drivers`} isSaving={busy} submitLabel={result?"Done":labels[dialog.action]} onClose={()=>{setDialog(null);setResult(null)}} onSubmit={e=>{e.preventDefault();if(result){setDialog(null);setResult(null)}else void submit()}}><fieldset disabled={busy} className="space-y-4">
 {error&&<ErrorBanner message={error}/>}<ul className="max-h-48 overflow-y-auto text-[13px] text-zinc-300">{dialog.entries.map(e=><li key={e.driver.id}>{e.driver.first_name} {e.driver.last_name}{e.status==="terminated"?" · Terminated":""}</li>)}</ul>
 {!result&&<>{dialog.action==="assign"&&<><Field label="Policy"><select required className={controlClass} value={policy} onChange={e=>setPolicy(e.target.value)}><option value="">Select policy</option>{data.policies.map(p=><option key={p.id} value={p.id}>{p.name}</option>)}</select></Field><p className="text-[13px] text-amber-400">Changing policies resets usage counters. Non ELD requires each driver’s saved PIN and truck number; ELD policies use their existing ELD connection.</p></>}
 <p className="text-[13px] text-zinc-400">{dialog.action==="remove"?"Remove these drivers from Relay? MSERP profiles and historical transactions are retained.":dialog.action==="disable"?"Stop fueling through these drivers’ assigned policies. Usage counters are preserved.":dialog.action==="enable"?"Enable these drivers’ assigned policies. Terminated drivers will be disabled again by automatic offboarding.":dialog.action==="unassign"?"Remove the assigned policy from these drivers. Reassigning resets usage counters.":"Each driver is checked separately. Drivers already on this policy or missing required details will be reported below."}</p></>}
 {busy&&<p role="status" className="text-[13px] text-zinc-400">Applying changes and refreshing Relay…</p>}
 {result&&<div role="status" className="space-y-2 text-[13px]"><p className="text-emerald-400">{result.results.filter(r=>!r.error).length} completed · {result.results.filter(r=>r.error).length} need review</p>{result.warning&&<ErrorBanner message={result.warning}/>}<ul className="max-h-64 overflow-y-auto space-y-2">{result.results.filter(r=>r.error).map(r=>{const e=dialog.entries.find(e=>e.driver.id===r.relayDriverId);return <li key={r.relayDriverId} className="text-amber-400">{e?.driver.first_name} {e?.driver.last_name}: {r.error}</li>})}</ul></div>}
 </fieldset></Modal>}
 </>
}
