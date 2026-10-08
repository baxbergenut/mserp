"use client";

import { ChevronLeft, ChevronRight } from "lucide-react";
import { addDays, shortDate } from "@/app/gross-board/board";
import { payButtonClass } from "../driver-pay/DriverCard";

export default function EffectiveWeekPicker({ week, currentWeek, disabled, onChange }: {
  week: string; currentWeek: string; disabled: boolean; onChange: (week: string) => void;
}) {
  const end = addDays(week, 6);
  return <div className="flex shrink-0 items-center gap-2" role="group" aria-label="Effective week">
    <span className="text-xs text-zinc-400">Effective week</span>
    <div className="flex items-center gap-1 rounded-lg border border-zinc-800 bg-card p-1">
      <button type="button" aria-label="Previous week" className={payButtonClass} disabled={disabled || week <= "2000-01-03"} onClick={() => onChange(addDays(week, -7))}><ChevronLeft className="h-4 w-4" /></button>
      <span className="min-w-28 px-1.5 text-center font-mono text-sm text-zinc-100" data-week-start={week} title={`${week} to ${end}`}>{shortDate(week)}–{shortDate(end)}</span>
      <button type="button" aria-label="Next week" className={payButtonClass} disabled={disabled || week >= "2100-12-27"} onClick={() => onChange(addDays(week, 7))}><ChevronRight className="h-4 w-4" /></button>
    </div>
    <span className="text-xs text-zinc-500">{week.slice(0, 4)}{week.slice(0, 4) !== end.slice(0, 4) ? ` / ${end.slice(0, 4)}` : ""}</span>
    <button type="button" className={payButtonClass} disabled={disabled} onClick={() => onChange(currentWeek)}>This week</button>
  </div>;
}
