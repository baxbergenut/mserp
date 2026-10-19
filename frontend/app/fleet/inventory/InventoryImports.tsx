"use client";

import { useEffect, useState } from "react";
import { commitInventoryImport, fetchInventoryImports, previewInventoryImport, reviewInventoryImport } from "@/app/lib/api";
import type { InventoryImport, InventoryImportRow, InventorySnapshot } from "@/app/lib/types";
import { ErrorBanner, Field, Modal, controlClass } from "@/app/components/management/ManagementUI";
import { InventoryRecordFields, inventoryLabel, type InventoryContext } from "./InventoryForms";

export function InventoryImports({ context, onCommitted }: { context: InventoryContext; onCommitted: () => void }) {
  const [batches, setBatches] = useState<InventoryImport[]>([]), [batch, setBatch] = useState<InventoryImport | null>(null);
  const [busy, setBusy] = useState(false), [error, setError] = useState(""), [search, setSearch] = useState(""), [filter, setFilter] = useState("pending"), [page, setPage] = useState(1);
  const [editing, setEditing] = useState<InventoryImportRow | null>(null);
  useEffect(() => { void fetchInventoryImports().then(setBatches).catch(reason => setError(reason.message)); }, []);
  const counts = batch ? { include: batch.rows.filter(row => row.decision === "include").length, pending: batch.rows.filter(row => !row.reviewed).length, flagged: batch.rows.filter(row => row.issues?.length).length, archived: batch.rows.filter(row => row.decision !== "include").length } : null;
  const rows = batch?.rows.filter(row => (filter !== "pending" || !row.reviewed) && (filter !== "flagged" || row.issues?.length) && (filter !== "included" || row.decision === "include") && (!search || JSON.stringify([row.sources, row.record?.data, row.reason]).toLowerCase().includes(search.toLowerCase()))) || [];
  const apply = (row: InventoryImportRow) => { if (batch) setBatch({ ...batch, rows: batch.rows.map(item => item.key === row.key ? row : item) }); setEditing(null); };
  const save = async (commit = false) => {
    if (!batch || busy) return; setBusy(true); setError("");
    try { let result = await reviewInventoryImport(batch); setBatch(result); if (commit) { result = await commitInventoryImport(result); setBatch(result); onCommitted(); } setBatches(await fetchInventoryImports()); }
    catch (reason) { setError(reason instanceof Error ? reason.message : "Unable to save review"); }
    finally { setBusy(false); }
  };
  // Include proposed records so reviewers can link cameras and service lines to
  // devices not yet committed, without copying raw UUIDs from another row.
  const proposedContext: InventoryContext = { ...context, data: { ...context.data, assets: [...context.data.assets, ...(batch?.rows.filter(row => row.collection === "assets" && row.record.version === 0 && row.decision === "include").map(row => row.record) || [])] } };
  return <section className="space-y-4">
    <div><h2 className="text-base font-semibold">Migration review</h2><p className="mt-1 text-xs text-zinc-500">Administrator only. Upload a dated fleet snapshot, resolve identities and assignments, and review every operational outcome. Import never establishes warehouse stock or verified installations.</p></div>
    {error && <ErrorBanner message={error} />}
    <div className="flex flex-wrap items-end gap-3">
      <Field label="Source snapshot JSON (maximum 10 MB)"><input type="file" accept=".json,application/json" disabled={busy} className={controlClass} onChange={event => { const file = event.target.files?.[0]; if (!file) return; if (file.size > 10 * 1024 * 1024) { setError("Snapshot exceeds 10 MB"); return; } setBusy(true); setError(""); void file.text().then(text => previewInventoryImport(JSON.parse(text) as InventorySnapshot)).then(result => { setBatch(result); setFilter("pending"); }).catch(reason => setError(reason.message)).finally(() => setBusy(false)); }} /></Field>
      <Field label="Saved batches"><select className={controlClass} value={batch?.id || ""} onChange={event => setBatch(batches.find(item => item.id === event.target.value) || null)}><option value="">Select a batch…</option>{batches.map(item => <option key={item.id} value={item.id}>{item.capturedAt} · {item.status}</option>)}</select></Field>
    </div>
    {batch && <>
      <p className="break-all text-xs text-zinc-500">Captured {batch.capturedAt} · Source {batch.spreadsheetId} · Batch {batch.id} · {batch.status} · {counts?.include} included / {counts?.pending} awaiting review / {counts?.flagged} flagged / {counts?.archived} archived or excluded</p>
      <div className="flex flex-wrap gap-3"><input aria-label="Search import rows" className={`${controlClass} max-w-sm`} placeholder="Search source, identifier, unit…" value={search} onChange={event => { setSearch(event.target.value); setPage(1); }} /><select aria-label="Import outcome filter" className={`${controlClass} w-auto`} value={filter} onChange={event => { setFilter(event.target.value); setPage(1); }}>{["pending", "flagged", "included", "all"].map(value => <option key={value} value={value}>{value}</option>)}</select>{batch.status === "preview" && <><button disabled={busy} className="rounded-lg border border-zinc-700 px-4 py-2 text-sm" onClick={() => setBatch({ ...batch, rows: batch.rows.map(row => row.collection && !row.issues?.length && row.decision === "include" ? { ...row, reviewed: true } : row) })}>Mark unflagged rows reviewed</button><button disabled={busy} className="rounded-lg border border-zinc-700 px-4 py-2 text-sm" onClick={() => void save()}>Save review</button><button disabled={busy || !!counts?.pending} className="rounded-lg bg-blue-600 px-4 py-2 text-sm disabled:opacity-40" onClick={() => void save(true)}>Commit reviewed import</button></>}</div>
      <div className="space-y-2">{rows.slice((page - 1) * 40, page * 40).map(row => <div key={row.key} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-zinc-800 p-3 text-xs"><div className="min-w-0"><p className="font-medium text-zinc-200">{row.collection ? inventoryLabel(row.record) : "Archived source row"} · {row.decision} · {row.reviewed ? "Reviewed" : "Needs review"}</p><p className="mt-1 break-all text-zinc-500">{row.sources.join(", ")}</p>{row.issues?.length > 0 && <p className="mt-1 max-w-3xl text-amber-400">{Array.from(new Set(row.issues)).join(" · ")}</p>}{row.reason && <p className="mt-1 text-zinc-400">{row.reason}</p>}</div>{batch.status === "preview" && <button className="shrink-0 text-blue-400" onClick={() => setEditing(structuredClone(row))}>Review</button>}</div>)}</div>
      {!rows.length && <p className="text-sm text-zinc-500">No rows match this filter.</p>}
      {rows.length > 40 && <div className="flex gap-3 text-xs"><button disabled={page <= 1} onClick={() => setPage(value => value - 1)}>Previous</button><span>Page {page} of {Math.ceil(rows.length / 40)}</span><button disabled={page >= Math.ceil(rows.length / 40)} onClick={() => setPage(value => value + 1)}>Next</button></div>}
    </>}
    {editing && <Modal title="Review source outcome" description={editing.sources.join(", ")} isSaving={false} submitLabel="Apply to draft review" onClose={() => setEditing(null)} onSubmit={event => { event.preventDefault(); apply(editing); }}>
      <div className="space-y-4">
        {editing.issues?.length > 0 && <ul className="list-inside list-disc text-xs text-amber-400">{Array.from(new Set(editing.issues)).map(issue => <li key={issue}>{issue}</li>)}</ul>}
        <Field label="Outcome"><select className={controlClass} value={editing.decision} onChange={event => setEditing({ ...editing, decision: event.target.value as InventoryImportRow["decision"] })}>{editing.collection && <option value="include">Include / merge</option>}<option value="exclude">Exclude with reason</option><option value="archive">Archive only</option></select></Field>
        {editing.collection && editing.decision === "include" && <InventoryRecordFields key={editing.key} collection={editing.collection} value={editing.record.data} onChange={data => setEditing({ ...editing, record: { ...editing.record, data } })} context={proposedContext} />}
        <Field label="Resolution / exclusion reason"><textarea className={controlClass} required={editing.issues?.length > 0 || editing.decision !== "include"} maxLength={1000} value={editing.reason} onChange={event => setEditing({ ...editing, reason: event.target.value })} /></Field>
        <Field label="I reviewed this outcome"><input type="checkbox" required checked={editing.reviewed} onChange={event => setEditing({ ...editing, reviewed: event.target.checked })} /></Field>
      </div>
    </Modal>}
  </section>;
}
