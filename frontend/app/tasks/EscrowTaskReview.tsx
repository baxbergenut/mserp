"use client";

import { useEffect, useState } from "react";
import { completeEscrowTask, fetchEscrowTask } from "../lib/api";
import type { CustomTask, Escrow, EscrowTaskDetail } from "../lib/types";
import { usePermissions } from "../lib/access";
import { Modal, Field, controlClass, ErrorBanner } from "../components/management/ManagementUI";
import { EscrowReleaseForm } from "../accounting/escrow/EscrowReleaseForm";
import { decimalDisplay, hundredths } from "../gross-board/board";

export function EscrowTaskReview({ task, onClose, onCompleted }: { task: CustomTask; onClose: () => void; onCompleted: () => void }) {
 const permissions = usePermissions();
 const canWrite = permissions.includes("tasks.write") && permissions.includes("escrow.write");
 const [data, setData] = useState<EscrowTaskDetail | null>(null);
 const [revision, setRevision] = useState(0);
 const [reason, setReason] = useState("");
 const [error, setError] = useState("");
 const [saving, setSaving] = useState(false);
 const [release, setRelease] = useState<{ escrow: Escrow; anchor: { left: number; bottom: number } } | null>(null);
 useEffect(() => {
  let active = true;
  fetchEscrowTask(task.id).then(value => { if (active) { setData(value); setError(""); } }).catch(e => { if (active) setError(e instanceof Error ? e.message : "Could not load escrow review"); });
  return () => { active = false; };
 }, [task.id, revision]);
 async function submit() {
  if (!canWrite) { onClose(); return; }
  if (!data || release || saving) return;
  setSaving(true); setError("");
  try { await completeEscrowTask(task.id, { decision: data.decision, reason: data.decision === "released" ? "" : reason, versions: Object.fromEntries(data.escrows.map(e => [e.id, e.version])) }); onCompleted(); }
  catch(e) { setError(e instanceof Error ? e.message : "Could not complete review"); }
  finally { setSaving(false); }
 }
 return <><Modal title={task.title} isSaving={saving} onClose={onClose} submitLabel={canWrite ? "Complete escrow review" : "Close"} onSubmit={event => { event.preventDefault(); void submit(); }}>
  <div className="space-y-4">
   {error && <ErrorBanner message={error} />}
   {!data ? <p role="status" className="text-zinc-400">Loading escrow…</p> : <>
    <p className="text-xs text-zinc-400">Terminated {data.terminationDate} · Review due {data.dueDate}</p>
    <div className="space-y-2">{data.escrows.length === 0 ? <p className="text-sm text-zinc-400">No escrow balance is recorded.</p> : data.escrows.map(escrow => <div className="flex items-center gap-3 rounded-lg border border-zinc-800 p-3" key={escrow.id}><div className="flex-1 text-sm"><p>{escrow.driverName}</p><p className="text-xs text-zinc-400">Balance {decimalDisplay(hundredths(escrow.heldAmount), true)} · Released {decimalDisplay(hundredths(escrow.releasedAmount), true)}</p></div>{canWrite && hundredths(escrow.heldAmount) > BigInt(0) && <button type="button" className="ui-button" onClick={event => setRelease({ escrow, anchor: event.currentTarget.getBoundingClientRect() })}>Release</button>}</div>)}</div>
    {canWrite && data.decision !== "released" && <Field label="Reason"><textarea aria-label="Reason" required maxLength={5000} className={controlClass} rows={3} value={reason} onChange={event => setReason(event.target.value)} /></Field>}

   </>}
  </div>
 </Modal>{release && <EscrowReleaseForm {...release} onClose={() => setRelease(null)} onSaved={() => { setRelease(null); setData(null); setRevision(n => n + 1); }} />}</>;
}
