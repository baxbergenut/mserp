"use client";

import { currentChargeWeek } from "@/app/accounting/driver-charges/charges";
import { controlClass, Field } from "./ManagementUI";

export function AssignmentWeekField({ value, onChange, team = false }: { value: string | undefined; onChange: (week: string) => void; team?: boolean }) {
  if (value === undefined) return null;
  const today = new Intl.DateTimeFormat("en-CA", { timeZone: "America/New_York", year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
  const label = team ? "Assignment starts week (Monday)" : "Assignment effective date";
  return <Field label={label} wide hint={team ? "Team membership changes start on Monday. Finalized payroll must be reopened first." : "Choose any date. Earlier days keep their assignments. Finalized payroll covering this date or later must be reopened first."}>
    <div className="flex items-center gap-2">
      <input aria-label={label} type="date" required min={team ? "2000-01-03" : "2000-01-01"} max="2100-12-31" step={team ? 7 : 1} value={value} onChange={event => onChange(event.target.value)} className={controlClass} />
      <button type="button" onClick={() => onChange(team ? currentChargeWeek() : today)} className="shrink-0 rounded-lg border border-zinc-700 px-3 py-2 text-xs text-zinc-300 hover:bg-zinc-800">{team ? "This week" : "Today"}</button>
    </div>
  </Field>;
}
