"use client";


import dynamic from "next/dynamic";
import { memo, useCallback, useContext, useMemo, useState } from "react";
import {fetchDriver,fetchFleetStatusCatalog,fetchTruckAssignmentChoices} from "@/app/lib/api";
import {DriverStatusDialog} from "../drivers/DriverStatusDialog";
import { DriverIntakeSetup } from "../drivers/DriverIntakeSetup";
import type {Driver,TruckAssignmentChoice} from "@/app/lib/types";
import {useEffect} from "react";
import { Banknote, CloudCheck, GripVertical, History, MapPin, Route, Truck } from "lucide-react";
import { HeaderPinButton, PinnablePageHeader } from "@/app/components/PinnablePageHeader";
import { PageHeader } from "@/app/components/PageHeader";
import { MetricCard } from "@/app/components/MetricCard";
import { SkeletonBar } from "@/app/components/WeeklyTableSkeleton";
import { controlClass, Modal, Field } from "@/app/components/management/ManagementUI";
import { IntentLink as Link } from "@/app/components/IntentLink";
import { readMemory, ViewMemoryContext, useViewState } from "@/app/lib/viewMemory";
import { canCreateDriver, usePermissions } from "@/app/lib/access";
import { spreadsheetGridEvents } from "@/app/lib/spreadsheetGrid";
import { compactLoadLocation } from "@/app/lib/usStates";
import { PhoneCell } from "./PhoneCell";
import { LocationCell, formatCoordinates } from "./LocationCell";
// Give each deferred dialog its own boundary so a cold download cannot hide AppShell.
const DriverLeaveDialog = dynamic(() => import("./DriverLeaveDialog").then(m => m.DriverLeaveDialog), { loading: () => null });
import { formatDriverLeave, leaveToday, leavePlanDetails } from "@/app/lib/driverLeave";
import { StatusCell } from "./StatusCell";
import type { BoardLoads as BoardLoadsView, BoardStop, DriverBoardEntry, DriverBoardDriver } from "@/app/lib/types";
import { decimalDisplay, indexBoardEntries, totals } from "@/app/gross-board/board";
import { statuses, statusColor, statusLabel, nextDeliveryStops, previewCurrentLoad } from "./board";
import { NextStopDialog } from "./NextStopDialog";
import { boardColumns as columns, useBoardColumnWidths } from "./useBoardColumnWidths";
import { ETACell, appointmentCellText } from "./ETACell";
import { useDriverBoard } from "./useDriverBoard";
const BoardHistory = dynamic(() => import("./BoardHistory").then(m => m.BoardHistory), { loading: () => null });
import { BoardFilter } from "@/app/components/management/BoardFilter";
import { TrailerCell } from "./TrailerCell";
const BoardLoads = dynamic(() => import("./BoardLoads").then(m => m.BoardLoads), { loading: () => null });
import { CurrentLoadInput } from "./CurrentLoadInput";
const ETAEditor = dynamic(() => import("./ETAEditor").then(m => m.ETAEditor), { loading: () => null });
import { StatusBoardScrollArea } from "./StatusBoardScrollArea";
import { CellPresence } from "./CellPresence";
import { TruckAssignmentCell } from "./TruckAssignmentCell";
import { useDispatcherOrder } from "./useDispatcherOrder";
import { useDriverRowDrag } from "./useDriverRowDrag";

const buttonClass = "inline-flex items-center gap-2 rounded-lg border border-zinc-700/70 px-3 py-2 text-xs text-zinc-300 hover:bg-zinc-800 disabled:opacity-40";
const cellClass = "overflow-hidden text-ellipsis whitespace-nowrap border-b border-r border-zinc-800/80";
const locationTime = (value: string) => new Date(value).toLocaleString("en-US", { timeZone: "America/New_York", month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });

const TextCell = memo(function TextCell({ entry, field, label, limit, disabled, edit }: {
  entry: DriverBoardEntry; field: "currentLoad" | "trailerNumber" | "destination" | "notes" | "homeTime";
  label: string; limit: number; disabled: boolean;
  edit: (id: string, field: keyof DriverBoardEntry, value: string) => void;
}) {
  return <input type="text" aria-label={label} title={entry[field]} maxLength={limit} disabled={disabled}
    value={entry[field]} onChange={event => edit(entry.driverId, field, event.target.value)}
    className="block h-8 w-full min-w-0 bg-transparent px-1.5 text-[11px] leading-4 text-zinc-200 outline-none placeholder:text-zinc-700 focus:bg-zinc-800 focus:ring-1 focus:ring-inset focus:ring-blue-500" />;
});

export default function DriverBoardPage() {
  const [truckChoices, setTruckChoices] = useState<TruckAssignmentChoice[] | null>(null);
  const [setupDriver, setSetupDriver] = useState<{ intakeId: string; fullName: string } | null>(null);
  const [restriction,setRestriction]=useState<{id:string;name:string;status:string}|null>(null);
 const [restrictionReason,setRestrictionReason]=useState("");
 const [terminating,setTerminating]=useState<Driver|null>(null);
  const [statusChoices,setStatusChoices]=useState(statuses);
  useEffect(()=>{const c=new AbortController();fetchFleetStatusCatalog(c.signal).then(v=>setStatusChoices(v.rules.map(r=>r.boardStatus))).catch(()=>{});return()=>c.abort();},[]);
  const [etaEditor, setEtaEditor] = useState<{ id: string; name: string; value: string; anchor: HTMLButtonElement } | null>(null);
  const [pickupEditor, setPickupEditor] = useState<{ id: string; name: string; planId: string; pickup: BoardStop; anchor: HTMLButtonElement } | null>(null);
  const [nextStop, setNextStop] = useState<{ id: string; name: string; status: string; loads: BoardLoadsView } | null>(null);
  const [leaveEditor, setLeaveEditor] = useState<{ id: string; name: string; status: string; changingStatus: boolean } | null>(null);
 const [today, setToday] = useState(leaveToday);
 useEffect(() => { const timer = setInterval(() => setToday(leaveToday()), 30000); return () => clearInterval(timer); }, []);
 const state = useDriverBoard(nextStop?.id ?? etaEditor?.id ?? pickupEditor?.id ?? leaveEditor?.id ?? restriction?.id);
  const { board: sourceBoard, entries, loading, saving, dirty, error, refreshError, eldRefreshing, edit, save, reload, refreshELD, leaving } = state;
  const dispatcherOrder = useDispatcherOrder(sourceBoard?.drivers ?? []);
  const board = useMemo(() => sourceBoard ? { ...sourceBoard, drivers: dispatcherOrder.drivers } : null, [sourceBoard, dispatcherOrder.drivers]);
  const permissions = usePermissions();
  const canEdit = permissions.includes("driver_board.write");
  const scope = useContext(ViewMemoryContext);
  const [selectedDispatchers, setSelectedDispatchers] = useViewState<string[] | null>("page:dispatcherChecks", () => {
    const read = <T,>(name: string, fallback: T) => readMemory<T>(scope + ":view:page:" + name, fallback);
    const dispatcher = read<string>("dispatcher", "all"), view = read<string>("view", "all");
    if (dispatcher !== "all") return [dispatcher];
    if (view === "my") return read<string[]>("myDispatchers", []);
    return view === "all" ? null : read<{id: string; dispatcherIds: string[]}[]>("savedViews", []).find(v => v.id === view)?.dispatcherIds ?? null;
  });
  const [search, setSearch] = useViewState("page:search", "");
  const [headerPinned, setHeaderPinned] = useViewState("page:headerPinned", false);
  const [selectedStatuses, setSelectedStatuses] = useViewState<string[] | null>("page:statusChecks", () => {
    const legacy = readMemory<string>(scope + ":view:page:status", "all");
    return legacy === "all" ? null : [legacy];
  });
  const [history, setHistory] = useState<{ driverId?: string; name: string; ids: string[]; field?: string } | null>(null);
  const [loadDriver, setLoadDriver] = useState<{ id: string; name: string } | null>(null);
  const [destinationError, setDestinationError] = useState("");
  const [orderError, setOrderError] = useState("");
  const [orderNotice, setOrderNotice] = useState("");
  const canAssignTruck = permissions.includes("drivers.edit") && permissions.includes("trucks.read");
  const refreshTruckChoices = useCallback(() => {
    if (canAssignTruck) void fetchTruckAssignmentChoices().then(setTruckChoices).catch(err => setOrderError(err instanceof Error ? err.message : "Could not load trucks"));
  }, [canAssignTruck]);
  useEffect(() => {
    refreshTruckChoices();
    window.addEventListener("focus", refreshTruckChoices);
    return () => window.removeEventListener("focus", refreshTruckChoices);
  }, [refreshTruckChoices]);
  const drivers = (board?.drivers ?? []).filter(d =>
    (selectedDispatchers === null || selectedDispatchers.includes(d.dispatcherId)) &&
    (selectedStatuses === null || selectedStatuses.includes(entries[d.id]?.status ?? "")) &&
    [d.fullName, d.truckUnit, d.phone, d.dispatcherName, d.location ? formatCoordinates(d.location) : "", entries[d.id]?.currentLoad, entries[d.id]?.trailerNumber, ...(board?.loads[d.id]?.next ?? []).map(p => p.number)].join(" ").toLowerCase().includes(search.trim().toLowerCase()));
  const autoOrder = board?.autoOrderDispatchers;
  const rowDrag = useDriverRowDrag(drivers, moveDriver, autoOrder);
  const renderedDrivers = rowDrag.drivers;
  const { table, minimumWidth, tableWidth, columnWidth } = useBoardColumnWidths(renderedDrivers.map(driver => {
    const entry = entries[driver.id];
    const loads = previewCurrentLoad(board?.loads[driver.id], entry, board?.entries.find(e => e.driverId === driver.id));
    const next = loads?.next[0];
    const pickup = next?.stops.find(stop => stop.type.toLowerCase() === "pickup");
    return [entry.currentLoad, "", driver.truckUnit, "", "", "", "", statusLabel(entry.status),
      ["HOME", "HOME (YARD)", "VACATION"].includes(entry.status) ? compactLoadLocation([driver.city, driver.state].filter(Boolean).join(", ")) : loads?.destinationSource ? compactLoadLocation(loads.sourceDestination) : entry.destination,
      appointmentCellText(entry.eta, loads?.automation).full,
      [next?.number, pickup?.schedule?.label || (pickup ? "PU —" : "")].filter(Boolean).join("  ")];
  }));
  const dispatchers: [string, string][] = [["", "Unassigned"], ...Array.from(new Map((board?.drivers ?? []).filter(d => d.dispatcherId).map(d => [d.dispatcherId, d.dispatcherName])).entries())];
  const pendingDrivers = (board?.pendingDrivers ?? []).filter(d => (selectedDispatchers === null || selectedDispatchers.includes("")) && (selectedStatuses === null || selectedStatuses.includes("NEW DRIVER")) && d.fullName.toLowerCase().includes(search.trim().toLowerCase()));
  const index = useMemo(() => indexBoardEntries(board?.grossEntries ?? []), [board]);
  const ids = new Set(drivers.map(d => d.id));
  const summary = totals((board?.grossEntries ?? []).filter(e => ids.has(e.driverId)));
  const groups = new Map<string, ReturnType<typeof totals>>();
  for (const d of renderedDrivers) {
    const sum = totals(index.byDriver.get(d.id) ?? []);
    const group = groups.get(d.dispatcherId) ?? { original: BigInt(0), driver: BigInt(0), miles: BigInt(0) };
    group.original += sum.original; group.driver += sum.driver; group.miles += sum.miles;
    groups.set(d.dispatcherId, group);
  }
  const disabled = !canEdit || loading || leaving || state.actionBusy;
  const orderDisabled = disabled || saving || dirty;
  async function toggleAutoOrder(dispatcherId: string, dispatcherName: string) {
    const enabled = !autoOrder?.[dispatcherId];
    setOrderError(""); setOrderNotice("");
    try {
      await state.setAutoOrder(dispatcherId, enabled);
      setOrderNotice(`Auto-order ${enabled ? "on" : "off"} for ${dispatcherName}.`);
    } catch (err) { setOrderError(err instanceof Error ? err.message : "Auto-order could not be saved. Try again."); }
  }
  async function moveDriver(id: string, target: string, after: boolean, revision: string, dispatcher?: string) {
    setOrderError(""); setOrderNotice("");
    try {
      await state.reorderDrivers(id, target, after, revision, dispatcher);
      setOrderNotice("Saved.");
      return true;
    } catch (err) { setOrderError(err instanceof Error ? err.message : "Driver order could not be saved. Try again."); return false; }
  }
  async function moveDispatcher(id: string, target: string, after: boolean) {
    setOrderError(""); setOrderNotice("");
    try {
      const persisted = dispatcherOrder.reorder(id, target, after);
      setOrderNotice(persisted ? "Your dispatcher order is saved in this browser." : "Order changed for this tab. Browser storage is unavailable.");
      return true;
    } catch (err) { setOrderError(err instanceof Error ? err.message : "Dispatcher order could not be saved. Try again."); return false; }
  }
  function textCell(entry: DriverBoardEntry, name: string, field: Parameters<typeof TextCell>[0]["field"], label: string, limit: number) {
    return <TextCell entry={entry} field={field} label={`${name} · ${label}`} limit={limit} disabled={disabled} edit={edit} />;
  }

  function groupHeader(d: Pick<DriverBoardDriver, "dispatcherId" | "dispatcherName"> & Partial<DriverBoardDriver>) {
    const group = groups.get(d.dispatcherId) ?? { original: BigInt(0), driver: BigInt(0), miles: BigInt(0) };
    const enabled = !!autoOrder?.[d.dispatcherId];
    return <tr key={`dispatcher:${d.dispatcherId}`} data-dispatcher-id={d.dispatcherId} className="h-8 bg-blue-500/10 text-blue-200" aria-label={`${d.dispatcherName} totals`}>
                <th scope="rowgroup" colSpan={4} className="border-b border-r border-zinc-700 bg-zinc-900 px-3 text-left font-medium">
                  <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-2 whitespace-nowrap">
                    <span className="truncate" title={d.dispatcherName}>{d.dispatcherName}{d.dispatcherExtension == null ? "" : ` (${d.dispatcherExtension})`}</span>
                    <button type="button" role="switch" aria-checked={enabled} aria-label={`Auto-order for ${d.dispatcherName}`} data-grid-ignore
                      disabled={orderDisabled || rowDrag.active || !board}
                      title="Ready, Dispatched, Enroute, Reserved, Rest, Home, then other statuses"
                      onClick={() => void toggleAutoOrder(d.dispatcherId, d.dispatcherName)}
                      className="inline-flex h-7 items-center gap-1.5 rounded-full px-1.5 text-[10px] focus-visible:outline-2 focus-visible:outline-blue-400 disabled:cursor-default">
                      Auto-order
                      <span aria-hidden="true" className={`inline-flex h-5 w-9 items-center rounded-full p-0.5 ${enabled ? "bg-linear-to-r from-emerald-300 to-blue-500" : "bg-zinc-600"}`}>
                        <span className={`size-4 rounded-full bg-white shadow-sm transition-transform motion-reduce:transition-none ${enabled ? "translate-x-4" : "translate-x-0"}`} />
                      </span>
                    </button>
                    {d.mainUpdaterName && <span title={`${d.mainUpdaterName}${d.mainUpdaterExtension == null ? "" : ` (${d.mainUpdaterExtension})`}`} className="status-board-updater truncate text-right text-[11px]">{d.mainUpdaterName}{d.mainUpdaterExtension == null ? "" : ` (${d.mainUpdaterExtension})`}</span>}
                  </div>
                </th>
                <td className="border-b border-r border-zinc-700 px-2 text-center font-mono">{decimalDisplay(group.original, true)}</td>
                <td className="border-b border-r border-zinc-700 px-2 text-center font-mono">{decimalDisplay(group.driver, true)}</td>
                <td colSpan={2} className="border-b border-zinc-700" />
                <td colSpan={columns.length - 8} className="border-b border-zinc-700 px-3 text-[11px]">
                  <div className="flex min-w-0 items-center justify-between gap-3 overflow-hidden whitespace-nowrap">
                    {d.afterHoursUpdaterName && <span title={`${d.afterHoursUpdaterName}${d.afterHoursUpdaterExtension == null ? "" : ` (${d.afterHoursUpdaterExtension})`}`} className="status-board-updater min-w-0 truncate">{d.afterHoursUpdaterName}{d.afterHoursUpdaterExtension == null ? "" : ` (${d.afterHoursUpdaterExtension})`}</span>}
                    <span className="ml-auto text-zinc-400">{decimalDisplay(group.miles)} mi</span>
                  </div>
                </td>
              </tr>;
  }

  return <div className="space-y-5 animate-fade-in">
    <PageHeader><div><div className="flex items-center gap-3"><Truck className="h-5 w-5 text-zinc-500" /><h1 className="text-lg font-semibold text-zinc-100">Status Board</h1><span className="rounded-full bg-zinc-800/60 px-2.5 py-0.5 text-xs text-zinc-400">{loading ? <SkeletonBar className="h-3 w-4" /> : drivers.length + pendingDrivers.length}</span></div></div></PageHeader>

    <PinnablePageHeader pinned={headerPinned}>
      <div className="grid gap-3 sm:grid-cols-3">
      <MetricCard loading={loading} compact label="Weekly total gross" value={board ? decimalDisplay(summary.original, true) : "—"} icon={Banknote} />
      <MetricCard loading={loading} compact label="Weekly driver gross" value={board ? decimalDisplay(summary.driver, true) : "—"} icon={Banknote} />
      <MetricCard loading={loading} compact label="Weekly miles" value={board ? decimalDisplay(summary.miles) : "—"} icon={Route} />
    </div>

    <div className="flex flex-wrap items-center gap-3">
      <HeaderPinButton pinned={headerPinned} onToggle={() => setHeaderPinned(value => !value)} />
      <input aria-label="Search Status Board" placeholder="Search driver, truck, trailer or load…" value={search} onChange={event => setSearch(event.target.value)} className={`${controlClass} max-w-80`} />


      <BoardFilter label="Dispatcher" options={dispatchers} selected={selectedDispatchers} onChange={setSelectedDispatchers} onReorder={moveDispatcher} reorderDisabled={loading || leaving || rowDrag.active || state.actionBusy} pinnedFirst="" />
      <BoardFilter label="Status" options={[["", "No status"], ...statusChoices.map(s => [s, statusLabel(s)] as [string, string])]} selected={selectedStatuses} onChange={setSelectedStatuses} />
    <div className="ml-auto flex flex-wrap items-center gap-2">
      <span className="flex items-center gap-1.5 text-xs text-zinc-400" title={state.connected ? "Updates are live" : "Reconnecting to live updates"}><span className={`h-1.5 w-1.5 rounded-full ${state.connected ? "bg-emerald-400" : "bg-amber-400"}`} />{state.connected ? "Live" : "Connecting…"}</span>
      <span role="status" className={`flex items-center gap-1.5 text-xs ${error && dirty ? "text-red-300" : "text-zinc-400"}`}><CloudCheck className="h-4 w-4" />{loading ? "Loading…" : error && dirty ? "Not saved" : saving ? "Saving…" : dirty ? "Saving…" : canEdit ? "Saved" : "Read only"}</span>
      {board?.eld.configured && board.eld.lastError && <span className="text-xs text-amber-300">Telematics refresh failed</span>}
      {canEdit && board?.eld.configured && <button className={buttonClass} onClick={() => void refreshELD()} disabled={loading || saving || dirty || eldRefreshing} title={board.eld.lastSuccessAt ? `Telematics last refreshed ${locationTime(board.eld.lastSuccessAt)} NY` : "Refresh telematics locations"}><MapPin className="h-4 w-4" />{eldRefreshing ? "Refreshing…" : "Refresh locations"}</button>}
      <button className={buttonClass} disabled={loading} onClick={() => setHistory({ name: "Status Board history", ids: [] })}><History className="h-4 w-4" />History</button></div></div>

    </PinnablePageHeader>

    {error && <div role="alert" className="flex flex-wrap items-center gap-3 rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-300"><span>{error}{dirty && " Your unsaved edits are retained."}</span>{dirty ? <button className={buttonClass} disabled={saving} onClick={() => void save()}>Retry save</button> : <button className={buttonClass} disabled={loading} onClick={reload}>Retry</button>}</div>}
    {refreshError && <p role="status" className="text-xs text-amber-300">{refreshError}<button className={`${buttonClass} ml-2`} onClick={reload} disabled={loading || saving}>Retry</button></p>}
    {destinationError && <div role="alert" className="flex items-center gap-3 rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-300"><span>{destinationError}</span><button className={buttonClass} disabled={loading || saving} onClick={() => { setDestinationError(""); void reload(); }}>Reload board</button></div>}
    {orderError && <div role="alert" className="flex items-center gap-3 rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-300"><span>{orderError}</span><button className={buttonClass} disabled={loading || saving} onClick={() => { setOrderError(""); void reload(); }}>Reload board</button></div>}
    <span role="status" aria-live="polite" className="sr-only">{orderNotice}</span>
    <StatusBoardScrollArea loading={loading} headerPinned={headerPinned}>
      <table ref={table} {...spreadsheetGridEvents} onContextMenu={event => {
        if (event.defaultPrevented) return;
        const cell = (event.target as HTMLElement).closest<HTMLTableCellElement>("td,th");
        const row = cell?.closest<HTMLTableRowElement>("tr[data-driver-id]");
        const driver = board?.drivers.find(d => d.id === row?.dataset.driverId);
        const fields = ["currentLoad", "", "", "trailerNumber", "", "", "", "status", "destination", "eta", "loadPlan", "notes", "leavePlan"];
        const field = cell ? fields[cell.cellIndex] : "";
        if (!driver || !field) return;
        event.preventDefault(); setHistory({ driverId: driver.id, name: driver.fullName + " · Cell history", ids: [driver.id], field });
      }} data-spreadsheet-grid data-status-board-grid className="table-fixed border-separate border-spacing-0 text-[11px]" style={{ minWidth: minimumWidth, width: tableWidth }}>
        <colgroup>{columns.map(([label], i) => <col key={label} style={{ width: columnWidth(i) }} />)}</colgroup>
        <thead className="sticky top-0 z-20"><tr>{columns.map(([label], i) => <th key={label} scope="col" className={`h-8 whitespace-nowrap border-b border-r border-zinc-700 bg-zinc-900 px-1.5 ${[4, 5, 7].includes(i) ? "text-center" : "text-left"} font-medium leading-4 text-zinc-400`}>{label}</th>)}</tr></thead>
        <tbody>
          {!loading && (selectedDispatchers === null || selectedDispatchers.includes("")) && groupHeader({ dispatcherId: "", dispatcherName: "Unassigned" })}
          {!loading && pendingDrivers.map(d => <tr key={d.intakeId} data-pending-intake-id={d.intakeId} className="bg-zinc-950/20">
            <td className={`${cellClass} bg-zinc-950`} />
            <th scope="row" className={`${cellClass} bg-zinc-950 h-8 px-1.5 text-left font-medium text-zinc-200`} title={d.fullName}>{d.fullName}</th>
            <td colSpan={5} className={cellClass} />
            <td className={`${cellClass} px-1.5 text-center ${statusColor("NEW DRIVER")}`}>New driver</td>
            <td colSpan={columns.length - 8} className={`${cellClass} px-3 text-amber-300`}>
              {canCreateDriver(permissions) && permissions.includes("tasks.review") ? <button type="button" onClick={() => setSetupDriver(d)} className="hover:underline">Needs setup</button> : "Needs setup"}
            </td>
          </tr>)}
          {loading ? Array.from({ length: 12 }, (_, i) => <tr key={i}>{columns.map(([label]) => <td key={label} className={`${cellClass} h-8 px-2`}><SkeletonBar className="h-3 w-3/4" /></td>)}</tr>) : renderedDrivers.flatMap((d, i) => {
            const entry = entries[d.id];
            const homeAddress = compactLoadLocation([d.city, d.state].filter(Boolean).join(", "));
            const atHome = ["HOME", "HOME (YARD)", "VACATION"].includes(entry.status);
            const sum = totals(index.byDriver.get(d.id) ?? []);
            const loads = previewCurrentLoad(board?.loads[d.id], entry, board?.entries.find(e => e.driverId === d.id));
            const stop = loads?.current?.stops.find(s => s.key === loads.stopKey);
            const stopType = stop?.type.toLowerCase();
            const stopLabel = stopType === "pickup" ? "PU" : stopType === "delivery" ? "DEL" : "";
            const destinationBusy = dirty || saving || loading || leaving;
            const next = loads?.next[0];
            const nextStop = next?.stops.find(stop => stop.type.toLowerCase() === "pickup");
            const nextPickup = nextStop?.schedule;
            const dragDisabled = orderDisabled || !!autoOrder?.[d.dispatcherId];
            return [
              d.dispatcherId !== "" && (i === 0 || renderedDrivers[i - 1].dispatcherId !== d.dispatcherId) ? groupHeader(d) : null,
              <tr key={d.id} className="group bg-zinc-950/20 hover:bg-zinc-800/20" data-driver-id={d.id}>
                <td className={`${cellClass} bg-zinc-950`}><CurrentLoadInput value={entry.currentLoad} label={`${d.fullName} · Current load`} warning={loads?.resolutionWarning} disabled={disabled} entries={index.byDriver.get(d.id) ?? []} week={board!.weekStart} onChange={value => edit(d.id, "currentLoad", value)} /></td>
                <th scope="row" title={d.fullName} className={`${cellClass} bg-zinc-950 px-1.5 text-left font-medium text-zinc-200`}><div className="flex items-center gap-1">
                  {canEdit && <button type="button" data-grid-ignore aria-label={`Move ${d.fullName}`} title={autoOrder?.[d.dispatcherId] ? "Turn off Auto-order to move drivers." : "Drag to change the shared driver order or move between dispatchers. Arrow Up / Down moves rows; Alt + Arrow Up unassigns."} disabled={dragDisabled || !board?.driverOrderRevisions?.[d.dispatcherId]}
                    className="flex h-8 shrink-0 touch-none select-none cursor-grab items-center text-zinc-500 hover:text-zinc-200 focus-visible:outline-2 focus-visible:outline-blue-400 active:cursor-grabbing disabled:cursor-default disabled:opacity-30"
                    onPointerDown={event => {
                      const revision = board?.driverOrderRevisions?.[d.dispatcherId];
                      if (!dragDisabled && revision) { rowDrag.start(event, d, revision); setOrderNotice(""); }
                    }}
                    onKeyDown={event => {
                      if (event.key !== "ArrowUp" && event.key !== "ArrowDown") return;
                      event.preventDefault(); event.stopPropagation();
                      if (event.altKey && event.key === "ArrowUp" && d.dispatcherId) {
                        if (!dragDisabled && !autoOrder?.[""] && !rowDrag.active) void moveDriver(d.id, "", false, board!.driverOrderRevisions[d.dispatcherId], "");
                        return;
                      }
                      const target = drivers[i + (event.key === "ArrowUp" ? -1 : 1)];
                      const revision = board?.driverOrderRevisions?.[d.dispatcherId];
                      if (target && revision && !dragDisabled && !autoOrder?.[target.dispatcherId] && !rowDrag.active) void moveDriver(d.id, target.id, event.key === "ArrowDown", revision);
                    }}><GripVertical className="h-4 w-4" /></button>}
                  <span className="min-w-0 truncate">{permissions.includes("drivers.read") ? <Link href={`/drivers/detail?id=${d.id}`} className="hover:text-blue-300">{d.fullName}</Link> : d.fullName}</span><span className="shrink-0 font-mono text-[10px] text-zinc-400">{`(${d.driverType})`}</span><span className="ml-auto"><LocationCell name={d.fullName} location={d.location} /></span></div></th>
                <td title={d.truckUnit} className={`${cellClass} px-1 font-mono text-zinc-300`}>{canAssignTruck ? <TruckAssignmentCell id={d.id} name={d.fullName} truckId={d.truckId || ""} unit={d.truckUnit} choices={truckChoices} disabled={loading || leaving || state.actionBusy || saving || dirty} onFocus={refreshTruckChoices} onError={setOrderError} onSaved={unit => { setOrderNotice(`Truck ${unit} assigned effective today.`); state.refresh(); refreshTruckChoices(); }} /> : d.truckId && permissions.includes("trucks.read") ? <Link href={`/trucks/detail?id=${d.truckId}`} className="hover:text-blue-300">{d.truckUnit}</Link> : d.truckUnit || "—"}</td>
                <TrailerCell entry={entry} name={d.fullName} disabled={disabled} edit={edit} onHistory={() => setHistory({ driverId: d.id, name: `${d.fullName} · Trailer history`, ids: [d.id], field: "trailerNumber" })} />
                <td className={`${cellClass} bg-blue-500/5 px-1 text-center font-mono text-zinc-200`}>{decimalDisplay(sum.original, true)}</td>
                <td className={`${cellClass} bg-blue-500/5 px-1 text-center font-mono text-zinc-200`}>{decimalDisplay(sum.driver, true)}</td>
                <td className={`${cellClass} leading-4`}><PhoneCell name={d.fullName} phone={d.phone} /></td>
                <td className={`${cellClass} ${statusColor(entry.status)}`}><StatusCell name={d.fullName} value={entry.status} disabled={disabled}
                  canAdvance={!!entry.currentLoad.trim() && !!loads && !destinationBusy && ["DISPATCHED", "ENROUTE", "RESERVED"].includes(entry.status)}
                  choices={statusChoices} onChange={value => {if(["SHOP","STOP","ACCIDENT"].includes(value)){setRestriction({id:d.id,name:d.fullName,status:value});setRestrictionReason("");return;}if(["HOME","HOME (YARD)","VACATION"].includes(value)){setLeaveEditor({id:d.id,name:d.fullName,status:value,changingStatus:true});return;}if(value==="Left"){if(!permissions.includes("drivers.edit")){setDestinationError("Driver-edit permission is required for termination.");return;}if(dirty||saving){setDestinationError("Save pending board changes before terminating a driver.");return;}void fetchDriver(d.id).then(setTerminating).catch(e=>setDestinationError(e.message));return;}edit(d.id,"status",value);}} onAdvance={async () => {
                    if (!loads || destinationBusy) return;
                    setDestinationError("");
                    const remaining = nextDeliveryStops(entry.status, loads);
                    if ((entry.status === "DISPATCHED" && remaining.length > 1) || (entry.status !== "DISPATCHED" && remaining.length > 0)) {
                      setNextStop({ id: d.id, name: d.fullName, status: entry.status, loads });
                      return;
                    }
                    try { await state.changeLoads(d.id, loads, { action: "advance" }); }
                    catch (err) { setDestinationError(err instanceof Error ? err.message : "Could not update load progress."); }
                  }} /></td>
                <td className={cellClass}>{atHome ? <span aria-label={`${d.fullName} · Destination from home address`} title={homeAddress || "Home address not set"} className="block truncate px-1.5 text-zinc-200">{homeAddress || "—"}</span> : <div className="flex h-8 items-center">
                  <input type="text" aria-label={d.fullName + " · Destination"} maxLength={500} disabled={disabled}
                    title={loads?.destinationSource ? loads.sourceDestination : entry.destination}
                    value={loads?.destinationSource ? compactLoadLocation(loads.sourceDestination) : entry.destination}
                    placeholder="Destination" onChange={event => edit(d.id, "destination", event.target.value)}
                    className={"h-8 w-full min-w-0 text-ellipsis bg-transparent px-1.5 text-[11px] outline-none focus:text-clip focus:bg-zinc-800 focus:ring-1 focus:ring-inset focus:ring-blue-500 " + (loads?.destinationSource ? "text-blue-200" : "text-zinc-200")} />
                  {stopLabel && <span className="shrink-0 pr-1.5 text-[11px] text-zinc-500">{stopLabel}</span>}
                </div>}</td>
                <td className={cellClass}><ETACell name={d.fullName} value={entry.eta} automation={loads?.automation} stop={stop} disabled={disabled} expanded={etaEditor?.id === d.id} onOpen={anchor => setEtaEditor(etaEditor?.id === d.id ? null : { id: d.id, name: d.fullName, value: entry.eta, anchor })} /></td>
                <td className={cellClass}><div className="flex h-8 w-full items-center gap-2"><button aria-label={`${d.fullName} · Next load`} title={next ? [next.number, nextPickup?.detail].filter(Boolean).join(" · ") : "Open load plans"} disabled={!loads} className="group h-full shrink-0 px-1.5 text-left text-[11px] text-zinc-200" onClick={() => setLoadDriver({ id: d.id, name: d.fullName })}>{next ? <span className={next.loadId === null ? "text-red-300" : "text-zinc-300 group-hover:text-blue-200"}>{next.number}</span> : "—"}</button>{next && nextStop && <button type="button" data-grid-action aria-label={`${d.fullName} · Next pickup time`} title={canEdit ? "Edit next pickup time" : nextPickup?.detail} disabled={!canEdit || disabled || next.loadId === null} aria-haspopup="dialog" aria-expanded={pickupEditor?.id === d.id} className="mr-1 shrink-0 rounded px-1 py-1 text-left text-[11px] text-zinc-200 transition-colors enabled:hover:bg-blue-500/20 enabled:hover:text-blue-200 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-blue-500" onClick={event => { setEtaEditor(null); setPickupEditor(pickupEditor?.id === d.id ? null : { id: d.id, name: d.fullName, planId: next.planId, pickup: nextStop, anchor: event.currentTarget }); }}>{nextPickup?.label || "PU —"}</button>}</div></td>
                <td className={cellClass}>{textCell(entry, d.fullName, "notes", "Notes", 5000)}</td>
                <td className={cellClass}><button type="button" aria-label={`${d.fullName} · Home time`} title={leavePlanDetails(entry.leavePlan) || entry.homeTime} disabled={disabled} className="block h-8 w-full truncate px-1.5 text-left text-[11px] text-zinc-200 enabled:hover:bg-zinc-800" onClick={() => setLeaveEditor({ id: d.id, name: d.fullName, status: entry.status, changingStatus: false })}>{formatDriverLeave(entry.leavePlan, entry.status, entry.homeTime, today)}</button></td>
                <td className={`${cellClass} px-1.5 text-zinc-200`} aria-label={`${d.fullName} · Driver address`} title={homeAddress || "Home address not set"}>{homeAddress || "—"}</td>
              </tr>,
            ];
          })}
          {!loading && drivers.length === 0 && pendingDrivers.length === 0 && <tr><td colSpan={columns.length} className="p-10 text-center text-zinc-500">{error && !board ? "Status Board could not be loaded. Use Retry to try again." : "No active drivers match this view."}</td></tr>}
        </tbody>
      </table>
    </StatusBoardScrollArea>
    <CellPresence peers={state.peers} />
    {nextStop && <NextStopDialog name={nextStop.name} status={nextStop.status} loads={nextStop.loads}
      onClose={() => setNextStop(null)} onApply={stopKey => state.changeLoads(nextStop.id, nextStop.loads, { action: "advance", stopKey })} />}
    {etaEditor && <ETAEditor key={`${etaEditor.id}:${board?.loads[etaEditor.id]?.automation?.context ?? ""}:${board?.loads[etaEditor.id]?.automation?.stopType ?? ""}`} anchor={etaEditor.anchor} name={etaEditor.name} value={etaEditor.value} automation={board?.loads[etaEditor.id]?.automation} waiting={dirty || saving || loading} onAction={async action => { const view = board?.loads[etaEditor.id]; if (view) await state.changeLoads(etaEditor.id, view, action); }} onClose={() => setEtaEditor(null)} onApply={value => { edit(etaEditor.id, "eta", value); setEtaEditor(null); }} />}
    {pickupEditor && <ETAEditor key={`${pickupEditor.id}:${pickupEditor.planId}:${pickupEditor.pickup.key}`} anchor={pickupEditor.anchor} name={pickupEditor.name} value="" pickup={board?.loads[pickupEditor.id]?.next.find(load => load.planId === pickupEditor.planId)?.stops.find(stop => stop.key === pickupEditor.pickup.key) ?? pickupEditor.pickup} waiting={dirty || saving || loading} onAction={async action => {
      const view = board?.loads[pickupEditor.id];
      if (!view || view.next[0]?.planId !== pickupEditor.planId) throw new Error("Next load changed. Close and reopen the pickup time.");
      await state.changeLoads(pickupEditor.id, view, { ...action, planId: pickupEditor.planId, stopKey: pickupEditor.pickup.key });
    }} onClose={() => setPickupEditor(null)} onApply={() => setPickupEditor(null)} />}
    {restriction&&<Modal title={`Shop · ${restriction.name}`} isSaving={false} submitLabel="Apply status" onClose={()=>setRestriction(null)} onSubmit={e=>{e.preventDefault();if(!restrictionReason.trim())return;edit(restriction.id,"status",restriction.status,{statusReason:restrictionReason.trim()});setRestriction(null);}}><Field label="Reason"><textarea aria-label="Truck status reason" required maxLength={1000} className={controlClass} value={restrictionReason} onChange={e=>setRestrictionReason(e.target.value)}/></Field><p className="mt-3 text-xs text-zinc-500">The truck will be marked Shop and cannot receive new dispatch until explicitly returned to service.</p></Modal>}
    {leaveEditor && <DriverLeaveDialog name={leaveEditor.name} initial={entries[leaveEditor.id]?.leavePlan} status={leaveEditor.status} changingStatus={leaveEditor.changingStatus} onClose={() => setLeaveEditor(null)} onApply={plan => { edit(leaveEditor.id, leaveEditor.changingStatus ? "status" : "homeTime", leaveEditor.changingStatus ? leaveEditor.status : "", { leavePlan: plan, homeTime: "" }); setLeaveEditor(null); }} />}
    {setupDriver && <DriverIntakeSetup key={setupDriver.intakeId} intakeId={setupDriver.intakeId} name={setupDriver.fullName} onClose={() => setSetupDriver(null)} onSaved={() => { setSetupDriver(null); setOrderNotice("Saved."); state.refresh(); }} />}
    {terminating&&<DriverStatusDialog driver={terminating} startTerminated onClose={()=>setTerminating(null)} onSaved={()=>{setTerminating(null);void reload();}}/>}
    {history && <BoardHistory key={`${history.ids.join(",")}:${history.field ?? ""}`} initialField={history.field} driverIds={history.ids} title={history.name} canUndo={canEdit} waiting={dirty || saving || loading} onUndo={(id, driverId) => state.undo(id, driverId, true)} onClose={() => setHistory(null)} onLoads={history.driverId ? () => { const d = board?.drivers.find(d => d.id === history.driverId); if (d) { setLoadDriver({ id: d.id, name: d.fullName }); setHistory(null); } } : undefined} />}
    {loadDriver && board?.loads[loadDriver.id] && <BoardLoads key={loadDriver.id} driverId={loadDriver.id} name={loadDriver.name} initial={board.loads[loadDriver.id]} canEdit={canEdit} waiting={dirty || saving || loading} onChange={state.changeLoads} onClose={() => setLoadDriver(null)} onHistory={() => { setHistory({ driverId: loadDriver.id, name: `${loadDriver.name} · History`, ids: [loadDriver.id] }); setLoadDriver(null); }} />}
  </div>;
}
