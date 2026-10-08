"use client";

import { useEffect, useState, type ReactNode } from "react";
import { IntentLink } from "@/app/components/IntentLink";
import { ErrorBanner, ManagementSearch, TablePagination, TableShell, controlClass } from "@/app/components/management/ManagementUI";
import { fetchEscrows } from "@/app/lib/api";
import type { Escrow, EscrowPage, EscrowRelease } from "@/app/lib/types";
import { useViewState } from "@/app/lib/viewMemory";
import { usePermissions } from "@/app/lib/access";
import { decimalDisplay, hundredths } from "@/app/gross-board/board";

import { Landmark, CircleDollarSign, Users } from "lucide-react";
import { MetricCard } from "@/app/components/MetricCard";
import { PaymentHistoryPanel } from "@/app/components/PaymentHistoryPanel";

import { EscrowOpeningForm } from "./EscrowOpeningForm";
import { EscrowReleaseForm } from "./EscrowReleaseForm";

const money = (value: string) => decimalDisplay(hundredths(value), true);
const statusLabels = { paid: "Fully collected", partial: "Partially collected", unpaid: "Not collected", released: "Released", partially_released: "Partially released", not_released: "Not released" };

export function EscrowTable({ driverId = "", toolbarEnd }: { driverId?: string; toolbarEnd?: ReactNode }) {
  const permissions = usePermissions();
  const [search, setSearch] = useViewState("escrow:search", "");
  const [status, setStatus] = useViewState("escrow:status", "");
  const [page, setPage] = useViewState("escrow:page", 1);
  const [pageSize, setPageSize] = useViewState("escrow:pageSize", 25);
  const [group, setGroup] = useViewState<"active" | "terminated">("escrow:group", "active");
  const statuses = group === "active" ? ["paid", "partial", "unpaid"] as const : ["released", "partially_released", "not_released"] as const;
  const [editor, setEditor] = useState<{ escrow: Escrow; release?: EscrowRelease; anchor: { left: number; bottom: number } } | null>(null);
  const [openingEditor, setOpeningEditor] = useState<Escrow | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [data, setData] = useState<EscrowPage | null>(null);
  const selected = data?.items.find(item => item.id === selectedId);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let cancelled = false;
    const timer = setTimeout(() => {
      setLoading(true); setError("");
      fetchEscrows({ page, pageSize, search: driverId ? "" : search, status: driverId ? "" : status, driverId, group: driverId ? "" : group, includeInactive: !!driverId }).then(result => {
        if (!cancelled) setData(result);
      }).catch(reason => { if (!cancelled) setError(reason instanceof Error ? reason.message : "Could not load escrow"); })
        .finally(() => { if (!cancelled) setLoading(false); });
    }, 150);
    return () => { cancelled = true; clearTimeout(timer); };
  }, [attempt, driverId, page, pageSize, search, status, group]);
  return <div className="clear-both space-y-4">
    {!driverId && <div role="tablist" aria-label="Escrow drivers" className="flex gap-2">{(["active", "terminated"] as const).map(tab => <button key={tab} role="tab" aria-selected={group === tab} className={group === tab ? "ui-button ui-button-primary" : "ui-button"} onClick={() => { setGroup(tab); setStatus(""); setPage(1); }}>{tab === "active" ? "Active" : "Terminated"}</button>)}</div>}
    {!driverId && <div className="grid gap-4 sm:grid-cols-3" aria-label="Escrow totals">
      <MetricCard compact label="Escrow balance" value={error ? "—" : money(data?.summary.held ?? "0")} icon={Landmark} loading={loading} />
      <MetricCard compact label={group === "active" ? "Still owed" : "Released"} value={error ? "—" : money((group === "active" ? data?.summary.remaining : data?.summary.released) ?? "0")} icon={CircleDollarSign} loading={loading} />
      <MetricCard compact label={group === "active" ? "Fully collected drivers" : "Released drivers"} value={error ? "—" : `${data?.summary.paidDrivers ?? 0} / ${data?.summary.drivers ?? 0}`} icon={Users} loading={loading} />
    </div>}
    <div className="flex flex-wrap items-center gap-3">
      {!driverId && <><ManagementSearch value={search} onChange={value => { setSearch(value); setPage(1); }} placeholder="Search drivers…" />
        <select aria-label="Escrow payment status" className={`${controlClass} sm:w-48`} value={status} onChange={event => { setStatus(event.target.value); setPage(1); }}>
          <option value="">All statuses</option>{statuses.map(value => <option key={value} value={value}>{statusLabels[value]}</option>)}
        </select>
</>}
      <button className="rounded-lg border border-zinc-800 px-3 py-2 text-xs text-zinc-400 hover:text-zinc-200" onClick={() => setAttempt(value => value + 1)}>Refresh</button>
      {toolbarEnd}
    </div>
    {error && <ErrorBanner message={error} />}
    <TableShell><table className="w-full min-w-[760px] text-left text-xs" aria-label="Driver escrow balances">
      <thead><tr className="border-b border-zinc-800 text-zinc-500">{["Driver", "Required", "Balance", group === "terminated" && !driverId ? "Released" : "Still owed", "Status", "Actions"].map(label => <th className="px-4 py-3 font-medium" key={label}>{label}</th>)}</tr></thead>
      <tbody>{loading ? <tr><td colSpan={6} className="px-4 py-10 text-center text-zinc-500" role="status">Loading escrow…</td></tr> : error ? null : !data?.items.length ? <tr><td colSpan={6} className="px-4 py-10 text-center text-zinc-500">No escrow records match these filters.</td></tr> : data.items.map(item => <tr key={item.id} className="border-b border-zinc-800/60 align-top text-zinc-300">
        <td className="px-4 py-3">{item.driverId && permissions.includes("fleet.read") ? <IntentLink className="text-blue-400 hover:text-blue-300" href={`/drivers/detail?id=${item.driverId}`}>{item.driverName}</IntentLink> : item.driverName}{!item.active && <span className="ml-2 text-[10px] text-zinc-500">Terminated</span>}</td>
        <td className="px-4 py-3 font-mono">{money(item.amount)}</td><td className="px-4 py-3 font-mono text-emerald-400">{money(item.heldAmount)}</td><td className="px-4 py-3 font-mono">{money(group === "terminated" && !driverId ? item.releasedAmount : item.remainingAmount)}</td>
        <td className="px-4 py-3"><span className={`whitespace-nowrap rounded-full px-2 py-1 text-[11px] ${["paid", "released"].includes(item.status) ? "bg-emerald-500/10 text-emerald-400" : ["partial", "partially_released"].includes(item.status) ? "bg-amber-500/10 text-amber-400" : "bg-zinc-800 text-zinc-400"}`}>{statusLabels[item.status]}</span></td>
        <td className="px-4 py-3"><div className="flex items-center gap-3">{permissions.includes("escrow.write") && item.driverId && <button className="text-blue-400 hover:text-blue-300 disabled:text-zinc-600" disabled={hundredths(item.heldAmount) <= BigInt(0)} onClick={event => setEditor({ escrow: item, anchor: event.currentTarget.getBoundingClientRect() })}>Release</button>}<button aria-label="Payment history" className="whitespace-nowrap text-blue-400 hover:text-blue-300" onClick={() => setSelectedId(item.id)}>History</button></div></td>
      </tr>)}</tbody>
    </table></TableShell>
    {!loading && !error && data && <TablePagination page={data.page} pageSize={data.pageSize} totalItems={data.total} totalPages={data.totalPages} onPageChange={setPage} onPageSizeChange={value => { setPageSize(value); setPage(1); }} />}
    {selected && <PaymentHistoryPanel title={`${selected.driverName} · Escrow`} paid={selected.paidAmount} remaining={selected.remainingAmount} previouslyPaid={selected.openingPaid} payments={selected.payments} driverId={selected.driverId} escrow held={selected.heldAmount} released={selected.releasedAmount} releases={selected.releases} onEditRelease={permissions.includes("escrow.write") ? (release, anchor) => { setEditor({ escrow: selected, release, anchor }); setSelectedId(null); } : undefined} onEditOpening={permissions.includes("escrow.write") ? () => { setOpeningEditor(selected); setSelectedId(null); } : undefined} onClose={() => setSelectedId(null)} />}
    {openingEditor && <EscrowOpeningForm escrow={openingEditor} onClose={() => setOpeningEditor(null)} onSaved={() => { setOpeningEditor(null); setLoading(true); setAttempt(value => value + 1); }} />}
    {editor && <EscrowReleaseForm {...editor} onClose={() => { setEditor(null); setLoading(true); setAttempt(value => value + 1); }} onSaved={() => { setEditor(null); setLoading(true); setAttempt(value => value + 1); }} />}
  </div>;
}
