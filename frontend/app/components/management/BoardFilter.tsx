"use client";

import { useId, useRef, useState } from "react";
import { ChevronDown, GripVertical } from "lucide-react";
import { useBoardFilterDrag } from "./useBoardFilterDrag";

// null means all, [] deliberately means none; new choices join only All.
export function BoardFilter({ label, options, selected, onChange, onReorder, reorderDisabled, pinnedFirst }: {
  label: string; options: [string, string][]; selected: string[] | null;
  onChange: (value: string[] | null) => void;
  onReorder?: (id: string, target: string, after: boolean) => Promise<boolean>;
  reorderDisabled?: boolean;
  pinnedFirst?: string;
}) {
  const plural = label === "Status" ? "Statuses" : `${label}s`;
  const id = useId();
  const panel = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const drag = useBoardFilterDrag(options, reorderDisabled ? undefined : onReorder, pinnedFirst);
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
      className="fixed m-0 w-60 max-w-[calc(100vw-16px)] overflow-y-auto rounded-lg border border-zinc-700 bg-zinc-900 p-2 text-xs text-zinc-200 ui-popover">
      <div className="mb-1 flex justify-between border-b border-zinc-700 pb-2">
        <button className="rounded px-2 py-1 hover:bg-zinc-800" onClick={() => onChange(null)}>Select all</button>
        <button className="rounded px-2 py-1 hover:bg-zinc-800" onClick={() => onChange([])}>Clear all</button>
      </div>
      <div data-filter-options>
        {drag.options.map(([value, name], index) => <div key={value} data-filter-option className="flex items-center rounded hover:bg-zinc-800">
          {onReorder && value !== pinnedFirst && <button type="button" aria-label={`Move ${name}`} title={`Drag to reorder ${name}; use Up or Down to move with keyboard`}
            disabled={reorderDisabled || drag.active} className="flex h-8 w-7 shrink-0 touch-none items-center justify-center text-zinc-500 hover:text-blue-300 disabled:opacity-40"
            onPointerDown={event => drag.start(event, value)} onKeyDown={event => {
              if (event.key !== "ArrowUp" && event.key !== "ArrowDown") return;
              event.preventDefault();
              const target = options[index + (event.key === "ArrowUp" ? -1 : 1)];
              if (target && target[0] !== pinnedFirst && !reorderDisabled && !drag.active) void onReorder(value, target[0], event.key === "ArrowDown");
            }}><GripVertical className="h-4 w-4" /></button>}
          <label className="flex min-w-0 flex-1 cursor-pointer items-center gap-2 px-2 py-2">
            <input type="checkbox" checked={selected === null || selected.includes(value)} onChange={event => {
              const current = selected ?? options.map(([key]) => key);
              onChange(event.target.checked ? [...current, value] : current.filter(key => key !== value));
            }} />{name}
          </label>
        </div>)}
      </div>
    </div>
  </>;
}
