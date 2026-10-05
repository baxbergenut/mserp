"use client";

import { Fragment, memo, useMemo, useState } from "react";
import { Banknote, CloudCheck, History, ListOrdered, MapPin, RefreshCw, Route, Truck } from "lucide-react";
import { PageHeader } from "@/app/components/PageHeader";
import { MetricCard } from "@/app/components/MetricCard";
import { SkeletonBar } from "@/app/components/WeeklyTableSkeleton";
import { controlClass } from "@/app/components/management/ManagementUI";
import { IntentLink as Link } from "@/app/components/IntentLink";
import { useViewState } from "@/app/lib/viewMemory";
import { usePermissions } from "@/app/lib/access";
import { compactLoadLocation } from "@/app/lib/usStates";
import { PhoneCell } from "./PhoneCell";
import { LocationCell, formatCoordinates } from "./LocationCell";
import { StatusCell } from "./StatusCell";
import type { DriverBoardEntry } from "@/app/lib/types";
import { addDays, decimalDisplay, indexBoardEntries, shortDate, totals } from "@/app/gross-board/board";
import { statuses, statusColor, formatETA, previewCurrentLoad } from "./board";
import { useDriverBoard } from "./useDriverBoard";
import { BoardHistory } from "./BoardHistory";
import { BoardViews, type BoardView } from "./BoardViews";
import { BoardLoads } from "./BoardLoads";
import { CurrentLoadInput } from "./CurrentLoadInput";
import { ETAEditor } from "./ETAEditor";

const columns = [
  ["Current load", 95], ["Driver", 125], ["Driver type", 55], ["Truck", 50], ["Trailer", 70],
  ["Original gross", 90], ["Driver gross", 90], ["Phone", 115], ["Status", 95],
  ["Latest location", 110], ["Destination", 130], ["ETA", 96], ["Next loads", 180], ["Notes", 180], ["Home time", 120], ["Driver home", 140],
] as const;
const columnWeight = columns.reduce((sum, column) => sum + column[1], 0);
const minimumWidth = columnWeight;
const driverColumnOffset = `max(${columns[0][1]}px, ${columns[0][1] / columnWeight * 100}%)`;
const buttonClass = "inline-flex items-center gap-2 rounded-lg border border-zinc-700/70 px-3 py-2 text-xs text-zinc-300 hover:bg-zinc-800 disabled:opacity-40";
const cellClass = "overflow-hidden text-ellipsis whitespace-nowrap border-b border-r border-zinc-800/80";
const locationTime = (value: string) => new Date(value).toLocaleString("en-US", { timeZone: "America/New_York", month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });

const TextCell = memo(function TextCell({ entry, field, label, limit, disabled, edit }: {
  entry: DriverBoardEntry; field: "currentLoad" | "trailerNumber" | "destination" | "notes" | "homeTime" | "driverHome";
  label: string; limit: number; disabled: boolean;
  edit: (id: string, field: keyof DriverBoardEntry, value: string) => void;
}) {
  return <input type="text" aria-label={label} title={entry[field]} maxLength={limit} disabled={disabled}
    value={entry[field]} onChange={event => edit(entry.driverId, field, event.target.value)}
    className="block h-8 w-full min-w-0 bg-transparent px-1.5 text-[11px] leading-4 text-zinc-200 outline-none placeholder:text-zinc-700 focus:bg-zinc-800 focus:ring-1 focus:ring-inset focus:ring-blue-500" />;
});

export default function DriverBoardPage() {
  const [etaEditor, setEtaEditor] = useState<{ id: string; name: string; value: string; anchor: HTMLButtonElement } | null>(null);
  const state = useDriverBoard(etaEditor !== null);
  const { board, entries, loading, saving, dirty, error, refreshError, eldRefreshing, edit, save, reload, refreshELD, leaving } = state;
  const permissions = usePermissions();
  const canEdit = permissions.includes("driver_board.write");
  const [dispatcher, setDispatcher] = useViewState("page:dispatcher", "all");
  const [search, setSearch] = useViewState("page:search", "");
  const [status, setStatus] = useViewState("page:status", "all");
  const [view, setView] = useViewState("page:view", "all");
  const [myIds, setMyIds] = useViewState<string[]>("page:myDispatchers", []);
  const [savedViews, setSavedViews] = useViewState<BoardView[]>("page:savedViews", []);
  const [history, setHistory] = useState<{ driverId?: string; name: string; ids: string[] } | null>(null);
  const [loadDriver, setLoadDriver] = useState<{ id: string; name: string } | null>(null);
  const [destinationError, setDestinationError] = useState("");
  const viewIds = view === "all" ? null : view === "my" ? myIds : savedViews.find(v => v.id === view)?.dispatcherIds ?? [];
  function selectView(id: string) { setView(id); setDispatcher("all"); }
  const drivers = (board?.drivers ?? []).filter(d =>
    (viewIds === null || viewIds.includes(d.dispatcherId)) &&
    (dispatcher === "all" || d.dispatcherId === dispatcher) && (status === "all" || entries[d.id]?.status === status) &&
    [d.fullName, d.truckUnit, d.phone, d.dispatcherName, d.location ? formatCoordinates(d.location) : "", entries[d.id]?.currentLoad, entries[d.id]?.trailerNumber, ...(board?.loads[d.id]?.next ?? []).map(p => p.number)].join(" ").toLowerCase().includes(search.trim().toLowerCase()));
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
      <PageHeader><div><div className="flex items-center gap-3"><Truck className="h-5 w-5 text-zinc-500" /><h1 className="text-lg font-semibold text-zinc-100">Status Board</h1><span className="rounded-full bg-zinc-800/60 px-2.5 py-0.5 text-xs text-zinc-400">{loading ? <SkeletonBar className="h-3 w-4" /> : drivers.length}</span></div><p className="mt-1.5 text-[13px] text-zinc-500">Live dispatch overview with this week’s Gross Board totals.</p></div></PageHeader>
      {board && <span className="mr-auto text-xs text-zinc-500">Week {shortDate(board.weekStart)}–{shortDate(addDays(board.weekStart, 6))} · New York · totals for shown drivers</span>}
      <span role="status" className={`flex items-center gap-1.5 text-xs ${error && dirty ? "text-red-300" : "text-zinc-400"}`}><CloudCheck className="h-4 w-4" />{loading ? "Loading…" : error && dirty ? "Not saved" : saving ? "Saving…" : dirty ? "Waiting to save…" : canEdit ? "All changes saved" : "Read only"}</span>
      {board?.eld.configured && board.eld.lastError && <span className="text-xs text-amber-300">Five ELD refresh failed</span>}
      {board?.eld.configured && (board.eld.unmatched + board.eld.ambiguous + board.eld.invalid > 0) && <span className="text-xs text-amber-300">Five ELD: {board.eld.unmatched + board.eld.ambiguous + board.eld.invalid} need review</span>}
      {canEdit && board?.eld.configured && <button className={buttonClass} onClick={() => void refreshELD()} disabled={loading || saving || dirty || eldRefreshing} title={board.eld.lastSuccessAt ? `Five ELD last refreshed ${locationTime(board.eld.lastSuccessAt)} NY` : "Refresh Five ELD locations"}><MapPin className="h-4 w-4" />{eldRefreshing ? "Refreshing…" : "Refresh locations"}</button>}
      <button className={buttonClass} onClick={reload} disabled={loading || saving}><RefreshCw className="h-4 w-4" />Reload</button>
      <button className={buttonClass} disabled={loading} onClick={() => setHistory({ name: "History · shown drivers", ids: drivers.map(d => d.id) })}><History className="h-4 w-4" />History</button>
    </div>

    <div className="grid gap-3 sm:grid-cols-3">
      <MetricCard loading={loading} compact label="Weekly original gross" value={board ? decimalDisplay(summary.original, true) : "—"} icon={Banknote} />
      <MetricCard loading={loading} compact label="Weekly driver gross" value={board ? decimalDisplay(summary.driver, true) : "—"} icon={Banknote} />
      <MetricCard loading={loading} compact label="Weekly miles" value={board ? decimalDisplay(summary.miles) : "—"} icon={Route} />
    </div>

    <div className="flex flex-wrap items-center gap-3">
      <input aria-label="Search Status Board" placeholder="Search driver, truck, trailer or load…" value={search} onChange={event => setSearch(event.target.value)} className={`${controlClass} max-w-80`} />
      <select aria-label="Dispatcher filter" value={dispatcher} onChange={event => setDispatcher(event.target.value)} className={`${controlClass} max-w-48`}><option value="all">All dispatchers</option>{dispatchers.map(([id, name]) => <option key={id} value={id}>{name}</option>)}</select>
      <select aria-label="Status filter" value={status} onChange={event => setStatus(event.target.value)} className={`${controlClass} max-w-44`}><option value="all">All statuses</option><option value="">No status</option>{statuses.map(s => <option key={s}>{s}</option>)}</select>
      <BoardViews mode={view} myIds={myIds} saved={savedViews} dispatchers={dispatchers} onSelect={selectView} onSave={(dispatcherIds, name) => {
        if (!name) { setMyIds(dispatcherIds); selectView("my"); return; }
        const existing = savedViews.find(v => v.name.toLowerCase() === name.toLowerCase());
        const next = { id: existing?.id ?? crypto.randomUUID(), name, dispatcherIds };
        setSavedViews(current => [...current.filter(v => v.id !== next.id), next]); selectView(next.id);
      }} onDelete={id => { setSavedViews(current => current.filter(v => v.id !== id)); selectView("all"); }} />
    </div>

    {error && <div role="alert" className="flex flex-wrap items-center gap-3 rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-300"><span>{error}{dirty && " Your unsaved edits are retained."}</span>{dirty && <button className={buttonClass} disabled={saving} onClick={() => void save()}>Retry save</button>}</div>}
    {refreshError && <p role="status" className="text-xs text-amber-300">{refreshError}</p>}
    {destinationError && <div role="alert" className="flex items-center gap-3 rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-300"><span>{destinationError}</span><button className={buttonClass} disabled={loading || saving} onClick={() => { setDestinationError(""); void reload(); }}>Reload board</button></div>}
    <div data-scroll-key="driver-board" className="max-h-[72vh] overflow-auto rounded-xl border border-zinc-800" aria-busy={loading}>
      <table className="w-full table-fixed border-separate border-spacing-0 text-[11px]" style={{ minWidth: minimumWidth }}>
        <colgroup>{columns.map(([label, width]) => <col key={label} style={{ width: `${width / columnWeight * 100}%` }} />)}</colgroup>
        <thead className="sticky top-0 z-20"><tr>{columns.map(([label], i) => <th key={label} scope="col" style={i < 2 ? { left: i === 0 ? 0 : driverColumnOffset } : undefined} className={`${i < 2 ? "sm:sticky sm:z-[21]" : ""} h-8 whitespace-nowrap border-b border-r border-zinc-700 bg-zinc-900 px-1.5 text-left font-medium leading-4 text-zinc-400`}>{label}</th>)}</tr></thead>
        <tbody>
          {loading ? Array.from({ length: 12 }, (_, i) => <tr key={i}>{columns.map(([label]) => <td key={label} className={`${cellClass} h-8 px-2`}><SkeletonBar className="h-3 w-3/4" /></td>)}</tr>) : drivers.map((d, i) => {
            const entry = entries[d.id];
            const sum = totals(index.byDriver.get(d.id) ?? []);
            const group = groups.get(d.dispatcherId)!;
            const loads = previewCurrentLoad(board?.loads[d.id], entry, board?.entries.find(e => e.driverId === d.id));
            const stop = loads?.current?.stops.find(s => s.key === loads.stopKey);
            const stopType = stop?.type.toLowerCase();
            const stopLabel = stopType === "pickup" ? "PU" : stopType === "delivery" ? "DEL" : "";
            const oppositeType = stopType === "pickup" ? "delivery" : stopType === "delivery" ? "pickup" : "";
            const otherStop = oppositeType ? loads?.current?.stops.find(s => s.type.toLowerCase() === oppositeType && s.location.trim()) : undefined;
            const destinationBusy = dirty || saving || loading || leaving;
            const next = loads?.next ?? [];
            return <Fragment key={d.id}>
              {(i === 0 || drivers[i - 1].dispatcherId !== d.dispatcherId) && <tr className="h-8 bg-blue-500/10 text-blue-200" aria-label={`${d.dispatcherName} totals`}>
                <th scope="rowgroup" colSpan={5} className="border-b border-r border-zinc-700 px-3 text-left font-medium">
                  <div className="flex min-w-0 items-center justify-between gap-3 overflow-hidden whitespace-nowrap">
                    <span className="min-w-0 truncate" title={d.dispatcherName}>{d.dispatcherName}{d.dispatcherExtension == null ? "" : ` (${d.dispatcherExtension})`}</span>
                    {d.mainUpdaterName && <span title={`${d.mainUpdaterName}${d.mainUpdaterExtension == null ? "" : ` (${d.mainUpdaterExtension})`}`} className="ml-auto min-w-0 truncate text-right text-[11px] text-blue-200/80">{d.mainUpdaterName}{d.mainUpdaterExtension == null ? "" : ` (${d.mainUpdaterExtension})`}</span>}
                  </div>
                </th>
                <td className="border-b border-r border-zinc-700 px-2 text-right font-mono">{decimalDisplay(group.original, true)}</td>
                <td className="border-b border-r border-zinc-700 px-2 text-right font-mono">{decimalDisplay(group.driver, true)}</td>
                <td colSpan={2} className="border-b border-zinc-700" />
                <td colSpan={columns.length - 9} className="border-b border-zinc-700 px-3 text-[11px]">
                  <div className="flex min-w-0 items-center justify-between gap-3 overflow-hidden whitespace-nowrap">
                    {d.afterHoursUpdaterName && <span title={`${d.afterHoursUpdaterName}${d.afterHoursUpdaterExtension == null ? "" : ` (${d.afterHoursUpdaterExtension})`}`} className="min-w-0 truncate text-blue-200/80">{d.afterHoursUpdaterName}{d.afterHoursUpdaterExtension == null ? "" : ` (${d.afterHoursUpdaterExtension})`}</span>}
                    <span className="ml-auto text-zinc-400">{decimalDisplay(group.miles)} mi</span>
                  </div>
                </td>
              </tr>}
              <tr className="group bg-zinc-950/20 hover:bg-zinc-800/20" data-driver-id={d.id}>
                <td className={`${cellClass} sm:sticky left-0 z-10 bg-zinc-950`}><div className="flex items-center"><div className="min-w-0 flex-1">{<CurrentLoadInput value={entry.currentLoad} label={`${d.fullName} · Current load`} disabled={disabled} entries={index.byDriver.get(d.id) ?? []} week={board!.weekStart} onChange={value => edit(d.id, "currentLoad", value)} />}</div><button aria-label={`${d.fullName} · Loads`} title={loads?.current?.warning || "Current and next loads"} disabled={!loads} className={`shrink-0 p-1 ${loads?.current?.warning ? "text-amber-300" : "text-zinc-500 hover:text-blue-300"}`} onClick={() => setLoadDriver({ id: d.id, name: d.fullName })}><ListOrdered className="h-3.5 w-3.5" /></button></div></td>
                <th scope="row" title={d.fullName} style={{ left: driverColumnOffset }} className={`${cellClass} sm:sticky z-10 bg-zinc-950 px-1.5 text-left font-medium text-zinc-200`}><div className="flex items-center gap-1"><span className="min-w-0 flex-1 truncate">{permissions.includes("fleet.read") ? <Link href={`/drivers/detail?id=${d.id}`} className="hover:text-blue-300">{d.fullName}</Link> : d.fullName}</span><button aria-label={`${d.fullName} · History`} title="Driver history" className="shrink-0 rounded p-1 text-zinc-500 hover:bg-zinc-800 hover:text-zinc-200" onClick={() => setHistory({ driverId: d.id, name: `${d.fullName} · History`, ids: [d.id] })}><History className="h-3 w-3" /></button></div></th>
                <td className={`${cellClass} px-0.5 text-center`}><span className="rounded bg-zinc-800 px-0.5 py-0.5 font-mono text-[10px] text-zinc-300">{d.driverType}</span></td>
                <td title={d.truckUnit} className={`${cellClass} px-1 font-mono text-zinc-300`}>{d.truckUnit || "—"}</td>
                <td className={cellClass}>{textCell(entry, d.fullName, "trailerNumber", "Trailer", 100)}</td>
                <td className={`${cellClass} bg-blue-500/5 px-1 text-right font-mono text-zinc-200`}>{decimalDisplay(sum.original, true)}</td>
                <td className={`${cellClass} bg-blue-500/5 px-1 text-right font-mono text-zinc-200`}>{decimalDisplay(sum.driver, true)}</td>
                <td className={`${cellClass} leading-4`}><PhoneCell name={d.fullName} phone={d.phone} /></td>
                <td className={`${cellClass} ${statusColor(entry.status)}`}><StatusCell name={d.fullName} value={entry.status} disabled={disabled}
                  canAdvance={!!entry.currentLoad.trim() && !!loads && !destinationBusy && ["DISPATCHED", "ENROUTE", "RESERVED"].includes(entry.status)}
                  onChange={value => edit(d.id, "status", value)} onAdvance={async () => {
                    if (!loads || destinationBusy) return;
                    setDestinationError("");
                    try { await state.changeLoads(d.id, loads, { action: "advance" }); }
                    catch (err) { setDestinationError(err instanceof Error ? err.message : "Could not update load progress."); }
                  }} /></td>
                <td className={cellClass}><LocationCell name={d.fullName} location={d.location} /></td>
                <td className={cellClass}>{loads?.destinationSource ? <button type="button" aria-label={`${d.fullName} · Destination from load`} disabled={!canEdit || destinationBusy || !otherStop}
                  title={`${stopLabel ? `${stopLabel} · ` : ""}${compactLoadLocation(loads.sourceDestination) || "Select a stop"}${!canEdit ? " · Read only" : destinationBusy ? " · Wait for board changes to save" : otherStop ? ` · Click to switch to ${oppositeType}: ${compactLoadLocation(otherStop.location)}` : " · Choose a stop in Loads"}`}
                  className="flex h-8 w-full items-center gap-1 px-1.5 text-left text-[11px] text-blue-200 enabled:hover:bg-zinc-800 focus:ring-1 focus:ring-inset focus:ring-blue-500 disabled:cursor-default"
                  onClick={async () => {
                    if (!otherStop || !canEdit || destinationBusy) return;
                    setDestinationError("");
                    try { await state.changeLoads(d.id, loads, { action: "source", stopKey: otherStop.key }); }
                    catch (err) { setDestinationError(err instanceof Error ? err.message : "Could not switch destination. Reload the board and try again."); }
                  }}><span className="min-w-0 flex-1 truncate">{compactLoadLocation(loads.sourceDestination) || "Select a stop"}</span>{stopLabel && <span className="shrink-0 text-[9px] font-normal text-zinc-500">{stopLabel}</span>}</button> : textCell(entry, d.fullName, "destination", "Destination", 500)}</td>
                <td className={cellClass}><button type="button" aria-label={`${d.fullName} · ETA`} title={formatETA(entry.eta, true)} disabled={disabled} className="block h-8 w-full truncate whitespace-nowrap px-1.5 text-left text-[11px] text-zinc-200 hover:bg-zinc-800 focus:ring-1 focus:ring-inset focus:ring-blue-500" aria-haspopup="dialog" aria-expanded={etaEditor?.id === d.id} onClick={event => setEtaEditor(etaEditor?.id === d.id ? null : { id: d.id, name: d.fullName, value: entry.eta, anchor: event.currentTarget })}>{formatETA(entry.eta)}</button></td>
                <td className={cellClass}><button aria-label={`${d.fullName} · Next loads`} title={next.map(p => p.number).join(" → ") || "Open load plans"} disabled={!loads} className="group block h-8 w-full truncate px-1.5 text-left text-[11px]" onClick={() => setLoadDriver({ id: d.id, name: d.fullName })}>{next.length ? <>{next.slice(0, 2).map((load, index) => <Fragment key={load.planId}>{index > 0 && " → "}<span className={load.loadId === null ? "text-red-300" : "text-zinc-300 group-hover:text-blue-200"}>{load.number}</span></Fragment>)}{next.length > 2 && <span className="text-zinc-300 group-hover:text-blue-200">{` (+${next.length - 2})`}</span>}</> : "—"}</button></td>
                <td className={cellClass}>{textCell(entry, d.fullName, "notes", "Notes", 5000)}</td>
                <td className={cellClass}>{textCell(entry, d.fullName, "homeTime", "Home time", 500)}</td>
                <td className={cellClass}>{textCell(entry, d.fullName, "driverHome", "Driver home", 300)}</td>
              </tr>
            </Fragment>;
          })}
          {!loading && drivers.length === 0 && <tr><td colSpan={columns.length} className="p-10 text-center text-zinc-500">{error && !board ? "Status Board could not be loaded. Use Reload to retry." : "No active drivers match this view."}</td></tr>}
        </tbody>
      </table>
    </div>
    <p className="text-[11px] text-zinc-500">{canEdit ? "Edits save after 5 seconds. Driver home also updates the driver profile. " : ""}Dispatch details carry forward; weekly gross and miles come from Gross Board. Truck, phone and dispatcher follow the driver profile.</p>
    {etaEditor && <ETAEditor anchor={etaEditor.anchor} name={etaEditor.name} value={etaEditor.value} onClose={() => setEtaEditor(null)} onApply={value => { edit(etaEditor.id, "eta", value); setEtaEditor(null); }} />}
    {history && <BoardHistory driverIds={history.ids} title={history.name} canUndo={canEdit} waiting={dirty || saving || loading} onUndo={state.undo} onClose={() => setHistory(null)} onLoads={history.driverId ? () => { const d = board?.drivers.find(d => d.id === history.driverId); if (d) { setLoadDriver({ id: d.id, name: d.fullName }); setHistory(null); } } : undefined} />}
    {loadDriver && board?.loads[loadDriver.id] && <BoardLoads key={loadDriver.id} driverId={loadDriver.id} name={loadDriver.name} initial={board.loads[loadDriver.id]} location={board.drivers.find(d => d.id === loadDriver.id)?.location ?? null} canEdit={canEdit} waiting={dirty || saving || loading} onChange={state.changeLoads} onClose={() => setLoadDriver(null)} onHistory={() => { setHistory({ driverId: loadDriver.id, name: `${loadDriver.name} · History`, ids: [loadDriver.id] }); setLoadDriver(null); }} />}
  </div>;
}
