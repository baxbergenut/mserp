"use client";

import { useEffect, useState } from "react";
import { fetchGrossBoardBalance } from "@/app/lib/api";
import type { GrossBoardBalanceLine, GrossBoardEntry } from "@/app/lib/types";
import { BoardDialog } from "./BoardDialog";
import { balanceLabel, decimalDisplay, hundredths, incompleteRates, rateBalance, rateChange, signedMoney } from "./board";

export function BalanceDetails({ driverId, driverName, week, opening, openingIncomplete, entries, onClose }: {
  driverId: string; driverName: string; week: string; opening: string; openingIncomplete: number;
  entries: GrossBoardEntry[]; onClose: () => void;
}) {
  const [history, setHistory] = useState<GrossBoardBalanceLine[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let cancelled = false;
    fetchGrossBoardBalance(driverId, week).then((rows) => { if (!cancelled) { setHistory(rows); setError(""); } })
      .catch((err) => { if (!cancelled) setError(err instanceof Error ? err.message : "Unable to load history"); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [driverId, week, attempt]);
  const balance = rateBalance(opening, entries);
  const incomplete = openingIncomplete + incompleteRates(entries);
  const current: GrossBoardBalanceLine[] = entries.filter((entry) => !entry.dayStatus && (entry.loadNumber || entry.originalRate || entry.driverRate))
    .reduce<GrossBoardBalanceLine[]>((rows, entry) => {
      const change = rateChange(entry);
      const running = hundredths(rows.at(-1)?.balance ?? opening) + (change ?? BigInt(0));
      return [...rows, { date: entry.date, loadNumber: entry.loadNumber, originalRate: entry.originalRate, driverRate: entry.driverRate,
        change: change === null ? "" : decimalDisplay(change).replaceAll(",", ""), balance: decimalDisplay(running).replaceAll(",", ""), duplicate: entry.duplicate }];
    }, []);
  const rows = [...history.filter((line) => line.date < week), ...current].reverse();
  return <BoardDialog title={`${driverName} · Rate balance`} onClose={onClose}>
    <div className="grid grid-cols-3 gap-3 text-center">
      {[["Carried in", hundredths(opening)], ["This week", balance - hundredths(opening)], ["Ending balance", balance]].map(([label, value]) =>
        <div key={String(label)} className="rounded-lg border border-zinc-800 bg-zinc-900/50 p-3">
          <div className="text-[11px] text-zinc-500">{String(label)}</div>
          <div className="mt-1 font-mono text-sm">{signedMoney(value as bigint)}</div>
        </div>)}
    </div>
    <p className="mt-3 text-xs text-zinc-400">{balanceLabel(balance)} · Positive balances are available for later load additions; negative balances still need to be offset. History starts with the first saved board entry.</p>
    {incomplete > 0 && <p className="mt-3 text-xs text-amber-300">{incomplete} incomplete {incomplete === 1 ? "entry is" : "entries are"} excluded until a load number, original rate, and driver rate are entered.</p>}
    {error && <p role="alert" className="mt-4 text-red-300">{error} <button className="underline" onClick={() => setAttempt((value) => value + 1)}>Retry history</button></p>}
    {loading ? <p role="status" className="mt-5 text-zinc-500">Loading history…</p> : !error && <div className="mt-5 overflow-x-auto">
      <table className="w-full min-w-[560px] text-left text-xs">
        <thead className="text-zinc-500"><tr>{["Date", "Load", "Original", "Driver", "Change", "Balance"].map((label) => <th key={label} className="border-b border-zinc-800 px-2 py-2 font-medium">{label}</th>)}</tr></thead>
        <tbody>{rows.map((line) => <tr key={line.date} className={line.date >= week ? "bg-blue-500/5" : ""}>
          <td className="border-b border-zinc-800/60 p-2 whitespace-nowrap">{line.date}</td>
          <td className="border-b border-zinc-800/60 p-2">{line.loadNumber || "No load number"}{line.duplicate && <div className="text-amber-300">Repeated load · counted on earliest date</div>}</td>
          <td className="border-b border-zinc-800/60 p-2 font-mono">{line.originalRate === "" ? "—" : decimalDisplay(hundredths(line.originalRate), true)}</td>
          <td className="border-b border-zinc-800/60 p-2 font-mono">{line.driverRate === "" ? "—" : decimalDisplay(hundredths(line.driverRate), true)}</td>
          <td className="border-b border-zinc-800/60 p-2 font-mono">{line.change === "" ? line.duplicate ? "Excluded" : "Incomplete" : signedMoney(hundredths(line.change))}</td>
          <td className="border-b border-zinc-800/60 p-2 font-mono">{signedMoney(hundredths(line.balance))}</td>
        </tr>)}</tbody>
      </table>
      {rows.length === 0 && <p className="p-5 text-center text-zinc-500">No rate history yet.</p>}
    </div>}
  </BoardDialog>;
}
