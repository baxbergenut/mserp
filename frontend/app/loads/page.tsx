"use client";

import { useEffect, useMemo, useState } from "react";
import { AlertCircle, CheckCircle2, Package, RefreshCw } from "lucide-react";
import { fetchLoads, syncLoads } from "../lib/api";
import type { Load, SortKey, SortDir } from "../lib/types";
import {
  statusKey,
  filterLoads,
  sortLoads,
  type Filters,
  EMPTY_FILTERS,
} from "../lib/utils";
import { SearchBar } from "../components/SearchBar";
import { FilterBar } from "../components/FilterBar";
import { LoadsTable } from "./LoadsTable";

export default function LoadsPage() {
  const [loads, setLoads] = useState<Load[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [search, setSearch] = useState("");
  const [filters, setFilters] = useState<Filters>(EMPTY_FILTERS);
  const [sortKey, setSortKey] = useState<SortKey>("PickupTime");
  const [sortDir, setSortDir] = useState<SortDir>("desc");
  const [isSyncing, setIsSyncing] = useState(false);
  const [message, setMessage] = useState<{ type: "success" | "error"; text: string } | null>(null);

  useEffect(() => {
    fetchLoads()
      .then(setLoads)
      .catch((reason: unknown) =>
        setMessage({
          type: "error",
          text: reason instanceof Error ? reason.message : "Failed to load loads.",
        }),
      )
      .finally(() => setIsLoading(false));
  }, []);

  // Derive unique filter options from loaded data
  const statusOptions = useMemo(
    () => [...new Set(loads.map((l) => statusKey(l.Status)))].sort(),
    [loads],
  );
  const customerOptions = useMemo(
    () =>
      [...new Set(loads.map((l) => l.CustomerName).filter(Boolean))].sort(),
    [loads],
  );
  const driverOptions = useMemo(
    () => [...new Set(loads.map((l) => l.DriverName).filter(Boolean))].sort(),
    [loads],
  );
  const dispatcherOptions = useMemo(
    () =>
      [...new Set(loads.map((l) => l.DispatcherName).filter(Boolean))].sort(),
    [loads],
  );

  // Apply search → filter → sort pipeline
  const displayed = useMemo(() => {
    const filtered = filterLoads(loads, search, filters);
    return sortLoads(filtered, sortKey, sortDir);
  }, [loads, search, filters, sortKey, sortDir]);

  const handleSort = (key: SortKey) => {
    if (key === sortKey) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      setSortDir("desc");
    }
  };

  const isFiltered = search || Object.values(filters).some(Boolean);

  const handleSync = async () => {
    setIsSyncing(true);
    setMessage(null);
    try {
      const result = await syncLoads();
      const refreshedLoads = await fetchLoads();
      setLoads(refreshedLoads);
      setMessage({
        type: "success",
        text: `Synced ${result.saved} load${result.saved === 1 ? "" : "s"} from DataTruck for the last week.`,
      });
    } catch (reason) {
      setMessage({
        type: "error",
        text: reason instanceof Error ? reason.message : "DataTruck sync failed.",
      });
    } finally {
      setIsSyncing(false);
    }
  };

  return (
    <div className="space-y-5 animate-fade-in">
      {/* Header */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-3">
          <Package className="h-5 w-5 text-zinc-500" />
          <h1 className="text-lg font-semibold text-zinc-100">Loads</h1>
          {!isLoading && (
            <span className="rounded-full bg-zinc-800/60 px-2.5 py-0.5 text-[12px] font-medium text-zinc-400">
              {isFiltered
                ? `${displayed.length} of ${loads.length}`
                : loads.length}
            </span>
          )}
        </div>
        <button
          type="button"
          onClick={() => void handleSync()}
          disabled={isSyncing}
          className="inline-flex items-center justify-center gap-2 rounded-lg border border-zinc-700/80 bg-zinc-900/60 px-3.5 py-2 text-[13px] font-medium text-zinc-300 transition hover:border-zinc-600 hover:bg-zinc-800/70 hover:text-zinc-100 disabled:cursor-wait disabled:opacity-60"
        >
          <RefreshCw className={`h-4 w-4 ${isSyncing ? "animate-spin" : ""}`} />
          {isSyncing ? "Syncing one week…" : "Sync last week"}
        </button>
      </div>

      {message && (
        <div
          className={`flex items-center gap-2 rounded-lg border px-3 py-2.5 text-[13px] ${
            message.type === "success"
              ? "border-emerald-500/20 bg-emerald-500/5 text-emerald-300"
              : "border-red-500/20 bg-red-500/5 text-red-300"
          }`}
          role={message.type === "error" ? "alert" : "status"}
        >
          {message.type === "success" ? (
            <CheckCircle2 className="h-4 w-4 shrink-0" />
          ) : (
            <AlertCircle className="h-4 w-4 shrink-0" />
          )}
          {message.text}
        </div>
      )}

      {/* Search + Filters */}
      <div className="flex flex-wrap items-start gap-3">
        <SearchBar
          value={search}
          onChange={setSearch}
          placeholder="Search by load, customer, driver, truck…"
        />
        <FilterBar
          filters={filters}
          onChange={setFilters}
          statusOptions={statusOptions}
          customerOptions={customerOptions}
          dispatcherOptions={dispatcherOptions}
          driverOptions={driverOptions}
        />
      </div>

      {/* Table */}
      <LoadsTable
        loads={displayed}
        isLoading={isLoading}
        sortKey={sortKey}
        sortDir={sortDir}
        onSort={handleSort}
      />
    </div>
  );
}
