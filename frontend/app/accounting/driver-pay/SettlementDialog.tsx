"use client";
import { useState } from "react";
import { settleDriverPay, settleInvestorPay } from "@/app/lib/api";
import type { DriverPayWeek } from "@/app/lib/types";
import { Modal, ErrorBanner, controlClass } from "@/app/components/management/ManagementUI";
import { decimalDisplay } from "@/app/gross-board/board";
import { driverTotals } from "./pay";

export function SettlementDialog({ report, driverId, reopen, blockedIssue, onClose, onSaved, investor = false }: {
  investor?: boolean; report: DriverPayWeek; driverId?: string; reopen: boolean; blockedIssue?: string; onClose: () => void; onSaved: (report: DriverPayWeek) => void;
}) {
  const [reason, setReason] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const selected = report.drivers.filter(d => (!driverId ? !d.truckInactive : d.id === driverId) && Boolean(d.settlement?.finalized) === reopen);
  const net = selected.reduce((sum, d) => sum + driverTotals(d, d.edits).payable, BigInt(0));
  const issues = [...new Set([
    ...(blockedIssue ? [blockedIssue] : []),
    ...(report.issues ?? []),
    ...selected.flatMap(d => [
      ...(d.issues ?? []).map(issue => `${d.fullName}: ${issue}`),
      ...d.loads.flatMap(load => [
        ...load.issues.map(issue => `${d.fullName} · ${load.loadNumber}: ${issue}`),
        ...(load.fee === "" ? [`${d.fullName} · ${load.loadNumber}: Pay amount is missing`] : []),
      ]),
    ]),
  ])];
  return <Modal
    title={reopen ? `Reopen ${driverId ? (investor ? "truck" : "driver") : "week"}` : "Finalization issues"}
    description={`Week of ${report.weekStart} · ${selected.length} ${investor ? "truck" : "driver"}${selected.length === 1 ? "" : "s"}`}
    isSaving={busy}
    submitDisabled={selected.length === 0 || (!reopen && !blockedIssue)}
    submitLabel={reopen ? "Reopen settlement" : "Finalize anyway"}
    onClose={onClose}
    onSubmit={event => {
      event.preventDefault();
      if (busy || (!reopen && !blockedIssue)) return;
      setBusy(true); setError("");
      void (investor ? settleInvestorPay : settleDriverPay)(report.weekStart, report.revision, driverId, reopen, reason, !reopen)
        .then(onSaved).catch(e => setError(e.message)).finally(() => setBusy(false));
    }}
  >
    <div className="space-y-4">
      {error && <ErrorBanner message={error} />}
      {reopen && <p className="text-sm text-zinc-400">Unlock these settlements for corrections. The original records stay in history.</p>}
      <div className="max-h-64 overflow-auto rounded-lg border border-zinc-800">
        {selected.map(d => <div key={d.id} className="flex items-center justify-between gap-4 border-b border-zinc-800 px-3 py-2 text-sm text-zinc-300"><span>{d.fullName}</span><span className="font-mono">{decimalDisplay(driverTotals(d, d.edits).payable, true)}</span></div>)}
      </div>
      <p className="text-right text-sm text-zinc-300">Net settlement: <strong className="font-mono">{decimalDisplay(net, true)}</strong></p>
      {!reopen && <ul className="max-h-48 list-disc space-y-1 overflow-auto pl-5 text-sm text-amber-300">{issues.map((issue, index) => <li key={index}>{issue}</li>)}</ul>}
      {reopen && <label className="block text-sm text-zinc-400">Reason for reopening<textarea required maxLength={2000} className={`${controlClass} mt-2`} value={reason} onChange={e => setReason(e.target.value)} /></label>}
    </div>
  </Modal>;
}
