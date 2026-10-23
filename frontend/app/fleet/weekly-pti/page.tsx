"use client";
import {useEffect,useRef,useState} from "react";
import {GripVertical,ListChecks} from "lucide-react";
import Link from "@/app/components/AccessLink";
import {changePTI,createPTIWeek,fetchPTIMatrix,savePTILayout} from "@/app/lib/api";
import type {PTIGroup,PTIMatrix,PTIRow} from "@/app/lib/types";
import {usePermissions} from "@/app/lib/access";
import {currentChargeWeek} from "@/app/accounting/driver-charges/charges";
import {controlClass,ErrorBanner,ManagementHeader} from "@/app/components/management/ManagementUI";
import {usePTIRowDrag} from "./usePTIRowDrag";
type Mark="/"|"1"|"0";
const colors={"/":"#ffff00","1":"#00ff00","0":"#ff0000"};
const label=(week:string)=>new Date(week+"T12:00:00Z").toLocaleDateString("en-US",{timeZone:"UTC",month:"short",day:"numeric"});
const stamp=(at:string|null)=>at?new Date(at).toLocaleString("en-US",{timeZone:"America/New_York",year:"numeric",month:"short",day:"numeric",hour:"numeric",minute:"2-digit",timeZoneName:"short"}):"";
const markOf=(r?:PTIRow):Mark|""=>r?.mark??(r?.received&&r.review!=="pending"?"1":"");
const keyOf=(week:string,truck:string)=>week+":"+truck;
const nextWeek=(week:string)=>{const d=new Date(week+"T12:00:00Z");d.setUTCDate(d.getUTCDate()+7);return d.toISOString().slice(0,10);};
function blank(week:string,truck:PTIMatrix["trucks"][number]):PTIRow{return {weekStart:week,truckId:truck.id,truckUnit:truck.unit,crew:[],condition:"",snapshotEligibility:"unknown",snapshotReason:"",eligibility:"unknown",eligibilityReason:"",received:false,receivedAt:null,receivedBy:"",review:"pending",reviewedAt:null,reviewedBy:"",videoUrl:"",notes:"",updatedAt:null,updatedBy:"",version:0};}
export default function WeeklyPTIPage(){
 const canEdit=usePermissions().includes("pti.edit"),current=currentChargeWeek();
 const [data,setData]=useState<PTIMatrix|null>(null),[error,setError]=useState(""),[attempt,setAttempt]=useState(0),[search,setSearch]=useState(""),[busy,setBusy]=useState<Record<string,boolean>>({});
 const locks=useRef(new Set<string>()),scroller=useRef<HTMLDivElement>(null),positioned=useRef(false);
 const [visibleWeeks,setVisibleWeeks]=useState(9);
 const cells=useRef(new Map<string,HTMLInputElement>()),undoStack=useRef<{requestId:string;after:PTIRow;order:number}[]>([]),sequence=useRef(0),undoing=useRef(false),pendingUndo=useRef(false);
 const [layoutBusy,setLayoutBusy]=useState(false),layoutLock=useRef(false);
 const ready=data!==null;
 useEffect(()=>{
  const element=scroller.current,main=element?.closest("main");if(!element||!main)return;
  const fit=()=>{const top=element.getBoundingClientRect().top-main.getBoundingClientRect().top+main.scrollTop;const height=Math.max(160,main.clientHeight-top-24)+"px";if(element.style.height!==height)element.style.height=height;setVisibleWeeks(Math.max(1,Math.floor(((element.parentElement?.clientWidth??688)-114)/64)));};
  const observer=new ResizeObserver(fit);observer.observe(main);if(element.parentElement)observer.observe(element.parentElement);window.addEventListener("resize",fit);fit();
  return()=>{observer.disconnect();window.removeEventListener("resize",fit);};
 },[ready]);
 const refresh=()=>setAttempt(v=>v+1);
 useEffect(()=>{const c=new AbortController();fetchPTIMatrix(c.signal).then(next=>{setData(old=>{const prior=new Map(old?.rows.map(r=>[keyOf(r.weekStart,r.truckId),r]));return {...next,...(old&&old.layoutVersion>next.layoutVersion?{trucks:old.trucks,layoutVersion:old.layoutVersion}:{}),rows:next.rows.map(r=>{const saved=prior.get(keyOf(r.weekStart,r.truckId));return saved&&saved.version>r.version?saved:r;})};});}).catch(e=>{if(!c.signal.aborted)setError(e.message);});return()=>c.abort();},[attempt]);
 useEffect(()=>{const update=()=>{if(document.visibilityState==="visible")setAttempt(v=>v+1);};const timer=setInterval(update,30000);window.addEventListener("focus",update);return()=>{clearInterval(timer);window.removeEventListener("focus",update);};},[]);
 useEffect(()=>{if(data?.weeks.length&&!positioned.current&&scroller.current){scroller.current.scrollLeft=scroller.current.scrollWidth;positioned.current=true;}},[data]);
 useEffect(()=>{if(!Object.keys(busy).length&&!layoutBusy)return;const warn=(e:BeforeUnloadEvent)=>e.preventDefault();window.addEventListener("beforeunload",warn);return()=>window.removeEventListener("beforeunload",warn);},[busy,layoutBusy]);
 const save=async(row:PTIRow,mark:Mark|"")=>{
  const key=keyOf(row.weekStart,row.truckId);if(locks.current.has(key)||undoing.current)return;
  const requestId=crypto.randomUUID(),order=++sequence.current;
  locks.current.add(key);setBusy(old=>({...old,[key]:true}));setError("");
  try{const value=await changePTI(row.weekStart,row.truckId,{requestId,version:row.version,mark,received:mark==="1",review:mark==="1"?"checked":"pending",notes:row.notes});
   undoStack.current=[...undoStack.current,{requestId,after:value,order}].sort((a,b)=>a.order-b.order).slice(-100);
   setData(old=>old?{...old,rows:[...old.rows.filter(r=>keyOf(r.weekStart,r.truckId)!==key),value]}:old);
  }catch(e){pendingUndo.current=false;setError(e instanceof Error?e.message:"Could not save PTI");refresh();}
  finally{locks.current.delete(key);setBusy(old=>{const next={...old};delete next[key];return next;});if(!locks.current.size&&pendingUndo.current){pendingUndo.current=false;void undo();}}
 };
 const undo=async()=>{
  if(!canEdit||undoing.current)return;if(locks.current.size){pendingUndo.current=true;return;}const last=undoStack.current.at(-1);if(!last)return;
  const row=last.after,key=keyOf(row.weekStart,row.truckId);undoing.current=true;locks.current.add(key);setBusy(old=>({...old,[key]:true}));setError("");
  try{const value=await changePTI(row.weekStart,row.truckId,{requestId:crypto.randomUUID(),undoRequestId:last.requestId,version:row.version,mark:row.mark,received:row.received,review:row.review,notes:row.notes});
   undoStack.current.pop();const prior=undoStack.current.findLast(item=>keyOf(item.after.weekStart,item.after.truckId)===key);if(prior)prior.after={...prior.after,version:value.version};
   setData(old=>old?{...old,rows:[...old.rows.filter(r=>keyOf(r.weekStart,r.truckId)!==key),value]}:old);cells.current.get(key)?.focus();
  }catch(e){setError(e instanceof Error?e.message:"Could not undo PTI");undoStack.current.pop();refresh();}
  finally{undoing.current=false;locks.current.delete(key);setBusy(old=>{const next={...old};delete next[key];return next;});}
 };
 const rows=new Map(data?.rows.map(r=>[keyOf(r.weekStart,r.truckId),r])),weeks=[...(data?.weeks??[])];
 if(data){if(!weeks.length)weeks.push(current);while(weeks.length<visibleWeeks)weeks.push(nextWeek(weeks.at(-1)!));}
 const allTrucks=[...(data?.trucks??[])].sort((a,b)=>(a.position??Number.MAX_SAFE_INTEGER)-(b.position??Number.MAX_SAFE_INTEGER)||a.unit.localeCompare(b.unit,undefined,{numeric:true})||a.unit.localeCompare(b.unit));
 const trucks=allTrucks.filter(t=>t.unit.toLowerCase().includes(search.trim().toLowerCase()));
 const rearrange=async(id:string,drop:{group:PTIGroup;target?:string;after:boolean},version:number)=>{
  if(!canEdit||layoutLock.current||!data)return;
  const source=allTrucks.find(t=>t.id===id);if(!source||drop.target===id)return;
  const list=allTrucks.filter(t=>t.id!==id);let index=drop.target?list.findIndex(t=>t.id===drop.target):-1;
  if(drop.target&&(index<0||list[index].group!==drop.group))return;
  if(index<0){index=list.findLastIndex(t=>t.group===drop.group)+1;if(index===0)index=list.length;}else if(drop.after)index++;
  list.splice(index,0,{...source,group:drop.group});
  const ordered=(["company","owner_operator"] as const).flatMap(group=>list.filter(t=>t.group===group));
  layoutLock.current=true;setLayoutBusy(true);setError("");
  try{const result=await savePTILayout(version,ordered.map(t=>({truckId:t.id,group:t.group})));setData(old=>old?{...old,layoutVersion:result.version,trucks:ordered.map((t,position)=>({...t,position}))}:old);}
  catch(e){setError(e instanceof Error?e.message:"Could not save PTI order");refresh();}
  finally{layoutLock.current=false;setLayoutBusy(false);}
 };
 const drag=usePTIRowDrag(trucks,rearrange);
 const groups=[{id:"company" as const,name:"Company & investor trucks",trucks:drag.trucks.filter(t=>t.group==="company")},{id:"owner_operator" as const,name:"Owner-operated trucks",trucks:drag.trucks.filter(t=>t.group==="owner_operator")}];
 const orderedTrucks=groups.flatMap(g=>g.trucks);
 const move=(truck:string,week:string,dx:number,dy:number)=>{const targetTruck=orderedTrucks[orderedTrucks.findIndex(t=>t.id===truck)+dy],targetWeek=weeks[weeks.indexOf(week)+dx];if(targetTruck&&targetWeek)cells.current.get(keyOf(targetWeek,targetTruck.id))?.focus();};
 return <div className="space-y-3">
  <ManagementHeader title="Weekly PTI" icon={ListChecks}/>{error&&<ErrorBanner message={error}/>}
  <div className="flex flex-wrap items-center gap-3">
   <label className="w-48 shrink-0"><input aria-label="Search PTI" placeholder="Search truck" value={search} onChange={e=>setSearch(e.target.value)} className={controlClass}/></label>
   <button className="ui-button ml-auto" onClick={refresh}>Refresh</button>
   {canEdit&&data&&!data.weeks.includes(current)&&<button className="ui-button" onClick={()=>void createPTIWeek(current).then(refresh).catch(e=>setError(e.message))}>Start this week</button>}
  </div>
  {!data?<p className="py-8 text-sm text-zinc-500">Loading weekly PTI…</p>:<div ref={scroller} className="w-full max-w-full overflow-auto rounded-lg border border-zinc-700">
   <table style={{width:112+weeks.length*64,minWidth:"100%",tableLayout:"fixed"}} className="border-separate border-spacing-0 text-xs" aria-label="Weekly PTI matrix">
    <colgroup><col style={{width:112}}/>{weeks.map(w=><col key={w} style={{width:64}}/>)}</colgroup>
    <thead className="sticky top-0 z-20"><tr>
     <th style={{backgroundColor:"#6d9eeb",color:"#000"}} className="sticky left-0 z-30 min-w-28 border-b border-r border-zinc-700 bg-[#6d9eeb] px-3 py-2 text-left text-black">Unit #</th>
     {weeks.map(w=><th key={w} style={{backgroundColor:"#6d9eeb",color:"#000"}} className="border-b border-r border-zinc-700 px-1 py-2 text-center font-normal" title={w}>{label(w)}<span className="block text-[10px]">{w.slice(0,4)}</span></th>)}
    </tr></thead>
    {groups.map(group=><tbody key={group.name} aria-label={group.name}>
     <tr data-pti-group={group.id}><th colSpan={weeks.length+1} className="border-b border-zinc-700 bg-zinc-800 px-3 py-2 text-left font-medium"><span className="sticky left-3">{group.name} <span className="ml-2 text-zinc-400">{group.trucks.length}</span></span></th></tr>
     {group.trucks.map(truck=>{
      return <tr key={truck.id} data-pti-truck={truck.id} data-pti-category={group.id} aria-label={"Truck "+truck.unit}>
       <th className="sticky left-0 z-10 border-b border-r border-zinc-700 bg-zinc-950 px-3 py-1.5 text-left font-mono font-normal"><div className="flex items-center gap-1">{canEdit&&<button type="button" aria-label={"Move truck "+truck.unit} title="Drag to arrange PTI groups, or use Arrow Up / Arrow Down" disabled={layoutBusy||!!Object.keys(busy).length} className="-ml-2 flex h-5 shrink-0 touch-none select-none cursor-grab items-center text-zinc-500 hover:text-zinc-200 active:cursor-grabbing disabled:opacity-30" onPointerDown={e=>drag.start(e,truck,data.layoutVersion)} onKeyDown={e=>{if(e.key!=="ArrowUp"&&e.key!=="ArrowDown")return;e.preventDefault();const target=orderedTrucks[orderedTrucks.findIndex(t=>t.id===truck.id)+(e.key==="ArrowUp"?-1:1)];if(target&&!drag.active)void rearrange(truck.id,{group:target.group,target:target.id,after:e.key==="ArrowDown"},data.layoutVersion);}}><GripVertical className="h-3 w-3"/></button>}<Link className="text-blue-400" href={"/trucks/detail?id="+truck.id}>{truck.unit}</Link></div></th>
       {weeks.map(w=>{const key=keyOf(w,truck.id),entry=rows.get(key)??blank(w,truck),value=markOf(entry),editable=canEdit&&data.weeks.includes(w);const title=[entry.updatedBy,stamp(entry.updatedAt)].filter(Boolean).join(" · ");
        return <td key={w} className="border-b border-r border-zinc-700 p-0" style={{backgroundColor:value?colors[value]:undefined}}>
         <input ref={el=>{if(el)cells.current.set(key,el);else cells.current.delete(key);}} aria-label={"Truck "+truck.unit+" PTI "+w} title={title||(!data.weeks.includes(w)?"Upcoming week":undefined)} value={value} placeholder="—" readOnly={!editable||!!busy[key]} aria-busy={!!busy[key]} autoComplete="off" spellCheck={false}
          className={"h-8 w-full min-w-0 cursor-default caret-transparent selection:bg-transparent rounded-none border-0 bg-transparent p-0 text-center font-semibold outline-offset-[-2px] focus:outline-2 focus:outline-blue-600 "+(value?"text-black":"text-zinc-500")}
          onFocus={e=>e.currentTarget.select()}
          onKeyDown={e=>{if((e.ctrlKey||e.metaKey)&&e.key.toLowerCase()==="z"){e.preventDefault();if(!e.shiftKey)void undo();return;}if(e.ctrlKey||e.metaKey||e.altKey)return;const directions:Record<string,[number,number]>={ArrowLeft:[-1,0],ArrowRight:[1,0],ArrowUp:[0,-1],ArrowDown:[0,1],Enter:[0,e.shiftKey?-1:1]};if(directions[e.key]){e.preventDefault();move(truck.id,w,...directions[e.key]);return;}if(e.key.length===1||e.key==="Backspace"||e.key==="Delete"){e.preventDefault();if(editable&&!busy[key]&&(e.key==="Backspace"||e.key==="Delete")&&value){void save(entry,"");return;}if(editable&&!busy[key]&&(e.key==="/"||e.key==="1"||e.key==="0")&&e.key!==value)void save(entry,e.key);}}}
          onPaste={e=>{e.preventDefault();const v=e.clipboardData.getData("text");if(editable&&!busy[key]&&(v==="/"||v==="1"||v==="0")&&v!==value)void save(entry,v);}}
          onChange={e=>{const v=e.target.value;if(editable&&!busy[key]&&(v===""||v==="/"||v==="1"||v==="0")&&v!==value)void save(entry,v);}}/>
        </td>;
       })}
      </tr>;
     })}
    </tbody>)}
   </table>{!trucks.length&&<p className="p-8 text-center text-zinc-500">No trucks match your search.</p>}
  </div>}
 </div>;
}
