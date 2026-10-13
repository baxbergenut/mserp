"use client";


import { ColumnFilter } from "@/app/components/management/ColumnFilter";
import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { AlertTriangle, RefreshCw } from "lucide-react";
import { fetchDrivers, fetchTrucks, fetchWMTTransactions, linkWMTTransaction, syncWMTTransactions } from "../lib/api";
import type { Driver, Truck, WMTTransaction, WMTTransactions } from "../lib/types";
import { usePermissions } from "../lib/access";
import { useViewState } from "../lib/viewMemory";
import { useDebouncedValue } from "../lib/useDebouncedValue";
import { currentChargeWeek } from "../accounting/driver-charges/charges";
import { addDays } from "../gross-board/board";
import { controlClass, EmptyState, ErrorBanner, ManagementSearch, Modal, TablePagination, TableShell } from "../components/management/ManagementUI";

const money = (value: string) => Number(value).toLocaleString("en-US", { style: "currency", currency: "USD" });

export function TransactionsTab() {
  const permissions = usePermissions();
  const canWrite = permissions.includes("weighmytruck.write");
  const canLink = canWrite && permissions.includes("payroll.write") && permissions.includes("fleet.read");
  const canProfile = permissions.includes("fleet.read");
  const [from, setFrom] = useViewState("wmt:transactions:from", currentChargeWeek);
  const [to, setTo] = useViewState("wmt:transactions:to", () => addDays(currentChargeWeek(), 6));
  const [search, setSearch] = useViewState("wmt:transactions:search", "");
  const [page, setPage] = useViewState("wmt:transactions:page", 1);
  const [pageSize, setPageSize] = useViewState("wmt:transactions:pageSize", 25);
  const debounced = useDebouncedValue(search);
  const [data, setData] = useState<WMTTransactions | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const saving = useRef(false);
  const generation = useRef(0);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [entry, setEntry] = useState<WMTTransaction | null>(null);
  const [drivers, setDrivers] = useState<Driver[]>([]);
  const [trucks, setTrucks] = useState<Truck[]>([]);
  const [driverId, setDriverId] = useState("");
  const [truckId, setTruckId] = useState("");
  const load = useCallback(async (signal?: AbortSignal) => {
    const revision = ++generation.current;
    try {
      const next = await fetchWMTTransactions({ from, to, search: debounced, page, pageSize }, signal);
      if (revision === generation.current) { setData(next); setError(""); }
    } catch (e) { if (!signal?.aborted && revision === generation.current) setError(e instanceof Error ? e.message : "Could not load transactions."); }
    finally { if (revision === generation.current) setLoading(false); }
  }, [from, to, debounced, page, pageSize]);
  useEffect(() => {
    const controller = new AbortController();
    const timer = setTimeout(() => { setLoading(true); void load(controller.signal); }, 0);
    return () => { clearTimeout(timer); controller.abort(); };
  }, [load]);
  useEffect(() => { if (!notice) return; const timer = setTimeout(() => setNotice(""), 5000); return () => clearTimeout(timer); }, [notice]);
  async function sync() {
    if (saving.current) return; saving.current = true; setBusy(true); setError(""); setNotice("");
    try { await syncWMTTransactions(); await load(); setNotice("Transactions synced."); }
    catch (e) { setError(e instanceof Error ? e.message : "Sync failed."); }
    finally { saving.current = false; setBusy(false); }
  }
  async function openLink(value: WMTTransaction) {
    setError("");
    try {
      const [d, t] = await Promise.all([fetchDrivers(), fetchTrucks()]);
      setDrivers(d); setTrucks(t); setDriverId(value.driverId); setTruckId(value.truckId); setEntry(value);
    } catch (e) { setError(e instanceof Error ? e.message : "Could not load fleet records."); }
  }
  async function saveLink() {
    if (!entry || saving.current) return; saving.current = true; setBusy(true); setError("");
    try { await linkWMTTransaction(entry, driverId, truckId); setEntry(null); await load(); setNotice("Transaction linked."); }
    catch (e) { setError(e instanceof Error ? e.message : "Could not link transaction."); }
    finally { saving.current = false; setBusy(false); }
  }
  return <div className="space-y-4">
    <div className="flex flex-wrap items-end gap-2">


      <ManagementSearch value={search} onChange={v => { setSearch(v); setPage(1); }} placeholder="Search tickets, drivers or trucks" />
      <div className="ml-auto flex items-center gap-3"><span className="text-[13px] tabular-nums text-zinc-300">Total {money(data?.totalAmount ?? "0")}</span>{canWrite && <button className="ui-button" disabled={busy} onClick={() => void sync()}><RefreshCw className={`h-4 w-4 ${busy ? "animate-spin" : ""}`} />Sync</button>}</div>
    </div>
    {error && <ErrorBanner message={error} />}{notice && <p role="status" className="text-[13px] text-emerald-400">{notice}</p>}
    <TableShell><table className="w-full min-w-[900px] text-left text-[13px]">
      <thead className="bg-zinc-900/60 text-[12px] text-zinc-500"><tr>{["Date", "Ticket", "Driver", "Truck", "Location", "Charge to", "Amount", ""].map((s,i) => <th key={i} className={`h-8 px-3 font-medium ${s === "Amount" ? "text-right" : ""}`}>{s === "Date" ? <ColumnFilter label="Date" active={!!(from || to)}><label className="text-[12px] text-zinc-400">From<input aria-label="From" type="date" value={from} className={controlClass} onChange={e => { setFrom(e.target.value); setPage(1); }} /></label><label className="text-[12px] text-zinc-400">To<input aria-label="To" type="date" value={to} className={controlClass} onChange={e => { setTo(e.target.value); setPage(1); }} /></label></ColumnFilter> : s}</th>)}</tr></thead>
      <tbody className="divide-y divide-zinc-800/60">{loading ? <tr><td colSpan={8} className="p-8 text-center text-zinc-500" role="status">Loading…</td></tr> : !data?.items.length ? <tr><td colSpan={8}>{<EmptyState message="No scale transactions in this date range." />}</td></tr> : <>{data.items.map(t => <tr key={t.id} className={t.warning ? "bg-amber-500/5" : "hover:bg-zinc-800/20"}>
        <td className="whitespace-nowrap px-3 py-1.5" title={t.time}>{t.date}</td>
        <td className="px-3 py-1.5" title={t.originalTicket ? `Reweigh of ${t.originalTicket}` : undefined}>{t.ticket}</td>
        <td className="px-3 py-1.5"><div className="flex items-center gap-2">{t.driverId && canProfile ? <Link href={`/drivers/detail?id=${t.driverId}`} className="hover:text-accent">{t.driverName}</Link> : t.driverName || "Unlinked"}{t.warning && <span title={t.warning}><AlertTriangle aria-label="Needs attention" className="h-4 w-4 text-amber-400" /></span>}</div></td>
        <td className="px-3 py-1.5">{t.truckId && canProfile ? <Link href={`/trucks/detail?id=${t.truckId}`} className="hover:text-accent">{t.truckUnit || "Truck"}</Link> : t.truckUnit || "Unlinked"}</td>
        <td className="px-3 py-1.5 text-zinc-400" title={t.location}>{[t.city,t.state].filter(Boolean).join(", ") || t.location}</td>
        <td className="px-3 py-1.5" title={t.investorName}>{t.payer === "driver" ? "Driver" : t.payer === "investor" ? "Truck investor" : "Company"}</td>
        <td className="px-3 py-1.5 text-right tabular-nums">{money(t.amount)}</td>
        <td className="px-3 py-1.5 text-right">{canLink && <button className="ui-button" disabled={busy} onClick={() => void openLink(t)}>{t.warning ? "Link" : "Edit link"}</button>}</td>
      </tr>)}</>}</tbody>
    </table></TableShell>
    <TablePagination page={data?.page ?? page} pageSize={pageSize} totalItems={data?.total ?? 0} totalPages={data?.totalPages ?? 1} onPageChange={setPage} onPageSizeChange={v => { setPageSize(v); setPage(1); }} />
    {data?.lastSync && <p className="text-[12px] text-zinc-500">Last synced {new Date(data.lastSync).toLocaleString()}</p>}
    {entry && <Modal title={`Link ticket ${entry.ticket}`} submitLabel="Save" isSaving={busy} onClose={() => setEntry(null)} onSubmit={e => { e.preventDefault(); void saveLink(); }}>
      {error && <ErrorBanner message={error} />}
      <div className="space-y-4"><label className="block text-[13px]">Driver<select aria-label="Driver" required value={driverId} className={controlClass} onChange={e => setDriverId(e.target.value)}><option value="">Select driver</option>{drivers.map(d => <option key={d.id} value={d.id}>{d.fullName}</option>)}</select></label>
      <label className="block text-[13px]">Truck<select aria-label="Truck" required value={truckId} className={controlClass} onChange={e => setTruckId(e.target.value)}><option value="">Select truck</option>{trucks.map(t => <option key={t.id} value={t.id}>{t.unitNumber}</option>)}</select></label></div>
    </Modal>}
  </div>;
}
