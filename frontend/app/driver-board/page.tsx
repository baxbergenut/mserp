"use client";

import { Fragment, memo, useMemo, useState } from "react";
import { Banknote, CloudCheck, History, RefreshCw, Route, Truck } from "lucide-react";
import { PageHeader } from "@/app/components/PageHeader";
import { MetricCard } from "@/app/components/MetricCard";
import { SkeletonBar } from "@/app/components/WeeklyTableSkeleton";
import { controlClass } from "@/app/components/management/ManagementUI";
import { IntentLink as Link } from "@/app/components/IntentLink";
import { useViewState } from "@/app/lib/viewMemory";
import { usePermissions } from "@/app/lib/access";
import { formatPhone } from "@/app/lib/phone";
import type { DriverBoardEntry } from "@/app/lib/types";
import { addDays, decimalDisplay, indexBoardEntries, shortDate, totals } from "@/app/gross-board/board";
import { statuses, statusColor } from "./board";
import { useDriverBoard } from "./useDriverBoard";
import { BoardHistory } from "./BoardHistory";
import { BoardViews, type BoardView } from "./BoardViews";

const columns = [
  ["Current load", 100], ["Driver", 140], ["Driver type", 75], ["Truck", 50], ["Trailer", 80],
  ["Original gross", 100], ["Driver gross", 100], ["Phone", 132], ["Status", 110],
  ["Origin / destination", 150], ["ETA", 96], ["Notes", 180], ["Home time", 120], ["Driver home", 140], ["Dispatcher", 120],
] as const;
const columnWeight = columns.reduce((sum, column) => sum + column[1], 0);
const minimumWidth = columnWeight;
const driverColumnOffset = `max(100px, ${100 / columnWeight * 100}%)`;
const buttonClass = "inline-flex items-center gap-2 rounded-lg border border-zinc-700/70 px-3 py-2 text-xs text-zinc-300 hover:bg-zinc-800 disabled:opacity-40";
const cellClass = "overflow-hidden text-ellipsis whitespace-nowrap border-b border-r border-zinc-800/80";

const TextCell = memo(function TextCell({ entry, field, label, limit, disabled, edit }: {
  entry: DriverBoardEntry; field: "currentLoad" | "trailerNumber" | "destination" | "eta" | "notes" | "homeTime" | "driverHome";
  label: string; limit: number; disabled: boolean;
  edit: (id: string, field: keyof DriverBoardEntry, value: string) => void;
}) {
  return <input type="text" aria-label={label} title={entry[field]} maxLength={limit} disabled={disabled}
    value={entry[field]} onChange={event => edit(entry.driverId, field, event.target.value)}
    className="block h-8 w-full min-w-0 bg-transparent px-1.5 text-[11px] leading-4 text-zinc-200 outline-none placeholder:text-zinc-700 focus:bg-zinc-800 focus:ring-1 focus:ring-inset focus:ring-blue-500" />;
});

export default function DriverBoardPage() {
  const state = useDriverBoard();
  const { board, entries, loading, saving, dirty, error, refreshError, edit, save, reload, leaving } = state;
  const permissions = usePermissions();
  const canEdit = permissions.includes("driver_board.write");
  const [dispatcher, setDispatcher] = useViewState("page:dispatcher", "all");
  const [search, setSearch] = useViewState("page:search", "");
  const [status, setStatus] = useViewState("page:status", "all");
  const [view, setView] = useViewState("page:view", "all");
  const [myIds, setMyIds] = useViewState<string[]>("page:myDispatchers", []);
  const [savedViews, setSavedViews] = useViewState<BoardView[]>("page:savedViews", []);
  const [history, setHistory] = useState<{ driverId?: string; name: string; ids: string[] } | null>(null);
  const viewIds = view === "all" ? null : view === "my" ? myIds : savedViews.find(v => v.id === view)?.dispatcherIds ?? [];
  function selectView(id: string) { setView(id); setDispatcher("all"); }
  const drivers = (board?.drivers ?? []).filter(d =>
    (viewIds === null || viewIds.includes(d.dispatcherId)) &&
    (dispatcher === "all" || d.dispatcherId === dispatcher) && (status === "all" || entries[d.id]?.status === status) &&
    [d.fullName, d.truckUnit, d.phone, d.dispatcherName, entries[d.id]?.currentLoad, entries[d.id]?.trailerNumber].join(" ").toLowerCase().includes(search.trim().toLowerCase()));
  const dispatchers = Array.from(new Map((board?.drivers ?? []).map(d => [d.dispatcherId, d.dispatcherName])).entries());
  const index = useMemo(() => indexBoardEntries(board?.grossEntries ?? []), [board]);
  const ids = new Set(drivers.map(d => d.id));
  const summary = totals((board?.grossEntries ?? []).filter(e => ids.has(e.driverId)));
  const groups = new Map<string, ReturnType<typeof totals>>();
  for (const d of drivers) {
    const sum = totals(index.byDriver.get(d.id) ?? []);
    const group = groups.get(d.dispatcherId) ?? { original: BigInt(0), driver: BigInt(0), miles: BigInt(0) };
    group.original += sum.original; group.driver += sum.driver; group.miles += sum.miles;
    groups.set(d.dispatcherId, group);
  }
  const disabled = !canEdit || loading || leaving;
  function textCell(entry: DriverBoardEntry, name: string, field: Parameters<typeof TextCell>[0]["field"], label: string, limit: number) {
    return <TextCell entry={entry} field={field} label={`${name} · ${label}`} limit={limit} disabled={disabled} edit={edit} />;
  }

  return <div className="space-y-5 animate-fade-in">
    <div className="flex flex-wrap items-center justify-end gap-3">
      <PageHeader><div><div className="flex items-center gap-3"><Truck className="h-5 w-5 text-zinc-500" /><h1 className="text-lg font-semibold text-zinc-100">Driver Board</h1><span className="rounded-full bg-zinc-800/60 px-2.5 py-0.5 text-xs text-zinc-400">{loading ? <SkeletonBar className="h-3 w-4" /> : drivers.length}</span></div><p className="mt-1.5 text-[13px] text-zinc-500">Live dispatch overview with this week’s Gross Board totals.</p></div></PageHeader>
      <span role="status" className={`flex items-center gap-1.5 text-xs ${error && dirty ? "text-red-300" : "text-zinc-400"}`}><CloudCheck className="h-4 w-4" />{loading ? "Loading…" : error && dirty ? "Not saved" : saving ? "Saving…" : dirty ? "Waiting to save…" : canEdit ? "All changes saved" : "Read only"}</span>
      <button className={buttonClass} onClick={reload} disabled={loading || saving}><RefreshCw className="h-4 w-4" />Reload</button>
      <button className={buttonClass} disabled={loading} onClick={() => setHistory({ name: "History · shown drivers", ids: drivers.map(d => d.id) })}><History className="h-4 w-4" />History</button>
    </div>

    <div className="grid gap-3 sm:grid-cols-3">
      <MetricCard loading={loading} compact label="Weekly original gross" value={board ? decimalDisplay(summary.original, true) : "—"} icon={Banknote} />
      <MetricCard loading={loading} compact label="Weekly driver gross" value={board ? decimalDisplay(summary.driver, true) : "—"} icon={Banknote} />
      <MetricCard loading={loading} compact label="Weekly miles" value={board ? decimalDisplay(summary.miles) : "—"} icon={Route} />
    </div>

    <BoardViews mode={view} myIds={myIds} saved={savedViews} dispatchers={dispatchers} onSelect={selectView} onSave={(dispatcherIds, name) => {
      if (!name) { setMyIds(dispatcherIds); selectView("my"); return; }
      const existing = savedViews.find(v => v.name.toLowerCase() === name.toLowerCase());
      const next = { id: existing?.id ?? crypto.randomUUID(), name, dispatcherIds };
      setSavedViews(current => [...current.filter(v => v.id !== next.id), next]); selectView(next.id);
    }} onDelete={id => { setSavedViews(current => current.filter(v => v.id !== id)); selectView("all"); }} />
    <div className="flex flex-wrap items-center gap-3">
      <input aria-label="Search Driver Board" placeholder="Search driver, truck, trailer or load…" value={search} onChange={event => setSearch(event.target.value)} className={`${controlClass} max-w-80`} />
      <select aria-label="Dispatcher filter" value={dispatcher} onChange={event => setDispatcher(event.target.value)} className={`${controlClass} max-w-48`}><option value="all">All dispatchers</option>{dispatchers.map(([id, name]) => <option key={id} value={id}>{name}</option>)}</select>
      <select aria-label="Status filter" value={status} onChange={event => setStatus(event.target.value)} className={`${controlClass} max-w-44`}><option value="all">All statuses</option><option value="">No status</option>{statuses.map(s => <option key={s}>{s}</option>)}</select>
      {board && <span className="ml-auto text-xs text-zinc-500">Week {shortDate(board.weekStart)}–{shortDate(addDays(board.weekStart, 6))} · New York · totals for shown drivers</span>}
    </div>

    {error && <div role="alert" className="flex flex-wrap items-center gap-3 rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-300"><span>{error}{dirty && " Your unsaved edits are retained."}</span>{dirty && <button className={buttonClass} disabled={saving} onClick={() => void save()}>Retry save</button>}</div>}
    {refreshError && <p role="status" className="text-xs text-amber-300">{refreshError}</p>}
    <div data-scroll-key="driver-board" className="max-h-[72vh] overflow-auto rounded-xl border border-zinc-800" aria-busy={loading}>
      <table className="w-full table-fixed border-separate border-spacing-0 text-[11px]" style={{ minWidth: minimumWidth }}>
        <colgroup>{columns.map(([label, width]) => <col key={label} style={{ width: `${width / columnWeight * 100}%` }} />)}</colgroup>
        <thead className="sticky top-0 z-20"><tr>{columns.map(([label], i) => <th key={label} scope="col" style={i < 2 ? { left: i === 0 ? 0 : driverColumnOffset } : undefined} className={`${i < 2 ? "sm:sticky sm:z-[21]" : ""} h-8 whitespace-nowrap border-b border-r border-zinc-700 bg-zinc-900 px-1.5 text-left font-medium leading-4 text-zinc-400`}>{label}</th>)}</tr></thead>
        <tbody>
          {loading ? Array.from({ length: 12 }, (_, i) => <tr key={i}>{columns.map(([label]) => <td key={label} className={`${cellClass} h-8 px-2`}><SkeletonBar className="h-3 w-3/4" /></td>)}</tr>) : drivers.map((d, i) => {
            const entry = entries[d.id];
            const sum = totals(index.byDriver.get(d.id) ?? []);
            const group = groups.get(d.dispatcherId)!;
            return <Fragment key={d.id}>
              {(i === 0 || drivers[i - 1].dispatcherId !== d.dispatcherId) && <tr className="h-8 bg-blue-500/10 text-blue-200" aria-label={`${d.dispatcherName} totals`}><th scope="rowgroup" colSpan={5} className="border-b border-zinc-700 px-3 text-left font-medium">{d.dispatcherName}</th><td className="border-b border-zinc-700 px-2 text-right font-mono">{decimalDisplay(group.original, true)}</td><td className="border-b border-zinc-700 px-2 text-right font-mono">{decimalDisplay(group.driver, true)}</td><td colSpan={8} className="border-b border-zinc-700 px-3 text-right text-[11px] text-zinc-400">{decimalDisplay(group.miles)} mi</td></tr>}
              <tr className="group bg-zinc-950/20 hover:bg-zinc-800/20" data-driver-id={d.id}>
                <td className={`${cellClass} sm:sticky left-0 z-10 bg-zinc-950`}>{textCell(entry, d.fullName, "currentLoad", "Current load", 300)}</td>
                <th scope="row" title={d.fullName} style={{ left: driverColumnOffset }} className={`${cellClass} sm:sticky z-10 bg-zinc-950 px-1.5 text-left font-medium text-zinc-200`}><div className="flex items-center gap-1"><span className="min-w-0 flex-1 truncate">{permissions.includes("fleet.read") ? <Link href={`/drivers/detail?id=${d.id}`} className="hover:text-blue-300">{d.fullName}</Link> : d.fullName}</span><button aria-label={`${d.fullName} · History`} title="Driver history" className="shrink-0 rounded p-1 text-zinc-500 hover:bg-zinc-800 hover:text-zinc-200" onClick={() => setHistory({ driverId: d.id, name: `${d.fullName} · History`, ids: [d.id] })}><History className="h-3 w-3" /></button></div></th>
                <td className={`${cellClass} px-0.5 text-center`}><span className="rounded bg-zinc-800 px-0.5 py-0.5 font-mono text-[10px] text-zinc-300">{d.driverType}</span></td>
                <td title={d.truckUnit} className={`${cellClass} px-1 font-mono text-zinc-300`}>{d.truckUnit || "—"}</td>
                <td className={cellClass}>{textCell(entry, d.fullName, "trailerNumber", "Trailer", 100)}</td>
                <td className={`${cellClass} bg-blue-500/5 px-1 text-right font-mono text-zinc-200`}>{decimalDisplay(sum.original, true)}</td>
                <td className={`${cellClass} bg-blue-500/5 px-1 text-right font-mono text-zinc-200`}>{decimalDisplay(sum.driver, true)}</td>
                <td className={`${cellClass} px-1 leading-4 text-zinc-400`}>{formatPhone(d.phone) || "—"}</td>
                <td className={`${cellClass} ${statusColor(entry.status)}`}><select aria-label={`${d.fullName} · Status`} title={entry.status} value={entry.status} disabled={disabled} onChange={event => edit(d.id, "status", event.target.value)} className="h-8 w-full bg-transparent px-0.5 text-[10px] font-medium outline-none focus:ring-1 focus:ring-inset focus:ring-blue-500"><option value="" className="bg-zinc-900 text-zinc-300">—</option>{statuses.map(s => <option key={s} className={statusColor(s)}>{s}</option>)}</select></td>
                <td className={cellClass}>{textCell(entry, d.fullName, "destination", "Origin / destination", 500)}</td>
                <td className={cellClass}>{textCell(entry, d.fullName, "eta", "ETA", 500)}</td>
                <td className={cellClass}>{textCell(entry, d.fullName, "notes", "Notes", 5000)}</td>
                <td className={cellClass}>{textCell(entry, d.fullName, "homeTime", "Home time", 500)}</td>
                <td className={cellClass}>{textCell(entry, d.fullName, "driverHome", "Driver home", 300)}</td>
                <td title={d.dispatcherName} className={`${cellClass} px-1.5 text-zinc-400`}>{d.dispatcherName}</td>
              </tr>
            </Fragment>;
          })}
          {!loading && drivers.length === 0 && <tr><td colSpan={15} className="p-10 text-center text-zinc-500">{error && !board ? "Driver Board could not be loaded. Use Reload to retry." : "No active drivers match this view."}</td></tr>}
        </tbody>
      </table>
    </div>
    <p className="text-[11px] text-zinc-500">{canEdit ? "Edits save after 5 seconds. Driver home also updates the driver profile. " : ""}Dispatch details carry forward; weekly gross and miles come from Gross Board. Truck, phone and dispatcher follow the driver profile.</p>
    {history && <BoardHistory driverIds={history.ids} title={history.name} canUndo={canEdit} waiting={dirty || saving || loading} onUndo={state.undo} onClose={() => setHistory(null)} />}
  </div>;
}
