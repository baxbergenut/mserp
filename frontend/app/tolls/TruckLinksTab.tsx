"use client";

import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import Link from "@/app/components/AccessLink";
import { TableFilters } from "@/app/components/management/TableFilters";
import { TransactionFlagIcon } from "../components/TransactionFlagIcon";
import { fetchPrePassTruckLinks, previewPrePassTruckLink, savePrePassTruckLink, undoPrePassTruckLink, reviewTollFlag, reviewTollFlagsBulk } from "../lib/api";
import { usePermissions } from "../lib/access";
import type { PrePassTruckLinks, PrePassTruckLinkInput, PrePassTruckMapping, PrePassTruckPreview, Toll } from "../lib/types";
import { controlClass, EmptyState, ErrorBanner, Field, Modal, TablePagination, TableShell } from "../components/management/ManagementUI";

const money = (value: string | number) => Number(value).toLocaleString("en-US", { style: "currency", currency: "USD" });
const identity = (key: string) => key.startsWith("tag:") ? `Tag ${key.slice(4)}` : `Unit ${key.slice(5)}`;

export function TruckLinksTab({ actions }: { actions?: ReactNode }) {
  const permissions = usePermissions();
  const canWrite = permissions.includes("tolls.link"), canProfile = permissions.includes("trucks.read");
  const [data, setData] = useState<PrePassTruckLinks | null>(null);
  const [from, setFrom] = useState("");
  const [showReviewed, setShowReviewed] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(25);
  const [selected, setSelected] = useState<string[]>([]);
  const saving = useRef(false);
  const [undo, setUndo] = useState<PrePassTruckMapping | null>(null);
  const [input, setInput] = useState<PrePassTruckLinkInput | null>(null);
  const [preview, setPreview] = useState<PrePassTruckPreview | null>(null);
  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      const result = await fetchPrePassTruckLinks(from, signal, showReviewed);
      if (!signal?.aborted) { setData(result); setSelected(ids => ids.filter(id => result.transactions.some(t => t.id === id))); }
    } catch (e) { if (!signal?.aborted) setError(e instanceof Error ? e.message : "Could not load tolls."); }
  }, [from, showReviewed]);
  useEffect(() => { const c = new AbortController(); const timer = setTimeout(() => void load(c.signal), 0); return () => { clearTimeout(timer); c.abort(); }; }, [load]);
  useEffect(() => { if (!notice) return; const timer = setTimeout(() => setNotice(""), 5000); return () => clearTimeout(timer); }, [notice]);
  function open(ids: string[]) {
    setError(""); setPreview(null);
    setInput({ id: crypto.randomUUID(), transactionIds: ids, environment: "", identityKey: "", truckId: "", from: "", to: "", revision: "" });
  }
  async function markReviewed(toll: Toll) {
    if (saving.current || !toll.reviewRevision) return;
    saving.current = true; setBusy(true); setError("");
    try {
      await reviewTollFlag(toll.id, { id: crypto.randomUUID(), revision: toll.reviewRevision, version: toll.reviewVersion || "", reviewed: !toll.reviewed });
      setNotice(toll.reviewed ? "Reopened for review." : toll.truckId ? "Marked reviewed." : "Marked reviewed. This transaction still needs a truck link.");
      await load();
    } catch (e) { setError(e instanceof Error ? e.message : "Could not update review status."); }
    finally { saving.current = false; setBusy(false); }
  }
  async function markSelectedReviewed() {
    if (saving.current || flaggedSelected.length === 0 || flaggedSelected.length > 100) return;
    saving.current = true; setBusy(true); setError("");
    const changes = flaggedSelected.map(t => ({ tollId: t.id, id: crypto.randomUUID(), revision: t.reviewRevision!, version: t.reviewVersion || "" }));
    try {
      const result = await reviewTollFlagsBulk(changes);
      const completed = new Set(changes.map(t => t.tollId));
      setSelected(ids => ids.filter(id => !completed.has(id)));
      setNotice(`${result.reviewed} transaction${result.reviewed === 1 ? "" : "s"} marked reviewed.`);
      await load();
    } catch (e) { setError(e instanceof Error ? e.message : "Could not review selected transactions."); await load(); }
    finally { saving.current = false; setBusy(false); }
  }
  async function submit() {
    if (saving.current) return;
    saving.current = true; setBusy(true); setError("");
    try {
      if (undo) { await undoPrePassTruckLink(undo.id, undo.version); setUndo(null); setNotice("Mapping undone."); await load(); }
      else if (input && !preview) setPreview(await previewPrePassTruckLink(input));
      else if (input && preview && !preview.closed && preview.count > 0) {
        await savePrePassTruckLink({ ...input, revision: preview.revision });
        setInput(null); setPreview(null); setSelected([]); setNotice("Saved."); await load();
      }
    } catch (e) { setError(e instanceof Error ? e.message : "Could not link tolls."); setPreview(null); }
    finally { saving.current = false; setBusy(false); }
  }
  const transactions = data?.transactions ?? [];
  const flaggedSelected = transactions.filter(t => selected.includes(t.id) && t.truckId && t.flag?.status === "review" && !t.reviewed && t.reviewRevision);
  const totalPages = Math.max(1, Math.ceil(transactions.length / pageSize));
  const currentPage = Math.min(page, totalPages);
  const shown = transactions.slice((currentPage - 1) * pageSize, currentPage * pageSize);
  const truck = data?.trucks.find(t => t.id === input?.truckId);
  const reviewed = transactions.filter(t => input?.transactionIds?.includes(t.id));
  const allSelected = shown.length > 0 && shown.every(t => selected.includes(t.id));
  return <section className="space-y-4" aria-label="PrePass truck links">
    <div className="flex flex-wrap items-center gap-3">
      <TableFilters><label className="flex items-center gap-2 text-[13px] text-zinc-400">Posted since<input aria-label="Attention tolls posted since" disabled={busy} type="date" className={controlClass.replace("w-full", "w-auto")} value={from || data?.dateFrom || ""} onChange={e => { setFrom(e.target.value); setSelected([]); setData(null); setPage(1); }} /></label><label className="flex items-center gap-2 text-[13px] text-zinc-400"><input type="checkbox" disabled={busy} checked={showReviewed} onChange={e => { setShowReviewed(e.target.checked); setSelected([]); setData(null); setPage(1); }} />Show reviewed</label></TableFilters>
      {canWrite && selected.length > 0 && <><span className="text-[13px] text-zinc-400">{selected.length} selected · {flaggedSelected.length} flagged</span><button className="ui-button ui-button-primary" disabled={busy || selected.length > 100} onClick={() => open(selected)}>Link selected</button><button className="ui-button" disabled={busy || flaggedSelected.length === 0 || flaggedSelected.length > 100} onClick={() => void markSelectedReviewed()}>Mark selected reviewed</button><button className="ui-button" disabled={busy} onClick={() => setSelected([])}>Clear selection</button>{selected.length > 100 && <span className="text-[12px] text-amber-400">Select up to 100 transactions per action.</span>}</>}
      <div className="ml-auto">{actions}</div>
    </div>
    {error && !input && !undo && <ErrorBanner message={error} />}
    {notice && <p role="status" className="text-[13px] text-emerald-400">{notice}</p>}
    <TableShell><table className="w-full min-w-[780px] text-left text-[13px]">
      <thead><tr>
        {canWrite && <th className="px-3 py-2"><input type="checkbox" disabled={busy} aria-label="Select transactions on this page" checked={allSelected} onChange={e => setSelected(ids => e.target.checked ? [...new Set([...ids, ...shown.map(t => t.id)])] : ids.filter(id => !shown.some(t => t.id === id)))} /></th>}
        {["Post date", "Truck / transponder", "Plaza / route", "Amount", "Needs attention", ""].map((v, i) => <th key={i} className={`px-3 py-2 font-medium text-zinc-500 ${v === "Amount" ? "text-right" : ""}`}>{v}</th>)}
      </tr></thead>
      <tbody className="divide-y divide-zinc-800">{!data ? <tr><td colSpan={canWrite ? 7 : 6} className="p-8 text-center text-zinc-500" role="status">Loading…</td></tr> : !transactions.length ? <tr><td colSpan={canWrite ? 7 : 6}><EmptyState message="No toll transactions need attention in this date range." /></td></tr> : shown.map(t => <tr key={t.id}>
        {canWrite && <td className="px-3 py-2"><input type="checkbox" disabled={busy} aria-label={`Select transaction ${t.id}`} checked={selected.includes(t.id)} onChange={e => setSelected(ids => e.target.checked ? [...ids, t.id] : ids.filter(id => id !== t.id))} /></td>}
        <td className="px-3 py-2 font-mono tabular-nums">{t.postingDate}</td>
        <td className="px-3 py-2"><div>{t.truckId && canProfile ? <Link className="text-blue-400" href={`/trucks/detail?id=${t.truckId}`}>{t.truckUnit}</Link> : t.truckUnit || "Unknown"}</div><div className="font-mono text-[11px] text-zinc-500">Transponder {t.prePassTagId || "—"}</div>{t.equipmentUnit !== t.truckUnit && <div className="text-[11px] text-zinc-500">Source unit {t.equipmentUnit || "Unknown"}</div>}</td>
        <td className="px-3 py-2"><div>{[t.entryPlazaName || t.entryPlaza, t.exitPlazaName || t.exitPlaza].filter(Boolean).join(" → ")}</div><div className="text-[11px] text-zinc-500">{t.exitDate} {t.exitTime} · {t.agency}</div></td>
        <td className="px-3 py-2 text-right font-mono tabular-nums">{money(t.amount)}</td>
        <td className="px-3 py-2"><div className="flex items-center gap-1">{!t.truckId && <span className="text-amber-400">Unlinked</span>}{t.flag?.status === "review" && <><TransactionFlagIcon flag={t.flag} kind="toll" /><span className={t.reviewed ? "text-emerald-400" : "text-zinc-400"} title={t.reviewed ? `Reviewed by ${t.reviewedBy || "a user"} at ${t.reviewedAt}` : undefined}>{t.reviewed ? "Reviewed" : "Flagged"}</span></>}</div></td>
        <td className="px-3 py-2">{canWrite && <div className="flex flex-wrap gap-2"><button className="ui-button" disabled={busy} onClick={() => open([t.id])}>Link truck</button>{t.flag?.status === "review" && t.reviewRevision && <button className="ui-button" disabled={busy} onClick={() => void markReviewed(t)}>{t.reviewed ? "Reopen" : "Mark reviewed"}</button>}</div>}</td>
      </tr>)}</tbody>
    </table></TableShell>
    {data && <TablePagination page={currentPage} pageSize={pageSize} totalItems={transactions.length} totalPages={totalPages} onPageChange={setPage} onPageSizeChange={value => { setPageSize(value); setPage(1); }} />}
    {!!data?.mappings.length && <><h2 className="text-sm font-medium text-zinc-300">Saved truck mappings</h2><TableShell><table className="w-full text-left text-[13px]"><thead><tr>{["PrePass identity", "Truck", "Travel dates", "Linked tolls", ""].map((v, i) => <th key={i} className="px-3 py-2 font-medium text-zinc-500">{v}</th>)}</tr></thead><tbody className="divide-y divide-zinc-800">{data.mappings.map(m => <tr key={m.id}><td className="px-3 py-2">{identity(m.identityKey)}</td><td className="px-3 py-2">{canProfile ? <Link className="text-blue-400" href={`/trucks/detail?id=${m.truckId}`}>{m.unit}</Link> : m.unit}</td><td className="px-3 py-2">{m.from} → {m.to || "Ongoing"}</td><td className="px-3 py-2">{m.count}</td><td className="px-3 py-2">{canWrite && <button className="ui-button" onClick={() => { setError(""); setUndo(m); }}>Undo mapping</button>}</td></tr>)}</tbody></table></TableShell></>}
    {input && <Modal title={`Link ${input.transactionIds?.length} transaction${input.transactionIds?.length === 1 ? "" : "s"}`} isSaving={busy} submitLabel={preview ? "Link transactions" : "Preview selected tolls"} submitDisabled={!!preview && (preview.closed || preview.count === 0)} onClose={() => { setInput(null); setPreview(null); }} onSubmit={e => { e.preventDefault(); void submit(); }}><fieldset disabled={busy} className="space-y-4">
      {error && <ErrorBanner message={error} />}
      <Field label="Truck"><select required className={controlClass} value={input.truckId} onChange={e => { setInput({ ...input, truckId: e.target.value }); setPreview(null); setError(""); }}><option value="">Select truck</option>{data?.trucks.map(t => <option key={t.id} value={t.id}>{t.unit}{t.vin ? ` · ${t.vin}` : ""}{t.plate ? ` · ${t.plate}` : ""}</option>)}</select></Field>
      {truck && <p className="text-[13px] text-zinc-400">VIN: {truck.vin || "Not recorded"} · Plate: {truck.plate || "Not recorded"}</p>}
      <div className="max-h-48 overflow-y-auto text-[12px] text-zinc-400">{reviewed.map(t => <p key={t.id}>{t.exitDate} {t.exitTime} · {t.truckUnit || "Unknown"} · Transponder {t.prePassTagId || "—"} · {money(t.amount)}</p>)}</div>
      <p className="text-[12px] text-zinc-500">Only the selected transactions will be linked. Future imports keep their existing mapping rules. Load coverage flags are reassessed after linking.</p>
      {preview && <div role="status" className="rounded-lg border border-zinc-700 p-3 text-[13px]"><p>{preview.count} tolls · {money(preview.amount)} → truck {truck?.unit}</p>{preview.closed && <p className="text-red-400">Reopen affected statements before linking these tolls.</p>}</div>}
    </fieldset></Modal>}
    {undo && <Modal title="Undo truck mapping" isSaving={busy} submitLabel="Undo mapping" onClose={() => setUndo(null)} onSubmit={e => { e.preventDefault(); void submit(); }}>{error && <ErrorBanner message={error} />}<p className="text-sm text-zinc-300">Stop mapping {identity(undo.identityKey)} to {undo.unit} and unlink tolls assigned by this mapping, including later imports? Individually reviewed transaction links are preserved.</p></Modal>}
  </section>;
}
