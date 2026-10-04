"use client";

import { ChevronDown } from "lucide-react";
import { statuses } from "./board";

export function StatusCell({ name, value, disabled, canAdvance, onChange, onAdvance }: {
  name: string; value: string; disabled: boolean; canAdvance: boolean;
  onChange: (value: string) => void; onAdvance: () => void;
}) {
  return <div className="flex h-8 items-center">
    <button type="button" aria-label={`${name} · Advance load`} disabled={disabled || !canAdvance}
      onClick={event => { if (event.detail < 2) onAdvance(); }}
      className="h-8 min-w-0 flex-1 truncate px-1 text-left text-[10px] font-medium enabled:hover:bg-black/10 focus:ring-1 focus:ring-inset focus:ring-blue-500">{value || "—"}</button>
    <span className="relative h-8 w-5 shrink-0">
      <ChevronDown aria-hidden="true" className="pointer-events-none absolute left-1 top-2.5 h-3 w-3" />
      <select aria-label={`${name} · Status`} value={value} disabled={disabled} onChange={event => onChange(event.target.value)}
        className="absolute inset-0 h-full w-full cursor-pointer opacity-0">
        <option value="" className="bg-zinc-900 text-zinc-200">—</option>
        {statuses.map(status => <option key={status} className="bg-zinc-900 text-zinc-200">{status}</option>)}
      </select>
    </span>
  </div>;
}
