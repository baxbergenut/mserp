"use client";
import { useState } from "react";
import { formatPhone, normalizePhone } from "../lib/phone";
import type { Driver, RelayIdentityTask } from "../lib/types";
import { controlClass } from "../components/management/ManagementUI";

export function RelayTaskCard({ task, drivers, disabled, onDecision }: {
  task: RelayIdentityTask; drivers: Driver[]; disabled: boolean;
  onDecision: (driver: { id: string; name: string }, action: "link" | "reject") => void;
}) {
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState("");
  const search = normalizePhone(query) || query.trim().toLowerCase();
  const filtered = drivers.filter((d) => [d.fullName, d.email, d.phone, d.truckUnit].join(" ").toLowerCase().includes(search));
  const driver = drivers.find((d) => d.id === selected);
  return (
    <section className="rounded-xl border border-zinc-800 bg-card p-4 sm:p-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="text-base font-semibold text-zinc-100">{task.name || "Unnamed Relay account"}</h2>
          <p className="mt-1 break-all text-sm text-zinc-400">{task.email || "No email"} · {formatPhone(task.phone) || "No phone"}</p>
          <p className="mt-1 break-all text-xs text-zinc-500">{task.environment} · Relay ID: {task.relayDriverId}</p>
          {task.integrationId && <p className="mt-1 break-all text-xs text-zinc-500">Source card / integration ID: {task.integrationId}</p>}
        </div>
        <div className="text-sm text-zinc-400">
          <p>{task.transactionCount} purchases</p>
          {task.latestTransaction && <p className="mt-1 text-xs">Latest: {new Date(task.latestTransaction).toLocaleDateString()}</p>}
        </div>
      </div>
      {task.suggestions.length > 0 && <h3 className="mb-2 mt-5 text-xs font-semibold uppercase tracking-wide text-zinc-500">Suggested drivers</h3>}
      <div className="space-y-2">
        {task.suggestions.map((candidate) => (
          <div key={candidate.driverId} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-zinc-800/60 p-3">
            <div className="min-w-0">
              <p className="text-sm font-medium text-zinc-200">{candidate.name}{!candidate.active && <span className="ml-2 text-xs text-zinc-500">Inactive</span>}</p>
              <p className="break-all text-xs text-zinc-500">{candidate.email || "No email"} · {formatPhone(candidate.phone) || "No phone"}</p>
              <p className="mt-1 text-xs text-amber-300">{candidate.reasons.join(" · ")}</p>
            </div>
            <div className="flex gap-2">
              <button type="button" disabled={disabled} onClick={() => onDecision({ id: candidate.driverId, name: candidate.name }, "reject")}
                className="rounded-lg border border-zinc-700 px-3 py-2 text-xs text-zinc-400 hover:bg-zinc-800 disabled:opacity-50">Not a match</button>
              <button type="button" disabled={disabled} onClick={() => onDecision({ id: candidate.driverId, name: candidate.name }, "link")}
                className="rounded-lg bg-blue-600 px-3 py-2 text-xs text-white hover:bg-blue-500 disabled:opacity-50">Review link</button>
            </div>
          </div>
        ))}
      </div>
      <div className="mt-4 grid gap-2 sm:grid-cols-[1fr_1fr_auto]">
        <input aria-label={`Search drivers for ${task.name}`} className={controlClass} value={query}
          onChange={(e) => { setQuery(e.target.value); setSelected(""); }} placeholder="Search other drivers…" />
        <select aria-label={`Choose driver for ${task.name}`} className={controlClass} value={selected} onChange={(e) => setSelected(e.target.value)}>
          <option value="">Select a driver</option>
          {filtered.map((d) => <option key={d.id} value={d.id}>{d.fullName}{!d.active ? " (inactive)" : ""}{task.rejectedDriverIds.includes(d.id) ? " (previously dismissed)" : ""}</option>)}
        </select>
        <button type="button" disabled={disabled || !driver} onClick={() => { if (driver) onDecision({ id: driver.id, name: driver.fullName }, "link"); }}
          className="rounded-lg border border-blue-500/40 px-3 py-2 text-sm text-blue-400 hover:bg-blue-500/10 disabled:opacity-40">Review link</button>
      </div>
    </section>
  );
}
