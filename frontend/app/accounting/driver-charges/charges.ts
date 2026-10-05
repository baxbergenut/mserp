import type { ChargeEligibility, ChargeSchedule } from "@/app/lib/types";

export function currentChargeWeek() {
  const today = new Intl.DateTimeFormat("en-CA", { timeZone: "America/New_York", year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
  const d = new Date(`${today}T12:00:00Z`);
  d.setUTCDate(d.getUTCDate() - (d.getUTCDay() + 6) % 7);
  return d.toISOString().slice(0, 10);
}

export const eligibilityLabel = (rule: ChargeEligibility) => ({ calendar: "Every calendar week", loads: "Only weeks with Gross Board loads", no_loads: "Only weeks without Gross Board loads" })[rule];

export function validChargeWeek(week: string) {
  return /^\d{4}-\d{2}-\d{2}$/.test(week) && week >= "2000-01-03" && week <= "2100-12-27" && new Date(`${week}T12:00:00Z`).getUTCDay() === 1;
}

export function recurringCell(schedules: ChargeSchedule[], driverId: string, typeId: string, week: string) {
  const schedule = schedules.find(s => s.kind === "recurring" && s.driverId === driverId && s.typeId === typeId && s.startWeek <= week && (!s.endWeek || s.endWeek >= week));
  const phase = schedule?.phases.filter(p => p.weekStart <= week).at(-1);
  return { schedule, phase, included: !!phase && !phase.paused };
}
