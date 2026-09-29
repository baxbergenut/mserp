"use client";

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { AlertTriangle, Banknote, ChevronLeft, ChevronRight, CloudCheck, RefreshCw, UserRound, UsersRound } from "lucide-react";
import { fetchDriverPay, refreshDriverPayLoads, saveDriverPay } from "@/app/lib/api";
import type { DriverPayEdits, DriverPayWeek } from "@/app/lib/types";
import { currentChargeWeek } from "../driver-charges/charges";
import { MetricCard } from "@/app/components/MetricCard";
import { ManagementSearch, controlClass } from "@/app/components/management/ManagementUI";
import { addDays, decimalDisplay, shortDate } from "@/app/gross-board/board";
import { DriverCard, payButtonClass } from "./DriverCard";
import { SettlementDialog } from "./SettlementDialog";
import { driverTotals, normalizedPayEdits, reconcilePaySave, validAdjustments } from "./pay";

export default function DriverPayPage() {
  const [week, setWeek] = useState(() => currentChargeWeek());
  const [driverFilter, setDriverFilter] = useState("");
  const [settlement, setSettlement] = useState<{driverId?: string; reopen: boolean} | null>(null);
  useEffect(() => {
    const timer = setTimeout(() => {
    const params = new URLSearchParams(window.location.search); const requested = params.get("weekStart");
    if (requested && /^\d{4}-\d{2}-\d{2}$/.test(requested) && requested >= "2000-01-03" && requested <= "2100-12-27" && new Date(`${requested}T12:00:00Z`).getUTCDay() === 1) setWeek(requested);
    setDriverFilter(params.get("driverId") ?? "");
    }, 0); return () => clearTimeout(timer);
  }, []);
  const [report, setReport] = useState<DriverPayWeek | null>(null);
  const [changes, setChanges] = useState<Record<string, DriverPayEdits>>({});
  const [search, setSearch] = useState("");
  const [dispatcher, setDispatcher] = useState("all");
  const [opened, setOpened] = useState<Set<string>>(new Set());
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
    fetchDriverPay(week).then(value => { if (!cancelled) { setReport(value); setError(""); } })
      .catch(err => { if (!cancelled) setError(err instanceof Error ? err.message : "Unable to load driver pay"); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [week]);
  const edit = useCallback((id: string, update: (edits: DriverPayEdits) => DriverPayEdits) => {
    setChanges(current => ({ ...current, [id]: update(current[id] ?? savedRef.current[id]) })); setMessage("");
  }, []);
  const toggle = useCallback((id: string) => setOpened(current => { const next = new Set(current); if (next.has(id)) next.delete(id); else next.add(id); return next; }), []);

  const save = useCallback(async () => {
    if (savingRef.current || invalid || !dirty || loading || refreshing) return;
    savingRef.current = true; setSaving(true); setError("");
    try {
      for (const submitted of Object.values(changes)) {
        const saved = await saveDriverPay(normalizedPayEdits(submitted));
        setReport(current => current && ({ ...current, drivers: current.drivers.map(d => d.id === saved.driverId ? { ...d, edits: saved } : d) }));
        setChanges(current => reconcilePaySave(current, submitted, saved));
      }
      setMessage("All changes saved");
    } catch (err) { setError(err instanceof Error ? err.message : "Unable to save; your edits are still here."); }
    finally { savingRef.current = false; setSaving(false); }
  }, [changes, dirty, invalid, loading, refreshing]);
  useEffect(() => {
    if (!dirty || invalid || saving || loading || refreshing || error) return;
    const timer = setTimeout(() => { void save(); }, pendingWeek || pendingLink ? 0 : 5000);
    return () => clearTimeout(timer);
  }, [dirty, invalid, saving, loading, refreshing, error, save, pendingWeek, pendingLink]);
  useEffect(() => {
    if (!dirty) return;
    const beforeUnload = (event: BeforeUnloadEvent) => event.preventDefault();
    const leave = (event: MouseEvent) => {
      const link = (event.target as Element).closest?.("a[href]");
      if (link && !event.ctrlKey && !event.metaKey && !event.shiftKey) { event.preventDefault(); event.stopPropagation(); setPendingLink(link.getAttribute("href")); }
    };
    window.addEventListener("beforeunload", beforeUnload); document.addEventListener("click", leave, true);
    return () => { window.removeEventListener("beforeunload", beforeUnload); document.removeEventListener("click", leave, true); };
  }, [dirty]);
  useEffect(() => {
    if (dirty || saving || loading || refreshing || error) return;
    if (pendingLink) { window.location.assign(pendingLink); return; }
    if (pendingWeek) {
      const timer = setTimeout(() => { setReport(null); setLoading(true); setWeek(pendingWeek); setPendingWeek(null); setOpened(new Set()); setMessage(""); }, 0);
      return () => clearTimeout(timer);
    }
  }, [pendingWeek, pendingLink, dirty, saving, loading, refreshing, error]);
  async function reload(refreshSources = false) {
    if (dirty && !window.confirm("Discard unsaved payroll edits and reload this week?")) return;
    setChanges({}); setError(""); setMessage(""); setPendingWeek(null); setPendingLink(null);
    if (refreshSources) setRefreshing(true); else setLoading(true);
    try { setReport(await (refreshSources ? refreshDriverPayLoads(week) : fetchDriverPay(week))); setMessage(refreshSources ? "Load details refreshed" : "Reloaded"); }
    catch (err) { setError(err instanceof Error ? err.message : "Unable to reload this week"); }
    finally { setLoading(false); setRefreshing(false); }
  }
  async function prepareSettlement(reopen: boolean, driverId?: string) {
    if (dirty || saving || loading || refreshing) return;
    setRefreshing(true); setError("");
    try {
      // Autosave changes the revision. Preview the latest complete report before
      // accepting a finalization, including changes by other accounting users.
      const current = await fetchDriverPay(week);
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
  const switchWeek = (next: string) => { if (next !== week) { if (!dirty) setError(""); setPendingWeek(next); } };
  return <div className="space-y-5 animate-fade-in">
    <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
      <div><div className="flex items-center gap-3"><UserRound className="h-5 w-5 text-zinc-500" /><h1 className="text-lg font-semibold text-zinc-100">Driver pay</h1><span className="rounded-full bg-zinc-800/60 px-2.5 py-0.5 text-xs text-zinc-400">{drivers.length}</span></div><p className="mt-1.5 text-[13px] text-zinc-500">Weekly loads from Gross Board, profile tariffs, and driver adjustments.</p></div>
      <div className="flex flex-wrap items-center gap-2"><span role="status" title={invalid ? "Fuel/Toll: enter a number (zero is allowed) or reset to automatic. Other entries: enter a name and nonzero amount, or clear both cells. Positive amounts are reimbursements; negative amounts are charges." : undefined} className={`flex h-8 w-48 shrink-0 items-center gap-1.5 whitespace-nowrap text-xs ${error && dirty ? "text-red-300" : invalid ? "text-amber-300" : "text-zinc-500"}`}>{invalid ? <AlertTriangle className="h-4 w-4 shrink-0" /> : <CloudCheck className="h-4 w-4 shrink-0" />}{error && dirty ? "Not saved" : saving ? "Saving…" : invalid ? "Complete adjustment entries" : dirty ? "Waiting to save…" : message || "Saved automatically"}</span><button className={payButtonClass} disabled={saving || busy} onClick={() => void reload()}><RefreshCw className="h-3.5 w-3.5" />Reload</button><button className={payButtonClass} disabled={saving || busy || dirty || !report || !drivers.length} onClick={() => void reload(true)}>{refreshing ? "Refreshing…" : "Refresh load details"}</button></div>
    </div>
    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4"><MetricCard compact label="Drivers / loads" value={loading ? "—" : `${drivers.length} / ${summary.loads}`} icon={UsersRound} /><MetricCard compact label="Earned pay" value={loading ? "—" : decimalDisplay(summary.fee, true)} icon={Banknote} /><MetricCard compact label="Loads needing review" value={loading ? "—" : String(summary.review)} icon={AlertTriangle} /><MetricCard compact label={summary.review ? "Provisional payable" : "Total payable"} value={loading ? "—" : decimalDisplay(summary.payable, true)} icon={Banknote} /></div>
    <div className="flex flex-wrap items-center gap-3">
      <div className="flex items-center gap-1 rounded-lg border border-zinc-800 bg-card p-1"><button className={payButtonClass} aria-label="Previous week" disabled={busy || week <= "2000-01-03"} onClick={() => switchWeek(addDays(week, -7))}><ChevronLeft className="h-4 w-4" /></button><span className="min-w-36 px-2 text-center font-mono text-sm text-zinc-100">{shortDate(week)}–{shortDate(addDays(week, 6))}</span><button className={payButtonClass} aria-label="Next week" disabled={busy || week >= "2100-12-27"} onClick={() => switchWeek(addDays(week, 7))}><ChevronRight className="h-4 w-4" /></button></div>
      <span className="text-xs text-zinc-500">{week.slice(0, 4)}{week.slice(0, 4) !== addDays(week, 6).slice(0, 4) ? ` / ${addDays(week, 6).slice(0, 4)}` : ""}</span><button className={payButtonClass} disabled={busy} onClick={() => switchWeek(currentChargeWeek())}>This week</button><div className="min-w-48 flex-1"><ManagementSearch value={search} onChange={setSearch} placeholder="Driver, truck, or load…" /></div><label className="flex items-center gap-2 text-xs text-zinc-400">Dispatcher<select className={`${controlClass} !w-44`} value={dispatcher} onChange={event => setDispatcher(event.target.value)}><option value="all">All dispatchers</option>{dispatchers.map(([id, name]) => <option key={id} value={id}>{name}</option>)}</select></label><button type="button" className={payButtonClass} onClick={() => setOpened(new Set())}>Collapse all</button>
    </div>
    <div className="flex flex-wrap items-center gap-2 text-xs text-zinc-500"><span>{report?.drivers.filter(d => d.settlement?.finalized).length ?? 0} of {report?.drivers.length ?? 0} driver settlements finalized</span><button className={payButtonClass} disabled={busy || dirty || saving || !report?.drivers.some(d => !d.settlement?.finalized) || week > currentChargeWeek()} onClick={() => void prepareSettlement(false)}>Finalize week</button><button className={payButtonClass} disabled={busy || dirty || saving || !report?.drivers.some(d => d.settlement?.finalized)} onClick={() => void prepareSettlement(true)}>Reopen week</button>{driverFilter && <button className="text-blue-400" onClick={() => setDriverFilter("")}>Show all drivers</button>}</div>
    {error && <div role="alert" className="rounded-lg border border-red-500/20 bg-red-500/10 p-3 text-sm text-red-300">{error} {dirty && "Your unsaved edits are still here."}<button className={`${payButtonClass} ml-3`} disabled={saving || busy} onClick={() => dirty ? void save() : void reload()}>{dirty ? "Retry save" : "Retry"}</button>{dirty && <button className={`${payButtonClass} ml-2`} disabled={saving || busy} onClick={() => void reload()}>Reload saved version</button>}</div>}
    {(pendingWeek || pendingLink) && dirty && <p role="status" className="text-xs text-amber-300">{invalid ? "Complete or remove unfinished adjustments; enter zero or reset Fuel/Toll before leaving this week." : "Saving edits before leaving this week…"}{error && " Resolve the save error to continue."}<button className="ml-2 underline" onClick={() => { setPendingWeek(null); setPendingLink(null); }}>Stay here</button></p>}
    {refreshing && <p role="status" className="text-xs text-blue-300">Refreshing this week’s report…</p>}

    {loading ? <div role="status" className="rounded-xl border border-zinc-800 p-12 text-center text-sm text-zinc-500">Loading driver pay…</div> : report && drivers.length === 0 ? <div className="rounded-xl border border-zinc-800 p-12 text-center text-sm text-zinc-500">{search || dispatcher !== "all" ? "No drivers match these filters." : "No loads or driver adjustments for this week."}</div> : <div className="overflow-x-auto rounded-lg border border-zinc-800"><table className="w-full min-w-[960px] table-fixed border-separate border-spacing-0" aria-label="Weekly driver pay"><colgroup><col style={{ width: "24%" }} /><col style={{ width: "10%" }} /><col style={{ width: "15%" }} /><col style={{ width: "17%" }} /><col style={{ width: "16%" }} /><col style={{ width: "6%" }} /><col style={{ width: "12%" }} /></colgroup><thead><tr className="h-8 bg-zinc-900 text-left text-[11px] text-zinc-500">{["Driver", "Truck", "Driver type", "Tariff", "Dispatcher", "Loads", "Total payable"].map((label, i) => <th key={label} className={`border-b border-zinc-800 px-3 font-medium ${i >= 5 ? "text-right" : ""}`}>{label}</th>)}</tr></thead><tbody>{drivers.map(driver => <DriverCard key={`${week}:${driver.id}`} driver={driver} edits={changes[driver.id] ?? driver.edits} open={opened.has(driver.id)} onToggle={toggle} onEdit={edit} disabled={busy || !!driver.settlement?.finalized} chargeActionsDisabled={busy || dirty || saving || !!driver.settlement?.finalized} settlementDisabled={busy || dirty || saving} onSettlement={reopen => void prepareSettlement(reopen, driver.id)} onReload={() => reload()} />)}</tbody></table></div>}
    {settlement && report && <SettlementDialog report={report} {...settlement} onClose={() => setSettlement(null)} onSaved={value => { setReport(value); setSettlement(null); setMessage("Settlement updated"); }} />}
  </div>;
}
