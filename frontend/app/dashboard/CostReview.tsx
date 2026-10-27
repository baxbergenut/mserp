"use client";

import { useEffect, useState } from "react";
import Link from "@/app/components/AccessLink";
import { Modal } from "@/app/components/management/ManagementUI";
import { fetchDashboardCostReview } from "@/app/lib/api";
import type { PaginatedResponse } from "@/app/lib/types";
import type { DashboardCostReview, DashboardPeriod } from "@/app/lib/dashboardTypes";
import { money, range } from "./display";

export function CostReview({ kind, period, onClose }: { kind: "fuel" | "tolls"; period: DashboardPeriod; onClose: () => void }) {
  const [page, setPage] = useState(1);
  const [result, setResult] = useState<{ key: string; data: PaginatedResponse<DashboardCostReview> } | null>(null);
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  const key = `${kind}:${period.dateFrom}:${period.dateTo}:${page}:${retry}`;
  const data = result?.key === key ? result.data : null;
  useEffect(() => {
    const controller = new AbortController(); let active = true;
    fetchDashboardCostReview({ preset: "custom", dateFrom: period.dateFrom, dateTo: period.dateTo, kind, page, pageSize: 25 }, controller.signal)
      .then(data => { if (active) { setResult({ key, data }); setError(""); } })
      .catch(e => { if (active) setError(e instanceof Error ? e.message : "Unresolved transactions could not be loaded"); });
    return () => { active = false; controller.abort(); };
  }, [kind, period.dateFrom, period.dateTo, page, key]);

  return <Modal title={`Unresolved ${kind}`} description={range(period.dateFrom, period.dateTo)} isSaving={false} submitLabel="" hideSubmit cancelLabel="Close" onClose={onClose} onSubmit={e => e.preventDefault()} wide>
    <p className="text-xs text-zinc-500">These transactions are excluded from company costs until their truck, dated ownership, or amount is resolved. Payroll finalization does not resolve them.</p>
    {error && <p role="alert" className="my-3 text-sm text-amber-400">{error} <button type="button" className="underline" onClick={() => { setError(""); setRetry(v => v + 1); }}>Retry</button></p>}
    {!data && !error && <p role="status" className="py-5">Loading unresolved transactions…</p>}
    {data && <>
      <div className="my-3 max-h-96 overflow-auto"><table className="block w-full text-left text-xs sm:table"><thead className="hidden sm:table-header-group"><tr>{["Date / transaction", kind === "fuel" ? "Truck / driver" : "Truck", "Amount", "Needs review"].map(label => <th key={label} className="border-b border-zinc-800 p-2 font-medium text-zinc-500">{label}</th>)}</tr></thead>
        <tbody className="block sm:table-row-group">{data.items.map(row => <tr key={row.id} className="block border-b border-zinc-800/40 py-2 sm:table-row sm:py-0">
          <td className="block p-2 sm:table-cell"><span className="block whitespace-nowrap">{row.date}</span><span className="block max-w-48 break-all text-zinc-500">{row.reference}</span></td>
          <td className="block p-2 sm:table-cell"><span className="text-zinc-500 sm:hidden">Truck: </span>{row.truckId ? <Link href={`/trucks/detail?id=${encodeURIComponent(row.truckId)}`} className="text-blue-400 hover:underline">{row.truckUnit}</Link> : row.truckUnit}{row.driverName && <span className="mt-1 block text-zinc-400">{row.driverName}</span>}{row.assignedUnits.length > 0 && <span className="mt-1 block text-zinc-500">Assigned at purchase: {row.assignedUnits.join(", ")}</span>}</td>
          <td className="block whitespace-nowrap p-2 tabular-nums sm:table-cell"><span className="text-zinc-500 sm:hidden">Amount: </span>{money(row.amount)}</td>
          <td className="block p-2 text-amber-500 sm:table-cell sm:min-w-44">{row.reason}{row.ownershipDate && <span className="mt-1 block text-zinc-500">Ownership date: {row.ownershipDate}</span>}</td>
        </tr>)}</tbody></table>
        {data.items.length === 0 && <p className="py-6 text-center text-zinc-500">No unresolved transactions in this period.</p>}
      </div>
      <div className="flex items-center justify-between gap-2 text-xs"><span>{data.total} transactions · Page {data.page} of {data.totalPages}</span><div className="flex gap-2"><button type="button" className="ui-button" disabled={data.page <= 1} onClick={() => setPage(data.page - 1)}>Previous</button><button type="button" className="ui-button" disabled={data.page >= data.totalPages} onClick={() => setPage(data.page + 1)}>Next</button></div></div>
    </>}
  </Modal>;
}
