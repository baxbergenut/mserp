"use client";

import { formatPhone, normalizePhone } from "../lib/phone";

import { useViewState } from "@/app/lib/viewMemory";
import { useQuickCreate } from "@/app/lib/topNavigation";
import { usePermissions } from "@/app/lib/access";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { DriverSetupTasks } from "../drivers/DriverSetupTasks";
import { CustomTasks } from "./CustomTasks";
import { ListChecks, RefreshCw } from "lucide-react";
import { fetchDrivers, fetchRelayIdentityTasks, reviewRelayIdentity } from "../lib/api";
import type { Driver, RelayIdentityTask, PaginatedResponse } from "../lib/types";
import { useDebouncedValue } from "../lib/useDebouncedValue";
import { controlClass, ErrorBanner, ManagementHeader, ManagementSearch, Modal, TablePagination } from "../components/management/ManagementUI";

type Decision = { task: RelayIdentityTask; driver: { id: string; name: string }; action: "link" | "reject" };

export default function TasksPage() {
  const canReadFleet = usePermissions().includes("fleet.read");
  const [data, setData] = useState<PaginatedResponse<RelayIdentityTask> | null>(null);
  const [setupTotal, setSetupTotal] = useState(0);
  const [customTotal, setCustomTotal] = useState(0);
  const [creatingTask, setCreatingTask] = useState(false);
  const [drivers, setDrivers] = useState<Driver[]>([]);
  const [page, setPage] = useViewState("page:page", 1);
  const [pageSize, setPageSize] = useViewState("page:pageSize", 25);
  const [search, setSearch] = useViewState("page:search", "");
  const debouncedSearch = useDebouncedValue(search, 250);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [decision, setDecision] = useState<Decision | null>(null);
  const [saving, setSaving] = useState(false);
  const [revision, setRevision] = useState(0);
  const reload = useCallback(() => setRevision((n) => n + 1), []);
  useQuickCreate(() => setCreatingTask(true));

  useEffect(() => {
    let cancelled = false;
    Promise.resolve().then(() => {
      if (cancelled) return Promise.reject(new Error("cancelled"));
      setLoading(true);
      setError("");
      return Promise.all([fetchRelayIdentityTasks({ page, pageSize, search: debouncedSearch }), canReadFleet ? fetchDrivers() : Promise.resolve([])]);
    })
      .then(([tasks, fleet]) => { if (!cancelled) { setData(tasks); setDrivers(fleet); } })
      .catch((e) => { if (!cancelled) setError(e instanceof Error ? e.message : "Could not load tasks"); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [page, pageSize, debouncedSearch, revision, canReadFleet]);

  async function confirm() {
    if (!decision || saving) return;
    setSaving(true);
    setError("");
    try {
      const result = await reviewRelayIdentity(decision.task.id, decision.driver.id, decision.action);
      setNotice(decision.action === "link"
        ? `Linked to ${decision.driver.name}. ${result.transactionsLinked} existing transactions updated; future purchases on this Relay account will link automatically.`
        : `Suggestion dismissed for this Relay account. Its transactions remain available for review.`);
      setDecision(null);
      reload();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not save review");
    } finally { setSaving(false); }
  }

  return (
    <div className="space-y-5 animate-fade-in">
      <ManagementHeader icon={ListChecks} title="Tasks" count={(data?.total ?? 0) + setupTotal + customTotal}
        description="Manage team tasks, set up new drivers and review accounts that need your attention."
        actionLabel="Add task" onAction={() => setCreatingTask(true)} />
      <div className="flex flex-wrap items-center justify-between gap-3">
        <ManagementSearch value={search} onChange={(value) => { setSearch(value); setPage(1); }}
          placeholder="Search tasks, names, email, phone or ID…" />
        <div className="flex items-center gap-5">
          <button onClick={reload} className="inline-flex items-center gap-2 text-sm text-zinc-400 hover:text-zinc-200"><RefreshCw className="h-4 w-4" />Refresh</button>
          <Link href="/drivers" className="text-sm text-blue-400 hover:underline">Manage drivers</Link>
        </div>
      </div>
      <CustomTasks search={debouncedSearch} revision={revision} creating={creatingTask}
        onCloseCreate={() => setCreatingTask(false)} onCount={setCustomTotal} />
      {canReadFleet && <DriverSetupTasks search={debouncedSearch} revision={revision} onCount={setSetupTotal} />}
      {error && !decision && <ErrorBanner message={error} />}
      {notice && <p role="status" className="rounded-lg border border-emerald-500/20 bg-emerald-500/5 p-3 text-sm text-emerald-300">{notice}</p>}
      {loading ? <p role="status" className="py-12 text-center text-sm text-zinc-400">Loading Relay tasks…</p>
        : !error && data?.items.length === 0 ? <p className="text-sm text-zinc-500">{search ? "No matching Relay accounts." : "No Relay accounts awaiting review."}</p>
        : !error && data?.items.map((task) => (
          <RelayTaskCard key={task.id} task={task} drivers={drivers} disabled={saving}
            onDecision={(driver, action) => setDecision({ task, driver, action })} />
        ))}
      {!loading && !error && data && <TablePagination page={data.page} pageSize={data.pageSize} totalItems={data.total}
        totalPages={data.totalPages} onPageChange={setPage} onPageSizeChange={(size) => { setPageSize(size); setPage(1); }} />}
      {decision && <Modal title={decision.action === "link" ? "Confirm Relay account link" : "Dismiss this suggestion"}
        isSaving={saving} submitLabel={decision.action === "link" ? "Link account" : "Not a match"}
        onClose={() => { setDecision(null); setError(""); }}
        onSubmit={(event) => { event.preventDefault(); void confirm(); }}>
        {error && <ErrorBanner message={error} />}
        <p className="mt-3 text-sm leading-6 text-zinc-300">
          {decision.action === "link"
            ? `Link ${decision.task.name || "unnamed Relay account"} to ${decision.driver.name}? All unassigned purchases for this account and future purchases will use this driver. This can change driver settlements.`
            : `Stop suggesting ${decision.driver.name} for this Relay account? The account stays in the task queue for another match.`}
        </p>
        <p className="mt-3 break-all text-xs text-zinc-500">{decision.task.environment} · Relay ID: {decision.task.relayDriverId}</p>
      </Modal>}
    </div>
  );
}

function RelayTaskCard({ task, drivers, disabled, onDecision }: {
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
      <h3 className="mb-2 mt-5 text-xs font-semibold uppercase tracking-wide text-zinc-500">Suggested drivers</h3>
      {task.suggestions.length === 0 && <p className="text-sm text-zinc-400">No remaining suggestions. Search the fleet below or add the driver on the Drivers page, then refresh.</p>}
      <div className="space-y-2">
        {task.suggestions.map((candidate) => (
          <div key={candidate.driverId} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-zinc-800/60 p-3">
            <div className="min-w-0">
              <p className="text-sm font-medium text-zinc-200">{candidate.name}{!candidate.active && <span className="ml-2 text-xs text-zinc-500">Inactive</span>}</p>
              <p className="break-all text-xs text-zinc-500">{candidate.email || "No email"} · {formatPhone(candidate.phone) || "No phone"}</p>
              <p className="mt-1 text-xs text-amber-300">{candidate.reasons.join(" · ")}</p>
            </div>
            <div className="flex gap-2">
              <button disabled={disabled} onClick={() => onDecision({ id: candidate.driverId, name: candidate.name }, "reject")}
                className="rounded-lg border border-zinc-700 px-3 py-2 text-xs text-zinc-400 hover:bg-zinc-800 disabled:opacity-50">Not a match</button>
              <button disabled={disabled} onClick={() => onDecision({ id: candidate.driverId, name: candidate.name }, "link")}
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
        <button disabled={disabled || !driver} onClick={() => { if (driver) onDecision({ id: driver.id, name: driver.fullName }, "link"); }}
          className="rounded-lg border border-blue-500/40 px-3 py-2 text-sm text-blue-400 hover:bg-blue-500/10 disabled:opacity-40">Review link</button>
      </div>
    </section>
  );
}

