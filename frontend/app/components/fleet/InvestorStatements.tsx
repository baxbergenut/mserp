"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { fetchInvestorHistory } from "@/app/lib/api";
import type { InvestorStatementWeek, PaginatedResponse } from "@/app/lib/types";
import { useViewState } from "@/app/lib/viewMemory";
import { driverTotals } from "@/app/accounting/driver-pay/pay";
import { decimalDisplay, hundredths } from "@/app/gross-board/board";
import { EmptyState, ErrorBanner, LoadingTable, TableShell } from "../management/ManagementUI";

export function InvestorStatements({ investorId, truckId }: { investorId: string; truckId?: string }) {
  const [page, setPage] = useViewState(`InvestorStatements:${investorId}:page`, 1);
  const [data, setData] = useState<PaginatedResponse<InvestorStatementWeek> | null>(null);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let cancelled = false;
    fetchInvestorHistory(investorId, page).then(value => { if (!cancelled) { setData(value); setError(""); } }).catch(e => { if (!cancelled) setError(e.message); });
    return () => { cancelled = true; };
  }, [investorId, page, attempt]);
  const rows = data?.items.flatMap(week => week.trucks.filter(t => !truckId || t.id === truckId).map(truck => ({ week: week.weekStart, truck })));
  return <section className="space-y-3"><h2>Investor statements</h2>
    {error && <div className="space-y-2"><ErrorBanner message={error} /><button className="ui-button" onClick={() => setAttempt(v => v + 1)}>Retry statements</button></div>}
    <TableShell>{!data ? <LoadingTable columns={7} /> : !rows?.length ? <EmptyState message="No investor statements in these weeks." /> : <table className="w-full min-w-[860px] table-fixed text-left" aria-label="Investor statement history"><colgroup>{[14, 8, 16, 15, 17, 15, 15].map((width, index) => <col key={index} style={{ width: `${width}%` }} />)}</colgroup><thead><tr>{["Week of", "Truck", "Driver gross", "Owner share", "Net adjustments", "Net payable", "Statement"].map((label, index) => <th key={label} className={index >= 2 && index <= 5 ? "text-right" : "text-left"}>{label}</th>)}</tr></thead><tbody>{rows.map(({ week, truck }) => {
      const totals = driverTotals(truck, truck.edits);
      const gross = truck.loads.reduce((n, load) => n + hundredths(load.driverGross), BigInt(0));
      return <tr key={`${week}:${truck.id}`}><td>{week}</td><td><Link href={`/trucks/detail?id=${truck.id}`}>{truck.truckUnit}</Link></td><td className="whitespace-nowrap text-right tabular-nums">{decimalDisplay(gross, true)}</td><td className="whitespace-nowrap text-right tabular-nums">{decimalDisplay(totals.fee, true)}</td><td className="whitespace-nowrap text-right tabular-nums">{decimalDisplay(totals.payable - totals.fee, true)}</td><td className="whitespace-nowrap text-right tabular-nums">{decimalDisplay(totals.payable, true)}{totals.review > 0 && <span className="ml-2 text-xs text-amber-300">Review</span>}</td><td><Link className={truck.settlement?.finalized ? "text-emerald-400" : "text-blue-400"} href={`/accounting/investor-pay?weekStart=${week}&truckId=${truck.id}`}>{truck.settlement?.finalized ? "Finalized" : "Draft"} →</Link></td></tr>;
    })}</tbody></table>}</TableShell>
    {data && data.totalPages > 1 && <div className="flex items-center justify-end gap-3"><span className="text-xs text-zinc-400">Page {data.page} of {data.totalPages}</span><button className="ui-button" disabled={data.page <= 1} onClick={() => { setData(null); setPage(data.page - 1); }}>Newer</button><button className="ui-button" disabled={data.page >= data.totalPages} onClick={() => { setData(null); setPage(data.page + 1); }}>Older</button></div>}
  </section>;
}
