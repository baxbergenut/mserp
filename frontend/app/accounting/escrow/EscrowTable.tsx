"use client";

import { useEffect, useState } from "react";
import { IntentLink } from "@/app/components/IntentLink";
import { ErrorBanner, ManagementSearch, TablePagination, TableShell, controlClass } from "@/app/components/management/ManagementUI";
import { fetchEscrows } from "@/app/lib/api";
import type { Escrow, PaginatedResponse } from "@/app/lib/types";
import { useViewState } from "@/app/lib/viewMemory";
import { usePermissions } from "@/app/lib/access";
import { decimalDisplay, hundredths } from "@/app/gross-board/board";

const money = (value: string) => decimalDisplay(hundredths(value), true);
const statusLabels = { paid: "Fully paid", partial: "Partially paid", unpaid: "Not paid" };

export function EscrowTable({ driverId = "" }: { driverId?: string }) {
  const permissions = usePermissions();
  const [search, setSearch] = useViewState("escrow:search", "");
  const [status, setStatus] = useViewState("escrow:status", "");
  const [page, setPage] = useViewState("escrow:page", 1);
  const [pageSize, setPageSize] = useViewState("escrow:pageSize", 25);
  const [data, setData] = useState<PaginatedResponse<Escrow> | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let cancelled = false;
    const timer = setTimeout(() => {
      setLoading(true); setError("");
      fetchEscrows({ page, pageSize, search: driverId ? "" : search, status: driverId ? "" : status, driverId }).then(result => {
        if (!cancelled) setData(result);
      }).catch(reason => { if (!cancelled) setError(reason instanceof Error ? reason.message : "Could not load escrow"); })
        .finally(() => { if (!cancelled) setLoading(false); });
    }, 150);
    return () => { cancelled = true; clearTimeout(timer); };
  }, [attempt, driverId, page, pageSize, search, status]);
  return <div className="space-y-4">
    <div className="flex flex-wrap items-center gap-3">
      {!driverId && <><ManagementSearch value={search} onChange={value => { setSearch(value); setPage(1); }} placeholder="Search drivers…" />
        <select aria-label="Escrow payment status" className={`${controlClass} sm:w-48`} value={status} onChange={event => { setStatus(event.target.value); setPage(1); }}>
          <option value="">All payment statuses</option><option value="paid">Fully paid</option><option value="partial">Partially paid</option><option value="unpaid">Not paid</option>
        </select></>}
      <button className="rounded-lg border border-zinc-800 px-3 py-2 text-xs text-zinc-400 hover:text-zinc-200" onClick={() => setAttempt(value => value + 1)}>Refresh</button>
    </div>
    {error && <ErrorBanner message={error} />}
    <TableShell><table className="w-full min-w-[800px] text-left text-xs" aria-label="Driver escrow balances">
      <thead><tr className="border-b border-zinc-800 text-zinc-500">{["Driver", "Escrow target", "Paid", "Remaining", "Status", "Payments"].map(label => <th className="px-4 py-3 font-medium" key={label}>{label}</th>)}</tr></thead>
      <tbody>{loading ? <tr><td colSpan={6} className="px-4 py-10 text-center text-zinc-500" role="status">Loading escrow…</td></tr> : error ? null : !data?.items.length ? <tr><td colSpan={6} className="px-4 py-10 text-center text-zinc-500">No escrow records match these filters.</td></tr> : data.items.map(item => <tr key={item.id} className="border-b border-zinc-800/60 align-top text-zinc-300">
        <td className="px-4 py-3">{item.driverId && permissions.includes("fleet.read") ? <IntentLink className="text-blue-400 hover:text-blue-300" href={`/drivers/detail?id=${item.driverId}`}>{item.driverName}</IntentLink> : item.driverName}{!item.active && <span className="ml-2 text-[10px] text-zinc-500">Inactive</span>}</td>
        <td className="px-4 py-3 font-mono">{money(item.amount)}</td><td className="px-4 py-3 font-mono text-emerald-400">{money(item.paidAmount)}</td><td className="px-4 py-3 font-mono">{money(item.remainingAmount)}</td>
        <td className="px-4 py-3"><span className={`whitespace-nowrap rounded-full px-2 py-1 text-[11px] ${item.status === "paid" ? "bg-emerald-500/10 text-emerald-400" : item.status === "partial" ? "bg-amber-500/10 text-amber-400" : "bg-zinc-800 text-zinc-400"}`}>{statusLabels[item.status]}</span></td>
        <td className="px-4 py-3">{item.payments.length || hundredths(item.openingPaid) > BigInt(0) ? <details><summary className="cursor-pointer text-blue-400">Payment history</summary><div className="mt-3 space-y-2">
          {hundredths(item.openingPaid) > BigInt(0) && <p className="flex justify-between gap-4"><span>Previously paid</span><span className="font-mono">{money(item.openingPaid)}</span></p>}
          {item.payments.map(payment => <p key={payment.weekStart} className="flex justify-between gap-4"><span>{payment.weekStart < "2026-09-28" ? "Previously paid" : permissions.includes("payroll.read") ? <IntentLink href={`/accounting/driver-pay?weekStart=${payment.weekStart}`}>Week of {payment.weekStart}</IntentLink> : `Week of ${payment.weekStart}`}</span><span className="font-mono">{money(payment.amount)}</span></p>)}
        </div></details> : <span className="text-zinc-500">No payments yet</span>}</td>
      </tr>)}</tbody>
    </table></TableShell>
    {!loading && !error && data && <TablePagination page={data.page} pageSize={data.pageSize} totalItems={data.total} totalPages={data.totalPages} onPageChange={setPage} onPageSizeChange={value => { setPageSize(value); setPage(1); }} />}
  </div>;
}
