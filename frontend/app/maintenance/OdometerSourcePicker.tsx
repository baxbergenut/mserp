"use client";

import { useEffect, useState } from "react";
import { fetchOdometerSources, saveOdometerSource } from "../lib/api";
import type { MaintenanceOptions, MaintenanceReading, OdometerSource, OdometerSources } from "../lib/types";
import { ErrorBanner, Modal } from "../components/management/ManagementUI";
import { MaintenanceReadingEditor } from "./MaintenanceSettings";
import { today } from "./maintenance";

const names: Record<string,string> = {auto:"Automatic",samsara:"Samsara",five_eld:"Five ELD",manual:"Manual",service:"Last service"};
export function OdometerSourcePicker({truckId,canEdit,options,onClose,onSaved}:{truckId:string;canEdit:boolean;options:MaintenanceOptions;onClose:()=>void;onSaved:()=>Promise<void>}) {
 const [data,setData]=useState<OdometerSources|null>(null),[source,setSource]=useState<OdometerSource>("auto");
 const [clock]=useState(()=>Date.now());
 const [busy,setBusy]=useState(false),[error,setError]=useState(""),[manual,setManual]=useState<MaintenanceReading|null>(null),[revision,setRevision]=useState(0);
 useEffect(()=>{let active=true;fetchOdometerSources(truckId).then(v=>{if(active){setData(v);setSource(v.source);setError("");}}).catch(e=>{if(active)setError(e.message);});return()=>{active=false;};},[truckId,revision]);
 if(manual) return <MaintenanceReadingEditor initial={manual} options={options} requireObservation onClose={()=>setManual(null)} onSaved={async()=>{setRevision(v=>v+1);await onSaved();}} />;
 const canSave=canEdit&&!!data&&(source==="auto"||!!data.readings.find(v=>v.source===source)?.valid);
 return <Modal title={`Odometer${data?` · ${data.unitNumber}`:""}`} submitLabel="Use source" isSaving={busy} submitDisabled={!canSave} hideSubmit={!canEdit} onClose={onClose} onSubmit={e=>{e.preventDefault();if(!data||!canSave)return;setBusy(true);setError("");void saveOdometerSource(truckId,source,data.version).then(onSaved).then(onClose).catch(e=>setError(e.message)).finally(()=>setBusy(false));}}>
  {error&&<ErrorBanner message={error}/>}
  {!data?<p className="text-sm text-zinc-500">Loading readings…</p>:<div className="space-y-3">
   <p className="text-sm">Current: <strong className="tabular-nums">{data.current?.mileage?.toLocaleString()??"—"}</strong>{data.current&&<span className="text-zinc-500"> · {names[data.current.source]}</span>}</p>
   <div className="divide-y divide-zinc-800 rounded-lg border border-zinc-800">
    <label className="flex gap-3 p-3"><input aria-label="Use automatic odometer" type="radio" name="odometer-source" checked={source==="auto"} disabled={!canEdit} onChange={()=>setSource("auto")}/><span className="text-sm">Automatic<span className="block text-xs text-zinc-500">Prefer fresh Samsara, then valid Five ELD; retain saved history when neither is current.</span></span></label>
    {data.readings.map(v=><label key={v.source} className="flex gap-3 p-3"><input aria-label={`Use ${names[v.source]} odometer`} type="radio" name="odometer-source" checked={source===v.source} disabled={!canEdit||!v.valid} onChange={()=>setSource(v.source as OdometerSource)}/><span className="min-w-0 flex-1 text-sm"><span className="flex justify-between gap-4"><span>{names[v.source]}</span><span className="tabular-nums">{v.mileage?.toLocaleString()??"—"} mi</span></span><span className="block text-xs text-zinc-500">{v.observedAt?new Date(v.observedAt).toLocaleString():v.date??"No observation"}{v.observedAt&&clock-Date.parse(v.observedAt)>3600000?" · Last known":""}</span>{v.problem&&<span className="block text-xs text-amber-400">{v.problem}</span>}</span></label>)}
   </div>
   <p className="text-xs text-zinc-500">Applies to this truck across service programs and work order autofill. A chosen source stays selected until you change it; unavailable readings leave current mileage blank. Status Board always uses Five ELD.</p>
   {canEdit&&<button type="button" className="ui-button" onClick={()=>{const existing=data.manual?.date===today()?data.manual:null;setManual(existing??{id:"",truckId,recordId:null,date:today(),mileage:data.current?.mileage??NaN,source:"manual",version:0,actorName:"",updatedAt:"",observedAt:new Date().toISOString(),note:""});}}>{data.manual?.date===today()?"Correct manual reading":"Record manual reading"}</button>}
  </div>}
 </Modal>;
}
