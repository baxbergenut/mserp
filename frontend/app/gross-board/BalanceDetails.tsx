"use client";

import { useState, type FormEvent } from "react";
import type { GrossBoardBalanceLine, GrossBoardEntry } from "@/app/lib/types";
import { BoardDialog } from "./BoardDialog";
import { balanceLabel, decimalDisplay, hundredths, incompleteRates, rateBalance, rateChange, signedMoney, validDecimal } from "./board";

export function BalanceDetails({ driverName, opening, openingReference, openingIncomplete, entries, resetThisWeek, onReset, resetDisabled, onClose }: {
  driverName: string; opening: string; openingReference?: string; openingIncomplete: number;
  entries: GrossBoardEntry[]; resetThisWeek?: boolean; onReset?: (amount: string) => Promise<void>;
  resetDisabled: boolean; onClose: () => void;
}) {
  const [editing, setEditing] = useState(false);
  const [amount, setAmount] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const value = amount.trim();
    if (!onReset || saving || resetDisabled || !value || !validDecimal(value)) return;
    setSaving(true);
    setError("");
    try {
      await onReset(value);
      setEditing(false);
      setSaved(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to reset the rate balance");
    } finally {
      setSaving(false);
    }
  }

  const balance = rateBalance(opening, entries);
  const incomplete = openingIncomplete + incompleteRates(entries);
  const rows = entries.filter((entry) => !entry.deleted && !entry.dayStatus && (entry.loadNumber || entry.originalRate || entry.driverRate))
    .sort((a, b) => a.date.localeCompare(b.date) || a.slot - b.slot)
    .reduce<GrossBoardBalanceLine[]>((rows, entry) => {
      const change = rateChange(entry);
      const running = hundredths(rows.at(-1)?.balance ?? opening) + (change ?? BigInt(0));
      return [...rows, { date: entry.date, loadNumber: entry.loadNumber, originalRate: entry.originalRate, driverRate: entry.driverRate,
        change: change === null ? "" : decimalDisplay(change).replaceAll(",", ""), balance: decimalDisplay(running).replaceAll(",", ""), duplicate: entry.duplicate }];
    }, []).reverse();
  return <BoardDialog title={`${driverName} · Rate balance`} onClose={onClose}>
    <div className="grid grid-cols-3 gap-3 text-center">
      {[[resetThisWeek ? "Adjusted opening" : "Carried in", hundredths(opening)], ["This week", balance - hundredths(opening)], ["Ending balance", balance]].map(([label, value]) =>
        <div key={String(label)} className="rounded-lg border border-zinc-800 bg-zinc-900/50 p-3">
          <div className="text-[11px] text-zinc-500">{String(label)}</div>
          <div className="mt-1 font-mono text-sm">{signedMoney(value as bigint)}</div>
        </div>)}
    </div>
    <p className="mt-3 text-xs text-zinc-400">{balanceLabel(balance)} · Positive balances are available for later load additions; negative balances still need to be offset.</p>
    {openingReference && <p className="mt-2 text-xs text-zinc-400">Reviewed Sheets opening · {openingReference}. This rate balance does not change payroll earnings or debt.</p>}
    {incomplete > 0 && <p className="mt-3 text-xs text-amber-300">{incomplete} incomplete {incomplete === 1 ? "entry is" : "entries are"} excluded until a load number, total gross, and driver rate are entered.</p>}
    {onReset && <div className="mt-4">
      {editing ? <form onSubmit={submit} className="rounded-lg border border-zinc-800 p-3">
        <label htmlFor="new-rate-balance" className="block text-sm font-medium">New rate balance</label>
        <p id="rate-balance-help" className="mt-1 text-xs text-zinc-400">Set the ending balance for this week. Future load changes will move the balance from this amount.</p>
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <input id="new-rate-balance" aria-describedby="rate-balance-help" value={amount} onChange={event => setAmount(event.target.value)}
            placeholder="0.00" maxLength={14} required disabled={saving}
            className="w-32 rounded-md border border-zinc-700 bg-zinc-900 px-2 py-1.5 font-mono" />
          <button type="submit" disabled={resetDisabled || saving || !amount.trim() || !validDecimal(amount.trim())}
            className="rounded-md bg-blue-600 px-3 py-1.5 font-medium text-white hover:bg-blue-500 disabled:opacity-50">{saving ? "Resetting…" : "Confirm reset"}</button>
          <button type="button" disabled={saving} onClick={() => { setEditing(false); setError(""); }} className="px-2 py-1.5 text-zinc-400">Cancel</button>
        </div>
        {error && <p role="alert" className="mt-2 text-xs text-red-300">{error}</p>}
      </form> : <button type="button" onClick={() => { setAmount(""); setEditing(true); setSaved(false); }}
        className="rounded-md border border-zinc-700 px-3 py-1.5 text-xs hover:bg-zinc-800">Reset rate balance</button>}
      {saved && <p role="status" className="mt-2 text-xs text-emerald-300">Rate balance reset.</p>}
    </div>}
    <div className="mt-5 overflow-x-auto">
      <table className="w-full min-w-[560px] text-left text-xs">
        <thead className="text-zinc-500"><tr>{["Date", "Load", "Total gross", "Driver", "Change", "Balance"].map((label) => <th key={label} className="border-b border-zinc-800 px-2 py-2 font-medium">{label}</th>)}</tr></thead>
        <tbody>{rows.map((line, index) => <tr key={`${line.date}:${index}`}>
          <td className="border-b border-zinc-800/60 p-2 whitespace-nowrap">{line.date}</td>
          <td className="border-b border-zinc-800/60 p-2">{line.loadNumber || "No load number"}{line.duplicate && <div className="text-amber-300">Repeated load · counted on earliest date</div>}</td>
          <td className="border-b border-zinc-800/60 p-2 font-mono">{line.originalRate === "" ? "—" : decimalDisplay(hundredths(line.originalRate), true)}</td>
          <td className="border-b border-zinc-800/60 p-2 font-mono">{line.driverRate === "" ? "—" : decimalDisplay(hundredths(line.driverRate), true)}</td>
          <td className="border-b border-zinc-800/60 p-2 font-mono">{line.change === "" ? line.duplicate ? "Excluded" : "Incomplete" : signedMoney(hundredths(line.change))}</td>
          <td className="border-b border-zinc-800/60 p-2 font-mono">{signedMoney(hundredths(line.balance))}</td>
        </tr>)}</tbody>
      </table>
      {rows.length === 0 && <p className="p-5 text-center text-zinc-500">No rate changes for this week.</p>}
    </div>
  </BoardDialog>;
}
