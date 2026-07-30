"use client";

import { useCallback, useEffect, useState } from "react";
import {
  AlertCircle,
  CheckCircle2,
  Receipt,
  X,
} from "lucide-react";
import { fetchTollsPage, syncTolls } from "../lib/api";
import type { Toll } from "../lib/types";
import { useDebouncedValue } from "../lib/useDebouncedValue";
import {
  EmptyState,
  ErrorBanner,
  LoadingTable,
  ManagementHeader,
  ManagementSearch,
  TablePagination,
  TableShell,
} from "../components/management/ManagementUI";

const filterClass =
  "rounded-lg border border-zinc-800 bg-zinc-950 px-2.5 py-1.5 text-[13px] text-zinc-300 outline-none transition-colors focus:border-zinc-600";

function formatMoney(value: number) {
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
  }).format(value);
}

function formatDate(value: string | null) {
  if (!value) return "—";
  return new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "numeric",
    year: "numeric",
  }).format(new Date(`${value}T00:00:00`));
}

function routeLabel(toll: Toll) {
  if (!toll.entryPlaza) return toll.exitPlaza;
  return `${toll.entryPlaza} → ${toll.exitPlaza}`;
}

export default function TollsPage() {
  const [tolls, setTolls] = useState<Toll[]>([]);
  const [search, setSearch] = useState("");
  const [unit, setUnit] = useState("");
  const [agency, setAgency] = useState("");
  const [postFrom, setPostFrom] = useState("");
  const [postTo, setPostTo] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(25);
  const [total, setTotal] = useState(0);
  const [totalPages, setTotalPages] = useState(1);
  const [units, setUnits] = useState<string[]>([]);
  const [agencies, setAgencies] = useState<string[]>([]);
  const [displayedTotal, setDisplayedTotal] = useState(0);
  const [displayedTrucks, setDisplayedTrucks] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [isSyncing, setIsSyncing] = useState(false);
  const [message, setMessage] = useState<{
    type: "success" | "error";
    text: string;
  } | null>(null);
  const debouncedSearch = useDebouncedValue(search);

  const loadData = useCallback(async () => {
    setIsLoading(true);
    try {
      const response = await fetchTollsPage({
        page, pageSize, search: debouncedSearch, unit, agency, postFrom, postTo,
      });
      setTolls(response.items);
      setPage(response.page);
      setTotal(response.total);
      setTotalPages(response.totalPages);
      setUnits(response.options.units);
      setAgencies(response.options.agencies);
      setDisplayedTotal(response.summary.amount);
      setDisplayedTrucks(response.summary.truckCount);
      setLoadError("");
    } catch (reason) {
      setLoadError(reason instanceof Error ? reason.message : "Failed to load tolls");
    } finally {
      setIsLoading(false);
    }
  }, [agency, debouncedSearch, page, pageSize, postFrom, postTo, unit]);

  useEffect(() => {
    const timeout = window.setTimeout(() => void loadData(), 0);
    return () => window.clearTimeout(timeout);
  }, [loadData]);
  const hasFilters = Boolean(search || unit || agency || postFrom || postTo);

  const handleSync = async () => {
    if (isSyncing) return;
    setIsSyncing(true);
    setMessage(null);
    try {
      const result = await syncTolls();
      await loadData();
      setMessage({
        type: "success",
        text: `Checked ${result.daysFetched} missing/current day${result.daysFetched === 1 ? "" : "s"} and synced ${result.saved} toll transaction${result.saved === 1 ? "" : "s"}. ${result.daysSkipped} completed day${result.daysSkipped === 1 ? " was" : "s were"} already up to date.${result.unmatched > 0 ? ` ${result.unmatched} transaction${result.unmatched === 1 ? "" : "s"} could not yet be matched to a truck.` : ""}`,
      });
    } catch (reason) {
      setMessage({
        type: "error",
        text: reason instanceof Error ? reason.message : "PrePass toll sync failed.",
      });
    } finally {
      setIsSyncing(false);
    }
  };

  const clearFilters = () => {
    setSearch("");
    setUnit("");
    setAgency("");
    setPostFrom("");
    setPostTo("");
    setPage(1);
  };

  return (
    <div className="space-y-5 animate-fade-in">
      <ManagementHeader
        icon={Receipt}
        title="Tolls"
        description="Review truck toll charges fetched automatically from PrePass each day."
        count={total}
        actionLabel={isSyncing ? "Syncing…" : "Sync tolls"}
        onAction={() => void handleSync()}
      />

      {loadError && <ErrorBanner message={loadError} />}
      {message && (
        <div
          className={`flex items-start gap-2 rounded-lg border px-3 py-2.5 text-[13px] ${
            message.type === "success"
              ? "border-emerald-500/20 bg-emerald-500/5 text-emerald-300"
              : "border-red-500/20 bg-red-500/5 text-red-300"
          }`}
          role="status"
        >
          {message.type === "success" ? (
            <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" />
          ) : (
            <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
          )}
          <div className="min-w-0 flex-1">{message.text}</div>
          <button
            type="button"
            onClick={() => setMessage(null)}
            className="rounded p-0.5 opacity-60 transition hover:bg-white/5 hover:opacity-100"
            aria-label="Dismiss sync result"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      )}

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <div className="rounded-xl border border-zinc-800/60 bg-card px-4 py-3">
          <div className="text-[11px] font-medium uppercase tracking-wider text-zinc-600">Displayed tolls</div>
          <div className="mt-1 font-mono text-lg font-semibold tabular-nums text-zinc-200">{formatMoney(displayedTotal)}</div>
        </div>
        <div className="rounded-xl border border-zinc-800/60 bg-card px-4 py-3">
          <div className="text-[11px] font-medium uppercase tracking-wider text-zinc-600">Transactions</div>
          <div className="mt-1 font-mono text-lg font-semibold tabular-nums text-zinc-200">{total.toLocaleString()}</div>
        </div>
        <div className="rounded-xl border border-zinc-800/60 bg-card px-4 py-3">
          <div className="text-[11px] font-medium uppercase tracking-wider text-zinc-600">Trucks</div>
          <div className="mt-1 font-mono text-lg font-semibold tabular-nums text-zinc-200">{displayedTrucks.toLocaleString()}</div>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <ManagementSearch value={search} onChange={(value) => { setSearch(value); setPage(1); }} placeholder="Search unit, plaza, agency, tag…" />
        <select value={unit} onChange={(event) => { setUnit(event.target.value); setPage(1); }} className={filterClass}>
          <option value="">Truck</option>
          {units.map((value) => <option key={value} value={value}>{value}</option>)}
        </select>
        <select value={agency} onChange={(event) => { setAgency(event.target.value); setPage(1); }} className={filterClass}>
          <option value="">Agency</option>
          {agencies.map((value) => <option key={value} value={value}>{value}</option>)}
        </select>
        <div className="flex items-center gap-1.5">
          <input aria-label="Post date from" type="date" value={postFrom} onChange={(event) => { setPostFrom(event.target.value); setPage(1); }} className={filterClass} />
          <span className="text-[13px] text-zinc-700">–</span>
          <input aria-label="Post date to" type="date" value={postTo} onChange={(event) => { setPostTo(event.target.value); setPage(1); }} className={filterClass} />
        </div>
        {hasFilters && (
          <button type="button" onClick={clearFilters} className="inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-[13px] text-zinc-500 transition hover:bg-zinc-800/50 hover:text-zinc-300">
            <X className="h-3 w-3" /> Clear
          </button>
        )}
      </div>

      <TableShell>
        {isLoading ? (
          <LoadingTable columns={6} />
        ) : tolls.length === 0 ? (
          <EmptyState message={hasFilters ? "No tolls match these filters." : "No PrePass tolls have synced yet."} />
        ) : (
          <table className="w-full min-w-[980px] text-left text-[13px]">
            <thead>
              <tr className="border-b border-zinc-800/50 text-zinc-500">
                <th className="px-4 py-3 font-medium">Post date</th>
                <th className="px-4 py-3 font-medium">Truck</th>
                <th className="px-4 py-3 font-medium">Plaza / route</th>
                <th className="px-4 py-3 font-medium">Agency</th>
                <th className="px-4 py-3 font-medium">Class / miles</th>
                <th className="px-4 py-3 text-right font-medium">Amount</th>
              </tr>
            </thead>
            <tbody>
              {tolls.map((toll) => (
                <tr key={toll.id} className="border-b border-zinc-900/70 text-zinc-300 transition last:border-0 hover:bg-zinc-800/15">
                  <td className="px-4 py-3">
                    <div className="font-mono tabular-nums text-zinc-200">{formatDate(toll.postingDate)}</div>
                    <div className="mt-0.5 text-[11px] text-zinc-600">Invoice {formatDate(toll.invoiceDate)}</div>
                  </td>
                  <td className="px-4 py-3">
                    <div className="font-mono font-medium text-zinc-200">{toll.truckUnit}</div>
                    <div className="mt-0.5 text-[11px] text-zinc-600">{toll.readType}</div>
                  </td>
                  <td className="px-4 py-3">
                    <div className="text-zinc-300">{routeLabel(toll)}</div>
                    <div className="mt-0.5 font-mono text-[11px] tabular-nums text-zinc-600">{formatDate(toll.exitDate)} {toll.exitTime}</div>
                  </td>
                  <td className="px-4 py-3">
                    <div className="text-zinc-300">{toll.agency}</div>
                    <div className="mt-0.5 text-[11px] text-zinc-600">{toll.source}</div>
                  </td>
                  <td className="px-4 py-3 text-zinc-400">
                    Class {toll.tollClass}{toll.miles !== null ? ` · ${toll.miles.toLocaleString()} mi` : ""}
                  </td>
                  <td className={`px-4 py-3 text-right font-mono font-medium tabular-nums ${toll.amount < 0 ? "text-emerald-400" : "text-zinc-200"}`}>
                    {formatMoney(toll.amount)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </TableShell>
      {!isLoading && (
        <TablePagination
          page={page}
          pageSize={pageSize}
          totalItems={total}
          totalPages={totalPages}
          onPageChange={setPage}
          onPageSizeChange={(value) => { setPageSize(value); setPage(1); }}
        />
      )}

    </div>
  );
}
