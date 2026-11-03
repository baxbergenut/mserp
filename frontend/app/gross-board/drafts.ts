import type { GrossBoardEntry } from "@/app/lib/types";
import { emptyEntry, entryKey, hundredths, validDecimal } from "./board";

export type GrossDrafts = Record<string, GrossBoardEntry>;
const fields = ["loadNumber", "loadRecordId", "loadTripId", "dayStatus", "deleted", "originalRate", "driverRate", "miles", "enteredOriginalRate", "enteredMiles", "note", "acceptSystemValues"] as const;
const amounts = new Set<string>(["originalRate", "driverRate", "miles", "enteredOriginalRate", "enteredMiles"]);
const identity = ["loadNumber", "loadRecordId", "loadTripId", "dayStatus", "deleted"] as const;
function equal(a: GrossBoardEntry, b: GrossBoardEntry, field: typeof fields[number]) {
  const left = a[field], right = b[field];
  if (amounts.has(field) && typeof left === "string" && typeof right === "string" && left && right && validDecimal(left) && validDecimal(right)) return hundredths(left) === hundredths(right);
  const fallback = field === "loadRecordId" || field === "loadTripId" ? null : field === "deleted" || field === "acceptSystemValues" ? false : "";
  return (left ?? fallback) === (right ?? fallback);
}

// Replacing/clearing a load is one coherent action. Ordinary amount/note edits
// send only their changed fields; the server merges them under its write lock.
export function grossDraftFields(draft: GrossBoardEntry, baseline?: GrossBoardEntry): string[] {
  if (!baseline || identity.some(field => !equal(draft, baseline, field))) return [...fields];
  return fields.filter(field => !equal(draft, baseline, field));
}
function applyFields(base: GrossBoardEntry, draft: GrossBoardEntry, changed: string[]) {
  const next = { ...base };
  for (const field of fields) if (changed.includes(field)) Object.assign(next, { [field]: draft[field] });
  if (draft.splitReview) next.splitReview = draft.splitReview;
  return next;
}

export function reconcileGrossPatches(current: GrossDrafts, sent: GrossDrafts, committed: GrossBoardEntry[]) {
  const remaining = { ...current };
  for (const saved of committed) {
    const key = entryKey(saved.driverId, saved.date, saved.slot), draft = current[key];
    if (!draft) continue;
    const changed = grossDraftFields(draft, sent[key]);
    if (!changed.length && JSON.stringify(draft.splitReview) === JSON.stringify(sent[key]?.splitReview)) delete remaining[key];
    else remaining[key] = applyFields(saved, { ...draft, splitReview: JSON.stringify(draft.splitReview) === JSON.stringify(sent[key]?.splitReview) ? undefined : draft.splitReview }, changed);
  }
  return remaining;
}

export function restoreGrossDrafts(changes: GrossDrafts, baselines: GrossDrafts, entries: GrossBoardEntry[]) {
  const saved = Object.fromEntries(entries.map(entry => [entryKey(entry.driverId, entry.date, entry.slot), entry]));
  const restored: GrossDrafts = {}, bases: GrossDrafts = {};
  for (const [key, draft] of Object.entries(changes)) {
    const remote = saved[key] ?? emptyEntry(draft.driverId, draft.date, draft.slot);
    const changed = grossDraftFields(draft, baselines[key]);
    const next = applyFields(remote, draft, changed);
    if (!grossDraftFields(next, remote).length && !next.splitReview) continue;
    restored[key] = next; bases[key] = remote;
  }
  return { changes: restored, baselines: bases };
}

export function readGrossDrafts(key: string): { changes: GrossDrafts; baselines: GrossDrafts } | null {
  try {
    const value = JSON.parse(sessionStorage.getItem(key) ?? "null");
    if (!value || !value.changes || !value.baselines || Date.now() - value.at > 7 * 86400000) return null;
    return value;
  } catch { return null; }
}

export function writeGrossDrafts(key: string, changes: GrossDrafts, baselines: GrossDrafts) {
  try {
    if (Object.keys(changes).length) sessionStorage.setItem(key, JSON.stringify({ at: Date.now(), changes,
      baselines: Object.fromEntries(Object.keys(changes).map(id => [id, baselines[id]])) }));
    else sessionStorage.removeItem(key);
    return true;
  } catch { return false; }
}
