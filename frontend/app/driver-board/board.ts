import type { BoardLoads, DriverBoardEntry } from "@/app/lib/types";

export function progressStatus(status: string, loads?: BoardLoads) {
  return loads && (status === "ENROUTE" || status === "RESERVED") ? (loads.next.length ? "RESERVED" : "ENROUTE") : status;
}

// Preview a draft immediately; autosave resolves the same match transactionally.
export function previewCurrentLoad(loads: BoardLoads | undefined, entry: DriverBoardEntry, saved?: DriverBoardEntry): BoardLoads | undefined {
  if (!loads) return loads;
  const number = entry.currentLoad.trim().toLowerCase();
  const aligned = !loads.current || loads.current.number.trim().toLowerCase() === number;
  if (aligned && !entry.resolveCurrentLoad && entry.currentLoad === saved?.currentLoad) {
    if (!entry.statusEdited || entry.status === saved?.status) return loads;
    const kind = entry.status === "DISPATCHED" ? "pickup" : ["ENROUTE", "RESERVED"].includes(entry.status) ? "delivery" : "";
    if (!kind) return loads;
    const stop = loads.current?.stops.find(s => s.type.toLowerCase() === kind && s.location.trim());
    if (stop?.key === loads.stopKey) return loads;
    return { ...loads, automation: undefined, destinationSource: !!stop, sourceDestination: stop?.location ?? "", stopKey: stop?.key ?? "" };
  }
  const matches = loads.week.filter(p => p.number.trim().toLowerCase() === number);
  const chosen = matches[0];
  const unique = chosen && (matches.length === 1 || (chosen.loadId !== null && matches.every(p => p.loadId === chosen.loadId)));
  const current = unique ? chosen : null;
  const stop = current?.stops.find(s => s.type.toLowerCase() === "pickup" && s.location.trim());
  return { ...loads, automation: undefined, resolutionWarning: undefined, current, destinationSource: !!stop && !entry.destination, sourceDestination: stop?.location ?? "", stopKey: stop?.key ?? "" };
}

// Retain legacy history, while the live board shows a single time or mileage.
export function parseETA(value: string): { time: string; miles: string } | null {
  if (!value) return { time: "", miles: "" };
  const legacy = /^(\d{4}-\d{2}-\d{2})(?:T([01]\d|2[0-3]):([0-5]\d))?$/.exec(value);
  if (legacy) {
    const date = new Date(legacy[1] + "T12:00:00Z");
    if (!Number.isFinite(date.getTime()) || date.toISOString().slice(0, 10) !== legacy[1]) return null;
    return { time: legacy[2] ? legacy[2] + ":" + legacy[3] : "", miles: "" };
  }
  const match = /^(?:([01]\d|2[0-3]):([0-5]\d))?(?: ?·? ?(\d+(?:\.\d{1,2})?) mi)?$/.exec(value);
  return match && (match[1] || match[3]) ? { time: match[1] ? match[1] + ":" + match[2] : "", miles: match[3] ?? "" } : null;
}

export function formatETA(value: string, full = false) {
  const eta = parseETA(value);
  if (!eta) return value;
  if (full && /^\d{4}-\d{2}-\d{2}/.test(value)) return value.replace("T", " · ");
  // Older combined values did not record entry order. Use their last stored
  // component (miles) until edited, and preserve exact values in history.
  if (full && eta.time && eta.miles) return value;
  return (eta.miles ? eta.miles + " mi" : eta.time) || "—";
}

export const statuses = ["ENROUTE", "DISPATCHED", "RESERVED", "HOME", "VACATION", "SHOP", "RESET", "NO LOAD", "STUCK", "LATE DEL", "TRUCK ISSUE", "LEFT", "NEW DRIVER", "DEADHEAD", "LOAD CANCELLED", "REJECTED"];
export function statusColor(status: string) {
  // Exact TODAY sheet fills (Driver Board, gid 1851742271, column J).
  // Use dark labels for readable contrast on the sheet's bright backgrounds.
  switch (status) {
    case "ENROUTE": return "bg-[#3d85c6] text-black";
    case "DISPATCHED": return "bg-[#fbbc04] text-black";
    case "RESERVED": return "bg-[#b7e1cd] text-black";
    case "HOME": return "bg-[#ff0000] text-black";
    case "VACATION": return "bg-[#e06666] text-black";
    case "SHOP": return "bg-[#ff9900] text-black";
    case "LEFT": return "bg-white text-black";
  }
  // Retain the existing palette for statuses without an equivalent sheet rule.
  if (["ENROUTE", "DEADHEAD"].includes(status)) return "bg-blue-500/15 text-blue-300";
  if (["DISPATCHED", "SHOP", "LATE DEL"].includes(status)) return "bg-amber-500/15 text-amber-300";
  if (["RESERVED", "NEW DRIVER"].includes(status)) return "bg-emerald-500/15 text-emerald-300";
  if (["HOME", "VACATION", "RESET"].includes(status)) return "bg-violet-500/15 text-violet-300";
  if (["STUCK", "TRUCK ISSUE", "REJECTED", "LOAD CANCELLED"].includes(status)) return "bg-red-500/15 text-red-300";
  return "text-zinc-400";
}

// An in-flight save may finish after more typing. Keep the new text, while
// advancing both concurrency tokens to the committed response.
export function reconcileDriverBoard(current: Record<string, DriverBoardEntry>, snapshot: Record<string, DriverBoardEntry>, committed: DriverBoardEntry[]) {
  const remaining = { ...current };
  for (const entry of committed) {
    const draft = current[entry.driverId];
    if (!draft || JSON.stringify(draft) === JSON.stringify(snapshot[entry.driverId])) delete remaining[entry.driverId];
    else remaining[entry.driverId] = { ...draft, version: entry.version };
  }
  return remaining;
}

export function isOldLocation(reportedAt: string, now: number): boolean {
  return now - Date.parse(reportedAt) >= 3 * 60 * 60 * 1000;
}
