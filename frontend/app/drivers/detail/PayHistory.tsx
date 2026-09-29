"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { fetchDriverPayHistory, fetchSettlementHistory } from "@/app/lib/api";
import type { DriverPayHistoryRow, PaginatedResponse, SettlementEvent } from "@/app/lib/types";
import { ErrorBanner, TablePagination } from "@/app/components/management/ManagementUI";
import { decimalDisplay } from "@/app/gross-board/board";
import { driverTotals } from "@/app/accounting/driver-pay/pay";
import { DriverCard } from "@/app/accounting/driver-pay/DriverCard";

function SettlementLog({ driverId, week }: { driverId: string; week: string }) {
  const [events, setEvents] = useState<SettlementEvent[] | null>(null);
  const [error, setError] = useState("");
  useEffect(() => { let cancelled = false; fetchSettlementHistory(driverId, week).then(rows => { if (!cancelled) setEvents(rows); }).catch(e => { if (!cancelled) setError(e.message); }); return () => { cancelled = true; }; }, [driverId, week]);
  return <div className="px-4 py-3 text-xs text-zinc-400">{error && <ErrorBanner message={error} />}{events?.map(event => <div key={event.version} className="flex flex-wrap gap-3 border-t border-zinc-800 py-2"><span className="capitalize">{event.action} · revision {event.version}</span><span>{event.actor} · {new Date(event.createdAt).toLocaleString()}</span><span>{event.reason}</span><span className="ml-auto font-mono">Recorded net {decimalDisplay(driverTotals(event.report, event.report.edits).payable, true)}</span></div>)}</div>;
}

export function PayHistory({ driverId }: { driverId: string }) {
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(25);
  const [data, setData] = useState<PaginatedResponse<DriverPayHistoryRow> | null>(null);
  const [open, setOpen] = useState<string | null>(null);
  const [error, setError] = useState("");
  useEffect(() => { let cancelled = false; fetchDriverPayHistory(driverId, page, pageSize).then(result => { if (!cancelled) { setData(result); setError(""); } }).catch(e => { if (!cancelled) setError(e.message); }); return () => { cancelled = true; }; }, [driverId, page, pageSize]);
  return <section className="space-y-4"><div><h2 className="text-sm font-semibold text-zinc-100">Weekly pay history</h2><p className="mt-1 text-xs text-zinc-500">Earnings, reimbursements and deductions from Driver Pay. Finalized settlements are fixed records; draft weeks follow current source details.</p></div>
    {error && <ErrorBanner message={error} />}
    {!data ? <p className="py-10 text-center text-sm text-zinc-500">Loading pay history…</p> : !data.items.length ? <p className="py-10 text-center text-sm text-zinc-500">No payroll weeks recorded yet.</p> : <div className="space-y-2">{data.items.map(({ weekStart, driver }) => {
      const totals = driverTotals(driver, driver.edits); const expanded = open === weekStart;
      return <div key={weekStart} className="overflow-hidden rounded-xl border border-zinc-800 bg-zinc-900/20">
        <button type="button" aria-expanded={expanded} aria-label={`Pay week ${weekStart}`} onClick={() => setOpen(expanded ? null : weekStart)} className="grid w-full grid-cols-2 items-center gap-4 p-4 text-left sm:grid-cols-5">
          <span className="text-sm font-medium text-zinc-200">Week of {weekStart}<span className={`mt-1 block text-xs ${driver.settlement?.finalized ? "text-emerald-400" : "text-amber-300"}`}>{driver.settlement?.finalized ? "Finalized" : driver.settlement ? "Reopened" : "Draft"}</span></span>
          <span className="text-xs text-zinc-500">Load earnings<span className="mt-1 block font-mono text-sm text-zinc-200">{decimalDisplay(totals.fee, true)}</span></span>
          <span className="text-xs text-zinc-500">Net adjustments<span className="mt-1 block font-mono text-sm text-zinc-300">{decimalDisplay(totals.payable - totals.fee, true)}</span></span>
          <span className="text-xs text-zinc-500">Loads<span className="mt-1 block font-mono text-sm text-zinc-300">{driver.loads.length}{totals.review ? " · Needs review" : ""}</span></span>
          <span className="text-xs text-zinc-500">Net payable<span className="mt-1 block font-mono text-base font-semibold text-zinc-100">{decimalDisplay(totals.payable, true)}</span></span>
        </button>
        {expanded && <><div className="flex flex-wrap justify-between gap-2 border-t border-zinc-800 px-4 py-3 text-xs"><span className="text-zinc-500">{driver.settlement?.finalized ? `Settled by ${driver.settlement.finalizedBy} on ${new Date(driver.settlement.finalizedAt).toLocaleString()}` : "Draft calculation; no finalized settlement yet."}</span><Link className="text-blue-400" href={`/accounting/driver-pay?weekStart=${weekStart}&driverId=${driverId}`}>Open this week in Driver Pay</Link></div><div className="overflow-x-auto"><table className="w-full min-w-[960px]"><tbody><DriverCard driver={driver} edits={driver.edits} disabled open onToggle={() => {}} onEdit={() => {}} chargeActionsDisabled onReload={async () => {}} /></tbody></table></div>{driver.settlement && <SettlementLog driverId={driverId} week={weekStart} />}</>}
      </div>;
    })}</div>}
    {data && data.total > 0 && <TablePagination page={data.page} pageSize={data.pageSize} totalItems={data.total} totalPages={data.totalPages} onPageChange={next => { setData(null); setOpen(null); setPage(next); }} onPageSizeChange={size => { setData(null); setOpen(null); setPage(1); setPageSize(size); }} />}
  </section>;
}
