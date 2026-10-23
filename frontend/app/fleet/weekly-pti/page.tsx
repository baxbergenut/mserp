"use client";
import {useEffect,useRef,useState} from "react";
import {ListChecks} from "lucide-react";
import Link from "@/app/components/AccessLink";
import {changePTI,createPTIWeek,fetchPTIMatrix} from "@/app/lib/api";
import type {PTIMatrix,PTIRow} from "@/app/lib/types";
import {usePermissions} from "@/app/lib/access";
import {currentChargeWeek} from "@/app/accounting/driver-charges/charges";
import {controlClass,ErrorBanner,ManagementHeader} from "@/app/components/management/ManagementUI";
type Mark="/"|"1"|"0";
const colors={"/":"#ffff00","1":"#00ff00","0":"#ff0000"};
const label=(week:string)=>new Date(week+"T12:00:00Z").toLocaleDateString("en-US",{timeZone:"UTC",month:"short",day:"numeric"});
const stamp=(at:string|null)=>at?new Date(at).toLocaleString("en-US",{timeZone:"America/New_York",year:"numeric",month:"short",day:"numeric",hour:"numeric",minute:"2-digit",timeZoneName:"short"}):"";
const markOf=(r?:PTIRow):Mark|""=>r?.mark??(r?.received&&r.review!=="pending"?"1":"");
const keyOf=(week:string,truck:string)=>week+":"+truck;
function blank(week:string,truck:PTIMatrix["trucks"][number]):PTIRow{return {weekStart:week,truckId:truck.id,truckUnit:truck.unit,crew:[],condition:"",snapshotEligibility:"unknown",snapshotReason:"",eligibility:"unknown",eligibilityReason:"",received:false,receivedAt:null,receivedBy:"",review:"pending",reviewedAt:null,reviewedBy:"",videoUrl:"",notes:"",updatedAt:null,updatedBy:"",version:0};}
export default function WeeklyPTIPage(){
 const canEdit=usePermissions().includes("pti.edit"),current=currentChargeWeek();
 const [data,setData]=useState<PTIMatrix|null>(null),[error,setError]=useState(""),[attempt,setAttempt]=useState(0),[search,setSearch]=useState(""),[selected,setSelected]=useState(current),[busy,setBusy]=useState<Record<string,boolean>>({}),[drafts,setDrafts]=useState<Record<string,{value:string;base:PTIRow}>>({});
 const locks=useRef(new Set<string>()),scroller=useRef<HTMLDivElement>(null),positioned=useRef(false);
 const refresh=()=>setAttempt(v=>v+1);
 useEffect(()=>{const c=new AbortController();fetchPTIMatrix(c.signal).then(next=>{setData(old=>{const prior=new Map(old?.rows.map(r=>[keyOf(r.weekStart,r.truckId),r]));return {...next,rows:next.rows.map(r=>{const saved=prior.get(keyOf(r.weekStart,r.truckId));return saved&&saved.version>r.version?saved:r;})};});}).catch(e=>{if(!c.signal.aborted)setError(e.message);});return()=>c.abort();},[attempt]);
 useEffect(()=>{const update=()=>{if(document.visibilityState==="visible")setAttempt(v=>v+1);};const timer=setInterval(update,30000);window.addEventListener("focus",update);return()=>{clearInterval(timer);window.removeEventListener("focus",update);};},[]);
 useEffect(()=>{if(data?.weeks.length&&!positioned.current&&scroller.current){scroller.current.scrollLeft=scroller.current.scrollWidth;positioned.current=true;}},[data]);
 useEffect(()=>{if(!Object.keys(drafts).length&&!Object.keys(busy).length)return;const warn=(e:BeforeUnloadEvent)=>e.preventDefault();window.addEventListener("beforeunload",warn);return()=>window.removeEventListener("beforeunload",warn);},[drafts,busy]);
 const save=async(row:PTIRow,mark:Mark,notes=row.notes)=>{
  const key=keyOf(row.weekStart,row.truckId);if(locks.current.has(key))return;
  locks.current.add(key);setBusy(old=>({...old,[key]:true}));setError("");
  try{const value=await changePTI(row.weekStart,row.truckId,{requestId:crypto.randomUUID(),version:row.version,mark,received:mark==="1",review:mark==="1"?"checked":"pending",notes});
   setData(old=>old?{...old,rows:[...old.rows.filter(r=>keyOf(r.weekStart,r.truckId)!==key),value]}:old);setDrafts(old=>{const next={...old};delete next[key];return next;});
  }catch(e){setError(e instanceof Error?e.message:"Could not save PTI");refresh();}
  finally{locks.current.delete(key);setBusy(old=>{const next={...old};delete next[key];return next;});}
 };
 const rows=new Map(data?.rows.map(r=>[keyOf(r.weekStart,r.truckId),r])),weeks=data?.weeks??[],noteWeek=weeks.includes(selected)?selected:weeks.at(-1)??current;
 const trucks=(data?.trucks??[]).filter(t=>t.unit.toLowerCase().includes(search.trim().toLowerCase())).sort((a,b)=>a.unit.localeCompare(b.unit,undefined,{numeric:true})||a.unit.localeCompare(b.unit));
 const groups=[{name:"Company & investor trucks",trucks:trucks.filter(t=>t.ownership!=="owner_operator")},{name:"Owner-operated trucks",trucks:trucks.filter(t=>t.ownership==="owner_operator")}];
 return <div className="space-y-4 p-4 md:p-6">
  <ManagementHeader title="Weekly PTI" icon={ListChecks}/>{error&&<ErrorBanner message={error}/>}
  <div className="flex flex-wrap items-center gap-3">
   <label className="w-48 shrink-0"><input aria-label="Search PTI" placeholder="Search truck" value={search} onChange={e=>setSearch(e.target.value)} className={controlClass}/></label>
   <label className="flex shrink-0 items-center gap-2 whitespace-nowrap text-xs text-zinc-400">Notes for<select aria-label="Notes week" className={controlClass} value={noteWeek} onChange={e=>setSelected(e.target.value)}>{weeks.map(w=><option key={w} value={w}>{label(w)}, {w.slice(0,4)}</option>)}</select></label>
   <button className="ui-button ml-auto" onClick={refresh}>Refresh</button>
   {canEdit&&data&&!weeks.includes(current)&&<button className="ui-button" onClick={()=>void createPTIWeek(current).then(refresh).catch(e=>setError(e.message))}>Start this week</button>}
  </div>
  {!data?<p className="py-8 text-sm text-zinc-500">Loading weekly PTI…</p>:<div ref={scroller} className="max-h-[calc(100dvh-220px)] overflow-auto rounded-lg border border-zinc-700">
   <table className="w-full border-separate border-spacing-0 text-xs" aria-label="Weekly PTI matrix">
    <thead className="sticky top-0 z-20"><tr>
     <th style={{backgroundColor:"#6d9eeb",color:"#000"}} className="sticky left-0 z-30 min-w-28 border-b border-r border-zinc-700 bg-[#6d9eeb] px-3 py-2 text-left text-black">Unit #</th>
     {weeks.map(w=><th key={w} style={{backgroundColor:"#6d9eeb",color:"#000"}} className="min-w-20 border-b border-r border-zinc-700 bg-[#6d9eeb] p-0 text-black"><button className={"w-full px-2 py-2 "+(w===noteWeek?"font-bold underline underline-offset-4":"font-normal")} onClick={()=>setSelected(w)} title={w}>{label(w)}<span className="block text-[10px]">{w.slice(0,4)}</span></button></th>)}
     <th className="min-w-64 border-b border-r border-zinc-700 bg-zinc-900 px-3 py-2 text-left font-medium">Notes · {label(noteWeek)}</th>
     <th className="min-w-36 border-b border-zinc-700 bg-zinc-900 px-3 py-2 text-left font-medium">Checked by</th>
    </tr></thead>
    {groups.map(group=><tbody key={group.name} aria-label={group.name}>
     <tr><th colSpan={weeks.length+3} className="border-b border-zinc-700 bg-zinc-800 px-3 py-2 text-left font-medium"><span className="sticky left-3">{group.name} <span className="ml-2 text-zinc-400">{group.trucks.length}</span></span></th></tr>
     {group.trucks.map(truck=>{
      const noteKey=keyOf(noteWeek,truck.id),row=rows.get(noteKey)??blank(noteWeek,truck),draft=drafts[noteKey],mark=markOf(row);
      return <tr key={truck.id} aria-label={"Truck "+truck.unit}>
       <th className="sticky left-0 z-10 border-b border-r border-zinc-700 bg-zinc-950 px-3 py-1.5 text-left font-mono font-normal"><Link className="text-blue-400" href={"/trucks/detail?id="+truck.id}>{truck.unit}</Link></th>
       {weeks.map(w=>{const key=keyOf(w,truck.id),entry=rows.get(key)??blank(w,truck),value=markOf(entry);const title=[entry.updatedBy,stamp(entry.updatedAt),entry.notes].filter(Boolean).join(" · ");
        return <td key={w} className="border-b border-r border-zinc-700 p-0" style={{backgroundColor:value?colors[value]:undefined}}>
         <select aria-label={"Truck "+truck.unit+" PTI "+w} title={title||undefined} value={value} disabled={!canEdit||!!busy[key]} className={"h-8 w-full cursor-pointer appearance-none rounded-none border-0 bg-transparent text-center font-semibold outline-offset-[-2px] focus:outline-2 focus:outline-blue-600 disabled:cursor-default "+(value?"text-black":"text-zinc-500")} onChange={e=>{const v=e.target.value;if(v==="/"||v==="1"||v==="0")void save(drafts[key]?.base??entry,v,drafts[key]?.value??entry.notes);}}>
          <option value="" disabled>—</option>{(["/","1","0"] as const).map(v=><option key={v} value={v} style={{backgroundColor:colors[v],color:"#000"}}>{v}</option>)}
         </select>
        </td>;
       })}
       <td className="border-b border-r border-zinc-700 px-2">
        {canEdit?<><input type="text" aria-label={"Truck "+truck.unit+" notes"} className="h-8 w-full bg-transparent px-1 text-xs outline-none focus:ring-1 focus:ring-blue-500" placeholder="Add a note…" value={draft?.value??row.notes} maxLength={5000} disabled={!!busy[noteKey]||!mark} onChange={e=>setDrafts(old=>({...old,[noteKey]:{base:old[noteKey]?.base??row,value:e.target.value}}))} onBlur={()=>{if(draft&&mark&&draft.value!==draft.base.notes)void save(draft.base,mark,draft.value);}} onKeyDown={e=>{if(e.key==="Enter"&&draft&&mark){e.preventDefault();void save(draft.base,mark,draft.value);}}}/>
         {draft&&draft.base.version!==row.version&&<button className="text-[10px] text-blue-400 underline" onClick={()=>{if(mark)void save(row,mark,draft.value);}}>Save my note on the latest entry</button>}
        </>:<span title={row.notes}>{row.notes}</span>}
       </td>
       <td className="border-b border-zinc-700 px-3 text-zinc-400"><span title={stamp(row.reviewedAt)||undefined}>{mark==="1"?row.reviewedBy:""}</span></td>
      </tr>;
     })}
    </tbody>)}
   </table>{!trucks.length&&<p className="p-8 text-center text-zinc-500">No trucks match your search.</p>}
  </div>}
 </div>;
}
