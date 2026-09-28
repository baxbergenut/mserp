"use client";

import { memo, useState } from "react";
import type { GrossBoardEntry } from "@/app/lib/types";
import { mismatch, validDecimal } from "./board";
import { LoadStatusPicker } from "./LoadStatusPicker";
import { BoardDialog } from "./BoardDialog";

type DayCellProps = {
  entry: GrossBoardEntry;
  driverName: string;
  disabled: boolean;
  onChange: (driverId: string, date: string, update: (current: GrossBoardEntry) => GrossBoardEntry) => void;
};

export const DayCell = memo(function DayCell({ entry, driverName, disabled, onChange }: DayCellProps) {
  const [reviewing, setReviewing] = useState(false);
  const confirmed = entry.loadRecordId !== null;
  const rateMismatch = confirmed && !entry.acceptSystemValues && mismatch(entry.enteredOriginalRate, entry.originalRate);
  const milesMismatch = confirmed && !entry.acceptSystemValues && mismatch(entry.enteredMiles, entry.miles);
  const needsReview = rateMismatch || milesMismatch;
  const label = `${driverName}, ${entry.date}`;
  const fieldClass = "h-8 w-full min-w-0 border-0 border-b border-zinc-800/70 bg-transparent px-2 text-center font-mono text-xs outline-none focus:bg-blue-500/10 focus:ring-1 focus:ring-inset focus:ring-blue-500 disabled:opacity-50";
  return (
    <td className="border-r border-b border-zinc-800/70 p-0 align-top">
      <LoadStatusPicker entry={entry} label={label} disabled={disabled} needsReview={needsReview} onReview={() => setReviewing(true)} onChange={onChange} />
      {!entry.dayStatus && (["originalRate", "driverRate", "miles"] as const).map((field) => {
        const locked = confirmed && field !== "driverRate";
        const valid = validDecimal(entry[field], field === "miles");
        const different = field === "originalRate" ? rateMismatch : field === "miles" ? milesMismatch : false;
        const entered = field === "originalRate" ? entry.enteredOriginalRate : entry.enteredMiles;
        return <input key={field} aria-label={`${label}, ${field === "originalRate" ? "original rate" : field === "driverRate" ? "driver rate" : "miles"}`}
          aria-invalid={!valid} inputMode="decimal" value={entry[field]} readOnly={locked} disabled={disabled}
          title={different ? `Entered: ${entered}; system: ${entry[field] || "missing"}. Use the review icon for details.` : locked ? "Locked to the system load" : !valid ? "Enter a number with up to 2 decimal places" : undefined}
          placeholder={locked ? "—" : "0.00"}
          onChange={(event) => { const value = event.target.value; onChange(entry.driverId, entry.date, (current) => ({ ...current, [field]: value,
            ...(field === "originalRate" ? { enteredOriginalRate: value } : field === "miles" ? { enteredMiles: value } : {}),
          })); }}
          className={`${fieldClass} ${different || !valid ? "!bg-red-500/15 text-red-300" : locked ? "text-emerald-200/70" : "text-zinc-300"}`}
        />;
      })}
      {reviewing && <BoardDialog title={`Review load ${entry.loadNumber}`} onClose={() => setReviewing(false)}>
        <p className="mb-4 text-zinc-400">{driverName} · {entry.date}. Totals use the system values. Your entered values are retained below for comparison.</p>
        <table className="w-full text-left text-sm">
          <thead className="text-zinc-500"><tr><th className="p-2">Field</th><th>Entered</th><th>System</th></tr></thead>
          <tbody>{[["Original rate", entry.enteredOriginalRate, entry.originalRate, rateMismatch], ["Miles", entry.enteredMiles, entry.miles, milesMismatch]].map(([name, entered, actual, different]) =>
            <tr key={String(name)} className={different ? "bg-red-500/10 text-red-300" : "text-zinc-400"}><th className="p-2 font-medium">{name}</th><td>{entered || "—"}</td><td>{actual || "—"}</td></tr>)}</tbody>
        </table>
        <p className="mt-4 text-xs text-zinc-500">Driver rate stays as entered. If the imported data is wrong, correct the source load; the board will refresh automatically.</p>
        <button className="mt-5 rounded-lg bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-500" onClick={() => {
          onChange(entry.driverId, entry.date, (current) => ({ ...current, acceptSystemValues: true,
            enteredOriginalRate: current.originalRate, enteredMiles: current.miles }));
          setReviewing(false);
        }}>Accept system values</button>
      </BoardDialog>}
    </td>
  );
}, (previous, next) => previous.driverName === next.driverName && previous.disabled === next.disabled && previous.onChange === next.onChange
  && previous.entry.driverId === next.entry.driverId && previous.entry.date === next.entry.date
  && previous.entry.dayStatus === next.entry.dayStatus
  && previous.entry.version === next.entry.version && previous.entry.loadNumber === next.entry.loadNumber
  && previous.entry.loadRecordId === next.entry.loadRecordId && previous.entry.originalRate === next.entry.originalRate
  && previous.entry.driverRate === next.entry.driverRate && previous.entry.miles === next.entry.miles
  && previous.entry.enteredOriginalRate === next.entry.enteredOriginalRate && previous.entry.enteredMiles === next.entry.enteredMiles
  && previous.entry.acceptSystemValues === next.entry.acceptSystemValues && previous.entry.duplicate === next.entry.duplicate);
