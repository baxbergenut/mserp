"use client";

import { useEffect, useState } from "react";
import { CalendarRange, Pencil, Plus, Settings } from "lucide-react";
import Link from "@/app/components/AccessLink";
import { fetchMaintenanceAccess, fetchMaintenanceOptions, fetchProgramTrucks, fetchServicePrograms } from "@/app/lib/api";
import type { ExpenseCategoryAccess, MaintenanceOptions, ProgramTruck, ServiceProgram } from "@/app/lib/types";
import { ErrorBanner, ManagementHeader, ManagementSearch, TablePagination, TableShell } from "@/app/components/management/ManagementUI";
import { OdometerSourcePicker } from "@/app/maintenance/OdometerSourcePicker";
import { MaintenanceWorkspace } from "@/app/maintenance/MaintenanceWorkspace";
import { today } from "@/app/maintenance/maintenance";
import { AddProgramTrucks, ProgramEditor, ProgramServiceEditor, ProgramTruckEditor } from "./ProgramEditors";

export const programInterval = (p: Pick<ServiceProgram, "intervalMiles" | "intervalDays" | "intervalMonths">) => [p.intervalMiles != null && `${p.intervalMiles.toLocaleString()} miles`, p.intervalDays != null && `${p.intervalDays} days`, p.intervalMonths != null && `${p.intervalMonths} months`].filter(Boolean).join(" / ");
const number = (n: number | null) => n == null ? "—" : n.toLocaleString();
const defaultProgram="00000000-0000-4000-8000-000000000020";
const emptyProgram = (): ServiceProgram => ({ id: "", name: "", instructions: "", legacyPM: false, intervalMiles: null, intervalDays: null, intervalMonths: null, warningMiles: 1000, warningDays: 14, active: true, useDefaultAssignees: true, assigneeIds: [], assigneeRoleIds: [], version: 0, truckCount: 0, upcomingCount: 0, overdueCount: 0 });

export function ServiceProgramsWorkspace({ truckId }: { truckId?: string }) {
  const [access, setAccess] = useState<ExpenseCategoryAccess | null>(null), [options, setOptions] = useState<MaintenanceOptions | null>(null);
  const [programs, setPrograms] = useState<ServiceProgram[]>([]), [members, setMembers] = useState<ProgramTruck[]>([]);
  const [selected, setSelected] = useState(truckId ? "" : defaultProgram), [search, setSearch] = useState("");
  const [page, setPage] = useState(1), [size, setSize] = useState(25), [revision, setRevision] = useState(0);
  const [error, setError] = useState(""), [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState<ServiceProgram | null>(null), [adding, setAdding] = useState(false), [truckSettings, setTruckSettings] = useState<ProgramTruck | null>(null);
  const [service, setService] = useState<{ truck: ProgramTruck; baseline: boolean } | null>(null), [readingTruck, setReadingTruck] = useState<string | null>(null), [recordId, setRecordId] = useState<string | null>(null);
  const [clock,setClock]=useState(()=>Date.now());
  const refresh = async () => { setRevision(v => v + 1); };
  useEffect(() => { const restore=()=>{if(!truckId)setSelected(new URLSearchParams(window.location.search).get("program")??defaultProgram);};const timer=setTimeout(restore,0);window.addEventListener("popstate",restore);return()=>{clearTimeout(timer);window.removeEventListener("popstate",restore);}; }, [truckId]);
  useEffect(() => { let active = true; fetchMaintenanceAccess().then(v => { if (active) setAccess(v); }).catch(e => { if (active) { setError(e.message); setLoading(false); } }); return () => { active = false; }; }, []);
  useEffect(() => {
    if (!access?.canView) return;
    const abort = new AbortController();
    void Promise.all([fetchServicePrograms(abort.signal), fetchProgramTrucks(selected, truckId, abort.signal), fetchMaintenanceOptions()]).then(([p, t, o]) => {
      if (!abort.signal.aborted) { setPrograms(p); setMembers(t); setOptions(o); setError(""); setLoading(false); }
    }).catch(e => { if (!abort.signal.aborted) { setError(e.message); setLoading(false); } });
    return () => { abort.abort(); };
  }, [access, selected, truckId, revision]);
  useEffect(() => { const update = () => { if (!document.hidden) {setClock(Date.now());setRevision(v => v + 1);} }; const timer = setInterval(update, 30000); window.addEventListener("focus", update); return () => { clearInterval(timer); window.removeEventListener("focus", update); }; }, []);
  const program = programs.find(p => p.id === selected);
  const chooseProgram = (id: string) => { setSelected(id); setMembers([]); setPage(1); setLoading(true); setSearch(""); if (!truckId) { const url = new URL(window.location.href); if (id) url.searchParams.set("program", id); else url.searchParams.set("program", ""); window.history.replaceState(null, "", url); } };
  const rows = members.filter(t => (!selected || t.programId === selected) && t.enrolled && t.active && `${t.unitNumber} ${t.programName}`.toLowerCase().includes(search.toLowerCase()));
  const visible = rows.slice((page - 1) * size, page * size);
  const listed = programs.filter(p => p.name.toLowerCase().includes(search.toLowerCase()));
  const serviceProgram = service && programs.find(p => p.id === service.truck.programId);
  return <div className="space-y-4">
    {!truckId && <ManagementHeader title="Service programs" icon={CalendarRange} count={programs.length} />}
    {error && <ErrorBanner message={error} />}
    {access && !access.canView ? <p className="text-sm text-zinc-500">Your role cannot view service programs.</p> : <>
      <div className="flex flex-wrap items-center gap-3">
        <select aria-label="Service program" className="ui-button max-w-full" value={selected} onChange={e => chooseProgram(e.target.value)}><option value="">{truckId ? "All assigned programs" : "All programs"}</option>{programs.map(p => <option key={p.id} value={p.id}>{p.name}{p.active ? "" : " (paused)"}</option>)}</select>
        <ManagementSearch value={search} onChange={v => { setSearch(v); setPage(1); }} placeholder={selected || truckId ? "Search trucks or programs…" : "Search programs…"} />
        {program && access?.canEdit && <button type="button" className="ui-button ml-auto" onClick={() => setAdding(true)}><Plus className="h-4 w-4" />Add trucks</button>}
        {!truckId && options?.canManagePrograms && access?.canEdit && <button type="button" className={`ui-button ${program ? "" : "ml-auto"}`} onClick={() => setEditing(program ?? emptyProgram())}>{program ? <Settings className="h-4 w-4" /> : <Plus className="h-4 w-4" />}{program ? "Edit program" : "Add program"}</button>}
      </div>
      {program?.instructions && <p className="max-w-lg truncate text-xs text-zinc-400" title={program.instructions}>{program.instructions}</p>}
      {!selected && !truckId ? <TableShell><table className="w-full text-left text-[13px]"><thead><tr className="border-b border-zinc-800 text-xs text-zinc-500">{["Program", "Interval", "Trucks", "Due soon", "Due / overdue", ""].map((label, i) => <th key={i} className="px-3 py-2 font-medium">{label}</th>)}</tr></thead><tbody>
        {listed.map(p => <tr key={p.id} className="border-b border-zinc-800/60"><td className="px-3 py-2"><button type="button" className="text-blue-400" onClick={() => chooseProgram(p.id)}>{p.name}</button></td><td className="px-3 py-2">{programInterval(p)}</td><td className="px-3 py-2 tabular-nums">{p.truckCount}</td><td className="px-3 py-2 tabular-nums text-amber-400">{p.upcomingCount || "—"}</td><td className="px-3 py-2 tabular-nums text-red-400">{p.overdueCount || "—"}</td><td className="px-3 py-2">{options?.canManagePrograms && access?.canEdit && <button type="button" aria-label={`Edit ${p.name}`} className="ui-button" onClick={() => setEditing(p)}><Pencil className="h-4 w-4" /></button>}</td></tr>)}
        {!listed.length && <tr><td colSpan={6} className="p-8 text-center text-zinc-500">{loading ? "Loading programs…" : "No matching programs."}</td></tr>}
      </tbody></table></TableShell> : <>
        <TableShell><table className="w-full min-w-[760px] text-left text-[13px]"><thead><tr className="border-b border-zinc-800 text-xs text-zinc-500">
          {!truckId && <th className="px-3 py-2 font-medium">Truck</th>}{!selected && <th className="px-3 py-2 font-medium">Program</th>}
          {["Last service", "Service odometer", "Current odometer", "Miles since service"].map(label => <th key={label} className="px-3 py-2 font-medium">{label}</th>)}
          <th className="px-3 py-2 text-right font-medium">Actions</th>
        </tr></thead><tbody>
          {visible.map(t => { const fresh = (t.currentSource === "five_eld" || t.currentSource === "samsara") ? !!t.currentReportedAt && clock - Date.parse(t.currentReportedAt) <= 3600000 : !!t.currentDate && Date.parse(today())-Date.parse(t.currentDate)<=7*86400000; return <tr key={`${t.programId}:${t.truckId}`} data-service-status={t.status} className={`border-b border-zinc-800/60 ${t.status === "due" || t.status === "overdue" ? "bg-red-500/10" : t.status === "upcoming" ? "bg-amber-500/10" : ""}`}>
            {!truckId && <td className="px-3 py-2"><Link className="text-blue-400" href={`/trucks/detail?id=${t.truckId}`}>{t.unitNumber}</Link>{!t.active && <span className="block text-xs text-zinc-500">Inactive</span>}{!t.enrolled && <span className="block text-xs text-zinc-500">Removed</span>}</td>}
            {!selected && <td className="px-3 py-2"><Link className="text-blue-400" href={`/fleet/service-programs?program=${t.programId}`}>{t.programName}</Link></td>}
            <td className="px-3 py-2">{t.lastRecordId ? <button type="button" className="text-blue-400" onClick={() => setRecordId(t.lastRecordId)}>{t.lastDate || "Unknown date"}</button> : t.lastDate ?? "No recorded service"}</td>
            <td className="px-3 py-2 tabular-nums">{number(t.lastMiles)}</td>
            <td className={`px-3 py-2 tabular-nums ${fresh ? "" : "text-zinc-500"}`} title={`${t.currentSource || "Saved reading"} · ${t.currentReportedAt ? new Date(t.currentReportedAt).toLocaleString() : t.currentDate || "Unavailable"}`} data-odometer-fresh={fresh}><button type="button" className="hover:underline" aria-label={`Odometer sources for ${t.unitNumber}`} onClick={() => setReadingTruck(t.truckId)}>{number(t.currentMiles)}</button></td>
            <td className="px-3 py-2 tabular-nums" data-miles-since-service>{number(t.milesSinceService)}</td>
            <td className="px-3 py-2">{access?.canEdit && <div className="flex items-center justify-end gap-2">{t.enrolled && programs.find(p => p.id === t.programId)?.active && <button type="button" className="ui-button" onClick={() => setService({ truck: t, baseline: false })}>Record service</button>}<button type="button" className="ui-button" aria-label={`Program settings for ${t.unitNumber} ${t.programName}`} onClick={() => setTruckSettings(t)}><Settings className="h-4 w-4" /></button></div>}</td>
          </tr>; })}
          {!visible.length && <tr><td colSpan={5 + (!truckId ? 1 : 0) + (!selected ? 1 : 0)} className="p-8 text-center text-zinc-500">{loading ? "Loading trucks…" : "No matching assigned trucks."}</td></tr>}
        </tbody></table></TableShell>
        <TablePagination page={page} pageSize={size} totalItems={rows.length} totalPages={Math.max(1, Math.ceil(rows.length / size))} onPageChange={setPage} onPageSizeChange={v => { setSize(v); setPage(1); }} />
      </>}
    </>}
    {editing && options && <ProgramEditor initial={editing} options={options} onClose={() => setEditing(null)} onSaved={refresh} />}
    {adding && program && options && <AddProgramTrucks program={program} members={members} options={options} truckId={truckId} onClose={() => setAdding(false)} onSaved={refresh} />}
    {truckSettings && <ProgramTruckEditor truck={truckSettings} onClose={() => setTruckSettings(null)} onSaved={refresh} />}
    {service && serviceProgram && options && <ProgramServiceEditor truck={service.truck} program={serviceProgram} options={options} baseline={service.baseline} onClose={() => setService(null)} onSaved={refresh} />}
    {readingTruck && options && <OdometerSourcePicker truckId={readingTruck} canEdit={!!access?.canEdit} options={options} onClose={() => setReadingTruck(null)} onSaved={refresh} />}
    {recordId && <MaintenanceWorkspace key={recordId} modalOnly recordId={recordId} onRecordClose={() => setRecordId(null)} onRecordSaved={() => setRevision(v => v + 1)} />}
  </div>;
}
