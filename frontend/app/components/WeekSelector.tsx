"use client";

import { useMemo } from "react";
import { ChevronDown, ChevronLeft, ChevronRight } from "lucide-react";
import { FIRST_CLUSTIE_WEEK, payrollWeekGroups, payrollWeekLabel, payrollWeekOptions } from "@/app/accounting/payrollWeek";
import { controlClass } from "./management/ManagementUI";

type WeekSelectorProps = {
  label?: string;
  week: string;
  weeks?: string[];
  firstWeek?: string;
  lastWeek: string;
  disabled?: boolean;
  onChange: (week: string) => void;
  jump?: { label: string; onClick: () => void; disabled?: boolean };
};

const arrow = "ui-button shrink-0 !px-2 focus-visible:outline-2 focus-visible:outline-blue-500 disabled:cursor-not-allowed disabled:opacity-40";

export function WeekSelector({
  label = "Week", week, disabled, onChange, jump,
  weeks, firstWeek = FIRST_CLUSTIE_WEEK, lastWeek,
}: WeekSelectorProps) {
  const groups = useMemo(() => weeks
    ? payrollWeekGroups(weeks.filter(date => date >= firstWeek && date <= lastWeek))
    : payrollWeekOptions(firstWeek, lastWeek), [weeks, firstWeek, lastWeek]);
  const dates = useMemo(() => groups.flatMap(([, options]) => options.map(option => option.date)), [groups]);
  const options = useMemo(() => groups.map(([year, items]) => <optgroup key={year} label={String(year)}>
    {items.map(({ date, label }) => <option key={date} value={date}>{label}</option>)}
  </optgroup>), [groups]);
  const index = dates.indexOf(week);
  const previous = index < 0 ? undefined : dates[index + 1];
  const next = index > 0 ? dates[index - 1] : undefined;

  return <div className="flex max-w-full flex-wrap items-center gap-2" role="group" aria-label={`${label} navigation`}>
    <div className="flex max-w-full min-w-0 items-stretch gap-1">
      <button type="button" aria-label="Previous week" title="Previous week" className={arrow} disabled={disabled || !previous} onClick={() => previous && onChange(previous)}>
        <ChevronLeft aria-hidden="true" className="h-4 w-4" />
      </button>
      <div className="relative min-w-0">
        <select aria-label={label} data-week-start={week} className={controlClass + " h-full min-h-8 appearance-none !py-0 !pr-8 font-mono"}
          value={week} disabled={disabled} onChange={event => onChange(event.target.value)}>
          {options}
        </select>
        <ChevronDown aria-hidden="true" className="pointer-events-none absolute right-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-zinc-500" />
      </div>
      <button type="button" aria-label="Next week" title="Next week" className={arrow} disabled={disabled || !next} onClick={() => next && onChange(next)}>
        <ChevronRight aria-hidden="true" className="h-4 w-4" />
      </button>
    </div>
    <span className="text-xs tabular-nums text-zinc-400">{payrollWeekLabel(week).year}</span>
    {jump && <button type="button" className="ui-button shrink-0 whitespace-nowrap disabled:cursor-not-allowed disabled:opacity-40"
      disabled={disabled || jump.disabled} onClick={jump.onClick}>{jump.label}</button>}
  </div>;
}
