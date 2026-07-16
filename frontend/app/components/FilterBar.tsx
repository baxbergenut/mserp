"use client";

import { X } from "lucide-react";
import { statusLabel, type Filters, EMPTY_FILTERS } from "../lib/utils";

export type { Filters };
export { EMPTY_FILTERS };

interface FilterBarProps {
  filters: Filters;
  onChange: (filters: Filters) => void;
  statusOptions: string[];
  customerOptions: string[];
  dispatcherOptions: string[];
  driverOptions: string[];
}

function Select({
  value,
  onChange,
  options,
  placeholder,
  labelFor,
}: {
  value: string;
  onChange: (v: string) => void;
  options: string[];
  placeholder: string;
  labelFor?: (v: string) => string;
}) {
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="rounded-lg border border-zinc-800 bg-zinc-950 px-2.5 py-1.5 text-[13px] text-zinc-300 outline-none transition-colors focus:border-zinc-600"
    >
      <option value="">{placeholder}</option>
      {options.map((opt) => (
        <option key={opt} value={opt}>
          {labelFor ? labelFor(opt) : opt}
        </option>
      ))}
    </select>
  );
}

export function FilterBar({
  filters,
  onChange,
  statusOptions,
  customerOptions,
  dispatcherOptions,
  driverOptions,
}: FilterBarProps) {
  const set = (patch: Partial<Filters>) => onChange({ ...filters, ...patch });
  const hasActiveFilters = Object.values(filters).some(Boolean);

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Select
        value={filters.status}
        onChange={(v) => set({ status: v })}
        options={statusOptions}
        placeholder="Status"
        labelFor={statusLabel}
      />
      <Select
        value={filters.customer}
        onChange={(v) => set({ customer: v })}
        options={customerOptions}
        placeholder="Customer"
      />
      <Select
        value={filters.dispatcher}
        onChange={(v) => set({ dispatcher: v })}
        options={dispatcherOptions}
        placeholder="Dispatcher"
      />
      <Select
        value={filters.driver}
        onChange={(v) => set({ driver: v })}
        options={driverOptions}
        placeholder="Driver"
      />

      <div className="flex items-center gap-1.5">
        <input
          type="date"
          value={filters.pickupFrom}
          onChange={(e) => set({ pickupFrom: e.target.value })}
          className="rounded-lg border border-zinc-800 bg-zinc-950 px-2.5 py-1.5 text-[13px] text-zinc-400 outline-none transition-colors focus:border-zinc-600"
        />
        <span className="text-[13px] text-zinc-700">–</span>
        <input
          type="date"
          value={filters.pickupTo}
          onChange={(e) => set({ pickupTo: e.target.value })}
          className="rounded-lg border border-zinc-800 bg-zinc-950 px-2.5 py-1.5 text-[13px] text-zinc-400 outline-none transition-colors focus:border-zinc-600"
        />
      </div>

      {hasActiveFilters && (
        <button
          onClick={() => onChange(EMPTY_FILTERS)}
          className="inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-[13px] text-zinc-500 transition-colors hover:bg-zinc-800/50 hover:text-zinc-300"
        >
          <X className="h-3 w-3" />
          Clear
        </button>
      )}
    </div>
  );
}
