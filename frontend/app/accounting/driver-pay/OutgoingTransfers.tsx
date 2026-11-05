"use client";

import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { fetchOutgoingLoadTransfers } from "@/app/lib/api";
import type { OutgoingLoadTransfer } from "@/app/lib/types";
import { shortDate } from "@/app/gross-board/board";
import { ReturnLoadDialog } from "./ReturnLoadDialog";

const TransferContext = createContext<{ rows: OutgoingLoadTransfer[]; canEdit: boolean; disabled: boolean; open: (row: OutgoingLoadTransfer) => void }>({ rows: [], canEdit: false, disabled: true, open: () => {} });
export const useOutgoingTransfers = () => useContext(TransferContext);

export function OutgoingTransfers({ investor, week, statementId, search, dispatcherId, refreshKey, canEdit, disabled, onReturned, children }: {
  investor: boolean; week: string; statementId: string; search: string; dispatcherId: string;
  refreshKey: unknown; canEdit: boolean; disabled: boolean; onReturned: () => void; children: ReactNode;
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
  const value = useMemo(() => ({ rows: result?.key === key ? result.rows : [], canEdit, disabled: disabled || !!error, open: setTarget }), [result,key,canEdit,disabled,error]);
  return <TransferContext.Provider value={value}>{children}
    {error && <p role="alert" className="text-xs text-red-300">{error} <button type="button" onClick={() => setRetry(value => value+1)} className="underline">Retry transferred loads</button></p>}
    {target && <ReturnLoadDialog target={target} investor={investor} onClose={() => setTarget(null)} onReturned={() => { setRetry(value => value+1); onReturned(); }} />}
  </TransferContext.Provider>;
}

export function OutgoingTransferNote({ row }: { row: OutgoingLoadTransfer }) {
  const { canEdit, disabled, open } = useOutgoingTransfers();
  return <span className="inline-flex items-center gap-2 whitespace-nowrap text-[11px] text-zinc-500" title="Not included in this week’s payroll totals">
    <span className="font-mono text-zinc-400">{row.loadNumber}</span><span>Moved to {shortDate(row.settlementDate)}</span>
    {canEdit && <button data-grid-ignore type="button" disabled={disabled} aria-label={`Return ${row.loadNumber} to original date`} onClick={() => open(row)} className="text-blue-400 underline-offset-2 hover:underline disabled:opacity-40">Return</button>}
  </span>;
}

// Keep restoration reachable when there is no payable statement to expand.
export function UnlistedOutgoingTransfers({ statementIds }: { statementIds: string[] }) {
  const { rows } = useOutgoingTransfers();
  const absent = rows.filter(row => !statementIds.includes(row.statementId));
  if (!absent.length) return null;
  return <div className="text-xs">{absent.map(row => <div key={`${row.driverId}:${row.sourceDate}:${row.sourceSlot}`} className="flex min-h-8 items-center gap-3 border-b border-zinc-800 px-3">
    <span className="text-zinc-400">{row.driverName} · {row.truckUnit} · {shortDate(row.sourceDate)}</span><OutgoingTransferNote row={row} />
  </div>)}</div>;
}
