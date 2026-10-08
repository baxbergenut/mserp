"use client";

import { useViewState } from "@/app/lib/viewMemory";

import { useEffect, useState } from "react";
import Link from "next/link";
import { History } from "lucide-react";
import { fetchDriverAssignments, fetchTruckAssignments } from "@/app/lib/api";
import type { AssignmentHistoryEntry } from "@/app/lib/types";
import { EmptyState, ErrorBanner, LoadingTable, TableShell } from "@/app/components/management/ManagementUI";

function dateTime(value: string) {
  return new Date(value).toLocaleString("en-US", {
    month: "short", day: "numeric", year: "numeric", hour: "numeric", minute: "2-digit",
  });
}

export function AssignmentHistory({ driverId, truckId }: { driverId: string; truckId?: never } | { truckId: string; driverId?: never }) {
  const [entries, setEntries] = useState<AssignmentHistoryEntry[] | null>(null);
  const [error, setError] = useState("");
  const [filter, setFilter] = useViewState("AssignmentHistory:filter", "truck");
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let cancelled = false;
    (truckId ? fetchTruckAssignments(truckId) : fetchDriverAssignments(driverId!)).then((data) => {
      if (!cancelled) { setEntries(data); setError(""); }
    }).catch((reason) => {
      if (!cancelled) setError(reason instanceof Error ? reason.message : "Failed to load assignment history");
    });
    return () => { cancelled = true; };
  }, [driverId, truckId, attempt]);

  const visible = entries?.filter((entry) => !!truckId || entry.kind === (filter === "dispatcher" ? "dispatcher" : "truck"));
  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <History className="h-4 w-4 text-zinc-500" />
          <h2 className="text-sm font-semibold text-zinc-100">Assignment history</h2>
        </div>
        {!truckId && <select aria-label="Assignment type" value={filter === "dispatcher" ? "dispatcher" : "truck"} onChange={(event) => setFilter(event.target.value)} className="rounded-lg border border-zinc-800 bg-zinc-900 px-3 py-2 text-xs text-zinc-300">
          <option value="truck">Trucks</option>
          <option value="dispatcher">Dispatchers</option>
        </select>}
      </div>
      {error ? <div className="space-y-2"><ErrorBanner message={error} /><button className="text-xs text-blue-400 hover:text-blue-300" onClick={() => setAttempt((value) => value + 1)}>Retry history</button></div> : (
        <TableShell>
          {!visible ? <LoadingTable columns={4} /> : visible.length === 0 ? <EmptyState message="No assignment history is recorded for this selection." /> : (
            <table className="w-full min-w-[720px] text-left text-xs">
              <thead><tr className="border-b border-zinc-800/50 text-zinc-500">
                {[truckId ? "Driver" : filter === "dispatcher" ? "Dispatcher" : "Truck", "Started", "Ended", "Source"].map((title) => <th key={title} className="px-4 py-3 font-medium">{title}</th>)}
              </tr></thead>
              <tbody>{visible.map((entry) => (
                <tr key={`${entry.kind}-${entry.id}`} className="border-b border-zinc-900/70 text-zinc-300 last:border-0">
                  <td className="px-4 py-3">{entry.relatedId ? <Link href={`/${entry.kind === "driver" ? "drivers" : entry.kind === "dispatcher" ? "dispatchers" : "trucks"}/detail?id=${entry.relatedId}`} className="text-blue-400 hover:text-blue-300">{entry.name}</Link> : entry.name}</td>
                  <td className="px-4 py-3">{entry.startKnown ? dateTime(entry.assignedAt) : <span title={`Recorded ${dateTime(entry.assignedAt)}`}>Unknown</span>}</td>
                  <td className="px-4 py-3">{entry.unassignedAt ? dateTime(entry.unassignedAt) : <span className="text-emerald-400">Current</span>}</td>
                  <td className="max-w-80 px-4 py-3 text-zinc-500">{entry.source || "ERP assignment"}</td>
                </tr>
              ))}</tbody>
            </table>
          )}
        </TableShell>
      )}
    </section>
  );
}
