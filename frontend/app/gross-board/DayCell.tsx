"use client";

import { memo, useCallback, useEffect, useRef, useState } from "react";
import type { GrossBoardEntry } from "@/app/lib/types";
import { Plus, X } from "lucide-react";
import { editAmount, emptyEntry, mismatch, validDecimal } from "./board";
import { LoadStatusPicker } from "./LoadStatusPicker";
import { BoardDialog } from "./BoardDialog";

type DayCellProps = {
  entry: GrossBoardEntry;
  id?: string;
  highlighted?: boolean;
  driverName: string;
  disabled: boolean;
  onAdd?: (driverId: string, date: string) => void;
  onChange: (driverId: string, date: string, slot: number, update: (current: GrossBoardEntry) => GrossBoardEntry) => void;
};

export const DayCell = memo(function DayCell({ entry, driverName, disabled, onChange, onAdd, id, highlighted }: DayCellProps) {
  const cellRef = useRef<HTMLTableCellElement>(null);
  useEffect(() => {
    if (!highlighted) return;
    cellRef.current?.scrollIntoView({ block: "nearest", inline: "center" });
    cellRef.current?.focus({ preventScroll: true });
  }, [highlighted]);
  const pickerChange = useCallback((id: string, date: string, update: (current: GrossBoardEntry) => GrossBoardEntry) => onChange(id, date, entry.slot, update), [onChange, entry.slot]);
  const [reviewing, setReviewing] = useState(false);
  const confirmed = entry.loadRecordId !== null;
  const rateMismatch = confirmed && !entry.acceptSystemValues && mismatch(entry.enteredOriginalRate, entry.systemOriginalRate ?? "");
  const milesMismatch = confirmed && !entry.acceptSystemValues && mismatch(entry.enteredMiles, entry.systemMiles ?? "");
  const needsReview = rateMismatch || milesMismatch;
  const label = `${driverName}, ${entry.date}`;
  const fieldClass = "h-8 w-full min-w-0 border-0 border-b border-zinc-800/70 bg-transparent px-2 text-center font-mono text-xs outline-none focus:bg-blue-500/10 focus:ring-1 focus:ring-inset focus:ring-blue-500 disabled:opacity-50";
  return (
    <td id={id} ref={cellRef} tabIndex={highlighted ? -1 : undefined} data-highlighted={highlighted || undefined} aria-label={highlighted ? `Selected load ${entry.loadNumber}, ${driverName}, ${entry.date}, slot ${entry.slot + 1}` : undefined} className={`border-r border-b border-zinc-800/70 p-0 align-top ${highlighted ? "bg-blue-500/10 outline-2 -outline-offset-2 outline-blue-400" : ""}`}>
      <div className="flex h-6 items-center justify-between border-b border-zinc-800/70 px-1 text-[10px] text-zinc-500">
        <span>Load {entry.slot + 1}</span><div className="flex gap-1">
          {onAdd && <button type="button" onClick={() => onAdd(entry.driverId, entry.date)} disabled={disabled} className="rounded p-0.5 hover:bg-zinc-700 hover:text-blue-300" aria-label={`${label}, add another load`}><Plus className="h-3 w-3" /></button>}
          <button type="button" disabled={disabled} aria-label={`${label}, remove load`} className="rounded p-0.5 hover:bg-zinc-700 hover:text-red-300" onClick={() => onChange(entry.driverId, entry.date, entry.slot, current => ({ ...emptyEntry(current.driverId, current.date, current.slot), version: current.version, deleted: current.slot > 0 }))}><X className="h-3 w-3" /></button>
        </div>
      </div>
      <LoadStatusPicker entry={entry} label={label} disabled={disabled} needsReview={needsReview} onReview={() => setReviewing(true)} onChange={pickerChange} />
      {!entry.dayStatus && (["originalRate", "driverRate", "miles"] as const).map((field) => {
        const valid = validDecimal(entry[field], field === "miles");
        const different = field === "originalRate" ? rateMismatch : field === "miles" ? milesMismatch : false;
        const entered = field === "originalRate" ? entry.enteredOriginalRate : entry.enteredMiles;
        const system = field === "originalRate" ? entry.systemOriginalRate : entry.systemMiles;
        return <input key={field} aria-label={`${label}, ${field === "originalRate" ? "original rate" : field === "driverRate" ? "driver rate" : "miles"}`}
          aria-invalid={!valid} inputMode="decimal" value={entry[field]} disabled={disabled}
          title={different ? `Entered: ${entered}; system: ${system || "missing"}. Totals use the entered value.` : !valid ? "Enter a number with up to 2 decimal places" : undefined}
          placeholder="0.00"
          onChange={(event) => { const value = event.target.value; onChange(entry.driverId, entry.date, entry.slot, current => editAmount(current, field, value)); }}
          className={`${fieldClass} ${different || !valid ? "!bg-red-500/15 text-red-300" : "text-zinc-300"}`}
        />;
      })}
      {reviewing && <BoardDialog title={`Review load ${entry.loadNumber}`} onClose={() => setReviewing(false)}>
        <p className="mb-4 text-zinc-400">{driverName} · {entry.date}. Totals use your entered values. Current system values are shown for comparison.</p>
        <table className="w-full text-left text-sm">
          <thead className="text-zinc-500"><tr><th className="p-2">Field</th><th>Entered</th><th>System</th></tr></thead>
          <tbody>{[["Original rate", entry.enteredOriginalRate, entry.systemOriginalRate, rateMismatch], ["Miles", entry.enteredMiles, entry.systemMiles, milesMismatch]].map(([name, entered, actual, different]) =>
            <tr key={String(name)} className={different ? "bg-red-500/10 text-red-300" : "text-zinc-400"}><th className="p-2 font-medium">{name}</th><td>{entered || "—"}</td><td>{actual || "—"}</td></tr>)}</tbody>
        </table>
        <p className="mt-4 text-xs text-zinc-500">Driver gross stays as entered. Accepting system values replaces the original gross and miles with the values shown here.</p>
        <button className="mt-5 rounded-lg bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-500" onClick={() => {
          onChange(entry.driverId, entry.date, entry.slot, (current) => ({ ...current, acceptSystemValues: true,
            originalRate: current.systemOriginalRate ?? "", miles: current.systemMiles ?? "",
            enteredOriginalRate: current.systemOriginalRate ?? "", enteredMiles: current.systemMiles ?? "" }));
          setReviewing(false);
        }}>Accept system values</button>
      </BoardDialog>}
    </td>
  );
}, (previous, next) => previous.driverName === next.driverName && previous.disabled === next.disabled && previous.onChange === next.onChange
  && previous.onAdd === next.onAdd && previous.id === next.id && previous.highlighted === next.highlighted
  && previous.entry.slot === next.entry.slot && previous.entry.deleted === next.entry.deleted
  && previous.entry.driverId === next.entry.driverId && previous.entry.date === next.entry.date
  && previous.entry.dayStatus === next.entry.dayStatus
  && previous.entry.version === next.entry.version && previous.entry.loadNumber === next.entry.loadNumber
  && previous.entry.loadRecordId === next.entry.loadRecordId && previous.entry.originalRate === next.entry.originalRate
  && previous.entry.driverRate === next.entry.driverRate && previous.entry.miles === next.entry.miles
  && previous.entry.enteredOriginalRate === next.entry.enteredOriginalRate && previous.entry.enteredMiles === next.entry.enteredMiles
  && previous.entry.systemOriginalRate === next.entry.systemOriginalRate && previous.entry.systemMiles === next.entry.systemMiles
  && previous.entry.acceptSystemValues === next.entry.acceptSystemValues && previous.entry.duplicate === next.entry.duplicate);
