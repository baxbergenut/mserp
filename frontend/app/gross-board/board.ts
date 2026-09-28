import type { GrossBoardEntry } from "@/app/lib/types";

export function monday(date = new Date()) {
  const local = new Date(Date.UTC(date.getFullYear(), date.getMonth(), date.getDate()));
  local.setUTCDate(local.getUTCDate() - (local.getUTCDay() + 6) % 7);
  return local.toISOString().slice(0, 10);
}

export function addDays(date: string, days: number) {
  const value = new Date(`${date}T12:00:00Z`);
  value.setUTCDate(value.getUTCDate() + days);
  return value.toISOString().slice(0, 10);
}

export const shortDate = (date: string) => `${date.slice(5, 7)}/${date.slice(8, 10)}`;
export const entryKey = (driverId: string, date: string) => `${driverId}:${date}`;
export const emptyEntry = (driverId: string, date: string): GrossBoardEntry => ({
  driverId, date, loadNumber: "", loadRecordId: null, originalRate: "", driverRate: "", miles: "", version: 0,
});

export function validDecimal(value: string, nonnegative = false) {
  return value === "" || (/^-?\d{1,10}(\.\d{1,2})?$/.test(value) && (!nonnegative || !value.startsWith("-")));
}

// Keep money and mileage in hundredths; never accumulate binary floats.
export function hundredths(value: string) {
  if (!validDecimal(value) || value === "") return BigInt(0);
  const negative = value.startsWith("-");
  const [whole, fraction = ""] = value.replace(/^-/, "").split(".");
  return (BigInt(whole) * BigInt(100) + BigInt(fraction.padEnd(2, "0"))) * BigInt(negative ? -1 : 1);
}

export function decimalDisplay(value: bigint, currency = false) {
  const absolute = value < BigInt(0) ? -value : value;
  return `${value < BigInt(0) ? "-" : ""}${currency ? "$" : ""}${(absolute / BigInt(100)).toLocaleString("en-US")}.${(absolute % BigInt(100)).toString().padStart(2, "0")}`;
}

export function totals(entries: GrossBoardEntry[]) {
  return entries.reduce((sum, entry) => ({
    original: sum.original + hundredths(entry.originalRate),
    driver: sum.driver + hundredths(entry.driverRate),
    miles: sum.miles + hundredths(entry.miles),
  }), { original: BigInt(0), driver: BigInt(0), miles: BigInt(0) });
}

export function rpmDisplay(gross: bigint, miles: bigint) {
  if (miles === BigInt(0)) return "—";
  const sign = gross < BigInt(0) ? BigInt(-1) : BigInt(1);
  return decimalDisplay(sign * ((sign * gross * BigInt(100) + miles / BigInt(2)) / miles), true);
}

// Rebase edits made during an autosave onto the versions the server committed.
// Only the exact submitted draft may be removed from the pending queue.
export function reconcileAutosave(current: Record<string, GrossBoardEntry>, snapshot: Record<string, GrossBoardEntry>, committed: GrossBoardEntry[]) {
  const remaining = { ...current };
  for (const entry of committed) {
    const key = entryKey(entry.driverId, entry.date);
    const draft = current[key];
    if (!draft || JSON.stringify(draft) === JSON.stringify(snapshot[key])) { delete remaining[key]; continue; }
    const sameLoad = draft.loadNumber === snapshot[key].loadNumber && draft.loadRecordId === snapshot[key].loadRecordId;
    remaining[key] = { ...draft, version: entry.version,
      ...(sameLoad && entry.loadRecordId !== null ? { loadRecordId: entry.loadRecordId, originalRate: entry.originalRate, miles: entry.miles } : {}),
    };
  }
  return remaining;
}
