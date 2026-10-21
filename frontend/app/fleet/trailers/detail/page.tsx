"use client";

import { useEffect, useState } from "react";
import { FileBadge, Pencil, Truck } from "lucide-react";
import { PageHeader } from "@/app/components/PageHeader";
import { ErrorBanner, StatusBadge } from "@/app/components/management/ManagementUI";
import { usePermissions } from "@/app/lib/access";
import { fetchTrailers } from "@/app/lib/api";
import type { InventoryRecord } from "@/app/lib/types";
import { StatusBadge as TrailerStatusBadge } from "../../inventory/InventoryStatus";
import { TrailerEditor } from "../TrailerEditor";

function Details({ values }: { values: [string, string | undefined][] }) {
  return <dl className="grid gap-5 text-xs sm:grid-cols-2">{values.map(([label, value]) => <div key={label}><dt className="text-zinc-500">{label}</dt><dd className="mt-1 break-words text-zinc-300">{value || "Not recorded"}</dd></div>)}</dl>;
}

export default function TrailerDetailPage() {
  const canManage = usePermissions().includes("trailers.manage");
  const [trailer, setTrailer] = useState<InventoryRecord | null>(null);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [editing, setEditing] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    const id = new URLSearchParams(window.location.search).get("id");
    const timer = window.setTimeout(() => {
      setError("");
      if (!id) { setError("No trailer was selected."); return; }
      void fetchTrailers(controller.signal).then(records => {
        if (controller.signal.aborted) return;
        const record = records.find(item => item.id === id);
        if (!record) { setError("Trailer not found."); return; }
        setTrailer(record);
      }).catch(reason => { if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : "Failed to load trailer"); });
    }, 0);
    return () => { window.clearTimeout(timer); controller.abort(); };
  }, [attempt]);

  const d = trailer?.data;
  return <div className="space-y-5 animate-fade-in">
    {error && <><ErrorBanner message={error} /><button className="text-sm text-blue-400" onClick={() => setAttempt(value => value + 1)}>Retry trailer</button></>}
    {!trailer && !error && <p className="py-12 text-center text-zinc-500">Loading trailer…</p>}
    {trailer && d && <>
      <header className="overflow-hidden rounded-2xl border border-zinc-800 bg-zinc-900/30">
        <div className="flex flex-wrap items-start justify-between gap-4 p-5 sm:p-6">
          <div className="min-w-0"><PageHeader><div className="flex flex-wrap items-center gap-3"><Truck className="h-5 w-5 shrink-0 text-zinc-500" /><h1 className="text-xl font-semibold text-zinc-100">Trailer {d.unitNumber}</h1><StatusBadge active={d.active} /></div></PageHeader><p className="break-all font-mono text-xs text-zinc-400">VIN · {d.vin || "Not recorded"}</p></div>
          {canManage && <button className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-500" onClick={() => setEditing(true)}><Pencil className="h-3.5 w-3.5" />Edit trailer</button>}
        </div>
        <dl className="grid grid-cols-1 gap-px border-t border-zinc-800 bg-zinc-800 sm:grid-cols-2 md:grid-cols-4">
          <div className="min-w-0 break-words bg-zinc-950/90 px-5 py-4"><dt className="text-xs text-zinc-500">Storage location</dt><dd className="mt-1 text-sm font-medium text-zinc-200">{d.storageLocation || "Not recorded"}</dd></div>
          <div className="min-w-0 bg-zinc-950/90 px-5 py-4"><dt className="text-xs text-zinc-500">Availability</dt><dd className="mt-1"><TrailerStatusBadge value={d.status} /></dd></div>
          <div className="min-w-0 bg-zinc-950/90 px-5 py-4"><dt className="text-xs text-zinc-500">Condition</dt><dd className="mt-1"><TrailerStatusBadge value={d.condition} /></dd></div>
          <div className="min-w-0 break-words bg-zinc-950/90 px-5 py-4"><dt className="text-xs text-zinc-500">License plate</dt><dd className="mt-1 text-sm font-medium text-zinc-200">{[d.plate, d.plateState].filter(Boolean).join(" · ") || "Not recorded"}</dd></div>
        </dl>
      </header>
      <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1fr)_320px]">
        <div className="min-w-0 space-y-5">
          <section className="rounded-xl border border-zinc-800 p-5"><h2 className="mb-4 text-sm font-semibold text-zinc-100">Vehicle details</h2><Details values={[["Unit number", d.unitNumber], ["VIN", d.vin], ["Year", d.year], ["Make", d.make], ["License plate", d.plate], ["Plate state", d.plateState]]} /></section>
          <section className="rounded-xl border border-zinc-800 p-5"><h2 className="mb-4 flex items-center gap-2 text-sm font-semibold text-zinc-100"><FileBadge className="h-4 w-4 text-zinc-500" />Registration & documents</h2>{d.registrationUrl ? <a href={d.registrationUrl} target="_blank" rel="noreferrer" className="text-xs text-blue-400 hover:text-blue-300">Open registration</a> : <p className="text-xs text-zinc-500">No registration linked.</p>}</section>
          <section className="rounded-xl border border-zinc-800 p-5"><h2 className="mb-4 text-sm font-semibold text-zinc-100">Notes</h2><p className="whitespace-pre-wrap break-words text-[13px] text-zinc-300">{d.notes || "No notes recorded."}</p></section>
        </div>
        <aside className="min-w-0 rounded-xl border border-zinc-800 p-5"><h2 className="mb-4 text-sm font-semibold text-zinc-100">Storage & usage</h2><dl className="space-y-4 text-xs"><div><dt className="text-zinc-500">Usage notes</dt><dd className="mt-1 whitespace-pre-wrap break-words text-zinc-300">{d.usage || "Not recorded"}</dd></div></dl></aside>
      </div>
      {editing && canManage && <TrailerEditor key={trailer.id} trailer={trailer} onClose={() => setEditing(false)} onSaved={record => { setTrailer(record); setEditing(false); }} />}
    </>}
  </div>;
}
