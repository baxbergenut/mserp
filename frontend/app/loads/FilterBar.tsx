"use client";

import { statusLabel } from "../utils";

export interface Filters {
  status: string;
  customer: string;
  dispatcher: string;
  driver: string;
  pickupFrom: string;
  pickupTo: string;
}

export const EMPTY_FILTERS: Filters = {
  status: "",
  customer: "",
  dispatcher: "",
  driver: "",
  pickupFrom: "",
  pickupTo: "",
};

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
      className="rounded-md border border-zinc-800 bg-zinc-950 px-2.5 py-1.5 text-[13px] text-zinc-300 outline-none transition-colors focus:border-zinc-600"
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
          className="rounded-md border border-zinc-800 bg-zinc-950 px-2.5 py-1.5 text-[13px] text-zinc-400 outline-none transition-colors focus:border-zinc-600 [color-scheme:dark]"
        />
        <span className="text-[13px] text-zinc-700">–</span>
        <input
          type="date"
          value={filters.pickupTo}
          onChange={(e) => set({ pickupTo: e.target.value })}
          className="rounded-md border border-zinc-800 bg-zinc-950 px-2.5 py-1.5 text-[13px] text-zinc-400 outline-none transition-colors focus:border-zinc-600 [color-scheme:dark]"
        />
      </div>

      {hasActiveFilters && (
        <button
          onClick={() => onChange(EMPTY_FILTERS)}
          className="text-[13px] text-zinc-500 underline decoration-zinc-700 underline-offset-2 transition-colors hover:text-zinc-300"
        >
          Clear filters
        </button>
      )}
    </div>
  );
}
