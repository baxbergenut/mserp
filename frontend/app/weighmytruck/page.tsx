"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { AlertTriangle, LoaderCircle, RefreshCw, Scale } from "lucide-react";
import { changeWeighMyTruck, fetchWeighMyTruck, linkWeighMyTruck, verifyWeighMyTruck } from "../lib/api";
import { usePermissions } from "../lib/access";
import { useViewState } from "../lib/viewMemory";
import { formatPhone } from "../lib/phone";
import type { WeighMyTruckEntry, WeighMyTruckList } from "../lib/types";
import { controlClass, EmptyState, ErrorBanner, LoadingTable, ManagementHeader, ManagementSearch, Modal, TablePagination, TableShell } from "../components/management/ManagementUI";

export default function WeighMyTruckPage() {
  const permissions = usePermissions();
  const canWrite = permissions.includes("weighmytruck.write");
  const canViewProfile = permissions.includes("fleet.read");
  const [data, setData] = useState<WeighMyTruckList | null>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState("");
  const saving = useRef(false);
  const generation = useRef(0);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useViewState("wmt:search", "");
  const [filter, setFilter] = useViewState("wmt:filter", "enrolled");
  const [page, setPage] = useViewState("wmt:page", 1);
  const [pageSize, setPageSize] = useViewState("wmt:pageSize", 25);
  const [dialog, setDialog] = useState<{ entry: WeighMyTruckEntry; kind: "link" | "verify" } | null>(null);
  const [driverId, setDriverId] = useState("");
  const [enrolled, setEnrolled] = useState(true);
  const [reason, setReason] = useState("");

  const load = useCallback(async (signal?: AbortSignal) => {
    const revision = ++generation.current;
    try {
      const next = await fetchWeighMyTruck(signal);
      if (revision === generation.current) setData(next);
    } catch (e) {
      if (!signal?.aborted && revision === generation.current) setError(e instanceof Error ? e.message : "Could not load WeighMyTruck.");
    } finally { if (revision === generation.current) setLoading(false); }
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    const initial = setTimeout(() => void load(controller.signal), 0);
    const refresh = () => { if (!saving.current && document.visibilityState === "visible") void load(controller.signal); };
    const interval = setInterval(refresh, 30000);
    window.addEventListener("focus", refresh);
    return () => { controller.abort(); clearTimeout(initial); clearInterval(interval); window.removeEventListener("focus", refresh); };
  }, [load]);

  async function mutate(entry: WeighMyTruckEntry, operation: () => Promise<void>, message: string) {
    if (saving.current) return;
    saving.current = true; generation.current++; setBusy(entry.id || entry.driverId); setError(""); setNotice("");
    try { await operation(); setNotice(message); setDialog(null); }
    catch (e) { setError(e instanceof Error ? e.message : "The change could not be completed."); }
    finally { await load(); saving.current = false; setBusy(""); }
  }
  const items = data?.items ?? [];
  const enrolledCount = items.filter(e => e.enrolled).length;
  const warnings = items.filter(e => e.warning || e.state !== "confirmed").length;
  const shown = items.filter(e => {
    const included = filter === "all" || (filter === "enrolled" && (e.enrolled || e.state !== "confirmed")) || (filter === "available" && !e.enrolled && e.driverStatus !== "terminated" && e.driverId) || (filter === "warnings" && (e.warning || e.state !== "confirmed"));
    return included && `${e.name} ${e.email} ${e.phone} ${e.driverCode}`.toLowerCase().includes(search.trim().toLowerCase());
  });
  const totalPages = Math.max(1, Math.ceil(shown.length / pageSize));
  const currentPage = Math.min(page, totalPages);
  const visible = shown.slice((currentPage - 1) * pageSize, currentPage * pageSize);
  const openVerify = (entry: WeighMyTruckEntry) => { setDialog({ entry, kind: "verify" }); setEnrolled(entry.enrolled); setReason(""); };

  return <div className="space-y-4">
    <ManagementHeader icon={Scale} title="WeighMyTruck" count={enrolledCount} secondaryAction={<button className="ui-button" disabled={!!busy} onClick={() => { setError(""); void load(); }}><RefreshCw className="h-4 w-4" />Refresh</button>} />
    {error && <ErrorBanner message={error} />}
    {notice && <p role="status" className="text-[13px] text-emerald-400">{notice}</p>}
    {data && (!data.configured || !data.initialized) && <ErrorBanner message={!data.configured ? "WeighMyTruck is not configured on the server." : "Import the existing fleet roster before adding or removing drivers."} />}
    <div className="flex flex-wrap items-center gap-2">
      <ManagementSearch value={search} onChange={v => { setSearch(v); setPage(1); }} placeholder="Search drivers, email or code" />
      <select aria-label="Membership filter" className={`${controlClass} max-w-52`} value={filter} onChange={e => { setFilter(e.target.value); setPage(1); }}>
        <option value="enrolled">Added ({enrolledCount})</option><option value="available">Available to add</option><option value="warnings">Needs attention ({warnings})</option><option value="all">All drivers</option>
      </select>
    </div>
    <TableShell>{loading ? <LoadingTable columns={6} /> : !visible.length ? <EmptyState message="No drivers match this view." /> : <table className="w-full min-w-[880px] text-left text-[13px]">
      <thead className="bg-zinc-900/60 text-[12px] text-zinc-500"><tr>{["Driver", "Email", "Phone", "Code", "Membership", ""].map((label, i) => <th key={i} className="h-8 px-3 font-medium">{label}</th>)}</tr></thead>
      <tbody className="divide-y divide-zinc-800/60">{visible.map(entry => <tr key={entry.id || entry.driverId} className={entry.warning ? "bg-amber-500/5" : "hover:bg-zinc-800/20"}>
        <td className="px-3 py-1.5"><div className="flex items-center gap-2">{entry.warning && <AlertTriangle className="h-4 w-4 shrink-0 text-amber-400" />}{entry.driverId && canViewProfile ? <Link href={`/drivers/detail?id=${entry.driverId}`} className="text-zinc-200 hover:text-accent">{entry.name}</Link> : <span>{entry.name}</span>}<span className={entry.driverStatus === "terminated" ? "text-red-400" : "text-zinc-500"}>{entry.driverStatus === "unlinked" ? "Unlinked" : entry.driverStatus === "active" ? "" : entry.driverStatus}</span></div>
          {(entry.warning || (!entry.enrolled && entry.addBlockReason)) && <p className="max-w-md text-[12px] text-amber-400">{entry.warning || entry.addBlockReason}</p>}
        </td>
        <td className="px-3 py-1.5 text-zinc-400">{entry.email || "—"}</td><td className="whitespace-nowrap px-3 py-1.5 text-zinc-400">{formatPhone(entry.phone) || "—"}</td><td className="px-3 py-1.5 text-zinc-400">{entry.driverCode || "—"}</td>
        <td className="whitespace-nowrap px-3 py-1.5"><span className={entry.state !== "confirmed" ? "text-amber-400" : entry.enrolled ? "text-emerald-400" : "text-zinc-500"}>{entry.state === "review" ? "Needs verification" : entry.state === "pending" ? "Pending" : entry.enrolled ? "Added" : "Not added"}</span></td>
        <td className="px-3 py-1.5">{canWrite && <div className="flex justify-end gap-2">
          {!entry.driverId && entry.state === "confirmed" && <button className="ui-button" disabled={!!busy} onClick={() => { setDriverId(""); setDialog({ entry, kind: "link" }); }}>Link driver</button>}
          {entry.id && (entry.state === "confirmed" || entry.canVerify) && <button className="ui-button" disabled={!!busy} onClick={() => openVerify(entry)}>Verify</button>}
          {entry.state === "confirmed" && <button className={entry.enrolled ? "ui-button text-red-400" : "ui-button-primary"} title={!entry.enrolled ? entry.addBlockReason : undefined} aria-label={`${entry.enrolled ? "Remove" : "Add"} ${entry.name}`} disabled={!!busy || !data?.configured || !data.initialized || (!entry.enrolled && !!entry.addBlockReason)} onClick={() => void mutate(entry, () => changeWeighMyTruck(entry, !entry.enrolled), `${entry.name} ${entry.enrolled ? "removed from" : "added to"} WeighMyTruck.`)}>
            {busy === (entry.id || entry.driverId) && <LoaderCircle className="h-4 w-4 animate-spin" />}{entry.enrolled ? "Remove" : "Add"}
          </button>}
        </div>}</td>
      </tr>)}</tbody>
    </table>}</TableShell>
    <TablePagination page={currentPage} pageSize={pageSize} totalItems={shown.length} totalPages={totalPages} onPageChange={setPage} onPageSizeChange={v => { setPageSize(v); setPage(1); }} />
    <p className="text-[12px] text-zinc-500">Membership is tracked in MSERP. Changes made directly on the WeighMyTruck website need to be verified here.</p>
    {dialog && <Modal title={`${dialog.kind === "link" ? "Link driver" : "Verify membership"}: ${dialog.entry.name}`} isSaving={!!busy} submitLabel="Save" onClose={() => setDialog(null)} onSubmit={event => {
      event.preventDefault();
      if (dialog.kind === "link") void mutate(dialog.entry, () => linkWeighMyTruck(dialog.entry, driverId), "Driver linked.");
      else void mutate(dialog.entry, () => verifyWeighMyTruck(dialog.entry, enrolled, reason), "Membership verification recorded.");
    }}>
      {error && <ErrorBanner message={error} />}
      {dialog.kind === "link" ? <label className="block space-y-2 text-[13px]">MSERP driver<select required className={controlClass} value={driverId} onChange={e => setDriverId(e.target.value)}><option value="">Select driver</option>{items.filter(e => e.driverId && !e.id).map(e => <option key={e.driverId} value={e.driverId}>{e.name} · {e.driverStatus}</option>)}</select></label> : <div className="space-y-3">
        <p className="text-[13px] text-zinc-400">Check <strong>{dialog.entry.email}</strong> in the <a className="text-accent underline" href="https://weighmytruck.com/Fleet/DriverList" target="_blank" rel="noreferrer">WeighMyTruck driver list</a>, then record its current membership. This only corrects MSERP’s tracking.</p>
        <label className="block space-y-2 text-[13px]">Website membership<select className={controlClass} value={String(enrolled)} onChange={e => setEnrolled(e.target.value === "true")}><option value="true">Present in fleet</option><option value="false">Not in fleet</option></select></label>
        <label className="block space-y-2 text-[13px]">Verification note<input required maxLength={1000} className={controlClass} value={reason} onChange={e => setReason(e.target.value)} /></label>
      </div>}
    </Modal>}
  </div>;
}
