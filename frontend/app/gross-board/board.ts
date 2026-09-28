import type { GrossBoardEntry, GrossBoardLoad } from "@/app/lib/types";

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
  enteredOriginalRate: "", enteredMiles: "", duplicate: false,
});

export function validDecimal(value: string, nonnegative = false) {
  return value === "" || (/^-?\d{1,10}(\.\d{1,2})?$/.test(value) && (!nonnegative || !value.startsWith("-")));
}

// Keep money and mileage in hundredths; never accumulate binary floats.
export function hundredths(value: string) {
  if (!/^-?\d+(\.\d{1,2})?$/.test(value)) return BigInt(0);
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

export function matchLoad(entry: GrossBoardEntry, load: GrossBoardLoad): GrossBoardEntry {
  return { ...entry, loadNumber: load.loadNumber, loadRecordId: load.id,
    enteredOriginalRate: entry.enteredOriginalRate || entry.originalRate,
    enteredMiles: entry.enteredMiles || entry.miles,
    originalRate: load.originalRate, miles: load.miles, acceptSystemValues: false };
}

export function mismatch(entered: string, actual: string) {
  return entered !== "" && (actual === "" || hundredths(entered) !== hundredths(actual));
}

export function rateChange(entry: GrossBoardEntry): bigint | null {
  if (entry.duplicate || !entry.loadNumber.trim() || entry.originalRate === "" || entry.driverRate === ""
    || !validDecimal(entry.originalRate) || !validDecimal(entry.driverRate)) return null;
  return hundredths(entry.originalRate) - hundredths(entry.driverRate);
}

export function rateBalance(opening: string, entries: GrossBoardEntry[]) {
  return hundredths(opening) + entries.reduce((sum, entry) => sum + (rateChange(entry) ?? BigInt(0)), BigInt(0));
}

export function incompleteRates(entries: GrossBoardEntry[]) {
  return entries.filter((entry) => !entry.duplicate && rateChange(entry) === null &&
    (entry.loadNumber.trim() || entry.originalRate || entry.driverRate)).length;
}

export const balanceLabel = (balance: bigint) => balance > BigInt(0) ? "Available" : balance < BigInt(0) ? "Uncovered" : "Balanced";
export const signedMoney = (value: bigint) => (value > BigInt(0) ? "+" : "") + decimalDisplay(value, true);

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
      ...(sameLoad && entry.loadRecordId !== null ? {
        loadRecordId: entry.loadRecordId, originalRate: entry.originalRate, miles: entry.miles,
        enteredOriginalRate: draft.enteredOriginalRate === snapshot[key].enteredOriginalRate ? entry.enteredOriginalRate : draft.enteredOriginalRate,
        enteredMiles: draft.enteredMiles === snapshot[key].enteredMiles ? entry.enteredMiles : draft.enteredMiles,
        duplicate: entry.duplicate,
        acceptSystemValues: draft.acceptSystemValues === snapshot[key].acceptSystemValues ? false : draft.acceptSystemValues,
      } : {}),
    };
  }
  return remaining;
}
