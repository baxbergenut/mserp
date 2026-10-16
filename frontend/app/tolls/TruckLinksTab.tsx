"use client";


import { ColumnFilter } from "@/app/components/management/ColumnFilter";
import Link from "@/app/components/AccessLink";
import { useCallback, useEffect, useRef, useState } from "react";
import { fetchPrePassTruckLinks, previewPrePassTruckLink, savePrePassTruckLink, undoPrePassTruckLink } from "../lib/api";
import { usePermissions } from "../lib/access";
import type { PrePassTruckGroup, PrePassTruckLinks, PrePassTruckLinkInput, PrePassTruckMapping, PrePassTruckPreview } from "../lib/types";
import { controlClass, EmptyState, ErrorBanner, Field, Modal, TableShell } from "../components/management/ManagementUI";
const money = (value: string) => Number(value).toLocaleString("en-US", { style: "currency", currency: "USD" });
const identity = (key: string) => key.startsWith("tag:") ? `Tag ${key.slice(4)}` : key.startsWith("unit:") ? `Unit ${key.slice(5)}` : "Missing tag and unit";

export function TruckLinksTab() {
 const permissions = usePermissions();
 const canWrite = permissions.includes("tolls.link"), canProfile = permissions.includes("drivers.read");
 const [data,setData] = useState<PrePassTruckLinks|null>(null);
 const [from,setFrom] = useState("");
 const [error,setError] = useState("");
 const [notice,setNotice] = useState("");
 const [busy,setBusy] = useState(false);
 const saving = useRef(false);
 const [group,setGroup] = useState<PrePassTruckGroup|null>(null);
 const [undo,setUndo] = useState<PrePassTruckMapping|null>(null);
 const [input,setInput] = useState<PrePassTruckLinkInput|null>(null);
 const [preview,setPreview] = useState<PrePassTruckPreview|null>(null);
 const load=useCallback(async(signal?:AbortSignal)=>{try{const result=await fetchPrePassTruckLinks(from,signal);if(!signal?.aborted)setData(result)}catch(e){if(!signal?.aborted)setError(e instanceof Error?e.message:"Could not load truck links.")}},[from]);
 useEffect(()=>{const c=new AbortController();const timer=setTimeout(()=>void load(c.signal),0);return()=>{clearTimeout(timer);c.abort()}},[load]);
 useEffect(()=>{if(!notice)return;const timer=setTimeout(()=>setNotice(""),5000);return()=>clearTimeout(timer)},[notice]);
 function open(g:PrePassTruckGroup){setError("");setPreview(null);setGroup(g);setInput({id:crypto.randomUUID(),environment:g.environment,identityKey:g.identityKey,truckId:"",from:g.firstExit,to:"",revision:""})}
 function change(patch:Partial<PrePassTruckLinkInput>){setInput(v=>v?{...v,...patch}:v);setPreview(null);setError("")}
 async function submit(){
  if(saving.current)return;saving.current=true;setBusy(true);setError("");
  try{
   if(undo){await undoPrePassTruckLink(undo.id,undo.version);setUndo(null);setNotice("Mapping undone. Its linked tolls are available for review.");await load()}
   else if(input && !preview){setPreview(await previewPrePassTruckLink(input))}
   else if(input && preview && !preview.closed && preview.count>0){await savePrePassTruckLink({...input,revision:preview.revision});setGroup(null);setInput(null);setPreview(null);setNotice("Truck linked. This mapping also applies to future imports within its dates.");await load()}
  }catch(e){setError(e instanceof Error?e.message:"Could not update truck links.");setPreview(null)}finally{saving.current=false;setBusy(false)}
 }
 const truck=data?.trucks.find(t=>t.id===input?.truckId);
 return <section className="space-y-4" aria-label="PrePass truck links">
  <div className="flex flex-wrap items-center justify-between gap-3"><p className="text-[13px] text-zinc-400">Match a PrePass tag once to link its tolls and future imports.</p></div>
  {error && !group && !undo && <ErrorBanner message={error}/>}{notice && <p role="status" className="text-[13px] text-emerald-400">{notice}</p>}
  <TableShell><table className="w-full text-left text-[13px]"><thead><tr>{["PrePass identity","Unit names","Transponder / plate","Unlinked tolls","Amount",""] .map((v,i)=><th key={i} className="px-3 py-2 text-zinc-500 font-medium">{v === "Unlinked tolls" ? <ColumnFilter label="Unlinked tolls" active={!!from}><label className="flex items-center gap-2 text-[13px] text-zinc-400">Posted since<input aria-label="Unlinked tolls posted since" type="date" className={controlClass.replace("w-full","w-auto")} value={from||data?.dateFrom||""} onChange={e=>setFrom(e.target.value)} /></label></ColumnFilter> : v}</th>)}</tr></thead><tbody className="divide-y divide-zinc-800">{!data ? <tr><td colSpan={6} className="p-8 text-center text-zinc-500" role="status">Loading…</td></tr> : !data.groups.length ? <tr><td colSpan={6}>{<EmptyState message="All tolls in this date range are linked to trucks."/>}</td></tr> : <>{data.groups.map(g=><tr key={`${g.environment}:${g.identityKey}`}><td className="px-3 py-2">{identity(g.identityKey)}{g.environment!=="production"&&<div className="text-amber-400">{g.environment}</div>}</td><td className="px-3 py-2">{g.units.join(", ")}</td><td className="px-3 py-2 font-mono text-[12px]">{g.devices.join(", ")}</td><td className="px-3 py-2">{g.count}<div className="text-[11px] text-zinc-500">Travel: {g.firstExit} – {g.lastExit}</div></td><td className="px-3 py-2">{money(g.amount)}</td><td className="px-3 py-2">{canWrite&&<button className="ui-button" disabled={!g.identityKey} onClick={()=>open(g)}>Link truck</button>}{!g.identityKey&&<span className="text-zinc-500">Identify in PrePass</span>}</td></tr>)}</>}</tbody></table></TableShell>
  {!!data?.mappings.length&&<><h2 className="text-sm font-medium text-zinc-300">Saved truck mappings</h2><TableShell><table className="w-full text-left text-[13px]"><thead><tr>{["PrePass identity","Truck","Travel dates","Linked tolls",""] .map((v,i)=><th key={i} className="px-3 py-2 text-zinc-500 font-medium">{v}</th>)}</tr></thead><tbody className="divide-y divide-zinc-800">{data.mappings.map(m=><tr key={m.id}><td className="px-3 py-2">{identity(m.identityKey)}{m.environment!=="production"&&` · ${m.environment}`}</td><td className="px-3 py-2">{canProfile ? <Link className="text-blue-400" href={`/trucks/detail?id=${m.truckId}`}>{m.unit}</Link> : m.unit}</td><td className="px-3 py-2">{m.from} – {m.to||"Ongoing"}</td><td className="px-3 py-2">{m.count}</td><td className="px-3 py-2">{canWrite&&<button className="ui-button" onClick={()=>{setError("");setUndo(m)}}>Undo mapping</button>}</td></tr>)}</tbody></table></TableShell></>}
  {group&&input&&<Modal title={`Link ${identity(group.identityKey)}`} isSaving={busy} submitLabel={preview?"Save mapping":"Preview matching tolls"} submitDisabled={!!preview && (preview.closed || preview.count===0)} onClose={()=>{setGroup(null);setInput(null);setPreview(null)}} onSubmit={e=>{e.preventDefault();void submit()}}><fieldset disabled={busy} className="space-y-4">
   {error&&<ErrorBanner message={error}/>}<p className="text-[13px] text-zinc-400">PrePass units: {group.units.join(", ")} · Transponder / plate: {group.devices.join(", ")}. Confirm the vehicle in PrePass using its VIN or plate.</p>
   <Field label="Truck"><select required className={controlClass} value={input.truckId} onChange={e=>change({truckId:e.target.value})}><option value="">Select truck</option>{data?.trucks.map(t=><option key={t.id} value={t.id}>{t.unit}{t.vin?` · ${t.vin}`:""}{t.plate?` · ${t.plate}`:""}</option>)}</select></Field>
   {truck&&<p className="text-[13px] text-zinc-400">VIN: {truck.vin||"Not recorded"} · Plate: {truck.plate||"Not recorded"}</p>}
   <div className="grid grid-cols-2 gap-3"><Field label="Travel dates from"><input required type="date" className={controlClass} value={input.from} onChange={e=>change({from:e.target.value})}/></Field><Field label="Through (optional)"><input type="date" min={input.from} className={controlClass} value={input.to} onChange={e=>change({to:e.target.value})}/></Field></div>
   <p className="text-[12px] text-zinc-500">Dates use the toll exit date. Preview includes every unlinked toll in these travel dates, including older postings. Leave Through blank for future imports. Use a date range if the tag moved between trucks.</p>
   {preview&&<div role="status" className="rounded-lg border border-zinc-700 p-3 text-[13px]"><p>{preview.count} tolls · {money(preview.amount)} → truck {truck?.unit}</p><p className="text-zinc-400">Source units: {preview.units.join(", ")}</p>{preview.units.length>1&&<p className="text-amber-400">Multiple source unit names. Check the travel dates before saving.</p>}{preview.closed?<p className="text-red-400">Reopen affected statements before linking these tolls.</p>:preview.count===0?<p className="text-amber-400">No unlinked tolls in this range. Change the dates.</p>:<p className="text-zinc-400">Future matching imports will use this truck within these dates.</p>}</div>}
  </fieldset></Modal>}
  {undo&&<Modal title="Undo truck mapping" isSaving={busy} submitLabel="Undo mapping" onClose={()=>setUndo(null)} onSubmit={e=>{e.preventDefault();void submit()}}>{error&&<ErrorBanner message={error}/>}<p className="text-sm text-zinc-300">Stop mapping {identity(undo.identityKey)} to {undo.unit} and unlink all tolls assigned by this mapping, including later imports? Other truck links are preserved.</p></Modal>}
 </section>
}
