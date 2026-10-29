import type { GrossBoard } from "@/app/lib/types";
import { entryKey } from "./board";

export function mergeGrossLive(current: GrossBoard | null, incoming: GrossBoard, protectedKeys: Set<string>, protectedDays: Set<string>) {
  if (!current || current.weekStart !== incoming.weekStart) return incoming;
  const previous = new Map(current.entries.map(e => [entryKey(e.driverId,e.date,e.slot), e]));
  const next = new Map(incoming.entries.map(e => [entryKey(e.driverId,e.date,e.slot), e]));
  for (const [key, e] of next) if (protectedDays.has(`${e.driverId}:${e.date}`)) protectedKeys.add(key);
  for (const [key, e] of previous) {
    const incomingEntry = next.get(key);
    // The visible Monday slot may alternate between its native row and a
    // weekend source. Their row versions are independent; an absent entry may
    // also mean a transfer was restored, rather than a stale row snapshot.
    const sameSource = incomingEntry
      && (incomingEntry.sourceDate ?? incomingEntry.date) === (e.sourceDate ?? e.date)
      && (incomingEntry.sourceSlot ?? incomingEntry.slot) === (e.sourceSlot ?? e.slot);
    const samePlacement = incomingEntry && (incomingEntry.transferVersion ?? 0) === (e.transferVersion ?? 0)
      && !!incomingEntry.transferOut === !!e.transferOut;
    const stale = sameSource && incomingEntry && (
      (samePlacement && incomingEntry.version < e.version)
      || ((incomingEntry.transferVersion ?? 0) > 0 && (incomingEntry.transferVersion ?? 0) < (e.transferVersion ?? 0))
    );
    if (protectedDays.has(`${e.driverId}:${e.date}`) || stale) protectedKeys.add(key);
  }
  for (const key of protectedKeys) { const e = previous.get(key); if (e) next.set(key,e); else next.delete(key); }
  return { ...incoming, entries: Array.from(next.values()) };
}
