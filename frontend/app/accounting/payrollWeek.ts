export function latestPayrollWeek(now = new Date()) {
  const today = new Intl.DateTimeFormat("en-CA", { timeZone: "America/New_York", year: "numeric", month: "2-digit", day: "2-digit" }).format(now);
  const day = new Date(`${today}T12:00:00Z`);
  day.setUTCDate(day.getUTCDate() - (day.getUTCDay() + 6) % 7 - 7);
  return day.toISOString().slice(0, 10);
}

export function payrollWeekLabel(monday: string) {
  // Thursday determines the ISO week year, including weeks that span New Year's.
  const thursday = new Date(`${monday}T00:00:00Z`);
  thursday.setUTCDate(thursday.getUTCDate() + 3);
  const year = thursday.getUTCFullYear();
  const week = Math.floor((thursday.getTime() - Date.UTC(year, 0, 1)) / (7 * 86_400_000)) + 1;
  return { year, label: `Week ${week} - ${monday.slice(5, 7)}/${monday.slice(8, 10)}` };
}

export function payrollWeekRangeLabel(monday: string) {
  const { year, label } = payrollWeekLabel(monday);
  const sunday = new Date(`${monday}T00:00:00Z`);
  sunday.setUTCDate(sunday.getUTCDate() + 6);
  const end = sunday.toISOString().slice(0, 10);
  return { year, label: `${label.replace(" - ", " · ")} → ${end.slice(5, 7)}/${end.slice(8, 10)}` };
}

export function payrollWeekGroups(weeks: string[]) {
  const groups = new Map<number, { date: string; label: string }[]>();
  for (const date of Array.from(new Set(weeks)).sort().reverse()) {
    const { year, label } = payrollWeekRangeLabel(date);
    if (!groups.has(year)) groups.set(year, []);
    groups.get(year)!.push({ date, label });
  }
  return Array.from(groups);
}

export function payrollWeekOptions(first: string, latest: string) {
  const weeks: string[] = [];
  const monday = new Date(`${first}T00:00:00Z`);
  const last = Date.parse(`${latest}T00:00:00Z`);
  while (monday.getTime() <= last) {
    const date = monday.toISOString().slice(0, 10);
    weeks.push(date);
    monday.setUTCDate(monday.getUTCDate() + 7);
  }
  return payrollWeekGroups(weeks);
}
