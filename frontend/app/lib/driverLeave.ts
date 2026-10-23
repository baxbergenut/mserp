import type { DriverLeavePlan } from "./types";

export function leaveToday(now = new Date()) {
  return new Intl.DateTimeFormat("en-CA", { timeZone: "America/New_York", year: "numeric", month: "2-digit", day: "2-digit" }).format(now);
}

export function leaveDate(value: string, today: string) {
  const date = new Date(value + (value.length === 7 ? "-01" : "") + "T12:00:00Z");
  const current = new Date(today + "T12:00:00Z");
  if (!Number.isFinite(date.getTime())) return "";
  if (value.length === 7) return date.toLocaleDateString("en-US", { timeZone: "UTC", month: "short", ...(value.slice(0, 4) !== today.slice(0, 4) ? { year: "numeric" } : {}) }).toUpperCase();
  const monday = new Date(current); monday.setUTCDate(current.getUTCDate() - (current.getUTCDay() + 6) % 7);
  if (date >= monday && date.getTime() < monday.getTime() + 7 * 86400000) return date.toLocaleDateString("en-US", { timeZone: "UTC", weekday: "short" }).toUpperCase();
  return `${date.getUTCDate()} ${date.toLocaleDateString("en-US", { timeZone: "UTC", month: "short" }).toUpperCase()}${value.slice(0, 4) !== today.slice(0, 4) ? ` ${value.slice(0, 4)}` : ""}`;
}

export function formatDriverLeave(plan: DriverLeavePlan | undefined, status: string, legacy = "", today = leaveToday()) {
  if (status.toUpperCase() === "VACATION") return plan?.vacationReturn ? `BACK ${leaveDate(plan.vacationReturn, today)}` : "BACK TBD";
  const home = ["HOME", "HOME (YARD)"].includes(status.toUpperCase());
  if (home && plan?.homeStart && plan.homeStart <= today) return plan.homeEnd ? `BACK ${leaveDate(plan.homeEnd, today)}` : "BACK TBD";
  // Finished plans remain in history, but don't advertise an old departure after
  // the driver has resumed work. An overdue return stays visible while Home.
  if (plan?.homeStart && (home || !plan.homeEnd || plan.homeEnd >= today)) return `HOME ${leaveDate(plan.homeStart, today)}`;
  if (home) return "BACK TBD";
  return legacy || "—";
}

export function leavePlanDetails(plan?: DriverLeavePlan) {
  return [plan?.homeStart && `Home: ${plan.homeStart} · Back: ${plan.homeEnd || "Unknown"}`, plan?.vacationReturn && `Vacation return: ${plan.vacationReturn}`].filter(Boolean).join("\n");
}
