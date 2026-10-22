"use client";

import {useEffect,useState} from "react";
import {useRouter,useSearchParams} from "next/navigation";
import {ChevronLeft,ChevronRight,ListChecks,History, Pencil} from "lucide-react";
import Link from "@/app/components/AccessLink";
import {changePTI,createPTIWeek,fetchPTIHistory,fetchPTIWeek} from "@/app/lib/api";
import type {PTIChange,PTIEvent,PTIRow,PTIWeek} from "@/app/lib/types";
import {usePermissions} from "@/app/lib/access";
import {statusLabel} from "@/app/lib/fleetStatus";
import {currentChargeWeek,validChargeWeek} from "@/app/accounting/driver-charges/charges";
import {controlClass,ErrorBanner,Field,ManagementHeader,Modal} from "@/app/components/management/ManagementUI";

const eligibilityLabels={required:"Required",exempt:"Exempt",unknown:"Needs confirmation"};
const reviewLabels={pending:"Not reviewed",checked:"Checked",issues:"Issues found"};
const stamp=(value:string|null)=>value?new Date(value).toLocaleString("en-US",{timeZone:"America/New_York",month:"short",day:"numeric",hour:"numeric",minute:"2-digit"}):"—";
function shiftWeek(week:string,days:number){const d=new Date(week+"T12:00:00Z");d.setUTCDate(d.getUTCDate()+days);return d.toISOString().slice(0,10);}
function formValues(row:PTIRow):PTIChange{return {requestId:"",version:row.version,received:row.received,review:row.review,eligibility:row.eligibility,eligibilityReason:row.eligibilityReason,videoUrl:row.videoUrl,notes:row.notes};}
function mergeWeek(prior:PTIWeek|null,next:PTIWeek):PTIWeek{
 if(!prior||prior.weekStart!==next.weekStart)return next;
 if(prior.exists&&!next.exists)return prior;
 return {...next,rows:next.rows.map(row=>{const old=prior.rows.find(v=>v.truckId===row.truckId);return old&&old.version>row.version?old:row;})};
}

export default function WeeklyPTIPage(){
 const router=useRouter(),query=useSearchParams(),current=currentChargeWeek();
 const selected=query.get("weekStart")??"";const week=validChargeWeek(selected)?selected:current;
 const navigate=(value:string)=>{if(validChargeWeek(value)&&value<=current)router.push(`/fleet/weekly-pti?weekStart=${value}`,{scroll:false});};
 return <div className="space-y-5 p-6">
  <ManagementHeader title="Weekly PTI" icon={ListChecks}/>
  <div className="flex flex-wrap items-center gap-2">
   <button className="ui-button" aria-label="Previous week" onClick={()=>navigate(shiftWeek(week,-7))}><ChevronLeft className="h-4 w-4"/></button>
   <label className="flex shrink-0 items-center gap-2 whitespace-nowrap text-xs">Week of<span className="w-40"><input aria-label="PTI week" type="date" min="2000-01-03" max={current} step={7} value={week} className={controlClass} onChange={e=>navigate(e.target.value)}/></span></label>
   <button className="ui-button" aria-label="Next week" disabled={week>=current} onClick={()=>navigate(shiftWeek(week,7))}><ChevronRight className="h-4 w-4"/></button>
   <button className="ui-button" onClick={()=>navigate(current)}>This week</button>
   <span className="text-xs text-zinc-500">Monday–Sunday · New York</span>
  </div>
  <Checklist key={week} week={week}/>
 </div>;
}

function Checklist({week}:{week:string}){
 const canEdit=usePermissions().includes("pti.edit");
 const [data,setData]=useState<PTIWeek|null>(null),[error,setError]=useState(""),[attempt,setAttempt]=useState(0);
 const [search,setSearch]=useState(""),[filter,setFilter]=useState(""),[reviewFilter,setReviewFilter]=useState("");
 const [saving,setSaving]=useState(""),[editing,setEditing]=useState<(PTIRow&{requestedReview?:PTIRow["review"]})|null>(null),[history,setHistory]=useState<PTIRow|null>(null);
 useEffect(()=>{
  const c=new AbortController();fetchPTIWeek(week,c.signal).then(v=>{setData(old=>mergeWeek(old,v));setError("");}).catch(e=>{if(!c.signal.aborted)setError(e.message);});return()=>c.abort();
 },[week,attempt]);
 useEffect(()=>{const refresh=()=>{if(document.visibilityState==="visible")setAttempt(v=>v+1);};const timer=setInterval(refresh,30000);window.addEventListener("focus",refresh);return()=>{clearInterval(timer);window.removeEventListener("focus",refresh);};},[]);
 const accept=(row:PTIRow)=>{setData(old=>old?{...old,rows:old.rows.map(v=>v.truckId===row.truckId&&v.version<=row.version?row:v)}:old);};
 const save=async(row:PTIRow,input:PTIChange)=>{
  setSaving(row.truckId);setError("");
  try{const value=await changePTI(week,row.truckId,{...input,requestId:crypto.randomUUID()});accept(value);return value;}
  catch(e){setError(e instanceof Error?e.message:"PTI could not be saved");setAttempt(v=>v+1);throw e;}
  finally{setSaving("");}
 };
 const start=async()=>{setSaving("week");try{setData(await createPTIWeek(week));setError("");}catch(e){setError(e instanceof Error?e.message:"Checklist could not be created");}finally{setSaving("");}};
 const all=data?.rows??[],required=all.filter(r=>r.eligibility==="required"),unknown=all.filter(r=>r.eligibility==="unknown").length;
 const summary:[[string,number],[string,number],[string,number],[string,number],[string,number],[string,number]]=[
  ["Required",required.length],["Received",required.filter(r=>r.received).length],["Not received",required.filter(r=>!r.received).length],
  ["Not reviewed",all.filter(r=>r.received&&r.review==="pending").length],["Issues found",all.filter(r=>r.review==="issues").length],["Exempt",all.filter(r=>r.eligibility==="exempt").length]
 ];
 const rows=all.filter(r=>(!search||[r.truckUnit,...r.crew.map(d=>d.name),r.notes].join(" ").toLowerCase().includes(search.toLowerCase()))&&
  (!filter||(filter==="received"?r.received:filter==="missing"?r.eligibility==="required"&&!r.received:r.eligibility===filter))&&(!reviewFilter||(r.received&&r.review===reviewFilter)));
 return <>
  {error&&<ErrorBanner message={error}/>}
  {!data?<div className="py-8 text-sm text-zinc-500">{error?<button className="ui-button" onClick={()=>setAttempt(v=>v+1)}>Retry</button>:"Loading weekly PTI…"}</div>:!data.exists?<div className="rounded-xl border border-zinc-800 p-6 text-sm text-zinc-400">
   <p>No checklist has been created for this week.</p>{week===currentChargeWeek()&&canEdit&&<button className="ui-button mt-3" disabled={!!saving} onClick={()=>void start()}>Create weekly checklist</button>}
  </div>:<>
   <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">{summary.map(([label,count])=><div key={label} className="rounded-xl border border-zinc-800 p-4"><p className="text-xl font-semibold tabular-nums">{count}</p><p className="mt-1 text-xs text-zinc-500">{label}</p></div>)}</div>
   {unknown>0&&<div role="status" className="rounded-lg border border-amber-900/60 bg-amber-500/5 px-4 py-3 text-xs text-amber-300">{unknown} trucks need Monday eligibility confirmed. {data.lateSnapshot?"A timely Monday snapshot wasn’t captured; confirm Monday’s actual situation.":"Monday status history is incomplete."}<button className="ml-2 underline" onClick={()=>setFilter("unknown")}>Show trucks</button></div>}
   <div className="flex flex-wrap items-center gap-2">
    <input aria-label="Search PTI" placeholder="Search truck, driver or notes…" className={controlClass+" max-w-sm"} value={search} onChange={e=>setSearch(e.target.value)}/>
    <div className="w-48"><select aria-label="PTI filter" className={controlClass} value={filter} onChange={e=>setFilter(e.target.value)}><option value="">All trucks</option><option value="required">Required</option><option value="missing">Not received</option><option value="received">Received</option><option value="exempt">Exempt</option><option value="unknown">Needs confirmation</option></select></div>
    <div className="w-44"><select aria-label="Review filter" className={controlClass} value={reviewFilter} onChange={e=>setReviewFilter(e.target.value)}><option value="">All reviews</option>{Object.entries(reviewLabels).map(([value,label])=><option key={value} value={value}>{label}</option>)}</select></div>
    <button className="ui-button ml-auto" onClick={()=>setAttempt(v=>v+1)}>Refresh</button>
   </div>
   <p className="text-xs text-zinc-500">One video per truck. Eligibility is based on Monday’s status (New York).</p>
   <div className="overflow-x-auto rounded-xl border border-zinc-800"><table className="w-full text-left text-[13px]"><thead className="bg-zinc-900 text-xs text-zinc-400"><tr>{["Truck","Driver / team","Monday eligibility","Video received","Review","Notes / video","Updated by",""].map((label,i)=><th key={label||i} className="px-3 py-3 font-medium">{label}</th>)}</tr></thead>
    <tbody>{rows.map(row=><tr key={row.truckId} className="border-t border-zinc-800 align-top" aria-label={`Truck ${row.truckUnit}`}>
     <td className="px-3 py-3 font-mono"><Link className="text-blue-400" href={`/trucks/detail?id=${row.truckId}`}>{row.truckUnit}</Link></td>
     <td className="px-3 py-3">{row.crew.length?row.crew.map(d=><div key={d.id}><Link href={`/drivers/detail?id=${d.id}`} className="hover:text-blue-300">{d.name}</Link></div>):<span className="text-zinc-500">No driver</span>}</td>
     <td className="px-3 py-3"><span title={row.eligibilityReason} className={row.eligibility==="unknown"?"text-amber-300":row.eligibility==="exempt"?"text-zinc-500":"text-zinc-200"}>{eligibilityLabels[row.eligibility]}</span>{canEdit&&row.eligibility==="unknown"&&<button className="ml-2 text-xs text-blue-400" disabled={!!saving} onClick={()=>setEditing(row)}>Confirm</button>}</td>
     <td className="px-3 py-2">{canEdit?<select aria-label={`Truck ${row.truckUnit} video received`} className={controlClass+" min-w-36"} disabled={!!saving} value={row.received?"yes":"no"} onChange={e=>{const received=e.target.value==="yes";void save(row,{...formValues(row),received,...(!received?{review:"pending",videoUrl:""}:{})}).catch(()=>{});}}><option value="no">Not received</option><option value="yes">Received</option></select>:row.received?"Received":"Not received"}</td>
     <td className="px-3 py-2">{canEdit&&row.received?<select aria-label={`Truck ${row.truckUnit} review`} className={controlClass+" min-w-36"} disabled={!!saving} value={row.review} onChange={e=>{if(e.target.value==="issues"){setEditing({...row,requestedReview:"issues"});return;}void save(row,{...formValues(row),review:e.target.value as PTIRow["review"]}).catch(()=>{});}}>{Object.entries(reviewLabels).map(([value,label])=><option key={value} value={value}>{label}</option>)}</select>:row.received?reviewLabels[row.review]:<span className="text-zinc-600">—</span>}</td>
     <td className="max-w-xs px-3 py-3"><p title={row.notes} className="line-clamp-2 text-xs text-zinc-400">{row.notes}</p>{row.videoUrl&&<a className="text-xs text-blue-400" href={row.videoUrl} target="_blank" rel="noopener noreferrer">Open video</a>}</td>
     <td className="px-3 py-3 text-xs text-zinc-500">{row.updatedBy||"—"}<div className="mt-1 whitespace-nowrap">{stamp(row.updatedAt)}</div></td>
     <td className="px-3 py-2"><div className="flex gap-1">{canEdit&&<button className="ui-button" aria-label={`Edit PTI truck ${row.truckUnit}`} disabled={!!saving} onClick={()=>setEditing(row)}><Pencil className="h-3.5 w-3.5"/></button>}<button className="ui-button" aria-label={`PTI history truck ${row.truckUnit}`} onClick={()=>setHistory(row)}><History className="h-3.5 w-3.5"/></button></div></td>
    </tr>)}</tbody></table>{!rows.length&&<p className="p-6 text-sm text-zinc-500">No trucks match these filters.</p>}</div>
   <p className="text-xs text-zinc-600">Checklist created {stamp(data.createdAt)} by {data.createdBy}. Times in New York.</p>
  </>}
  {editing&&<PTIEditor row={editing} busy={!!saving} close={()=>setEditing(null)} save={input=>save(editing,input).then(()=>setEditing(null))}/>}
  {history&&<PTIHistory row={history} close={()=>setHistory(null)}/>}
 </>;
}

function PTIEditor({row,busy,close,save}:{row:PTIRow&{requestedReview?:PTIRow["review"]};busy:boolean;close:()=>void;save:(input:PTIChange)=>Promise<void>}){
 const [form,setForm]=useState(()=>({...formValues(row),review:row.requestedReview??row.review})),[error,setError]=useState("");
 const update=<K extends keyof PTIChange>(key:K,value:PTIChange[K])=>setForm(old=>({...old,[key]:value}));
 return <Modal title={`Weekly PTI · Truck ${row.truckUnit}`} description="Confirm Monday eligibility, record receipt, and review the video." isSaving={busy} submitLabel="Save PTI" onClose={close} onSubmit={e=>{e.preventDefault();void save(form).catch(e=>setError(e instanceof Error?e.message:"Save failed"));}}>
  {error&&<ErrorBanner message={error}/>}<div className="space-y-4 p-1">
   <p className="text-xs text-zinc-400">Monday truck condition: {row.condition?statusLabel(row.condition):"Unknown"}. {row.crew.map(d=>`${d.name}: ${d.availability?statusLabel(d.availability):"Unknown"}`).join(" · ")||"No driver assigned"}</p>
   <p className="text-xs text-zinc-500">Original eligibility: {eligibilityLabels[row.snapshotEligibility as PTIRow["eligibility"]]} · {row.snapshotReason}</p>
   <Field label="Monday eligibility"><select aria-label="Monday eligibility" className={controlClass} value={form.eligibility} onChange={e=>update("eligibility",e.target.value as PTIRow["eligibility"])}>{Object.entries(eligibilityLabels).map(([v,label])=><option key={v} value={v}>{label}</option>)}</select></Field>
   <Field label="Eligibility reason"><input aria-label="Eligibility reason" className={controlClass} maxLength={1000} value={form.eligibilityReason} onChange={e=>update("eligibilityReason",e.target.value)}/></Field>
   <div className="grid gap-4 sm:grid-cols-2"><Field label="Video received"><select aria-label="Video received" className={controlClass} value={form.received?"yes":"no"} onChange={e=>{const received=e.target.value==="yes";setForm(old=>({...old,received,...(!received?{review:"pending",videoUrl:""}:{})}));}}><option value="no">Not received</option><option value="yes">Received</option></select></Field>
   <Field label="Video review"><select aria-label="Video review" className={controlClass} disabled={!form.received} value={form.review} onChange={e=>update("review",e.target.value as PTIRow["review"])}>{Object.entries(reviewLabels).map(([v,label])=><option key={v} value={v}>{label}</option>)}</select></Field></div>
   <Field label="Video link (optional)"><input aria-label="Video link" className={controlClass} type="url" maxLength={2000} disabled={!form.received} placeholder="https://…" value={form.videoUrl} onChange={e=>update("videoUrl",e.target.value)}/></Field>
   <Field label="Notes"><textarea aria-label="PTI notes" className={controlClass} rows={3} maxLength={5000} required={form.review==="issues"} value={form.notes} onChange={e=>update("notes",e.target.value)}/></Field>
   {row.receivedAt&&<p className="text-xs text-zinc-500">Receipt recorded {stamp(row.receivedAt)} by {row.receivedBy}. {row.reviewedAt&&`Reviewed ${stamp(row.reviewedAt)} by ${row.reviewedBy}.`}</p>}
   {row.review!=="pending"&&form.videoUrl!==row.videoUrl&&<p className="text-xs text-amber-300">Changing the video link resets its review to Not reviewed.</p>}
  </div>
 </Modal>;
}

function PTIHistory({row,close}:{row:PTIRow;close:()=>void}){
 const [events,setEvents]=useState<PTIEvent[]|null>(null),[error,setError]=useState("");
 useEffect(()=>{const c=new AbortController();fetchPTIHistory(row.weekStart,row.truckId,c.signal).then(setEvents).catch(e=>{if(!c.signal.aborted)setError(e.message);});return()=>c.abort();},[row.weekStart,row.truckId]);
 const changes=(event:PTIEvent)=>[
  event.before.eligibility!==event.after.eligibility?`Eligibility: ${eligibilityLabels[event.before.eligibility]} → ${eligibilityLabels[event.after.eligibility]}`:"",
  event.before.received!==event.after.received?`Video: ${event.after.received?"Received":"Not received"}`:"",
  event.before.review!==event.after.review?`Review: ${reviewLabels[event.after.review]}`:"",
  event.before.notes!==event.after.notes?`Notes: ${event.after.notes||"Cleared"}`:"",
  event.before.videoUrl!==event.after.videoUrl?`Video link: ${event.after.videoUrl||"Cleared"}`:"",
  event.before.eligibilityReason!==event.after.eligibilityReason?`Reason: ${event.after.eligibilityReason}`:""
 ].filter(Boolean);
 return <Modal title={`PTI history · Truck ${row.truckUnit}`} isSaving={false} submitLabel="" hideSubmit cancelLabel="Close" onClose={close} onSubmit={e=>e.preventDefault()}>
  {error&&<ErrorBanner message={error}/>}<div className="space-y-4 text-xs">{events===null?<p>Loading history…</p>:!events.length?<p className="text-zinc-500">No changes yet.</p>:events.map(event=><div key={event.id} className="border-b border-zinc-800 pb-3"><p className="mb-2 font-medium">{event.actorName} <span className="ml-2 font-normal text-zinc-500">{stamp(event.createdAt)}</span></p>{changes(event).map((line,i)=><p key={i} className="break-words text-zinc-400">{line}</p>)}</div>)}</div>
 </Modal>;
}
