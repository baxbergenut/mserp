"use client";

import { useCallback, useEffect, useState } from "react";
import { MaintenanceWorkspace } from "../../maintenance/MaintenanceWorkspace";
import Link from "next/link";
import { CalendarRange, Settings } from "lucide-react";
import { fetchMaintenanceAccess, fetchMaintenanceOptions, fetchPreventativeMaintenance } from "../../lib/api";
import type { MaintenanceOptions, MaintenanceSchedule, PreventativeTruck } from "../../lib/types";
import { ErrorBanner, ManagementHeader, ManagementSearch, Modal, TablePagination, TableShell } from "../../components/management/ManagementUI";
import { MaintenanceScheduleEditor } from "../../maintenance/MaintenanceSettings";

const miles = (value: number | null) => value == null ? "—" : value.toLocaleString();
const intervalLabel = (schedule?: MaintenanceSchedule) => !schedule ? "Not configured" : !schedule.enabled ? "Disabled" : [schedule.intervalMiles && `${schedule.intervalMiles.toLocaleString()} miles`, schedule.intervalDays && `${schedule.intervalDays} days`].filter(Boolean).join(" / ");

export default function PreventativeMaintenancePage() {
  const [recordId,setRecordId]=useState<string | null>(null);
  const [revision,setRevision]=useState(0);
  const [clock,setClock]=useState(()=>Date.now());
  useEffect(()=>{const refresh=()=>{if(document.hidden)return;setClock(Date.now());setRevision(v=>v+1);};const timer=setInterval(refresh,60000);window.addEventListener("focus",refresh);return()=>{clearInterval(timer);window.removeEventListener("focus",refresh);};},[]);
  const [options, setOptions] = useState<MaintenanceOptions | null>(null);
  const [items, setItems] = useState<PreventativeTruck[]>([]);
  const [search, setSearch] = useState("");
  const [inactive, setInactive] = useState(false);
  const [page, setPage] = useState(1); const [pageSize, setPageSize] = useState(25);
  const queryStamp=JSON.stringify([search,inactive,page,pageSize]);
  const [resultStamp,setResultStamp]=useState("");
  const [total, setTotal] = useState(0); const [totalPages, setTotalPages] = useState(1);
  const [error, setError] = useState(""); const [loading, setLoading] = useState(true);
  const [settings, setSettings] = useState(false); const [editing, setEditing] = useState<MaintenanceSchedule | null>(null);
  const [allowed, setAllowed] = useState<boolean | null>(null);
  useEffect(() => { let active = true; fetchMaintenanceAccess().then(access => { if(active) setAllowed(access.canView); }).catch(e => { if(active) { setAllowed(false); setError(e.message); } }); return () => { active = false; }; }, []);
  const loadOptions = useCallback(async () => { setOptions(await fetchMaintenanceOptions()); setRevision(v=>v+1); }, []);
  useEffect(() => { if(!allowed) return; let active = true; fetchMaintenanceOptions().then(value => { if(active) setOptions(value); }).catch(e => { if(active) setError(e.message); }); return () => { active = false; }; }, [allowed]);
  useEffect(() => {
    if(!allowed) return;
    const abort = new AbortController(); const timer = setTimeout(() => {
      setError("");
      void fetchPreventativeMaintenance({search,includeInactive:inactive,page,pageSize},abort.signal).then(result => {
        if(abort.signal.aborted) return;
        setItems(result.items); setResultStamp(queryStamp); setTotal(result.total); setTotalPages(result.totalPages); setPage(result.page);
      }).catch(e => { if(!abort.signal.aborted) { setError(e.message); setItems([]); setResultStamp(queryStamp); } }).finally(() => { if(!abort.signal.aborted) setLoading(false); });
    },200);
    return () => { clearTimeout(timer); abort.abort(); };
  }, [allowed, search, inactive, page, pageSize, revision, queryStamp]);
  if(allowed === false) return <div className="space-y-4"><ManagementHeader title="Preventative maintenance" icon={CalendarRange} />{error && <ErrorBanner message={error} />}<p className="text-sm text-zinc-500">Your role cannot view maintenance records.</p></div>;
  const defaultSchedule = options?.schedules.find(s => s.truckId === null && s.serviceCode === "pm");
  return <div className="space-y-5">
    <ManagementHeader title="Preventative maintenance" icon={CalendarRange} count={total} />
    {error && <ErrorBanner message={error} />}
    <div className="flex flex-wrap items-center gap-3"><ManagementSearch value={search} onChange={value => { setSearch(value); setPage(1); }} placeholder="Search trucks…" /><label className="flex items-center gap-2 text-xs text-zinc-400"><input type="checkbox" checked={inactive} onChange={e => { setInactive(e.target.checked); setPage(1); }} />Show inactive trucks</label><button type="button" className="ui-button ml-auto" onClick={() => setSettings(true)}><Settings className="h-4 w-4" />Settings</button></div>
    <p className="text-xs text-zinc-500">Fleet interval: {intervalLabel(defaultSchedule)}{defaultSchedule?.enabled && " · Truck-specific intervals take precedence."}</p>
    <TableShell><table className="w-full min-w-[900px] text-left text-[13px]"><thead><tr className="border-b border-zinc-800 text-xs text-zinc-500">{["Truck", "Last PM date", "Service odometer", "Current odometer", "Next due odometer / date", "Miles remaining", "Status", "Oil type"].map(title => <th key={title} className="p-3 font-medium">{title}</th>)}</tr></thead><tbody>
      {resultStamp===queryStamp && items.map(truck => {
        const fresh = truck.currentSource === "five_eld" && !!truck.currentReportedAt && clock-Date.parse(truck.currentReportedAt)<=3600000;
        const current=truck.currentMiles;
        const readingTitle=truck.currentReportedAt ? `Five ELD · ${new Date(truck.currentReportedAt).toLocaleString()}` : `Last saved reading · ${truck.currentDate ?? "Unavailable"}`;
        const highlight=truck.status === "overdue" || truck.status === "due" ? "bg-red-500/10" : truck.status === "upcoming" ? "bg-amber-500/10" : "";
        return <tr key={truck.truckId} data-service-status={truck.status} className={`border-b border-zinc-800/60 ${highlight}`}><td className="p-3"><Link className="text-blue-400" href={`/trucks/detail?id=${truck.truckId}`}>{truck.unitNumber}</Link>{!truck.active && <span className="block text-xs text-zinc-500">Inactive</span>}</td><td className="p-3">{truck.recordId ? <button type="button" className="text-blue-400" onClick={()=>setRecordId(truck.recordId)}>{truck.lastDate ?? "Unknown date"}</button> : "No recorded service"}</td><td className="p-3 tabular-nums">{miles(truck.lastMiles)}</td><td className={`p-3 tabular-nums ${fresh ? "text-zinc-200" : "text-zinc-500"}`} title={readingTitle} data-odometer-fresh={fresh}>{miles(current)}</td><td className="p-3 tabular-nums">{miles(truck.nextMiles)}{truck.nextDate && <span className="block text-xs text-zinc-500">{truck.nextDate}</span>}</td><td className="p-3 tabular-nums">{truck.remainingMiles == null ? "—" : truck.remainingMiles < 0 ? `${Math.abs(truck.remainingMiles).toLocaleString()} overdue` : truck.remainingMiles.toLocaleString()}</td><td className={`p-3 text-xs ${truck.status === "overdue" || truck.status === "due" ? "text-red-400" : truck.status === "upcoming" ? "text-amber-400" : "text-zinc-500"}`} title={truck.reason}>{({current:"Current",upcoming:"Due soon",due:"Due now",overdue:"Overdue",disabled:"Disabled",missing_information:"Needs information"} as Record<string,string>)[truck.status] ?? "Needs information"}</td><td className="p-3">{truck.oilType || "—"}</td></tr>;
      })}
      {(resultStamp!==queryStamp || !items.length) && <tr><td colSpan={8} className="p-8 text-center text-zinc-500">{loading || resultStamp!==queryStamp ? "Loading trucks…" : "No matching trucks."}</td></tr>}
    </tbody></table></TableShell>
    <TablePagination page={page} pageSize={pageSize} totalItems={total} totalPages={totalPages} onPageChange={setPage} onPageSizeChange={size => { setPageSize(size); setPage(1); }} />
    {recordId && <MaintenanceWorkspace key={recordId} modalOnly recordId={recordId} onRecordClose={()=>setRecordId(null)} onRecordSaved={()=>setRevision(v=>v+1)} />}
    {settings && !editing && <Modal title="Preventative maintenance settings" isSaving={false} submitLabel="Done" hideSubmit cancelLabel="Done" onClose={() => setSettings(false)} onSubmit={e => e.preventDefault()}><div className="space-y-4"><p className="text-xs text-zinc-500">Fleet defaults apply to trucks without a service-specific override. Miles and days are independent limits; the first reached is due.</p><div className="flex items-center gap-3 border-b border-zinc-800 py-3 text-sm"><span className="flex-1">PM Service<span className="block text-xs text-zinc-500">{intervalLabel(defaultSchedule)}</span></span>{options?.administrator && <button type="button" className="ui-button" onClick={() => setEditing(defaultSchedule ?? {id:"",truckId:null,serviceCode:"pm",intervalMiles:null,intervalDays:null,warningMiles:1000,warningDays:14,enabled:true,version:0})}>Set PM Service interval</button>}</div>{options && !options.administrator && <p className="text-xs text-zinc-500">An administrator can change fleet defaults.</p>}</div></Modal>}
    {editing && options && <MaintenanceScheduleEditor fleetOnly initial={editing} options={options} onClose={() => setEditing(null)} onSaved={loadOptions} />}
  </div>;
}
