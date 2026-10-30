import { statusClass, displayStatus } from "@/app/lib/statusColors";
import type { GrossBoardDayStatus, GrossBoardDriver, GrossBoardEntry, GrossBoardLoad } from "@/app/lib/types";

export function dispatcherRows(drivers: GrossBoardDriver[], dates: string[], entries: GrossBoardEntry[] = []) {
  return drivers.flatMap(driver => {
    const assignments = driver.assignments?.length ? driver.assignments : dates.map(date => ({ ...driver, date }));
    const groups = new Map<string, GrossBoardDriver & { dates: string[] }>();
    for (const assignment of assignments) {
      const group = groups.get(assignment.dispatcherId) ?? { ...driver, dispatcherId: assignment.dispatcherId,
        dispatcherName: assignment.dispatcherName, truckId: assignment.truckId, truckUnit: assignment.truckUnit,
        dates: [] };
      group.dates.push(assignment.date);
      if (group.truckId !== assignment.truckId && assignment.truckUnit) {
        group.truckId = "";
        group.truckUnit = Array.from(new Set([...group.truckUnit.split(" / ").filter(Boolean), assignment.truckUnit])).join(" / ");
      }
      groups.set(assignment.dispatcherId, group);
    }
    for (const entry of entries.filter(e => e.driverId === driver.id && e.transferVersion)) {
      const id=entry.creditedDispatcherId || "";
      const group=groups.get(id) ?? {...driver,dispatcherId:id,dispatcherName:entry.creditedDispatcherName || "Unassigned",dates:[]};
      if(!group.dates.includes(entry.date)) group.dates.push(entry.date);
      groups.set(id,group);
    }
    return [...groups.values()];
  }).sort((a,b) => a.dispatcherName.localeCompare(b.dispatcherName) || a.dispatcherId.localeCompare(b.dispatcherId) || a.fullName.localeCompare(b.fullName) || a.id.localeCompare(b.id));
}

export const entryForDispatcher = (entry: GrossBoardEntry, dispatcherId: string, driver?: GrossBoardDriver) => entry.transferVersion
  ? (entry.creditedDispatcherId || "") === dispatcherId
  : !driver || (driver.assignments?.length ? driver.assignments.some(a => a.date === entry.date && a.dispatcherId === dispatcherId) : driver.dispatcherId === dispatcherId);

export const dayStatuses: { value: Exclude<GrossBoardDayStatus, "">; aliases: string[]; color: string }[] = [
  { value: "SHOP", aliases: ["in shop", "repair", "maintenance"], color: "" },
  { value: "HOME", aliases: ["at home", "home time", "hometime"], color: "" },
  { value: "REST", aliases: ["rest"], color: "" },
  { value: "ENROUTE", aliases: ["enroute", "en route"], color: "" },
  { value: "REJECTED", aliases: ["reject", "refused"], color: "" },
  { value: "LOAD CANCELLED", aliases: ["cancelled", "canceled", "load canceled", "cancelled load", "canceled load"], color: "" },
  { value: "NO LOAD", aliases: ["no loads", "noload", "empty", "no load available"], color: "" },
  { value: "STUCK", aliases: ["stranded"], color: "" },
  { value: "LATE DEL", aliases: ["late delivery", "late del", "delayed delivery"], color: "" },
  { value: "TRUCK ISSUE", aliases: ["truck issues", "breakdown", "broken down"], color: "" },
  { value: "LEFT", aliases: ["driver left"], color: "" },
  { value: "NEW DRIVER", aliases: ["new hire", "newdriver"], color: "" },
  { value: "DEADHEAD", aliases: ["dead head", "deadheading"], color: "" },
].map(status => ({ ...status, color: statusClass(status.value, "driver") })) as { value: Exclude<GrossBoardDayStatus, "">; aliases: string[]; color: string }[];
const statusText = (text: string) => displayStatus(text).trim().toLowerCase().replace(/[-_]+/g, " ").replace(/\s+/g, " ");
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
  driverId, date, slot, deleted: false, loadNumber: "", note: "", loadRecordId: null, loadTripId: null, split: false, tripReview: false, tripReviewReason: "", trips: [], originalRate: "", driverRate: "", miles: "", version: 0,
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

// Plain editing text; keep the exact saved decimal for calculations.
export function compactDecimal(value: string) {
  return value.replace(/(\.\d*?[1-9])0+$|\.0+$/, "$1");
}

// Accept intermediate typing, but send only complete decimals to autosave.
// null rejects the edit (including a third fractional digit) before it is queued.
export function decimalInputValue(value: string, nonnegative = false): string | null {
  if (!/^-?\d{0,10}(\.\d{0,2})?$/.test(value) || (nonnegative && value.startsWith("-"))) return null;
  if (["", "-", ".", "-."].includes(value)) return "";
  return value.replace(/\.$/, "").replace(/^(-?)\./, (_, sign: string) => `${sign}0.`);
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

export function amountDisplay(value: string, currency = false) {
  return value === "" || !validDecimal(value) ? value : decimalDisplay(hundredths(value), currency);
}

export function totals(entries: GrossBoardEntry[]) {
  return entries.filter((entry) => !entry.dayStatus && !entry.deleted && !entry.duplicate).reduce((sum, entry) => ({
    original: sum.original + (entry.tripReview ? BigInt(0) : hundredths(entry.originalRate)),
    driver: sum.driver + hundredths(entry.driverRate),
    miles: sum.miles + (entry.tripReview ? BigInt(0) : hundredths(entry.miles)),
  }), { original: BigInt(0), driver: BigInt(0), miles: BigInt(0) });
}

export function matchLoad(entry: GrossBoardEntry, load: GrossBoardLoad): GrossBoardEntry {
  if (load.split) return { ...entry, dayStatus: "", loadNumber: load.loadNumber, loadRecordId: load.id,
    loadTripId: null, split: true, tripReview: true, tripReviewReason: load.tripReviewReason ?? "", trips: load.trips ?? [],
    originalRate: "", miles: "", systemOriginalRate: "", systemMiles: "", acceptSystemValues: false };
  const original = entry.enteredOriginalRate || entry.originalRate || load.originalRate;
  const miles = entry.enteredMiles || entry.miles || load.miles;
  return { ...entry, dayStatus: "", loadNumber: load.loadNumber, loadRecordId: load.id, loadTripId: null, split: false, tripReview: false, tripReviewReason: "", trips: load.trips ?? [],
    note: entry.loadNumber.trim().toLowerCase() === load.loadNumber.trim().toLowerCase() && (entry.loadRecordId === null || entry.loadRecordId === load.id) ? entry.note : "",
    enteredOriginalRate: entry.enteredOriginalRate || entry.originalRate,
    enteredMiles: entry.enteredMiles || entry.miles,
    originalRate: original, miles, systemOriginalRate: load.originalRate, systemMiles: load.miles,
    driverRate: entry.driverRate === "" ? original : entry.driverRate, acceptSystemValues: false };
}

// Follow original pay until the dispatcher has entered a different driver gross.
export function editAmount(entry: GrossBoardEntry, field: "originalRate" | "driverRate" | "miles", value: string): GrossBoardEntry {
  const effective = value === "" && entry.loadRecordId !== null && !entry.tripReview
    ? field === "originalRate" ? entry.systemOriginalRate ?? "" : field === "miles" ? entry.systemMiles ?? "" : value
    : value;
  return { ...entry, [field]: effective, acceptSystemValues: false,
    ...(field === "originalRate" ? { enteredOriginalRate: value,
      driverRate: !entry.split && (entry.driverRate === "" || (validDecimal(entry.driverRate) && validDecimal(entry.originalRate) && hundredths(entry.driverRate) === hundredths(entry.originalRate))) ? value : entry.driverRate,
    } : field === "miles" ? { enteredMiles: value } : {}),
  };
}

// Daily loads is a local draft. An earlier autosave can finish while it is open;
// apply only this editor's changes, keeping that save's version and metadata.
// Live updates from other editors stay fenced while the day editor is open.
export function applyDailyDraft(current: GrossBoardEntry, baseline: GrossBoardEntry | undefined, draft: GrossBoardEntry): GrossBoardEntry {
  if (!baseline) return { ...draft, version: current.version };
  const changed = Object.fromEntries(Object.entries(draft).filter(([field, value]) =>
    field !== "version" && JSON.stringify(value) !== JSON.stringify(baseline[field as keyof GrossBoardEntry])));
  if (!Object.keys(changed).length) return current;
  const sameLoad = draft.loadNumber === baseline.loadNumber && draft.loadRecordId === baseline.loadRecordId
    && draft.loadTripId === baseline.loadTripId && draft.dayStatus === baseline.dayStatus && draft.deleted === baseline.deleted;
  return { ...(sameLoad ? { ...current, ...changed } : draft), version: current.version };
}

export function mismatch(entered: string, actual: string) {
  return entered !== "" && (actual === "" || hundredths(entered) !== hundredths(actual));
}

export function rateChange(entry: GrossBoardEntry): bigint | null {
  if (entry.deleted || entry.dayStatus || entry.duplicate || entry.tripReview || !entry.loadNumber.trim() || entry.originalRate === "" || entry.driverRate === ""
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
    const sameLoad = !draft.dayStatus && draft.loadNumber === snapshot[key].loadNumber && draft.loadRecordId === snapshot[key].loadRecordId && draft.loadTripId === snapshot[key].loadTripId;
    remaining[key] = { ...draft, version: entry.version,
      ...(sameLoad && entry.loadRecordId !== null ? {
        loadRecordId: entry.loadRecordId, loadTripId: entry.loadTripId, split: entry.split, tripReview: entry.tripReview, tripReviewReason: entry.tripReviewReason, trips: entry.trips,
        originalRate: draft.originalRate === snapshot[key].originalRate ? entry.originalRate : draft.originalRate,
        driverRate: draft.driverRate === snapshot[key].driverRate ? entry.driverRate : draft.driverRate,
        miles: draft.miles === snapshot[key].miles ? entry.miles : draft.miles,
        systemOriginalRate: entry.systemOriginalRate, systemMiles: entry.systemMiles,
        enteredOriginalRate: draft.enteredOriginalRate === snapshot[key].enteredOriginalRate ? entry.enteredOriginalRate : draft.enteredOriginalRate,
        enteredMiles: draft.enteredMiles === snapshot[key].enteredMiles ? entry.enteredMiles : draft.enteredMiles,
        duplicate: entry.duplicate,
        acceptSystemValues: draft.acceptSystemValues === snapshot[key].acceptSystemValues ? false : draft.acceptSystemValues,
      } : {}),
    };
  }
  return remaining;
}

// Build lookup buckets once per board revision instead of scanning every entry
// for every driver/day. Keep tombstones and slot order for editing/versioning.
export function indexBoardEntries(entries: GrossBoardEntry[]) {
  const byDriver = new Map<string, GrossBoardEntry[]>();
  const byDay = new Map<string, GrossBoardEntry[]>();
  for (const entry of entries) {
    const driverEntries = byDriver.get(entry.driverId) ?? [];
    driverEntries.push(entry); byDriver.set(entry.driverId, driverEntries);
    const key = `${entry.driverId}:${entry.date}`;
    const dayEntries = byDay.get(key) ?? [];
    dayEntries.push(entry); byDay.set(key, dayEntries);
  }
  for (const day of byDay.values()) day.sort((a, b) => a.slot - b.slot);
  return { byDriver, byDay };
}
