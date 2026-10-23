"use client";
import { useState } from "react";
import type { DriverLeavePlan } from "@/app/lib/types";
import { Field, controlClass } from "../management/ManagementUI";

export function DriverLeaveFields({ plan, onChange, vacation, disabled = false }: {
  plan: DriverLeavePlan; onChange: (plan: DriverLeavePlan) => void; vacation: boolean; disabled?: boolean;
}) {
  const [precision, setPrecision] = useState(plan.vacationReturn?.length === 7 ? "month" : plan.vacationReturn ? "date" : "unknown");
  if (vacation) return <div className="space-y-3">
    <Field label="When will the driver be back?"><select aria-label="Vacation return precision" className={controlClass} value={precision} disabled={disabled} onChange={e => { setPrecision(e.target.value); onChange({ ...plan, vacationReturn: "" }); }}>
      <option value="unknown">Unknown (optional)</option><option value="date">Exact date</option><option value="month">Month</option>
    </select></Field>
    {precision !== "unknown" && <Field label={precision === "month" ? "Back month" : "Back date"}><input aria-label={precision === "month" ? "Back month" : "Back date"} className={controlClass} disabled={disabled} type={precision} min={precision === "month" ? "2000-01" : "2000-01-01"} max={precision === "month" ? "2100-12" : "2100-12-31"} value={plan.vacationReturn ?? ""} onChange={e => onChange({ ...plan, vacationReturn: e.target.value })} /></Field>}
  </div>;
  return <div className="grid grid-cols-2 gap-3">
    <Field label="Home start date"><input aria-label="Home start date" required className={controlClass} disabled={disabled} type="date" min="2000-01-01" max={plan.homeEnd || "2100-12-31"} value={plan.homeStart ?? ""} onChange={e => onChange({ ...plan, homeStart: e.target.value })} /></Field>
    <Field label="Back date"><input aria-label="Back date" required className={controlClass} disabled={disabled} type="date" min={plan.homeStart || "2000-01-01"} max="2100-12-31" value={plan.homeEnd ?? ""} onChange={e => onChange({ ...plan, homeEnd: e.target.value })} /></Field>
  </div>;
}
