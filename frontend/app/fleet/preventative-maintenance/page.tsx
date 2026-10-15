"use client";

import { useCallback, useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { MaintenanceWorkspace } from "../../maintenance/MaintenanceWorkspace";
import Link from "next/link";
import { CalendarRange, Settings } from "lucide-react";
import { fetchMaintenanceAccess, fetchMaintenanceOdometer, fetchMaintenanceOptions, fetchPreventativeMaintenance } from "../../lib/api";
import type { MaintenanceOptions, MaintenanceSchedule, PreventativeTruck } from "../../lib/types";
import { useViewState } from "../../lib/viewMemory";
import { ErrorBanner, ManagementHeader, ManagementSearch, Modal, TablePagination, TableShell } from "../../components/management/ManagementUI";
import { MaintenanceScheduleEditor } from "../../maintenance/MaintenanceSettings";
import { centsLabel, expenseCents } from "../../lib/expenseCosts";

type Telemetry = { mileage: number | null; reportedAt?: string; title: string };
const serviceName = (code: "pm" | "oil") => code === "pm" ? "PM Service" : "Oil change";
const costLabel = (value: string | null) => { const cost = value == null ? null : expenseCents(value); return cost == null ? "—" : centsLabel(cost); };
const miles = (value: number | null) => value == null ? "—" : value.toLocaleString();
const intervalLabel = (schedule?: MaintenanceSchedule) => !schedule ? "Not configured" : !schedule.enabled ? "Disabled" : [schedule.intervalMiles && `${schedule.intervalMiles.toLocaleString()} miles`, schedule.intervalDays && `${schedule.intervalDays} days`].filter(Boolean).join(" / ");

function ServiceNote({text}:{text:string}) {
  const id=useId();
  const timer=useRef<ReturnType<typeof setTimeout>|null>(null);
  const [position,setPosition]=useState<{left:number;top:number;above:boolean}|null>(null);
  useEffect(()=>()=>{if(timer.current)clearTimeout(timer.current);},[]);
  const keep=()=>{if(timer.current)clearTimeout(timer.current);};
  const hide=()=>{keep();timer.current=setTimeout(()=>setPosition(null),150);};
  const show=(element:HTMLElement)=>{keep();const r=element.getBoundingClientRect();setPosition({left:Math.max(8,Math.min(r.left,window.innerWidth-304)),top:r.top>280?r.top-4:r.bottom+4,above:r.top>280});};
  if(!text)return <>—</>;
  return <><span tabIndex={0} aria-describedby={position?id:undefined} className="block max-w-80 truncate" onMouseEnter={e=>show(e.currentTarget)} onMouseLeave={hide} onFocus={e=>show(e.currentTarget)} onBlur={hide} onKeyDown={e=>{if(e.key==="Escape")setPosition(null);}}>{text}</span>{position&&createPortal(<div id={id} role="tooltip" tabIndex={0} onMouseEnter={keep} onMouseLeave={hide} onFocus={keep} onBlur={hide} onKeyDown={e=>{if(e.key==="Escape")setPosition(null);}} className="fixed z-[100] max-h-64 w-72 overflow-y-auto whitespace-pre-wrap break-words rounded-lg border border-zinc-700 bg-zinc-900 p-3 text-xs text-zinc-200 shadow-xl" style={{left:position.left,top:position.top,transform:position.above?"translateY(-100%)":undefined}}>{text}</div>,document.body)}</>;
}

export default function PreventativeMaintenancePage() {
  const [recordId,setRecordId]=useState<string | null>(null);
  const [revision,setRevision]=useState(0);
  const [clock,setClock]=useState(()=>Date.now());
  useEffect(()=>{const refresh=()=>{setClock(Date.now());setRevision(v=>v+1);};const timer=setInterval(refresh,60000);window.addEventListener("focus",refresh);return()=>{clearInterval(timer);window.removeEventListener("focus",refresh);};},[]);
  const [service, setService] = useViewState<"pm" | "oil">("preventative:service", "pm");
  const [options, setOptions] = useState<MaintenanceOptions | null>(null);
  const [items, setItems] = useState<PreventativeTruck[]>([]);
  const [telemetry, setTelemetry] = useState<Record<string, Telemetry>>({});
  const [search, setSearch] = useState("");
  const [inactive, setInactive] = useState(false);
  const [page, setPage] = useState(1); const [pageSize, setPageSize] = useState(25);
  const [total, setTotal] = useState(0); const [totalPages, setTotalPages] = useState(1);
  const [error, setError] = useState(""); const [loading, setLoading] = useState(true);
  const [settings, setSettings] = useState(false); const [editing, setEditing] = useState<MaintenanceSchedule | null>(null);
  const [allowed, setAllowed] = useState<boolean | null>(null);
  useEffect(() => { let active = true; fetchMaintenanceAccess().then(access => { if(active) setAllowed(access.canView); }).catch(e => { if(active) { setAllowed(false); setError(e.message); } }); return () => { active = false; }; }, []);
  const loadOptions = useCallback(async () => { setOptions(await fetchMaintenanceOptions()); }, []);
  useEffect(() => { if(!allowed) return; let active = true; fetchMaintenanceOptions().then(value => { if(active) setOptions(value); }).catch(e => { if(active) setError(e.message); }); return () => { active = false; }; }, [allowed]);
  useEffect(() => {
    if(!allowed) return;
    const abort = new AbortController(); const timer = setTimeout(() => {
      setLoading(true); setError("");
      void fetchPreventativeMaintenance({serviceCode:service,search,includeInactive:inactive,page,pageSize},abort.signal).then(result => {
        if(abort.signal.aborted) return;
        setItems(result.items); setTotal(result.total); setTotalPages(result.totalPages); setPage(result.page);
      }).catch(e => { if(!abort.signal.aborted) { setError(e.message); setItems([]); } }).finally(() => { if(!abort.signal.aborted) setLoading(false); });
    },200);
    return () => { clearTimeout(timer); abort.abort(); };
  }, [allowed, service, search, inactive, page, pageSize, revision]);
  useEffect(() => {
    const abort = new AbortController(); let next = 0;
    // The vendor exposes mileage per truck; bound requests to visible rows and
    // cancel them on navigation. GETs never add dated service readings.
    async function worker() {
      while(next < items.length && !abort.signal.aborted) {
        const truck = items[next++];
        try {
          const result = await fetchMaintenanceOdometer(truck.truckId,abort.signal);
          if(abort.signal.aborted) return;
          const reading = result.reading;
          setTelemetry(values => ({...values,[truck.truckId]: {mileage:reading?.mileage ?? null,reportedAt:reading?.reportedAt,title:reading ? `Five ELD · ${new Date(reading.reportedAt).toLocaleString()}` : "Live odometer unavailable"}}));
        } catch { if(!abort.signal.aborted) setTelemetry(values => ({...values,[truck.truckId]: {mileage:null,title:"Live odometer unavailable"}})); }
      }
    }
    for(let i=0;i<Math.min(3,items.length);i++) void worker();
    return () => abort.abort();
  },[items]);
  if(allowed === false) return <div className="space-y-4"><ManagementHeader title="Preventative maintenance" icon={CalendarRange} />{error && <ErrorBanner message={error} />}<p className="text-sm text-zinc-500">Your role cannot view maintenance records.</p></div>;
  const defaultSchedule = options?.schedules.find(s => s.truckId === null && s.serviceCode === service);
  return <div className="space-y-5">
    <ManagementHeader title="Preventative maintenance" icon={CalendarRange} count={total} />
    {error && <ErrorBanner message={error} />}
    <div className="flex flex-wrap items-center gap-3"><ManagementSearch value={search} onChange={value => { setSearch(value); setPage(1); }} placeholder="Search trucks…" /><label className="flex items-center gap-2 text-xs text-zinc-400"><input type="checkbox" checked={inactive} onChange={e => { setInactive(e.target.checked); setPage(1); }} />Show inactive trucks</label><button type="button" className="ui-button ml-auto" onClick={() => setSettings(true)}><Settings className="h-4 w-4" />Settings</button></div>
    <nav aria-label="Preventative maintenance services" className="flex gap-1 border-b border-zinc-800">{(["pm","oil"] as const).map(code => <button key={code} type="button" role="tab" aria-selected={service===code} className={`border-b-2 px-3 py-2 text-xs ${service===code ? "border-blue-400 text-blue-400" : "border-transparent text-zinc-500"}`} onClick={() => { setService(code); setPage(1); }}>{serviceName(code)}</button>)}</nav>
    <p className="text-xs text-zinc-500">Fleet interval: {intervalLabel(defaultSchedule)}{defaultSchedule?.enabled && " · Truck-specific intervals take precedence."}</p>
    <TableShell><table className="w-full min-w-[1050px] text-left text-[13px]"><thead><tr className="border-b border-zinc-800 text-xs text-zinc-500">{["Truck",service === "pm" ? "Last PM date" : "Last oil change", "Vendor", "Service odometer", "Current odometer", "Service cost", ...(service === "oil" ? ["Oil type"] : []), "Last service note"].map(title => <th key={title} className="p-3 font-medium">{title}</th>)}</tr></thead><tbody>
      {!loading && items.map(truck => {
        const live = telemetry[truck.truckId];
        const fresh = live?.mileage != null && !!live.reportedAt && clock - Date.parse(live.reportedAt) <= 3600000;
        const current = live?.mileage ?? truck.currentMiles;
        const readingTitle = live?.mileage != null ? live.title : `${live?.title ?? "Reading Five ELD…"}${truck.currentDate ? ` · Last saved reading: ${truck.currentDate}` : " · No saved reading"}`;
        return <tr key={truck.truckId} className="border-b border-zinc-800/60"><td className="p-3"><Link className="text-blue-400" href={`/trucks/detail?id=${truck.truckId}`}>{truck.unitNumber}</Link>{!truck.active && <span className="block text-xs text-zinc-500">Inactive</span>}</td><td className="p-3">{truck.recordId ? <button type="button" className="text-blue-400" onClick={()=>setRecordId(truck.recordId)}>{truck.lastDate ?? "Unknown date"}</button> : "No recorded service"}</td><td className="p-3">{truck.vendorName || "—"}</td><td className="p-3 tabular-nums">{miles(truck.lastMiles)}</td><td className={`p-3 tabular-nums ${fresh ? "text-zinc-200" : "text-zinc-500"}`} title={readingTitle} data-odometer-fresh={fresh}>{miles(current)}</td><td className="p-3 font-mono" title={truck.cost == null ? "Historical cost unknown or invoice not allocated to this service" : "Allocated service cost, excluding payment fees"}>{costLabel(truck.cost)}</td>{service === "oil" && <td className="p-3">{truck.oilType || "—"}</td>}<td className="max-w-80 p-3 text-xs text-zinc-400"><ServiceNote text={truck.notes} /></td></tr>;
      })}
      {(loading || !items.length) && <tr><td colSpan={service==="oil" ? 8 : 7} className="p-8 text-center text-zinc-500">{loading ? "Loading trucks…" : "No matching trucks."}</td></tr>}
    </tbody></table></TableShell>
    <TablePagination page={page} pageSize={pageSize} totalItems={total} totalPages={totalPages} onPageChange={setPage} onPageSizeChange={size => { setPageSize(size); setPage(1); }} />
    {recordId && <MaintenanceWorkspace key={recordId} modalOnly recordId={recordId} onRecordClose={()=>setRecordId(null)} onRecordSaved={()=>setRevision(v=>v+1)} />}
    {settings && !editing && <Modal title="Preventative maintenance settings" isSaving={false} submitLabel="Done" hideSubmit cancelLabel="Done" onClose={() => setSettings(false)} onSubmit={e => e.preventDefault()}><div className="space-y-4"><p className="text-xs text-zinc-500">Fleet defaults apply to trucks without a service-specific override. Miles and days are independent limits; the first reached is due.</p>{(["pm","oil"] as const).map(code => { const saved = options?.schedules.find(s => s.truckId===null && s.serviceCode===code); return <div key={code} className="flex items-center gap-3 border-b border-zinc-800 py-3 text-sm"><span className="flex-1">{serviceName(code)}<span className="block text-xs text-zinc-500">{intervalLabel(saved)}</span></span>{options?.administrator && <button type="button" className="ui-button" onClick={() => setEditing(saved ?? {id:"",truckId:null,serviceCode:code,intervalMiles:null,intervalDays:null,warningMiles:1000,warningDays:14,enabled:true,version:0})}>Set {serviceName(code)} interval</button>}</div>; })}{options && !options.administrator && <p className="text-xs text-zinc-500">An administrator can change fleet defaults.</p>}</div></Modal>}
    {editing && options && <MaintenanceScheduleEditor fleetOnly initial={editing} options={options} onClose={() => setEditing(null)} onSaved={loadOptions} />}
  </div>;
}
