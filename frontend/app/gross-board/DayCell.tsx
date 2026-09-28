"use client";

import { memo, useEffect, useId, useState } from "react";
import { Check } from "lucide-react";
import { searchGrossBoardLoads } from "@/app/lib/api";
import type { GrossBoardEntry, GrossBoardLoad } from "@/app/lib/types";
import { validDecimal } from "./board";

type DayCellProps = {
  entry: GrossBoardEntry;
  driverName: string;
  disabled: boolean;
  onChange: (driverId: string, date: string, update: (current: GrossBoardEntry) => GrossBoardEntry) => void;
};

export const DayCell = memo(function DayCell({ entry, driverName, disabled, onChange }: DayCellProps) {
  const id = useId();
  const [suggestions, setSuggestions] = useState<GrossBoardLoad[]>([]);
  const [focused, setFocused] = useState(false);
  const [active, setActive] = useState(0);
  const [searchError, setSearchError] = useState(false);
  const query = entry.loadNumber.trim();
  const confirmed = entry.loadRecordId !== null;

  useEffect(() => {
    if (!query || confirmed || disabled) return;
    let cancelled = false;
    const timer = setTimeout(() => {
      searchGrossBoardLoads(query).then((loads) => {
        if (cancelled) return;
        setSearchError(false);
        setSuggestions(loads);
        setActive(0);
        const exact = loads.filter((load) => load.loadNumber.trim().toLowerCase() === query.toLowerCase());
        if (exact.length === 1) {
          const load = exact[0];
          onChange(entry.driverId, entry.date, (current) => current.loadNumber.trim() === query ? {
            ...current, loadNumber: load.loadNumber, loadRecordId: load.id,
            originalRate: load.originalRate, miles: load.miles,
          } : current);
        }
      }).catch(() => { if (!cancelled) { setSuggestions([]); setSearchError(true); } });
    }, 250);
    return () => { cancelled = true; clearTimeout(timer); };
  }, [query, confirmed, disabled, onChange, entry.driverId, entry.date]);

  const choose = (load: GrossBoardLoad) => {
    onChange(entry.driverId, entry.date, (current) => ({ ...current, loadNumber: load.loadNumber, loadRecordId: load.id, originalRate: load.originalRate, miles: load.miles }));
    setFocused(false);
  };
  const open = focused && !confirmed && suggestions.length > 0;
  const label = `${driverName}, ${entry.date}`;
  const fieldClass = "h-8 w-full min-w-0 border-0 border-b border-zinc-800/70 bg-transparent px-2 text-center font-mono text-xs outline-none focus:bg-blue-500/10 focus:ring-1 focus:ring-inset focus:ring-blue-500 disabled:opacity-50";
  return (
    <td className="border-r border-b border-zinc-800/70 p-0 align-top">
      <div className="relative">
        <input
          aria-label={`${label}, load number`}
          role="combobox" aria-autocomplete="list" aria-expanded={open} aria-controls={open ? id : undefined}
          aria-activedescendant={open ? `${id}-${active}` : undefined}
          title={confirmed ? "Confirmed system load. Original rate and miles come from Loads." : "Enter a load number or planning text"}
          maxLength={200} value={entry.loadNumber} disabled={disabled}
          onFocus={() => setFocused(true)} onBlur={() => setFocused(false)}
          onChange={(event) => {
            const loadNumber = event.target.value;
            setSuggestions([]); setSearchError(false); setFocused(true);
            onChange(entry.driverId, entry.date, (current) => ({ ...current, loadNumber, loadRecordId: null,
              originalRate: current.loadRecordId !== null ? "" : current.originalRate,
              miles: current.loadRecordId !== null ? "" : current.miles,
            }));
          }}
          onKeyDown={(event) => {
            if (event.key === "Escape") setFocused(false);
            if (!open) return;
            if (event.key === "ArrowDown" || event.key === "ArrowUp") {
              event.preventDefault(); setActive((value) => (value + (event.key === "ArrowDown" ? 1 : -1) + suggestions.length) % suggestions.length);
            }
            if (event.key === "Enter") { event.preventDefault(); choose(suggestions[active]); }
          }}
          className={`${fieldClass} !font-sans !px-6 ${confirmed ? "!bg-emerald-500/15 text-emerald-300" : "!bg-zinc-800/30 text-zinc-200"}`}
          placeholder="Load # / plan"
        />
        {confirmed && <Check aria-label="Confirmed load" className="pointer-events-none absolute right-2 top-2 h-4 w-4 text-emerald-400" />}
        {searchError && <span className="absolute right-1 top-1 text-amber-400" title="Load lookup unavailable. Save will verify this number." aria-label="Load lookup unavailable">!</span>}
        {open && <ul id={id} role="listbox" aria-label="Matching loads" className="absolute left-0 top-8 z-40 max-h-56 w-72 overflow-y-auto rounded-lg border border-zinc-700 bg-zinc-900 p-1 text-left shadow-xl">
          {suggestions.map((load, index) => <li key={load.id} id={`${id}-${index}`} role="option" aria-selected={index === active}
            onMouseDown={(event) => event.preventDefault()} onClick={() => choose(load)}
            className={`cursor-pointer rounded px-2 py-2 text-xs ${index === active ? "bg-blue-500/20" : "hover:bg-zinc-800"}`}>
            <div className="font-medium text-zinc-100">{load.loadNumber} · ${load.originalRate}</div>
            <div className="mt-1 text-zinc-400">{load.driverName || "Unassigned"} · {load.pickupDate || "No pickup date"} · {load.miles || "—"} mi</div>
            <div className="text-zinc-500">Record #{load.id}</div>
          </li>)}
        </ul>}
      </div>
      {(["originalRate", "driverRate", "miles"] as const).map((field) => {
        const locked = confirmed && field !== "driverRate";
        const valid = validDecimal(entry[field], field === "miles");
        return <input key={field} aria-label={`${label}, ${field === "originalRate" ? "original rate" : field === "driverRate" ? "driver rate" : "miles"}`}
          aria-invalid={!valid} inputMode="decimal" value={entry[field]} readOnly={locked} disabled={disabled}
          title={locked ? "Locked to the system load" : !valid ? "Enter a number with up to 2 decimal places" : undefined}
          placeholder={locked ? "—" : "0.00"}
          onChange={(event) => { const value = event.target.value; onChange(entry.driverId, entry.date, (current) => ({ ...current, [field]: value })); }}
          className={`${fieldClass} ${locked ? "text-emerald-200/70" : "text-zinc-300"} ${!valid ? "bg-red-500/15 text-red-300" : ""}`}
        />;
      })}
    </td>
  );
}, (previous, next) => previous.driverName === next.driverName && previous.disabled === next.disabled && previous.onChange === next.onChange
  && previous.entry.driverId === next.entry.driverId && previous.entry.date === next.entry.date
  && previous.entry.version === next.entry.version && previous.entry.loadNumber === next.entry.loadNumber
  && previous.entry.loadRecordId === next.entry.loadRecordId && previous.entry.originalRate === next.entry.originalRate
  && previous.entry.driverRate === next.entry.driverRate && previous.entry.miles === next.entry.miles);
