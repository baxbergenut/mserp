import type { GrossBoardDayStatus, GrossBoardEntry, GrossBoardLoad } from "@/app/lib/types";

export const dayStatuses: { value: Exclude<GrossBoardDayStatus, "">; aliases: string[]; color: string }[] = [
  { value: "SHOP", aliases: ["in shop", "repair", "maintenance"], color: "bg-amber-500/15 text-amber-300" },
  { value: "HOME", aliases: ["at home", "home time", "hometime"], color: "bg-violet-500/15 text-violet-300" },
  { value: "RESET", aliases: ["34 hour reset", "34 hr reset", "34 reset", "rest"], color: "bg-indigo-500/15 text-indigo-300" },
  { value: "IN TRANSIT", aliases: ["transit", "intransit", "on the road"], color: "bg-blue-500/15 text-blue-300" },
  { value: "REJECTED", aliases: ["reject", "refused"], color: "bg-rose-500/15 text-rose-300" },
  { value: "NO LOAD", aliases: ["no loads", "noload", "empty", "no load available"], color: "bg-zinc-500/15 text-zinc-300" },
  { value: "STUCK", aliases: ["stranded"], color: "bg-orange-500/15 text-orange-300" },
  { value: "LATE DEL", aliases: ["late delivery", "late del", "delayed delivery"], color: "bg-yellow-500/15 text-yellow-300" },
  { value: "TRUCK ISSUE", aliases: ["truck issues", "breakdown", "broken down"], color: "bg-red-500/15 text-red-300" },
  { value: "LEFT", aliases: ["driver left"], color: "bg-slate-500/15 text-slate-300" },
  { value: "NEW DRIVER", aliases: ["new hire", "newdriver"], color: "bg-teal-500/15 text-teal-300" },
  { value: "DEADHEAD", aliases: ["dead head", "deadheading"], color: "bg-cyan-500/15 text-cyan-300" },
];
const statusText = (text: string) => text.trim().toLowerCase().replace(/[-_]+/g, " ").replace(/\s+/g, " ");
export const exactDayStatus = (text: string) => dayStatuses.find((status) =>
  [status.value, ...status.aliases].some((name) => statusText(name) === statusText(text)));
export const suggestedDayStatuses = (text: string) => dayStatuses.filter((status) =>
  [status.value, ...status.aliases].some((name) => statusText(name).includes(statusText(text))));
export function setDayStatus(entry: GrossBoardEntry, dayStatus: GrossBoardDayStatus): GrossBoardEntry {
  return { ...emptyEntry(entry.driverId, entry.date, entry.slot), version: entry.version, dayStatus };
}

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
export const entryKey = (driverId: string, date: string, slot = 0) => `${driverId}:${date}:${slot}`;
export const emptyEntry = (driverId: string, date: string, slot = 0): GrossBoardEntry => ({
  driverId, date, slot, deleted: false, loadNumber: "", loadRecordId: null, originalRate: "", driverRate: "", miles: "", version: 0,
  enteredOriginalRate: "", enteredMiles: "", duplicate: false, dayStatus: "",
});

export function additionalLoadEntries(existing: GrossBoardEntry[], driverId: string, date: string) {
  const day = existing.filter(entry => entry.driverId === driverId && entry.date === date);
  const removed = day.find(entry => entry.deleted);
  const slot = removed?.slot ?? Math.max(0, ...day.map(entry => entry.slot)) + 1;
  if (slot > 99) return [];
  // An untouched day has a visible, unsaved slot zero. Keep it when adding.
  const first = day.some(entry => !entry.deleted) ? [] : [emptyEntry(driverId, date)];
  return [...first, { ...emptyEntry(driverId, date, slot), version: removed?.version ?? 0 }];
}

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
  return entries.filter((entry) => !entry.dayStatus && !entry.deleted).reduce((sum, entry) => ({
    original: sum.original + hundredths(entry.originalRate),
    driver: sum.driver + hundredths(entry.driverRate),
    miles: sum.miles + hundredths(entry.miles),
  }), { original: BigInt(0), driver: BigInt(0), miles: BigInt(0) });
}

export function matchLoad(entry: GrossBoardEntry, load: GrossBoardLoad): GrossBoardEntry {
  return { ...entry, dayStatus: "", loadNumber: load.loadNumber, loadRecordId: load.id,
    enteredOriginalRate: entry.enteredOriginalRate || entry.originalRate,
    enteredMiles: entry.enteredMiles || entry.miles,
    originalRate: load.originalRate, miles: load.miles, acceptSystemValues: false };
}

export function mismatch(entered: string, actual: string) {
  return entered !== "" && (actual === "" || hundredths(entered) !== hundredths(actual));
}

export function rateChange(entry: GrossBoardEntry): bigint | null {
  if (entry.deleted || entry.dayStatus || entry.duplicate || !entry.loadNumber.trim() || entry.originalRate === "" || entry.driverRate === ""
    || !validDecimal(entry.originalRate) || !validDecimal(entry.driverRate)) return null;
  return hundredths(entry.originalRate) - hundredths(entry.driverRate);
}

export function rateBalance(opening: string, entries: GrossBoardEntry[]) {
  return hundredths(opening) + entries.reduce((sum, entry) => sum + (rateChange(entry) ?? BigInt(0)), BigInt(0));
}

export function incompleteRates(entries: GrossBoardEntry[]) {
  return entries.filter((entry) => !entry.deleted && !entry.dayStatus && !entry.duplicate && rateChange(entry) === null &&
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
    const key = entryKey(entry.driverId, entry.date, entry.slot);
    const draft = current[key];
    if (!draft || JSON.stringify(draft) === JSON.stringify(snapshot[key])) { delete remaining[key]; continue; }
    const sameLoad = !draft.dayStatus && draft.loadNumber === snapshot[key].loadNumber && draft.loadRecordId === snapshot[key].loadRecordId;
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
