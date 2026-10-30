"use client";

import { useEffect, useState } from "react";
import { fetchPayCostTransactions } from "@/app/lib/api";
import type { PayCostTransactions } from "@/app/lib/types";
import { Modal } from "@/app/components/management/ManagementUI";
import { addDays, decimalDisplay, hundredths, shortDate } from "@/app/gross-board/board";

const money = (value: string) => decimalDisplay(hundredths(value), true);

export function CostTransactionsDialog({ kind, id, name, weekStart, cost, onClose }: {
  kind: "driver" | "investor"; id: string; name: string; weekStart: string; cost: "fuel" | "toll"; onClose: () => void;
}) {
  const [data, setData] = useState<PayCostTransactions | null>(null);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const label = cost === "fuel" ? "Fuel" : "Toll";
  useEffect(() => {
    const controller = new AbortController();
    fetchPayCostTransactions(kind, id, weekStart, cost, controller.signal).then(value => {
      if (!controller.signal.aborted) setData(value);
    }).catch(reason => { if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : "Transactions could not be loaded"); });
    return () => controller.abort();
  }, [kind, id, weekStart, cost, attempt]);
  useEffect(() => {
    const close = (event: KeyboardEvent) => { if (event.key === "Escape") onClose(); };
    document.addEventListener("keydown", close);
    return () => document.removeEventListener("keydown", close);
  }, [onClose]);
  return <Modal title={`${label} transactions`} description={`${name} · ${shortDate(weekStart)} – ${shortDate(addDays(weekStart, 6))}`} isSaving={false} hideSubmit submitLabel="" cancelLabel="Close" onClose={onClose} onSubmit={event => event.preventDefault()}>
    {error ? <div role="alert" className="text-sm text-red-300">{error}<button type="button" className="ui-button ml-3" onClick={() => { setError(""); setAttempt(value => value + 1); }}>Try again</button></div> : !data ? <p role="status" className="text-sm text-zinc-400">Loading transactions…</p> : <>
      <div className="mb-4 flex flex-wrap gap-x-6 gap-y-2 text-sm">
        <span>This week <strong className="font-mono">{money(data.sourceTotal)}</strong></span>
        <span>Carried <strong className="font-mono">{money(data.carry)}</strong></span>
        <span>Total due <strong className="font-mono">{money(data.due)}</strong></span>
      </div>
      {data.notice && <p role="status" className="mb-3 text-xs text-zinc-400">{data.notice}</p>}
      {data.transactions.length === 0 ? <p className="text-sm text-zinc-500">No {cost} transactions for this statement.</p> : <div className="overflow-x-auto">
        <table className="w-full text-left text-[13px]">
          <thead className="border-b border-zinc-800 text-xs text-zinc-500"><tr><th className="px-2 py-2">Date</th><th className="px-2 py-2">Transaction</th><th className="px-2 py-2 text-right">Amount</th><th className="px-2 py-2 text-right">Carried amount</th></tr></thead>
          <tbody>{data.transactions.map(row => <tr key={`${row.weekStart}:${row.id}`} className="border-b border-zinc-800/70">
            <td className="whitespace-nowrap px-2 py-2 align-top">{row.date ? shortDate(row.date) : "—"}{row.postedDate && <div className="text-[11px] text-zinc-500">Posted {shortDate(row.postedDate)}</div>}</td>
            <td className="px-2 py-2"><div>{row.description}{row.carried && <span className="ml-2 rounded bg-amber-500/10 px-1.5 py-0.5 text-[11px] text-amber-300">{row.weekStart ? `Carried from ${shortDate(row.weekStart)}` : "Carried balance"}</span>}</div><div className="text-xs text-zinc-500">{[row.location, row.truckUnit ? `Truck ${row.truckUnit}` : "", row.driverName].filter(Boolean).join(" · ")}</div>{row.transactionId && !row.balanceOnly && <div className="break-all text-[11px] text-zinc-500">{row.transactionId}</div>}</td>
            <td className="whitespace-nowrap px-2 py-2 text-right align-top font-mono">{money(row.amount)}</td>
            <td className="whitespace-nowrap px-2 py-2 text-right align-top font-mono">{row.carried ? money(row.remaining ?? row.amount) : "—"}</td>
          </tr>)}</tbody>
        </table>
      </div>}
    </>}
  </Modal>;
}
