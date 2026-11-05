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

export function payrollWeekOptions(first: string, latest: string) {
  const groups = new Map<number, { date: string; label: string }[]>();
  const monday = new Date(`${first}T00:00:00Z`);
  const last = Date.parse(`${latest}T00:00:00Z`);
  while (monday.getTime() <= last) {
    const date = monday.toISOString().slice(0, 10);
    const { year, label } = payrollWeekLabel(date);
    if (!groups.has(year)) groups.set(year, []);
    groups.get(year)!.push({ date, label });
    monday.setUTCDate(monday.getUTCDate() + 7);
  }
  return Array.from(groups).reverse();
}
