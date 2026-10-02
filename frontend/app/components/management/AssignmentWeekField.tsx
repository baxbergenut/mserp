"use client";

import { currentChargeWeek } from "@/app/accounting/driver-charges/charges";
import { controlClass, Field } from "./ManagementUI";

export function AssignmentWeekField({ value, onChange }: { value: string | undefined; onChange: (week: string) => void }) {
  if (value === undefined) return null;
  const currentWeek = currentChargeWeek();
  return <Field label="Assignment starts week (Monday)" wide hint="Choose when these assignment changes start. Earlier weeks keep their previous assignments.">
    <div className="flex items-center gap-2">
      <input aria-label="Assignment starts week (Monday)" type="date" required min="1970-01-05" step={7} max={currentWeek} value={value} onChange={event => onChange(event.target.value)} className={controlClass} />
      <button type="button" onClick={() => onChange(currentWeek)} className="shrink-0 rounded-lg border border-zinc-700 px-3 py-2 text-xs text-zinc-300 hover:bg-zinc-800">This week</button>
    </div>
  </Field>;
}
