"use client";

import { useCallback, useEffect, useState } from "react";
import { FileText, Truck } from "lucide-react";
import Link from "@/app/components/AccessLink";
import { ColumnFilter } from "@/app/components/management/ColumnFilter";
import { EmptyState, ErrorBanner, LoadingTable, ManagementHeader, ManagementSearch, RowActions, StatusBadge, TablePagination, TableShell, controlClass } from "@/app/components/management/ManagementUI";
import { usePermissions } from "@/app/lib/access";
import { fetchTrailers } from "@/app/lib/api";
import type { InventoryRecord } from "@/app/lib/types";
import { useViewState } from "@/app/lib/viewMemory";
import { newInventoryRecord } from "../inventory/InventoryForms";
import { conditionStates, readable, StatusBadge as TrailerStatusBadge } from "../inventory/InventoryStatus";
import { TrailerEditor } from "./TrailerEditor";

export default function TrailersPage() {
  const canManage = usePermissions().includes("trailers.manage");
  const [trailers, setTrailers] = useState<InventoryRecord[]>([]);
  const [search, setSearch] = useViewState("page:search", "");
  const [recordStatus, setRecordStatus] = useViewState("page:recordStatus", "active");
  const [availability, setAvailability] = useViewState("page:availability", "");
  const [condition, setCondition] = useViewState("page:condition", "");
  const [storage, setStorage] = useViewState("page:storage", "");
  const [page, setPage] = useViewState("page:page", 1);
  const [pageSize, setPageSize] = useViewState("page:pageSize", 25);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<InventoryRecord | null>(null);
  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true);
    try {
      const records = await fetchTrailers(signal);
      if (!signal?.aborted) { setTrailers(records); setError(""); }
    } catch (reason) {
      if (!signal?.aborted) setError(reason instanceof Error ? reason.message : "Failed to load trailers");
    } finally { if (!signal?.aborted) setLoading(false); }
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    const timer = window.setTimeout(() => void load(controller.signal), 0);
    return () => { window.clearTimeout(timer); controller.abort(); };
  }, [load]);

  const query = search.trim().toLowerCase();
  const filtered = trailers.filter(({ data }) =>
    (recordStatus === "all" || data.active === (recordStatus === "active")) &&
    (!availability || (data.status || "unknown") === availability) &&
    (!condition || (data.condition || "unknown") === condition) &&
    (!storage || data.storageLocation === storage) &&
    (!query || [data.unitNumber, data.vin, data.year, data.make, data.plate, data.plateState, data.storageLocation, data.usage, data.notes].some(value => value?.toLowerCase().includes(query))),
  ).sort((a, b) => (a.data.unitNumber || "").localeCompare(b.data.unitNumber || "", undefined, { numeric: true }));
  const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize));
  const currentPage = Math.min(page, totalPages);

  return <div className="space-y-5 animate-fade-in">
    {error && <div className="space-y-2"><ErrorBanner message={error} /><button className="text-sm text-blue-400" onClick={() => void load()}>Retry trailers</button></div>}
    <div className="flex flex-wrap items-center gap-3">
      <ManagementSearch value={search} onChange={value => { setSearch(value); setPage(1); }} placeholder="Search trailers…" />
      <div className="ml-auto flex flex-wrap items-center gap-2"><ManagementHeader icon={Truck} title="Trailers" count={filtered.length} actionLabel={canManage ? "Add Trailer" : undefined} onAction={() => setEditing(newInventoryRecord({ status: "stored_unused", condition: "unknown" }))} /></div>
    </div>
    <TableShell>
      <table className="w-full min-w-[980px] text-left text-[13px]">
        <thead><tr className="border-b border-zinc-800/50 text-zinc-500">
          {["Unit", "Equipment", "Plate", "Registration"].map(label => <th key={label} className="px-4 py-3 font-medium">{label}</th>)}
          <th className="px-4 py-3 font-medium"><ColumnFilter label="Storage" active={!!storage}>
            <select aria-label="Storage location" className={controlClass} value={storage} onChange={event => { setStorage(event.target.value); setPage(1); }}><option value="">All locations</option>{Array.from(new Set(trailers.map(item => item.data.storageLocation).filter((value): value is string => !!value))).sort().map(value => <option key={value}>{value}</option>)}</select>
          </ColumnFilter></th>
          <th className="px-4 py-3 font-medium"><ColumnFilter label="Availability" active={!!availability}>
            <select aria-label="Trailer availability" className={controlClass} value={availability} onChange={event => { setAvailability(event.target.value); setPage(1); }}><option value="">All availability</option>{["stored_unused", "needs_review", "ready", "repair", "retired", "unknown"].map(value => <option key={value} value={value}>{readable(value)}</option>)}</select>
          </ColumnFilter></th>
          <th className="px-4 py-3 font-medium"><ColumnFilter label="Condition" active={!!condition}>
            <select aria-label="Trailer condition" className={controlClass} value={condition} onChange={event => { setCondition(event.target.value); setPage(1); }}><option value="">All conditions</option>{conditionStates.map(value => <option key={value} value={value}>{readable(value)}</option>)}</select>
          </ColumnFilter></th>
          <th className="px-4 py-3 font-medium"><ColumnFilter label="Record" active={recordStatus !== "active"}>
            <select aria-label="Record status" className={controlClass} value={recordStatus} onChange={event => { setRecordStatus(event.target.value); setPage(1); }}><option value="active">Active</option><option value="inactive">Inactive</option><option value="all">All records</option></select>
          </ColumnFilter></th>
          <th className="px-4 py-3 text-right font-medium">Actions</th>
        </tr></thead>
        {!loading && <tbody>{filtered.slice((currentPage - 1) * pageSize, currentPage * pageSize).map(trailer => {
          const d = trailer.data;
          return <tr key={trailer.id} className="border-b border-zinc-900/70 text-zinc-300 transition last:border-0 hover:bg-zinc-800/15">
            <td className="px-4 py-3"><Link href={`/fleet/trailers/detail?id=${trailer.id}`} className="font-mono font-medium text-zinc-200 transition hover:text-blue-400">{d.unitNumber}</Link></td>
            <td className="px-4 py-3"><div>{[d.year, d.make].filter(Boolean).join(" ") || "—"}</div><div className="mt-0.5 font-mono text-[11px] text-zinc-600">{d.vin || "No VIN recorded"}</div></td>
            <td className="px-4 py-3 font-mono text-zinc-400">{[d.plate, d.plateState].filter(Boolean).join(" · ") || "—"}</td>
            <td className="px-4 py-3">{d.registrationUrl ? <a href={d.registrationUrl} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1.5 text-[12px] text-blue-400 hover:text-blue-300"><FileText className="h-3.5 w-3.5" />Registration</a> : <span className="text-zinc-600">—</span>}</td>
            <td className="px-4 py-3 text-zinc-400">{d.storageLocation || "—"}</td>
            <td className="px-4 py-3"><TrailerStatusBadge value={d.status} /></td>
            <td className="px-4 py-3"><TrailerStatusBadge value={d.condition} /></td>
            <td className="px-4 py-3"><StatusBadge active={d.active} /></td>
            <td className="px-4 py-3"><RowActions onEdit={canManage ? () => setEditing(trailer) : undefined} /></td>
          </tr>;
        })}</tbody>}
      </table>
      {loading ? <LoadingTable columns={9} /> : !filtered.length && <EmptyState message={search || recordStatus !== "active" || availability || condition || storage ? "No trailers match your search or filters." : "No trailers yet. Add your first trailer to get started."} />}
    </TableShell>
    {!loading && <TablePagination page={currentPage} pageSize={pageSize} totalItems={filtered.length} totalPages={totalPages} onPageChange={setPage} onPageSizeChange={value => { setPageSize(value); setPage(1); }} />}
    {editing && canManage && <TrailerEditor key={editing.id} trailer={editing} onClose={() => setEditing(null)} onSaved={() => { setEditing(null); void load(); }} />}
  </div>;
}
