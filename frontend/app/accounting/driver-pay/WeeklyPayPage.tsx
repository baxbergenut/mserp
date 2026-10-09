"use client";

import { PageHeader } from "@/app/components/PageHeader";
import { usePermissions } from "@/app/lib/access";
import { IntentLink as Link } from "@/app/components/IntentLink";

import { useRouter, useSearchParams } from "next/navigation";
import { useViewState, useRestoringView } from "@/app/lib/viewMemory";

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { AlertTriangle, Banknote, ChevronLeft, ChevronRight, CloudCheck, RefreshCw, UserRound, UsersRound } from "lucide-react";
import { fetchDriverPay, fetchInvestorPay, refreshDriverPayLoads, saveDriverPay, saveInvestorPay, acceptPaySystemValues, undoPaySystemValues } from "@/app/lib/api";
import type { DriverPayEdits, DriverPayLoad, DriverPayWeek } from "@/app/lib/types";
import { currentChargeWeek } from "../driver-charges/charges";
import { SkeletonBar, WeeklyTableSkeleton } from "@/app/components/WeeklyTableSkeleton";
import { MetricCard } from "@/app/components/MetricCard";
import { ManagementSearch, TablePagination, controlClass } from "@/app/components/management/ManagementUI";
import { addDays, decimalDisplay, hundredths, shortDate } from "@/app/gross-board/board";
import { DriverCard, payButtonClass } from "./DriverCard";
import { SettlementDialog } from "./SettlementDialog";
import { useDebouncedValue } from "@/app/lib/useDebouncedValue";
import { applyPaySave, driverTotals, normalizedPayEdits, paySourceError, reconcilePaySave, validAdjustments } from "./pay";
import { restorePayEdit } from "./payUndo";
import { isPayReplacementInput } from "./payGrid";

type PayUndoAction = { kind: "edit"; id: string; before: DriverPayEdits; after: DriverPayEdits; group: number } | { kind: "source"; id: string };

export function WeeklyPayPage({ investor = false }: { investor?: boolean }) {
  const permissions = usePermissions();
  const restoringView = useRestoringView();
  const savePay = investor ? saveInvestorPay : saveDriverPay;
  const router = useRouter();
  const params = useSearchParams();
  const requested = params.get("weekStart");
  const targetId = params.get(investor ? "truckId" : "driverId") ?? params.get("driverId");
  const targetKey = investor && params.has("truckId") ? "truckId" : "driverId";
  const initialWeek = requested && /^\d{4}-\d{2}-\d{2}$/.test(requested) && requested >= "2000-01-03" && requested <= "2100-12-27" && new Date(`${requested}T12:00:00Z`).getUTCDay() === 1 ? requested : undefined;
  const [week, setWeek] = useViewState("page:week", () => currentChargeWeek(), initialWeek);
  const [driverFilter, setDriverFilter] = useViewState("page:driverFilter", "", params.has(targetKey) ? targetId ?? "" : undefined);
  const [settlement, setSettlement] = useState<{driverId?: string; reopen: boolean} | null>(null);
  useEffect(() => {
    const timer = setTimeout(() => {
    if (initialWeek) setWeek(initialWeek);
    if (params.has(targetKey)) setDriverFilter(targetId ?? "");
    }, 0); return () => clearTimeout(timer);
  }, [setDriverFilter, setWeek, initialWeek, params, targetKey, targetId]);
  const [settlementReport, setSettlementReport] = useState<DriverPayWeek | null>(null);
  const [page, setPage] = useViewState("page:page", 1, targetId ? 1 : undefined);
  const [pageSize, setPageSize] = useViewState("page:pageSize", 25);
  const [report, setReport] = useState<DriverPayWeek | null>(null);
  const [changes, setChanges] = useState<Record<string, DriverPayEdits>>({});
  const [search, setSearch] = useViewState("page:search", "", targetId && !restoringView ? "" : undefined);
  const [dispatcher, setDispatcher] = useViewState("page:dispatcher", "all", targetId && !restoringView ? "all" : undefined);
  const debouncedSearch = useDebouncedValue(search);
  const fetchPay = useCallback((selectedWeek: string, signal?: AbortSignal) => {
    const query = { page, pageSize, search: debouncedSearch, dispatcherId: dispatcher === "all" ? "" : dispatcher, statementId: driverFilter };
    return investor ? fetchInvestorPay(selectedWeek, driverFilter || undefined, signal, query) : fetchDriverPay(selectedWeek, signal, query);
  }, [investor, page, pageSize, debouncedSearch, dispatcher, driverFilter]);
  const loadedQuery = useRef("");
  const queryKey = JSON.stringify([week, page, pageSize, debouncedSearch, dispatcher, driverFilter]);
  const [opened, setOpened] = useViewState<Set<string>>("page:opened", new Set(), targetId && !restoringView ? new Set([targetId]) : undefined);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [pendingWeek, setPendingWeek] = useState<string | null>(null);
  const [pendingLink, setPendingLink] = useState<string | null>(null);
  const savedRef = useRef<Record<string, DriverPayEdits>>({});
  const savingRef = useRef(false);
  const changesRef = useRef(changes);
  const undoStack = useRef<PayUndoAction[]>([]);
  const editGroup = useRef(0);
  const pendingUndo = useRef(false);
  const undoLatest = useRef<() => Promise<void>>(async () => {});
  const dirty = Object.keys(changes).length > 0;
  const sourceError = Object.values(changes).map(edits => paySourceError(edits, report?.drivers.find(row => row.id === edits.driverId))).find(Boolean);
  const invalid = Object.values(changes).some(edits => !validAdjustments(edits, report?.drivers.find(row => row.id === edits.driverId)));
  useLayoutEffect(() => { changesRef.current = changes; }, [changes]);
  useLayoutEffect(() => { undoStack.current = []; pendingUndo.current = false; }, [queryKey]);
  useEffect(() => {
    const focus = () => { editGroup.current += 1; };
    document.addEventListener("focusin", focus);
    return () => document.removeEventListener("focusin", focus);
  }, []);

  useLayoutEffect(() => { savedRef.current = Object.fromEntries((report?.drivers ?? []).map(d => [d.id, d.edits])); }, [report]);
  useEffect(() => {
    if (dirty || saving || refreshing || loadedQuery.current === queryKey) return;
    let cancelled = false;
    const controller = new AbortController();
    const timer = setTimeout(() => {
      setLoading(true);
      fetchPay(week, controller.signal).then(value => {
        if (!cancelled) { loadedQuery.current = queryKey; setReport(value); setError(""); }
      }).catch(err => { if (!cancelled) setError(err instanceof Error ? err.message : "Unable to load payroll"); })
        .finally(() => { if (!cancelled) setLoading(false); });
    }, 0);
    return () => { cancelled = true; controller.abort(); clearTimeout(timer); };
  }, [week, fetchPay, queryKey, dirty, saving, refreshing]);
  const edit = useCallback((id: string, update: (edits: DriverPayEdits) => DriverPayEdits) => {
    const before = changesRef.current[id] ?? savedRef.current[id];
    const after = update(before);
    if (JSON.stringify(before) === JSON.stringify(after)) return;
    const last = undoStack.current.at(-1);
    const typing = document.activeElement instanceof HTMLInputElement;
    if (typing && last?.kind === "edit" && last.id === id && last.group === editGroup.current) last.after = after;
    else undoStack.current.push({ kind: "edit", id, before, after, group: editGroup.current });
    if (undoStack.current.length > 100) undoStack.current.shift();
    changesRef.current = { ...changesRef.current, [id]: after };
    setChanges(changesRef.current); setMessage("");
  }, []);
  const toggle = useCallback((id: string) => setOpened(current => { const next = new Set(current); if (next.has(id)) next.delete(id); else next.add(id); return next; }), [setOpened]);

  const save = useCallback(async () => {
    if (savingRef.current || invalid || !dirty || loading || refreshing) return;
    savingRef.current = true; setSaving(true); setError("");
    try {
      for (const submitted of Object.values(changes)) {
        const saved = await savePay(normalizedPayEdits(submitted));
        setReport(current => current && applyPaySave(current, saved));
        setChanges(current => reconcilePaySave(current, submitted, saved));
      }
      setMessage("All changes saved");
    } catch (err) { setError(err instanceof Error ? err.message : "Unable to save; your edits are still here."); }
    finally { savingRef.current = false; setSaving(false); }
  }, [changes, dirty, invalid, loading, refreshing, savePay]);
  useEffect(() => {
    if (!dirty || invalid || saving || loading || refreshing || error) return;
    const timer = setTimeout(() => { void save(); }, 0);
    return () => clearTimeout(timer);
  }, [dirty, invalid, saving, loading, refreshing, error, save, pendingWeek, pendingLink]);
  useLayoutEffect(() => {
    undoLatest.current = async () => {
      if (loading || refreshing || pendingWeek || pendingLink || !permissions.includes("payroll.write")) return;
      if (savingRef.current) { pendingUndo.current = true; return; }
      const action = undoStack.current.at(-1);
      if (!action) return;
      if (action.kind === "source") {
        if (dirty) return;
        setRefreshing(true); setError("");
        try {
          await undoPaySystemValues(investor, action.id);
          undoStack.current.pop();
          const updated = await fetchPay(week);
          // A source refresh may also reveal another accountant's payroll save.
          undoStack.current = undoStack.current.filter(item => item.kind !== "edit" || updated.drivers.find(row => row.id === item.id)?.edits.version === savedRef.current[item.id]?.version);
          setReport(updated); setMessage("Change undone");
        }
        catch (err) { setError(err instanceof Error ? err.message : "Unable to undo this change"); }
        finally { setRefreshing(false); }
        return;
      }
      const driver = report?.drivers.find(row => row.id === action.id);
      if (!driver || driver.settlement?.finalized) return;
      const current = changesRef.current[action.id] ?? savedRef.current[action.id];
      const restored = restorePayEdit(current, action.before, action.after);
      const next = { ...changesRef.current, [action.id]: restored };
      if (JSON.stringify(normalizedPayEdits(restored)) === JSON.stringify(normalizedPayEdits(savedRef.current[action.id]))) delete next[action.id];
      undoStack.current.pop(); editGroup.current += 1;
      changesRef.current = next; setChanges(next); setError(""); setMessage("Change undone");
    };
  });
  useEffect(() => {
    const key = (event: KeyboardEvent) => {
      if (!(event.ctrlKey || event.metaKey) || event.altKey || event.shiftKey || event.repeat || event.key.toLowerCase() !== "z") return;
      const target = event.target as HTMLElement;
      const editableText = target.closest('input:not([readonly]):not(:disabled),textarea,[contenteditable="true"]');
        const replacing = isPayReplacementInput(target);
        if (target.closest('[role="dialog"]') || (editableText && !replacing && (dirty || !target.closest('[data-payroll-scroll]')))) return;
      if (!undoStack.current.length) return;
        if (replacing) target.closest<HTMLElement>("td")?.focus();
      event.preventDefault(); void undoLatest.current();
    };
    document.addEventListener("keydown", key);
    return () => document.removeEventListener("keydown", key);
  }, [dirty]);
  useEffect(() => {
    if (saving || !pendingUndo.current) return;
    pendingUndo.current = false;
    void undoLatest.current();
  }, [saving]);
  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => event.preventDefault();
    const leave = (event: MouseEvent) => {
      const link = (event.target as Element).closest?.("a[href]");
      if (!link || event.ctrlKey || event.metaKey || event.shiftKey || event.button !== 0) return;
      if (dirty) { event.preventDefault(); event.stopPropagation(); setPendingLink(link.getAttribute("href")); }
    };
    if (dirty) window.addEventListener("beforeunload", beforeUnload); document.addEventListener("click", leave, true);
    return () => { window.removeEventListener("beforeunload", beforeUnload); document.removeEventListener("click", leave, true); };
  }, [dirty]);
  useEffect(() => {
    if (dirty || saving || loading || refreshing || error) return;
    if (pendingLink) {
      if (pendingLink.startsWith("/") && !pendingLink.startsWith("//")) router.push(pendingLink, { scroll: false });
      else window.location.assign(pendingLink);
      return;
    }
    if (pendingWeek) {
      const timer = setTimeout(() => { setReport(null); setLoading(true); setWeek(pendingWeek); setPage(1); setPendingWeek(null); setOpened(new Set()); setMessage(""); }, 0);
      return () => clearTimeout(timer);
    }
  }, [pendingWeek, pendingLink, dirty, saving, loading, refreshing, error, setOpened, setWeek, setPage, router]);
  async function reload(refreshSources = false) {
    if (dirty && !window.confirm("Discard unsaved payroll edits and reload this week?")) return;
    undoStack.current = []; pendingUndo.current = false;
    setChanges({}); setError(""); setMessage(""); setPendingWeek(null); setPendingLink(null);
    if (refreshSources) setRefreshing(true); else setLoading(true);
    try { loadedQuery.current = queryKey; setReport(await (refreshSources ? refreshDriverPayLoads(week).then(() => fetchPay(week)) : fetchPay(week))); setMessage(refreshSources ? "Load details refreshed" : "Reloaded"); }
    catch (err) { setError(err instanceof Error ? err.message : "Unable to reload this week"); }
    finally { setLoading(false); setRefreshing(false); }
  }
  async function prepareSettlement(reopen: boolean, driverId?: string) {
    if (dirty || saving || loading || refreshing) return;
    setRefreshing(true); setError("");
    try {
      // Autosave changes the revision. Preview the latest complete report before
      // accepting a finalization, including changes by other accounting users.
      const current = await (investor ? fetchInvestorPay(week, driverFilter || undefined) : fetchDriverPay(week));
      setSettlementReport(current); setSettlement({ driverId, reopen });
    } catch (err) { setError(err instanceof Error ? err.message : "Unable to prepare settlement"); }
    finally { setRefreshing(false); }
  }
  async function acceptSource(load: DriverPayLoad, sourceDriverId: string, field: "originalRate" | "totalMiles") {
    if (dirty || saving || loading || refreshing || !load.loadRecordId || !load.boardVersion) return;
    setRefreshing(true); setError("");
    try {
      const receipt = await acceptPaySystemValues(investor, { field, driverId: load.sourceDriverId ?? sourceDriverId, date: load.date, slot: load.slot, version: load.boardVersion, loadRecordId: load.loadRecordId, originalRate: field === "originalRate" ? load.systemOriginalRate ?? "" : "", miles: field === "totalMiles" ? load.systemMiles ?? "" : "" });
      undoStack.current.push({ kind: "source", id: receipt.undoId });
      const updated = await fetchPay(week);
      undoStack.current = undoStack.current.filter(item => item.kind !== "edit" || updated.drivers.find(row => row.id === item.id)?.edits.version === savedRef.current[item.id]?.version);
      setReport(updated); setMessage("System values accepted");
    } catch (err) { setError(err instanceof Error ? err.message : "Unable to accept system values"); }
    finally { setRefreshing(false); }
  }
  const drivers = useMemo(() => report?.drivers ?? [], [report]);
  const dispatchers = useMemo(() => report?.pagination?.dispatchers.map(d => [d.id, d.name] as const) ?? Array.from(new Map((report?.drivers ?? []).map(d => [d.dispatcherId, d.dispatcherName])).entries()), [report]);
  const pageSummary = drivers.reduce((sum, d) => {
    const values = driverTotals(d, changes[d.id] ?? d.edits);
    return { fee: sum.fee + values.fee, payable: sum.payable + values.payable, review: sum.review + values.review, loads: sum.loads + d.loads.length };
  }, { fee: BigInt(0), payable: BigInt(0), review: 0, loads: 0 });
  const summary = report?.pagination ? {
    fee: hundredths(report.pagination.fee), payable: hundredths(report.pagination.payable) + drivers.reduce((sum, d) => sum + driverTotals(d, changes[d.id] ?? d.edits).payable - driverTotals(d, d.edits).payable, BigInt(0)), review: report.pagination.review, loads: report.pagination.loads,
  } : pageSummary;
  const busy = loading || refreshing;
  const setupRequired = investor ? report?.setupRequired ?? [] : [];
  const rowOrder = new Map(report?.pagination?.rowIds?.map((id, index) => [id, index]));
  const rows = [
    ...drivers.map(driver => ({ kind: "statement" as const, id: driver.id, name: driver.fullName, unit: driver.truckUnit, driver })),
    ...setupRequired.map(truck => ({ kind: "setup" as const, id: truck.truckId, name: truck.ownerName, unit: truck.truckUnit, truck })),
  ].sort((a, b) => rowOrder.size ? (rowOrder.get(a.id) ?? 0) - (rowOrder.get(b.id) ?? 0) : a.name.localeCompare(b.name) || a.unit.localeCompare(b.unit, undefined, { numeric: true }));
  const switchWeek = (next: string) => { if (next !== week) { if (!dirty) setError(""); setPendingWeek(next); } };
  return <div className="space-y-5 animate-fade-in">
    <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-end">
      <PageHeader><div><div className="flex items-center gap-3"><UserRound className="h-5 w-5 text-zinc-500" /><h1 className="text-lg font-semibold text-zinc-100">{investor ? "Investor pay" : "Driver pay"}</h1><span className="rounded-full bg-zinc-800/60 px-2.5 py-0.5 text-xs text-zinc-400">{loading ? <SkeletonBar className="h-3 w-4" /> : report?.pagination?.total ?? rows.length}</span></div></div></PageHeader>
      <div className="flex flex-wrap items-center gap-2"><span role="status" title={invalid ? "Fuel/Toll: enter a number (zero is allowed) or reset to automatic. Other entries: enter a name and nonzero amount, or clear both cells. Positive amounts are reimbursements; negative amounts are charges." : undefined} className={`flex h-8 w-48 shrink-0 items-center gap-1.5 whitespace-nowrap text-xs ${error && dirty ? "text-red-300" : invalid ? "text-amber-300" : "text-zinc-500"}`}>{invalid ? <AlertTriangle className="h-4 w-4 shrink-0" /> : <CloudCheck className="h-4 w-4 shrink-0" />}{error && dirty ? "Not saved" : saving ? "Saving…" : refreshing ? "Updating…" : invalid ? "Complete adjustment entries" : dirty ? "Waiting to save…" : loading ? "Loading…" : message || "Saved automatically"}</span><button className={payButtonClass} disabled={saving || busy} onClick={() => void reload()}><RefreshCw className="h-3.5 w-3.5" />Reload</button><button className={payButtonClass} disabled={saving || busy || dirty || !report || !drivers.length} onClick={() => void reload(true)}>{refreshing ? "Refreshing…" : "Refresh load details"}</button></div>
    </div>
    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4"><MetricCard loading={loading} compact label={investor ? "Trucks / loads" : "Drivers / loads"} value={loading ? "—" : `${report?.pagination?.total ?? rows.length} / ${summary.loads}`} icon={UsersRound} /><MetricCard loading={loading} compact label={investor ? "Investor share" : "Earned pay"} value={loading ? "—" : decimalDisplay(summary.fee, true)} icon={Banknote} /><MetricCard loading={loading} compact label="Loads needing review" value={loading ? "—" : String(summary.review)} icon={AlertTriangle} /><MetricCard loading={loading} compact label={summary.review ? "Provisional payable" : "Total payable"} value={loading ? "—" : decimalDisplay(summary.payable, true)} icon={Banknote} /></div>
    <div className="flex flex-wrap items-center gap-3">
      <div className="flex items-center gap-1 rounded-lg border border-zinc-800 bg-card p-1"><button className={payButtonClass} aria-label="Previous week" disabled={busy || week <= "2000-01-03"} onClick={() => switchWeek(addDays(week, -7))}><ChevronLeft className="h-4 w-4" /></button><span className="min-w-36 px-2 text-center font-mono text-sm text-zinc-100">{shortDate(week)}–{shortDate(addDays(week, 6))}</span><button className={payButtonClass} aria-label="Next week" disabled={busy || week >= "2100-12-27"} onClick={() => switchWeek(addDays(week, 7))}><ChevronRight className="h-4 w-4" /></button></div>
      <span className="text-xs text-zinc-500">{week.slice(0, 4)}{week.slice(0, 4) !== addDays(week, 6).slice(0, 4) ? ` / ${addDays(week, 6).slice(0, 4)}` : ""}</span><button className={payButtonClass} disabled={busy} onClick={() => switchWeek(currentChargeWeek())}>This week</button><div className="min-w-48 flex-1"><ManagementSearch value={search} onChange={value => { setSearch(value); setPage(1); }} placeholder={investor ? "Investor, truck, or load…" : "Driver, truck, or load…"} /></div><label className="flex items-center gap-2 text-xs text-zinc-400">Dispatcher<select className={`${controlClass} !w-44`} value={dispatcher} disabled={busy || dirty || saving} onChange={event => { setDispatcher(event.target.value); setPage(1); }}><option value="all">All dispatchers</option>{dispatchers.map(([id, name]) => <option key={id} value={id || "__unassigned"}>{name}</option>)}</select></label><button type="button" className={payButtonClass} onClick={() => setOpened(new Set())}>{investor ? "Collapse truck details" : "Collapse all"}</button>
    </div>
    <div className="flex flex-wrap items-center gap-2 text-xs text-zinc-500">{loading ? <SkeletonBar className="h-3 w-48" /> : <span>{report?.pagination?.finalized ?? report?.drivers.filter(d => !d.truckInactive && d.settlement?.finalized).length ?? 0} of {report?.pagination?.statements ?? report?.drivers.filter(d => !d.truckInactive).length ?? 0} settlements finalized</span>}<button className={payButtonClass} disabled={busy || dirty || saving || !(report?.pagination ? report.pagination.statements > report.pagination.finalized : report?.drivers.some(d => !d.truckInactive && !d.settlement?.finalized)) || week > currentChargeWeek()} onClick={() => void prepareSettlement(false)}>Finalize week</button><button className={payButtonClass} disabled={busy || dirty || saving || !(report?.pagination ? report.pagination.finalized > 0 : report?.drivers.some(d => !d.truckInactive && d.settlement?.finalized))} onClick={() => void prepareSettlement(true)}>Reopen week</button>{driverFilter && <button className="text-blue-400" onClick={() => { setDriverFilter(""); setPage(1); }} disabled={busy || dirty || saving}>{investor ? "Show all trucks" : "Show all drivers"}</button>}</div>
    {report?.issues?.map((issue, i) => <p key={i} role="alert" className="rounded border border-amber-500/20 bg-amber-500/5 p-3 text-xs text-amber-300">{issue}</p>)}
    {error && <div role="alert" className="rounded-lg border border-red-500/20 bg-red-500/10 p-3 text-sm text-red-300">{error} {dirty && "Your unsaved edits are still here."}<button className={`${payButtonClass} ml-3`} disabled={saving || busy} onClick={() => dirty ? void save() : void reload()}>{dirty ? "Retry save" : "Retry"}</button>{dirty && <button className={`${payButtonClass} ml-2`} disabled={saving || busy} onClick={() => void reload()}>Reload saved version</button>}</div>}
    {sourceError && <p role="alert" className="text-sm text-red-300">{sourceError} This edit has not been saved.</p>}
    {(pendingWeek || pendingLink) && dirty && <p role="status" className="text-xs text-amber-300">{invalid ? "Correct invalid amounts or remove unfinished adjustments; enter zero or reset Fuel/Toll before leaving this week." : "Saving edits before leaving this week…"}{error && " Resolve the save error to continue."}<button className="ml-2 underline" onClick={() => { setPendingWeek(null); setPendingLink(null); }}>Stay here</button></p>}

    {loading ? <WeeklyTableSkeleton /> : report && rows.length === 0 ? <div className="rounded-xl border border-zinc-800 p-12 text-center text-sm text-zinc-500">{search || dispatcher !== "all" ? investor ? "No trucks match these filters." : "No drivers match these filters." : "No loads or adjustments for this week."}</div> : <div data-payroll-scroll="drivers" className="weekly-content-enter overflow-x-auto rounded-lg border border-zinc-800"><table className="w-full min-w-[960px] table-fixed border-separate border-spacing-0" aria-label={investor ? "Weekly investor pay" : "Weekly driver pay"}><colgroup><col style={{ width: "24%" }} /><col style={{ width: "10%" }} /><col style={{ width: "15%" }} /><col style={{ width: "17%" }} /><col style={{ width: "16%" }} /><col style={{ width: "6%" }} /><col style={{ width: "12%" }} /></colgroup><thead><tr className="h-8 bg-zinc-900 text-left text-[11px] text-zinc-500">{[investor ? "Investor" : "Driver", "Truck", investor ? "Driver" : "Driver type", investor ? "Dispatcher" : "Tariff", investor ? "Tariff" : "Dispatcher", "Loads", "Total payable"].map((label, i) => <th key={label} className={`border-b border-zinc-800 px-3 font-medium ${i >= 5 ? "text-right" : ""}`}>{label}</th>)}</tr></thead><tbody>{rows.map(row => row.kind === "statement" ? <DriverCard key={`${week}:${row.driver.id}`} returnToPay driver={row.driver} edits={changes[row.driver.id] ?? row.driver.edits} open={opened.has(row.driver.id)} onToggle={toggle} onEdit={edit} disabled={busy || !!row.driver.settlement?.finalized} chargeActionsDisabled={busy || dirty || saving || !!row.driver.settlement?.finalized} settlementDisabled={busy || dirty || saving} onSettlement={reopen => void prepareSettlement(reopen, row.driver.id)} onReload={() => reload()} sourceActionDisabled={busy || dirty || saving || !!row.driver.settlement?.finalized} onAcceptSource={(load, field) => acceptSource(load, row.driver.id, field)} /> : <InvestorSetupRow key={`${week}:${row.id}`} truck={row.truck} canConfigure={permissions.includes("charges.read")} />)}</tbody></table></div>}
    {report?.pagination && !loading && <fieldset disabled={busy || dirty || saving} className="min-w-0"><TablePagination page={report.pagination.page} pageSize={pageSize} totalItems={report.pagination.total} totalPages={report.pagination.totalPages} onPageChange={setPage} onPageSizeChange={value => { setPageSize(value); setPage(1); }} /></fieldset>}
    {settlement && settlementReport && <SettlementDialog investor={investor} report={settlementReport} {...settlement} onClose={() => setSettlement(null)} onSaved={() => { setSettlement(null); setSettlementReport(null); loadedQuery.current = ""; void reload(); }} />}
  </div>;
}

function InvestorSetupRow({ truck, canConfigure }: { truck: NonNullable<DriverPayWeek["setupRequired"]>[number]; canConfigure: boolean }) {
  return <tr className="h-9 bg-card text-xs text-zinc-300">
    <th scope="row" className="border-b border-zinc-800 px-3 py-0 text-left font-medium"><div className="flex h-9 items-center gap-2 text-zinc-100"><span aria-hidden className="w-4 shrink-0" /><Link className="truncate" href={`/investors/detail?id=${truck.ownerId}`}>{truck.ownerName}</Link></div></th>
    <td className="border-b border-zinc-800 px-3"><Link href={`/trucks/detail?id=${truck.truckId}`}>{truck.truckUnit}</Link></td>
    <td className="px-3 text-zinc-400">{truck.driverId ? <Link href={`/drivers/detail?id=${truck.driverId}`}>{truck.driverName}</Link> : "—"}</td><td className="px-3 text-zinc-400">{truck.dispatcherName || "—"}</td>
    <td className="px-3 text-amber-300">{canConfigure ? <Link href="/accounting/driver-charges?tab=trucks">Setup required</Link> : "Setup required"}</td>
    <td className="border-b border-zinc-800 px-3 text-right text-zinc-400">—</td><td className="border-b border-zinc-800 px-3 text-right text-zinc-400">—</td>
  </tr>;
}
