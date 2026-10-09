"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { AlertTriangle, LoaderCircle, Plus, RefreshCw, Scale } from "lucide-react";
import { changeWeighMyTruck, fetchWeighMyTruck, linkWeighMyTruck } from "../lib/api";
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
  const [page, setPage] = useViewState("wmt:page", 1);
  const [pageSize, setPageSize] = useViewState("wmt:pageSize", 25);
  const [dialog, setDialog] = useState<{ kind: "add" } | { kind: "link"; entry: WeighMyTruckEntry } | null>(null);
  const [driverId, setDriverId] = useState("");

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
  useEffect(() => {
    if (!notice) return;
    const timer = setTimeout(() => setNotice(""), 5000);
    return () => clearTimeout(timer);
  }, [notice]);

  async function mutate(entry: WeighMyTruckEntry, operation: () => Promise<void>, message: string) {
    if (saving.current) return;
    saving.current = true; generation.current++; setBusy(entry.id || entry.driverId); setError(""); setNotice("");
    try { await operation(); setNotice(message); setDialog(null); }
    catch (e) { setError(e instanceof Error ? e.message : "The change could not be completed."); }
    finally { await load(); saving.current = false; setBusy(""); }
  }
  const items = data?.items ?? [];
  const enrolledCount = items.filter(e => e.enrolled).length;
  const needsAttention = (entry: WeighMyTruckEntry) => !!entry.warning || entry.state !== "confirmed";
  const shown = items.filter(e => e.enrolled && `${e.name} ${e.email} ${e.phone} ${e.driverCode}`.toLowerCase().includes(search.trim().toLowerCase()))
    .sort((a, b) => Number(needsAttention(b)) - Number(needsAttention(a)) || a.name.localeCompare(b.name));
  const available = items.filter(e => e.driverId && e.driverStatus === "active" && !e.enrolled)
    .sort((a, b) => a.name.localeCompare(b.name));
  const unresolvedAdds = items.filter(e => !e.enrolled && e.state !== "confirmed");
  const totalPages = Math.max(1, Math.ceil(shown.length / pageSize));
  const currentPage = Math.min(page, totalPages);
  const visible = shown.slice((currentPage - 1) * pageSize, currentPage * pageSize);

  return <div className="space-y-4">
    <ManagementHeader icon={Scale} title="WeighMyTruck" count={enrolledCount} secondaryAction={<>
      <button className="ui-button" disabled={!!busy} onClick={() => { setError(""); void load(); }}><RefreshCw className="h-4 w-4" />Refresh</button>
      {canWrite && <button className="ui-button ui-button-primary" disabled={!!busy || !data?.configured || !data.initialized} onClick={() => { setDriverId(""); setError(""); setDialog({ kind: "add" }); }}><Plus className="h-4 w-4" />Add driver</button>}
    </>} />
    {error && <ErrorBanner message={error} />}
    {notice && <p role="status" className="text-[13px] text-emerald-400">{notice}</p>}
    {data && (!data.configured || !data.initialized) && <ErrorBanner message={!data.configured ? "WeighMyTruck is not configured on the server." : "Import the existing fleet roster before adding or removing drivers."} />}
    {unresolvedAdds.map(entry => <ErrorBanner key={entry.id} message={`${entry.name}: ${entry.warning || "The add request has not completed. Refresh for the result."}`} />)}
    <div className="flex flex-wrap items-center gap-2">
      <ManagementSearch value={search} onChange={v => { setSearch(v); setPage(1); }} placeholder="Search drivers, email or code" />
    </div>
    <TableShell>{loading ? <LoadingTable columns={5} /> : !visible.length ? <EmptyState message={search ? "No added drivers match your search." : "No drivers have been added to WeighMyTruck."} /> : <table className="w-full min-w-[760px] text-left text-[13px]">
      <thead className="bg-zinc-900/60 text-[12px] text-zinc-500"><tr>{["Driver", "Email", "Phone", "Code", ""].map((label, i) => <th key={i} className="h-8 px-3 font-medium">{label}</th>)}</tr></thead>
      <tbody className="divide-y divide-zinc-800/60">{visible.map(entry => <tr key={entry.id} className={needsAttention(entry) ? "bg-amber-500/5" : "hover:bg-zinc-800/20"}>
        <td className="px-3 py-1.5"><div className="flex items-center gap-2">{entry.driverId && canViewProfile ? <Link href={`/drivers/detail?id=${entry.driverId}`} className="text-zinc-200 hover:text-accent">{entry.name}</Link> : <span>{entry.name}</span>}{needsAttention(entry) && <AlertTriangle aria-label="Needs attention" className="h-4 w-4 shrink-0 text-amber-400" />}<span className={entry.driverStatus === "terminated" ? "text-red-400" : "text-zinc-500"}>{entry.driverStatus === "unlinked" ? "Unlinked" : entry.driverStatus === "active" ? "" : entry.driverStatus}</span></div>
          {needsAttention(entry) && <p className="max-w-md text-[12px] text-amber-400">{entry.warning || "Change pending. Refresh for the result."}</p>}
        </td>
        <td className="px-3 py-1.5 text-zinc-400">{entry.email || "—"}</td><td className="whitespace-nowrap px-3 py-1.5 text-zinc-400">{formatPhone(entry.phone) || "—"}</td><td className="px-3 py-1.5 text-zinc-400">{entry.driverCode || "—"}</td>
        <td className="px-3 py-1.5">{canWrite && <div className="flex justify-end gap-2">
          {!entry.driverId && entry.state === "confirmed" && <button className="ui-button" disabled={!!busy} onClick={() => { setDriverId(""); setDialog({ entry, kind: "link" }); }}>Link driver</button>}
          <button className="ui-button text-red-400" aria-label={`Remove ${entry.name}`} disabled={!!busy || !data?.configured || !data.initialized || entry.state !== "confirmed"} onClick={() => void mutate(entry, () => changeWeighMyTruck(entry, false), `${entry.name} removed from WeighMyTruck.`)}>
            {busy === entry.id && <LoaderCircle className="h-4 w-4 animate-spin" />}Remove
          </button>
        </div>}</td>
      </tr>)}</tbody>
    </table>}</TableShell>
    <TablePagination page={currentPage} pageSize={pageSize} totalItems={shown.length} totalPages={totalPages} onPageChange={setPage} onPageSizeChange={v => { setPageSize(v); setPage(1); }} />
    {dialog && <Modal title={dialog.kind === "add" ? "Add driver to WeighMyTruck" : `Link driver: ${dialog.entry.name}`} isSaving={!!busy} submitLabel={dialog.kind === "add" ? "Add" : "Save"} onClose={() => setDialog(null)} onSubmit={event => {
      event.preventDefault();
      if (dialog.kind === "link") void mutate(dialog.entry, () => linkWeighMyTruck(dialog.entry, driverId), "Driver linked.");
      else {
        const entry = available.find(e => e.driverId === driverId);
        if (!entry || entry.state !== "confirmed" || entry.addBlockReason) { setError(entry?.addBlockReason || "Select an available active driver."); return; }
        void mutate(entry, async () => { await changeWeighMyTruck(entry, true); setSearch(""); setPage(1); }, `${entry.name} added to WeighMyTruck.`);
      }
    }}>
      {error && <ErrorBanner message={error} />}
      {dialog.kind === "link" ? <label className="block space-y-2 text-[13px]">MSERP driver<select required className={controlClass} value={driverId} onChange={e => setDriverId(e.target.value)}><option value="">Select driver</option>{items.filter(e => e.driverId && !e.id).map(e => <option key={e.driverId} value={e.driverId}>{e.name} · {e.driverStatus}</option>)}</select></label> : <div className="space-y-3">
        <label className="block space-y-2 text-[13px]">Active driver<select aria-label="Active driver" required className={controlClass} value={driverId} onChange={e => setDriverId(e.target.value)}>
          <option value="">Select driver</option>{available.map(entry => <option key={entry.driverId} value={entry.driverId} disabled={!!entry.addBlockReason || entry.state !== "confirmed"}>{entry.name}{entry.addBlockReason ? ` — ${entry.addBlockReason}` : entry.state !== "confirmed" ? " — Change pending" : ""}</option>)}
        </select></label>
        {!available.length && <p className="text-[12px] text-zinc-500">All active drivers have already been added.</p>}
      </div>}
    </Modal>}
  </div>;
}
