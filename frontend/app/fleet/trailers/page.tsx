"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { Check, Truck } from "lucide-react";
import { TableFilters } from "@/app/components/management/TableFilters";
import { EmptyState, ErrorBanner, LoadingTable, ManagementHeader, ManagementSearch, TablePagination, TableShell, controlClass } from "@/app/components/management/ManagementUI";
import { FleetStatusBadge } from "@/app/components/FleetStatusBadge";
import { usePermissions } from "@/app/lib/access";
import { fetchTrailersPage } from "@/app/lib/api";
import type { InventoryRecord, TrailerPage, TruckOperatingStatus } from "@/app/lib/types";
import { useViewState } from "@/app/lib/viewMemory";
import { useDebouncedValue } from "@/app/lib/useDebouncedValue";
import { newInventoryRecord } from "../inventory/InventoryForms";
import { TrailerEditor } from "./TrailerEditor";

const cards = [
  { code: "", label: "All", color: "text-zinc-200 bg-zinc-500/5", selected: "border-zinc-400 ring-zinc-400/30" },
  { code: "ready", label: "Ready", color: "text-emerald-400 bg-emerald-500/5", selected: "border-emerald-400 ring-emerald-400/30" },
  { code: "shop", label: "Shop", color: "text-amber-400 bg-amber-500/5", selected: "border-amber-400 ring-amber-400/30" },
  { code: "in_use", label: "In use", color: "text-blue-400 bg-blue-500/5", selected: "border-blue-400 ring-blue-400/30" },
  { code: "out_of_service", label: "OOS", color: "text-red-400 bg-red-500/5", selected: "border-red-400 ring-red-400/30" },
  { code: "terminated", label: "Terminated", color: "text-zinc-400 bg-zinc-500/5", selected: "border-zinc-400 ring-zinc-400/30" },
] as const;

export default function TrailersPage() {
  const router = useRouter();
  const canManage = usePermissions().includes("trailers.manage");
  const [data, setData] = useState<TrailerPage | null>(null);
  const [search, setSearch] = useViewState("page:search", "");
  const [status, setStatus] = useViewState("page:trailerStatus", "");
  const [storage, setStorage] = useViewState("page:storage", "");
  const [page, setPage] = useViewState("page:page", 1);
  const [pageSize, setPageSize] = useViewState("page:pageSize", 25);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<InventoryRecord | null>(null);
  const query = useDebouncedValue(search);
  const sequence = useRef(0);
  const load = useCallback(async (signal?: AbortSignal) => {
    const request = ++sequence.current;
    setLoading(true);
    try {
      const result = await fetchTrailersPage({search: query, status, storage, page, pageSize}, signal);
      if (!signal?.aborted && request === sequence.current) { setData(result); setError(""); }
    } catch (reason) {
      if (!signal?.aborted && request === sequence.current) setError(reason instanceof Error ? reason.message : "Failed to load trailers");
    } finally { if (!signal?.aborted && request === sequence.current) setLoading(false); }
  }, [query, status, storage, page, pageSize]);
  useEffect(() => {
    const controller = new AbortController();
    const timer = window.setTimeout(() => void load(controller.signal), 0);
    const refresh = () => { if (document.visibilityState === "visible") void load(controller.signal); };
    const interval = window.setInterval(refresh, 30000);
    window.addEventListener("focus", refresh);
    return () => { window.clearTimeout(timer); window.clearInterval(interval); window.removeEventListener("focus", refresh); controller.abort(); };
  }, [load]);

  return <div className="space-y-5 animate-fade-in">
    {error && <div className="space-y-2"><ErrorBanner message={error} /><button className="text-sm text-blue-400" onClick={() => void load()}>Retry trailers</button></div>}
    <div role="group" aria-label="Filter trailer status" className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
      {cards.map(card => {
        const selected = status === card.code;
        const count = data ? card.code ? data.statusCounts[card.code] : Object.values(data.statusCounts).reduce((a,b) => a+b, 0) : null;
        return <button key={card.code} type="button" aria-pressed={selected} aria-label={card.label + ", " + (count ?? "loading") + " trailers"}
          onClick={() => { setStatus(card.code); setPage(1); }}
          className={"rounded-xl border p-4 text-left transition focus:outline-none focus:ring-2 " + card.color + " " + (selected ? card.selected + " ring-2" : "border-zinc-800/80 hover:border-zinc-600")}>
          <span className="flex items-center justify-between text-xs font-medium">{card.label}{selected && <Check className="h-4 w-4" />}</span>
          <span className="mt-2 block text-2xl font-semibold tabular-nums">{count ?? "—"}</span>
        </button>;
      })}
    </div>
    <div className="flex flex-wrap items-center gap-3">
      <ManagementSearch value={search} onChange={value => { setSearch(value); setPage(1); }} placeholder="Search trailers…" />
      <TableFilters><select aria-label="Storage location" className={controlClass} value={storage} onChange={event => { setStorage(event.target.value); setPage(1); }}><option value="">All locations</option>{data?.storageLocations.map(value => <option key={value}>{value}</option>)}</select></TableFilters>
      <div className="ml-auto flex flex-wrap items-center gap-2"><ManagementHeader icon={Truck} title="Trailers" count={data?.total} actionLabel={canManage ? "Add Trailer" : undefined} onAction={() => setEditing(newInventoryRecord({ status: "ready" }))} /></div>
    </div>
    <TableShell>
      <table className="w-full min-w-[900px] text-left text-[13px]">
        <thead><tr className="border-b border-zinc-800/50 text-zinc-500">{["Unit", "Equipment", "Truck", "Storage", "Status", "Status notes"].map(label => <th key={label} className="px-4 py-3 font-medium">{label}</th>)}</tr></thead>
        {!loading && <tbody>{data?.items.map(trailer => {
          const d = trailer.data;
          const open = () => router.push("/fleet/trailers/detail?id=" + trailer.id);
          const notes = [["shop", "out_of_service", "terminated"].includes(d.status || "") ? d.statusReason : "", d.notes || d.usage].filter(Boolean).join(" · ");
          return <tr key={trailer.id} tabIndex={0} aria-label={"Open trailer " + d.unitNumber + " details"} onClick={open} onKeyDown={event => { if (event.key === "Enter" || event.key === " ") { event.preventDefault(); open(); } }}
            className="cursor-pointer border-b border-zinc-900/70 text-zinc-300 transition last:border-0 hover:bg-zinc-800/15 focus:outline-none focus:ring-2 focus:ring-inset focus:ring-blue-500/50">
            <td className="px-4 py-3"><div className="font-mono font-medium text-zinc-200">{d.unitNumber}</div><div className="mt-0.5 text-[11px] text-zinc-500">{[d.plate, d.plateState].filter(Boolean).join(" · ")}</div></td>
            <td className="px-4 py-3"><div>{[d.year, d.make].filter(Boolean).join(" ") || "—"}</div><div className="mt-0.5 font-mono text-[11px] text-zinc-600">{d.vin || "No VIN recorded"}</div></td>
            <td className="px-4 py-3">{trailer.truckUnit || "Unassigned"}</td>
            <td className="px-4 py-3 text-zinc-400">{d.storageLocation || "—"}</td>
            <td className="px-4 py-3"><FleetStatusBadge status={d.status as TruckOperatingStatus} showReady /></td>
            <td className="max-w-sm px-4 py-3 text-xs text-zinc-400"><p className="line-clamp-2 whitespace-pre-wrap break-words" title={notes}>{notes || "—"}</p></td>
          </tr>;
        })}</tbody>}
      </table>
      {loading ? <LoadingTable columns={6} /> : !data?.items.length && <EmptyState message="No trailers match your search or filters." />}
    </TableShell>
    {!loading && data && <TablePagination page={data.page} pageSize={data.pageSize} totalItems={data.total} totalPages={data.totalPages} onPageChange={setPage} onPageSizeChange={value => { setPageSize(value); setPage(1); }} />}
    {editing && canManage && <TrailerEditor key={editing.id} trailer={editing} onClose={() => setEditing(null)} onSaved={() => { setEditing(null); void load(); }} />}
  </div>;
}
