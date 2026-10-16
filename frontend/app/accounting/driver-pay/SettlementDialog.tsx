"use client";
import { useState } from "react";
import { settleDriverPay, settleInvestorPay } from "@/app/lib/api";
import type { DriverPayWeek } from "@/app/lib/types";
import { Modal, ErrorBanner, controlClass } from "@/app/components/management/ManagementUI";
import { decimalDisplay } from "@/app/gross-board/board";
import { driverTotals } from "./pay";

export function SettlementDialog({ report, driverId, reopen, onClose, onSaved, investor = false }: {
  investor?: boolean; report: DriverPayWeek; driverId?: string; reopen: boolean; onClose: () => void; onSaved: (report: DriverPayWeek) => void;
}) {
  const [reason, setReason] = useState("");
  const [override, setOverride] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const selected = report.drivers.filter(d => (!driverId ? !d.truckInactive : d.id === driverId) && Boolean(d.settlement?.finalized) === reopen);
  const net = selected.reduce((sum, d) => sum + driverTotals(d, d.edits).payable, BigInt(0));
  const issues = [
    ...(report.issues ?? []),
    ...selected.flatMap(d => [
      ...(d.issues ?? []).map(issue => `${d.fullName}: ${issue}`),
      ...d.loads.flatMap(load => [
        ...load.issues.map(issue => `${d.fullName} · ${load.loadNumber}: ${issue}`),
        ...(load.fee === "" ? [`${d.fullName} · ${load.loadNumber}: Pay amount is missing`] : []),
      ]),
    ]),
  ];
  return <Modal
    title={`${reopen ? "Reopen" : "Finalize"} ${driverId ? (investor ? "truck" : "driver") : "week"}`}
    description={`Week of ${report.weekStart} · ${selected.length} ${investor ? "truck" : "driver"}${selected.length === 1 ? "" : "s"}. ${investor ? "Whole-week actions include active trucks, regardless of filters." : "Whole-week actions include all settlements, regardless of filters."}`}
    isSaving={busy}
    submitDisabled={selected.length === 0}
    submitLabel={reopen ? "Reopen settlement" : override ? "Override and finalize" : "Finalize settlement"}
    onClose={onClose}
    onSubmit={event => {
      event.preventDefault();
      if (busy) return;
      setBusy(true); setError("");
      void (investor ? settleInvestorPay : settleDriverPay)(report.weekStart, report.revision, driverId, reopen, reason, !reopen && override)
        .then(onSaved).catch(e => setError(e.message)).finally(() => setBusy(false));
    }}
  >
    <div className="space-y-4">
      {error && <ErrorBanner message={error} />}
      <p className="text-sm text-zinc-400">{reopen ? "Unlock these settlements for corrections. The original records stay in history. Installments confirmed by finalization reopen; saved expense deductions remain paid until their amounts are corrected." : "Record these earnings and deductions as settled, save outstanding expense deductions, and confirm installment deductions. Amounts and source details will be fixed until the settlement is reopened."}</p>
      <div className="max-h-64 overflow-auto rounded-lg border border-zinc-800">
        {selected.map(d => <div key={d.id} className="flex items-center justify-between gap-4 border-b border-zinc-800 px-3 py-2 text-sm text-zinc-300"><span>{d.fullName}</span><span className="font-mono">{decimalDisplay(driverTotals(d, d.edits).payable, true)}</span></div>)}
      </div>
      <p className="text-right text-sm text-zinc-300">Net settlement: <strong className="font-mono">{decimalDisplay(net, true)}</strong></p>
      {!reopen && <>
        {issues.length > 0 && <div className="rounded-lg border border-amber-500/30 p-3 text-sm text-amber-400">
          <p className="font-medium">Issues needing review</p>
          <ul className="mt-2 max-h-40 list-disc space-y-1 overflow-auto pl-5">{issues.map((issue, index) => <li key={index}>{issue}</li>)}</ul>
        </div>}
        <label className="flex items-start gap-3 rounded-lg border border-amber-500/30 p-3 text-sm text-zinc-300">
          <input type="checkbox" className="mt-1" checked={override} disabled={busy} onChange={event => { setOverride(event.target.checked); setError(""); }} />
          <span><span className="block font-medium">Override all finalization checks</span><span className="mt-1 block text-xs text-zinc-400">Finalize the displayed amounts even with missing locations, unmatched loads, missing pay amounts, other review issues, or a future week. Missing pay amounts contribute zero. The override and unresolved issues stay in the settlement history.</span></span>
        </label>
      </>}
      {reopen && <label className="block text-sm text-zinc-400">Reason for reopening<textarea required maxLength={2000} className={`${controlClass} mt-2`} value={reason} onChange={e => setReason(e.target.value)} /></label>}
    </div>
  </Modal>;
}
