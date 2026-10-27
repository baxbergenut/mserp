"use client";
import { useEffect, useState } from "react";
import Link from "@/app/components/AccessLink";
import { Modal, controlClass } from "@/app/components/management/ManagementUI";
import { fetchDashboardLoads } from "@/app/lib/api";
import type { PaginatedResponse } from "@/app/lib/types";
import type { DashboardLoad, DashboardPeriod, DashboardMetric } from "@/app/lib/dashboardTypes";
import { money, number } from "./display";

export function LoadDetails({ period, selection, current, onClose }: { period: DashboardPeriod; selection: { dispatcherId: string; inclusion: string }; current?: DashboardMetric; onClose: () => void }) {
  const [inclusion, setInclusion] = useState(selection.inclusion);
  const [page, setPage] = useState(1);
  const [result, setResult] = useState<{ key: string; data: PaginatedResponse<DashboardLoad> } | null>(null);
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  const key = `${page}:${inclusion}`;
  const data = result?.key === key ? result.data : null;
  useEffect(() => { const controller = new AbortController(); let active = true;
    fetchDashboardLoads({ preset: "custom", dateFrom: period.dateFrom, dateTo: period.dateTo, dispatcherId: selection.dispatcherId, inclusion, page, pageSize: 25 }, controller.signal).then(data => { if (active) { setResult({ key, data }); setError(""); } }).catch(e => { if (active) setError(e instanceof Error ? e.message : "Load details could not be loaded"); });
    return () => { active = false; controller.abort(); };
  }, [period.dateFrom, period.dateTo, selection.dispatcherId, inclusion, page, key, retry]);
  return <Modal title="Gross reconciliation" description="Open a row on Gross Board to review its source." isSaving={false} submitLabel="" hideSubmit cancelLabel="Close" onClose={onClose} onSubmit={e => e.preventDefault()} wide>
    {current && !selection.dispatcherId && <div className="grid gap-2 rounded-lg bg-zinc-800/20 p-3 text-xs sm:grid-cols-2"><span>Raw load-row gross: <strong>{money(current.rawGross)}</strong></span><span>Counted original gross: <strong>{money(current.gross)}</strong></span><span>Provisional: {money(current.provisionalGross)}</span><span>Conflicting rows: {money(current.conflictGross)}</span><span>Repeated rows: {money(current.repeatedGross)}</span><span>Missing original gross: {current.missingGross}</span></div>}
    <p className="my-3 text-xs text-zinc-500">Included = first resolved occurrence. Provisional = unresolved identity. Conflict = differing values or drivers for one linked load. Repeat = a later identical occurrence, even across reporting periods. Gross Board itself is unchanged.</p>
    <select aria-label="Load inclusion" className={controlClass} value={inclusion} onChange={e => { setInclusion(e.target.value); setPage(1); }}>{[["", "All rows"], ["included", "Included"], ["provisional", "Provisional"], ["conflict", "Conflicts"], ["repeat", "Repeats"]].map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select>
    {error && <p role="alert" className="my-3 text-sm text-amber-400">{error} <button type="button" className="underline" onClick={() => setRetry(v => v + 1)}>Retry</button></p>}
    {!data && !error && <p role="status" className="py-5">Loading load details…</p>}
    {data && <><div className="my-3 max-h-96 overflow-auto"><table className="w-full text-left text-xs"><thead><tr>{["Date / load", "Driver / dispatcher", "Original", "Driver Gross", "Miles", "Included?"].map(label => <th key={label} className="border-b border-zinc-800 p-2 font-medium text-zinc-500">{label}</th>)}</tr></thead><tbody>{data.items.map(row => <tr key={`${row.driverId}:${row.date}:${row.slot}`} className="border-b border-zinc-800/40"><td className="p-2"><span className="block text-zinc-500">{row.date}</span><Link className="text-blue-400 hover:underline" href={`/gross-board?${new URLSearchParams({ driverId: row.driverId, date: row.date, slot: String(row.slot), loadNumber: row.loadNumber })}`}>{row.loadNumber}</Link></td><td className="p-2">{row.driverName}<span className="block text-zinc-500">{row.dispatcherName}</span></td><td className="p-2 tabular-nums">{money(row.gross)}</td><td className="p-2 tabular-nums">{money(row.driverGross)}</td><td className="p-2 tabular-nums">{number(row.miles)}</td><td className={`p-2 ${row.inclusion === "included" ? "text-emerald-400" : "text-amber-400"}`}>{row.inclusion}</td></tr>)}</tbody></table>{data.items.length === 0 && <p className="py-6 text-center text-zinc-500">No matching rows.</p>}</div><div className="flex items-center justify-between text-xs"><span>{data.total} rows · Page {data.page} of {data.totalPages}</span><div className="flex gap-2"><button type="button" className="ui-button" disabled={data.page <= 1} onClick={() => setPage(data.page - 1)}>Previous</button><button type="button" className="ui-button" disabled={data.page >= data.totalPages} onClick={() => setPage(data.page + 1)}>Next</button></div></div></>}
  </Modal>;
}
