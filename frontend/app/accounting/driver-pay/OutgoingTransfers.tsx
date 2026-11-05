"use client";

import { useEffect, useState } from "react";
import { fetchOutgoingLoadTransfers } from "@/app/lib/api";
import type { OutgoingLoadTransfer } from "@/app/lib/types";
import { shortDate } from "@/app/gross-board/board";
import { ReturnLoadDialog } from "./ReturnLoadDialog";

export function OutgoingTransfers({ investor, week, statementId, search, dispatcherId, refreshKey, canEdit, disabled, onReturned }: {
  investor: boolean; week: string; statementId: string; search: string; dispatcherId: string;
  refreshKey: unknown; canEdit: boolean; disabled: boolean; onReturned: () => void;
}) {
  const [result, setResult] = useState<{ key: string; rows: OutgoingLoadTransfer[] } | null>(null);
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  const [target, setTarget] = useState<OutgoingLoadTransfer | null>(null);
  const key = JSON.stringify([investor,week,statementId,search,dispatcherId]);
  useEffect(() => {
    const abort = new AbortController();
    fetchOutgoingLoadTransfers(investor, week, { statementId, search, dispatcherId }, abort.signal).then(value => {
      if (!abort.signal.aborted) { setResult({ key, rows: value.transfers ?? [] }); setError(""); }
    }).catch(err => { if (!abort.signal.aborted) setError(err instanceof Error ? err.message : "Unable to load transferred loads"); });
    return () => abort.abort();
  }, [investor,week,statementId,search,dispatcherId,key,refreshKey,retry]);
  const rows = result?.key === key ? result.rows : [];
  if (!rows.length && !error) return null;
  return <section aria-label="Transferred out" className="rounded-lg border border-zinc-700 px-4 py-3 text-xs">
    <h2 className="font-medium text-zinc-200">Transferred out{rows.length ? ` (${rows.length})` : ""}</h2>
    <p className="mt-1 text-zinc-500">Moved to next week. Not included in this week’s payroll totals.</p>
    {error && <p role="alert" className="mt-2 text-red-300">{error} <button type="button" onClick={() => setRetry(value => value+1)} className="underline">Retry transferred loads</button></p>}
    <ul className="mt-2 divide-y divide-zinc-800">{rows.map(row => <li key={`${row.driverId}:${row.sourceDate}:${row.sourceSlot}`} className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2">
      <span className="font-mono text-zinc-200">{row.loadNumber}</span><span>{row.driverName}{row.truckUnit ? ` · ${row.truckUnit}` : ""}</span>
      <span className="text-zinc-400">{shortDate(row.sourceDate)} → {shortDate(row.settlementDate)}</span>
      {canEdit && <button type="button" disabled={disabled || !!error} aria-label={`Return ${row.loadNumber} to original date`} onClick={() => setTarget(row)} className="ml-auto rounded border border-zinc-700 px-2 py-1 text-blue-300 hover:bg-zinc-800 disabled:opacity-40">Return to original date</button>}
    </li>)}</ul>
    {target && <ReturnLoadDialog target={target} investor={investor} onClose={() => setTarget(null)} onReturned={() => { setRetry(value => value+1); onReturned(); }} />}
  </section>;
}
