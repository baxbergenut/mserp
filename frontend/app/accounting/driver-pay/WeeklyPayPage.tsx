"use client";

import { PageHeader } from "@/app/components/PageHeader";
import { usePermissions } from "@/app/lib/access";
import { IntentLink as Link } from "@/app/components/IntentLink";

import { useRouter, useSearchParams } from "next/navigation";
import { useViewState } from "@/app/lib/viewMemory";

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { AlertTriangle, Banknote, ChevronLeft, ChevronRight, CloudCheck, RefreshCw, UserRound, UsersRound } from "lucide-react";
import { fetchDriverPay, fetchInvestorPay, refreshDriverPayLoads, saveDriverPay, saveInvestorPay } from "@/app/lib/api";
import type { DriverPayEdits, DriverPayWeek } from "@/app/lib/types";
import { currentChargeWeek } from "../driver-charges/charges";
import { SkeletonBar, WeeklyTableSkeleton } from "@/app/components/WeeklyTableSkeleton";
import { MetricCard } from "@/app/components/MetricCard";
import { ManagementSearch, controlClass } from "@/app/components/management/ManagementUI";
import { addDays, decimalDisplay, shortDate } from "@/app/gross-board/board";
import { DriverCard, payButtonClass } from "./DriverCard";
import { SettlementDialog } from "./SettlementDialog";
import { driverTotals, normalizedPayEdits, reconcilePaySave, validAdjustments } from "./pay";

export function WeeklyPayPage({ investor = false }: { investor?: boolean }) {
  const permissions = usePermissions();
  const fetchPay = investor ? fetchInvestorPay : fetchDriverPay;
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
  const [report, setReport] = useState<DriverPayWeek | null>(null);
  const [changes, setChanges] = useState<Record<string, DriverPayEdits>>({});
  const [search, setSearch] = useViewState("page:search", "", targetId ? "" : undefined);
  const [dispatcher, setDispatcher] = useViewState("page:dispatcher", "all", targetId ? "all" : undefined);
  const [opened, setOpened] = useViewState<Set<string>>("page:opened", new Set(), targetId ? new Set([targetId]) : undefined);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [pendingWeek, setPendingWeek] = useState<string | null>(null);
  const [pendingLink, setPendingLink] = useState<string | null>(null);
  const savedRef = useRef<Record<string, DriverPayEdits>>({});
  const savingRef = useRef(false);
  const dirty = Object.keys(changes).length > 0;
  const invalid = Object.values(changes).some(edits => !validAdjustments(edits));

  useLayoutEffect(() => { savedRef.current = Object.fromEntries((report?.drivers ?? []).map(d => [d.id, d.edits])); }, [report]);
  useEffect(() => {
    let cancelled = false;
    fetchPay(week).then(value => { if (!cancelled) { setReport(value); setError(""); } })
      .catch(err => { if (!cancelled) setError(err instanceof Error ? err.message : "Unable to load driver pay"); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [week, fetchPay]);
  const edit = useCallback((id: string, update: (edits: DriverPayEdits) => DriverPayEdits) => {
    setChanges(current => ({ ...current, [id]: update(current[id] ?? savedRef.current[id]) })); setMessage("");
  }, []);
  const toggle = useCallback((id: string) => setOpened(current => { const next = new Set(current); if (next.has(id)) next.delete(id); else next.add(id); return next; }), [setOpened]);

  const save = useCallback(async () => {
    if (savingRef.current || invalid || !dirty || loading || refreshing) return;
    savingRef.current = true; setSaving(true); setError("");
    try {
      for (const submitted of Object.values(changes)) {
        const saved = await savePay(normalizedPayEdits(submitted));
        setReport(current => current && ({ ...current, drivers: current.drivers.map(d => d.id === saved.driverId ? { ...d, edits: saved } : d) }));
        setChanges(current => reconcilePaySave(current, submitted, saved));
      }
      setMessage("All changes saved");
    } catch (err) { setError(err instanceof Error ? err.message : "Unable to save; your edits are still here."); }
    finally { savingRef.current = false; setSaving(false); }
  }, [changes, dirty, invalid, loading, refreshing, savePay]);
  useEffect(() => {
    if (!dirty || invalid || saving || loading || refreshing || error) return;
    const timer = setTimeout(() => { void save(); }, pendingWeek || pendingLink ? 0 : 750);
    return () => clearTimeout(timer);
  }, [dirty, invalid, saving, loading, refreshing, error, save, pendingWeek, pendingLink]);
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
      const timer = setTimeout(() => { setReport(null); setLoading(true); setWeek(pendingWeek); setPendingWeek(null); setOpened(new Set()); setMessage(""); }, 0);
      return () => clearTimeout(timer);
    }
  }, [pendingWeek, pendingLink, dirty, saving, loading, refreshing, error, setOpened, setWeek, router]);
  async function reload(refreshSources = false) {
    if (dirty && !window.confirm("Discard unsaved payroll edits and reload this week?")) return;
    setChanges({}); setError(""); setMessage(""); setPendingWeek(null); setPendingLink(null);
    if (refreshSources) setRefreshing(true); else setLoading(true);
    try { setReport(await (refreshSources ? refreshDriverPayLoads(week).then(() => fetchPay(week)) : fetchPay(week))); setMessage(refreshSources ? "Load details refreshed" : "Reloaded"); }
    catch (err) { setError(err instanceof Error ? err.message : "Unable to reload this week"); }
    finally { setLoading(false); setRefreshing(false); }
  }
  async function prepareSettlement(reopen: boolean, driverId?: string) {
    if (dirty || saving || loading || refreshing) return;
    setRefreshing(true); setError("");
    try {
      // Autosave changes the revision. Preview the latest complete report before
      // accepting a finalization, including changes by other accounting users.
      const current = await fetchPay(week);
      setReport(current); setSettlement({ driverId, reopen });
    } catch (err) { setError(err instanceof Error ? err.message : "Unable to prepare settlement"); }
    finally { setRefreshing(false); }
  }
  const drivers = useMemo(() => (report?.drivers ?? []).filter(d => {
    const query = search.trim().toLowerCase();
    return (!driverFilter || d.id === driverFilter) && (dispatcher === "all" || d.dispatcherId === dispatcher) && (!query || d.fullName.toLowerCase().includes(query) || d.truckUnit.toLowerCase().includes(query) || d.loads.some(l => l.loadNumber.toLowerCase().includes(query)));
  }), [report, dispatcher, search, driverFilter]);
  const dispatchers = useMemo(() => Array.from(new Map((report?.drivers ?? []).map(d => [d.dispatcherId, d.dispatcherName])).entries()), [report]);
  const summary = drivers.reduce((sum, d) => {
    const values = driverTotals(d, changes[d.id] ?? d.edits);
    return { fee: sum.fee + values.fee, payable: sum.payable + values.payable, review: sum.review + values.review, loads: sum.loads + d.loads.length };
  }, { fee: BigInt(0), payable: BigInt(0), review: 0, loads: 0 });
  const busy = loading || refreshing;
  const setupRequired = (investor ? report?.setupRequired ?? [] : []).filter(t => (dispatcher === "all" || dispatcher === "") && (!driverFilter || t.truckId === driverFilter) && (!search.trim() || `${t.ownerName} ${t.truckUnit}`.toLowerCase().includes(search.trim().toLowerCase())));
  const rows = [
    ...drivers.map(driver => ({ kind: "statement" as const, id: driver.id, name: driver.fullName, unit: driver.truckUnit, driver })),
    ...setupRequired.map(truck => ({ kind: "setup" as const, id: truck.truckId, name: truck.ownerName, unit: truck.truckUnit, truck })),
  ].sort((a, b) => a.name.localeCompare(b.name) || a.unit.localeCompare(b.unit, undefined, { numeric: true }));
  const switchWeek = (next: string) => { if (next !== week) { if (!dirty) setError(""); setPendingWeek(next); } };
  return <div className="space-y-5 animate-fade-in">
    <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-end">
      <PageHeader><div><div className="flex items-center gap-3"><UserRound className="h-5 w-5 text-zinc-500" /><h1 className="text-lg font-semibold text-zinc-100">{investor ? "Investor pay" : "Driver pay"}</h1><span className="rounded-full bg-zinc-800/60 px-2.5 py-0.5 text-xs text-zinc-400">{loading ? <SkeletonBar className="h-3 w-4" /> : rows.length}</span></div></div></PageHeader>
      <div className="flex flex-wrap items-center gap-2"><span role="status" title={invalid ? "Fuel/Toll: enter a number (zero is allowed) or reset to automatic. Other entries: enter a name and nonzero amount, or clear both cells. Positive amounts are reimbursements; negative amounts are charges." : undefined} className={`flex h-8 w-48 shrink-0 items-center gap-1.5 whitespace-nowrap text-xs ${error && dirty ? "text-red-300" : invalid ? "text-amber-300" : "text-zinc-500"}`}>{invalid ? <AlertTriangle className="h-4 w-4 shrink-0" /> : <CloudCheck className="h-4 w-4 shrink-0" />}{error && dirty ? "Not saved" : saving ? "Saving…" : invalid ? "Complete adjustment entries" : dirty ? "Waiting to save…" : loading ? "Loading…" : message || "Saved automatically"}</span><button className={payButtonClass} disabled={saving || busy} onClick={() => void reload()}><RefreshCw className="h-3.5 w-3.5" />Reload</button><button className={payButtonClass} disabled={saving || busy || dirty || !report || !drivers.length} onClick={() => void reload(true)}>{refreshing ? "Refreshing…" : "Refresh load details"}</button></div>
    </div>
    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4"><MetricCard loading={loading} compact label={investor ? "Trucks / loads" : "Drivers / loads"} value={loading ? "—" : `${rows.length} / ${summary.loads}`} icon={UsersRound} /><MetricCard loading={loading} compact label={investor ? "Investor share" : "Earned pay"} value={loading ? "—" : decimalDisplay(summary.fee, true)} icon={Banknote} /><MetricCard loading={loading} compact label="Loads needing review" value={loading ? "—" : String(summary.review)} icon={AlertTriangle} /><MetricCard loading={loading} compact label={summary.review ? "Provisional payable" : "Total payable"} value={loading ? "—" : decimalDisplay(summary.payable, true)} icon={Banknote} /></div>
    <div className="flex flex-wrap items-center gap-3">
      <div className="flex items-center gap-1 rounded-lg border border-zinc-800 bg-card p-1"><button className={payButtonClass} aria-label="Previous week" disabled={busy || week <= "2000-01-03"} onClick={() => switchWeek(addDays(week, -7))}><ChevronLeft className="h-4 w-4" /></button><span className="min-w-36 px-2 text-center font-mono text-sm text-zinc-100">{shortDate(week)}–{shortDate(addDays(week, 6))}</span><button className={payButtonClass} aria-label="Next week" disabled={busy || week >= "2100-12-27"} onClick={() => switchWeek(addDays(week, 7))}><ChevronRight className="h-4 w-4" /></button></div>
      <span className="text-xs text-zinc-500">{week.slice(0, 4)}{week.slice(0, 4) !== addDays(week, 6).slice(0, 4) ? ` / ${addDays(week, 6).slice(0, 4)}` : ""}</span><button className={payButtonClass} disabled={busy} onClick={() => switchWeek(currentChargeWeek())}>This week</button><div className="min-w-48 flex-1"><ManagementSearch value={search} onChange={setSearch} placeholder={investor ? "Investor, truck, or load…" : "Driver, truck, or load…"} /></div><label className="flex items-center gap-2 text-xs text-zinc-400">Dispatcher<select className={`${controlClass} !w-44`} value={dispatcher} onChange={event => setDispatcher(event.target.value)}><option value="all">All dispatchers</option>{dispatchers.map(([id, name]) => <option key={id} value={id}>{name}</option>)}</select></label><button type="button" className={payButtonClass} onClick={() => setOpened(new Set())}>{investor ? "Collapse truck details" : "Collapse all"}</button>
    </div>
    <div className="flex flex-wrap items-center gap-2 text-xs text-zinc-500">{loading ? <SkeletonBar className="h-3 w-48" /> : <span>{report?.drivers.filter(d => d.settlement?.finalized).length ?? 0} of {report?.drivers.length ?? 0} settlements finalized</span>}<button className={payButtonClass} disabled={busy || dirty || saving || !report?.drivers.some(d => !d.settlement?.finalized) || week > currentChargeWeek()} onClick={() => void prepareSettlement(false)}>Finalize week</button><button className={payButtonClass} disabled={busy || dirty || saving || !report?.drivers.some(d => d.settlement?.finalized)} onClick={() => void prepareSettlement(true)}>Reopen week</button>{driverFilter && <button className="text-blue-400" onClick={() => setDriverFilter("")}>{investor ? "Show all trucks" : "Show all drivers"}</button>}</div>
    {report?.issues?.map((issue, i) => <p key={i} role="alert" className="rounded border border-amber-500/20 bg-amber-500/5 p-3 text-xs text-amber-300">{issue}</p>)}
    {error && <div role="alert" className="rounded-lg border border-red-500/20 bg-red-500/10 p-3 text-sm text-red-300">{error} {dirty && "Your unsaved edits are still here."}<button className={`${payButtonClass} ml-3`} disabled={saving || busy} onClick={() => dirty ? void save() : void reload()}>{dirty ? "Retry save" : "Retry"}</button>{dirty && <button className={`${payButtonClass} ml-2`} disabled={saving || busy} onClick={() => void reload()}>Reload saved version</button>}</div>}
    {(pendingWeek || pendingLink) && dirty && <p role="status" className="text-xs text-amber-300">{invalid ? "Complete or remove unfinished adjustments; enter zero or reset Fuel/Toll before leaving this week." : "Saving edits before leaving this week…"}{error && " Resolve the save error to continue."}<button className="ml-2 underline" onClick={() => { setPendingWeek(null); setPendingLink(null); }}>Stay here</button></p>}
    {refreshing && <p role="status" className="text-xs text-blue-300">Refreshing this week’s report…</p>}

    {loading ? <WeeklyTableSkeleton /> : report && rows.length === 0 ? <div className="rounded-xl border border-zinc-800 p-12 text-center text-sm text-zinc-500">{search || dispatcher !== "all" ? investor ? "No trucks match these filters." : "No drivers match these filters." : "No loads or adjustments for this week."}</div> : <div data-payroll-scroll="drivers" className="weekly-content-enter overflow-x-auto rounded-lg border border-zinc-800"><table className="w-full min-w-[960px] table-fixed border-separate border-spacing-0" aria-label={investor ? "Weekly investor pay" : "Weekly driver pay"}><colgroup><col style={{ width: "24%" }} /><col style={{ width: "10%" }} /><col style={{ width: "15%" }} /><col style={{ width: "17%" }} /><col style={{ width: "16%" }} /><col style={{ width: "6%" }} /><col style={{ width: "12%" }} /></colgroup><thead><tr className="h-8 bg-zinc-900 text-left text-[11px] text-zinc-500">{[investor ? "Investor" : "Driver", "Truck", investor ? "Settlement" : "Driver type", "Tariff", "Dispatcher", "Loads", "Total payable"].map((label, i) => <th key={label} className={`border-b border-zinc-800 px-3 font-medium ${i >= 5 ? "text-right" : ""}`}>{label}</th>)}</tr></thead><tbody>{rows.map(row => row.kind === "statement" ? <DriverCard key={`${week}:${row.driver.id}`} returnToPay driver={row.driver} edits={changes[row.driver.id] ?? row.driver.edits} open={opened.has(row.driver.id)} onToggle={toggle} onEdit={edit} disabled={busy || !!row.driver.settlement?.finalized} chargeActionsDisabled={busy || dirty || saving || !!row.driver.settlement?.finalized} settlementDisabled={busy || dirty || saving} onSettlement={reopen => void prepareSettlement(reopen, row.driver.id)} onReload={() => reload()} /> : <InvestorSetupRow key={`${week}:${row.id}`} truck={row.truck} canConfigure={permissions.includes("charges.read")} />)}</tbody></table></div>}
    {settlement && report && <SettlementDialog investor={investor} report={report} {...settlement} onClose={() => setSettlement(null)} onSaved={value => { setReport(value); setSettlement(null); setMessage("Settlement updated"); }} />}
  </div>;
}

function InvestorSetupRow({ truck, canConfigure }: { truck: NonNullable<DriverPayWeek["setupRequired"]>[number]; canConfigure: boolean }) {
  return <tr className="h-8 bg-card text-xs text-zinc-300">
    <th scope="row" className="text-left font-medium"><div className="flex h-8 items-center gap-2 text-zinc-100"><span aria-hidden className="w-4 shrink-0" /><Link className="truncate" href={`/investors/detail?id=${truck.ownerId}`}>{truck.ownerName} {truck.truckUnit}</Link></div></th>
    <td><Link href={`/trucks/detail?id=${truck.truckId}`}>{truck.truckUnit}</Link></td>
    <td>Investor</td>
    <td className="text-amber-300">{canConfigure ? <Link href="/accounting/driver-charges?tab=trucks">Setup required</Link> : "Setup required"}</td>
    <td className="text-zinc-400">—</td><td className="text-right text-zinc-400">—</td><td className="text-right text-zinc-400">—</td>
  </tr>;
}
