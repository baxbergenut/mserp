"use client";

import { useId, useRef, useState } from "react";
import { ChevronDown } from "lucide-react";

// null means all, [] deliberately means none; new choices join only All.
export function BoardFilter({ label, options, selected, onChange, optionColor }: {
  label: string; options: [string, string][]; selected: string[] | null;
  onChange: (value: string[] | null) => void;
  optionColor?: (value: string) => string;
}) {
  const plural = label === "Status" ? "Statuses" : `${label}s`;
  const id = useId();
  const panel = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  return <>
    <button type="button" aria-label={`${label} filter`} aria-expanded={open} popoverTarget={id}
      className="inline-flex items-center gap-2 rounded-lg border border-zinc-700 bg-zinc-900 px-3 py-2 text-xs text-zinc-200"
      onClick={event => {
        const rect = event.currentTarget.getBoundingClientRect();
        if (panel.current) {
          panel.current.style.left = `${Math.max(8, Math.min(rect.left, innerWidth - 248))}px`;
          panel.current.style.top = `${rect.bottom + 4}px`;
          panel.current.style.maxHeight = `${Math.max(100, innerHeight - rect.bottom - 16)}px`;
        }
      }}>{selected === null ? `All ${plural.toLowerCase()}` : `${plural} (${selected.length})`}<ChevronDown className="h-3 w-3" /></button>
    <div id={id} ref={panel} popover="auto" role="group" aria-label={`${label} choices`} onToggle={event => setOpen(event.newState === "open")}
      className="fixed m-0 w-60 max-w-[calc(100vw-16px)] overflow-y-auto rounded-lg border border-zinc-700 bg-zinc-900 p-2 text-xs text-zinc-200 shadow-xl">
      <div className="mb-1 flex justify-between border-b border-zinc-700 pb-2">
        <button className="rounded px-2 py-1 hover:bg-zinc-800" onClick={() => onChange(null)}>Select all</button>
        <button className="rounded px-2 py-1 hover:bg-zinc-800" onClick={() => onChange([])}>Clear all</button>
      </div>
      {options.map(([value, name]) => <label key={value} className="flex cursor-pointer items-center gap-2 rounded px-2 py-2 hover:bg-zinc-800">
        <input type="checkbox" checked={selected === null || selected.includes(value)} onChange={event => {
          const current = selected ?? options.map(([key]) => key);
          onChange(event.target.checked ? [...current, value] : current.filter(key => key !== value));
        }} />{optionColor && value && <span aria-hidden="true" className={`h-3 w-3 shrink-0 rounded-sm border border-current/20 ${optionColor(value)}`} />}{name}
      </label>)}
    </div>
  </>;
}
