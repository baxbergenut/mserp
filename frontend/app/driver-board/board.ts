import type { DriverBoardEntry } from "@/app/lib/types";

export const statuses = ["ENROUTE", "DISPATCHED", "RESERVED", "HOME", "VACATION", "SHOP", "RESET", "NO LOAD", "STUCK", "LATE DEL", "TRUCK ISSUE", "LEFT", "NEW DRIVER", "DEADHEAD", "LOAD CANCELLED", "REJECTED"];
export function statusColor(status: string) {
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
    else remaining[entry.driverId] = { ...draft, version: entry.version, homeVersion: entry.homeVersion };
  }
  return remaining;
}
