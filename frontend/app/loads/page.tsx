"use client";
import { usePermissions } from "../lib/access";


import { TableFilters } from "@/app/components/management/TableFilters";
import { PageHeader } from "@/app/components/PageHeader";

import { useViewState } from "@/app/lib/viewMemory";

import { useCallback, useEffect, useState } from "react";
import { AlertCircle, CheckCircle2, Package, RefreshCw } from "lucide-react";
import { fetchLoadsPage, syncLoads } from "../lib/api";
import type { Load, SortDir, SortKey } from "../lib/types";
import { useDebouncedValue } from "../lib/useDebouncedValue";
import { type Filters, EMPTY_FILTERS } from "../lib/utils";
import { SearchBar } from "../components/SearchBar";
import { statusLabel } from "../lib/utils";
import { controlClass, TablePagination } from "../components/management/ManagementUI";
import { LoadsTable } from "./LoadsTable";

export default function LoadsPage() {
  const canSync=usePermissions().includes("loads.sync");
  const [loads, setLoads] = useState<Load[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [search, setSearch] = useViewState("page:search", "");
  const [filters, setFilters] = useViewState<Filters>("page:filters", EMPTY_FILTERS);
  const [sortKey, setSortKey] = useViewState<SortKey>("page:sortKey", "PickupTime");
  const [sortDir, setSortDir] = useViewState<SortDir>("page:sortDir", "desc");
  const [page, setPage] = useViewState("page:page", 1);
  const [pageSize, setPageSize] = useViewState("page:pageSize", 25);
  const [total, setTotal] = useState(0);
  const [totalPages, setTotalPages] = useState(1);
  const [statusOptions, setStatusOptions] = useState<string[]>([]);
  const [customerOptions, setCustomerOptions] = useState<string[]>([]);
  const [driverOptions, setDriverOptions] = useState<string[]>([]);
  const [dispatcherOptions, setDispatcherOptions] = useState<string[]>([]);
  const [isSyncing, setIsSyncing] = useState(false);
  const [message, setMessage] = useState<{
    type: "success" | "error";
    text: string;
  } | null>(null);
  const debouncedSearch = useDebouncedValue(search);

  const loadData = useCallback(async () => {
    setIsLoading(true);
    try {
      const result = await fetchLoadsPage({
        page,
        pageSize,
        search: debouncedSearch,
        ...filters,
        sort: sortKey,
        direction: sortDir,
      });
      setLoads(result.items);
      setPage(result.page);
      setTotal(result.total);
      setTotalPages(result.totalPages);
      setStatusOptions(result.options.statuses);
      setCustomerOptions(result.options.customers);
      setDriverOptions(result.options.drivers);
      setDispatcherOptions(result.options.dispatchers);
    } catch (reason) {
      setMessage({
        type: "error",
        text: reason instanceof Error ? reason.message : "Failed to load loads.",
      });
    } finally {
      setIsLoading(false);
    }
  }, [setPage, debouncedSearch, filters, page, pageSize, sortDir, sortKey]);

  useEffect(() => {
    const timeout = window.setTimeout(() => void loadData(), 0);
    return () => window.clearTimeout(timeout);
  }, [loadData]);

  const handleSort = (key: SortKey) => {
    setPage(1);
    if (key === sortKey) {
      setSortDir((direction) => (direction === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      setSortDir("desc");
    }
  };

  const isFiltered = Boolean(search || Object.values(filters).some(Boolean));

  const handleSync = async () => {
    setIsSyncing(true);
    setMessage(null);
    try {
      const result = await syncLoads();
      await loadData();
      setMessage({
        type: "success",
        text: `Synced ${result.saved} DataTruck load${result.saved === 1 ? "" : "s"}, including new records and recent changes.`,
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
      <PageHeader><div className="flex items-center gap-3">
          <Package className="h-5 w-5 text-zinc-500" />
          <h1 className="text-lg font-semibold text-zinc-100">Loads</h1>
          {!isLoading && (
            <span className="rounded-full bg-zinc-800/60 px-2.5 py-0.5 text-[12px] font-medium text-zinc-400">
              {isFiltered ? `${total.toLocaleString()} matching` : total.toLocaleString()}
            </span>
          )}
        </div></PageHeader>

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

      <div className="flex flex-wrap items-start gap-3">
        <SearchBar
          value={search}
          onChange={(value) => { setSearch(value); setPage(1); }}
          placeholder="Search by load, customer, driver, truck…"
        />
        <TableFilters>
          <select aria-label="Customer filter" className={controlClass} value={filters.customer} onChange={e => { setFilters({ ...filters, customer: e.target.value }); setPage(1); }}><option value="">All customers</option>{customerOptions.map(v => <option key={v}>{v}</option>)}</select>
          <select aria-label="Driver filter" className={controlClass} value={filters.driver} onChange={e => { setFilters({ ...filters, driver: e.target.value }); setPage(1); }}><option value="">All drivers</option>{driverOptions.map(v => <option key={v}>{v}</option>)}</select><select aria-label="Dispatcher filter" className={controlClass} value={filters.dispatcher} onChange={e => { setFilters({ ...filters, dispatcher: e.target.value }); setPage(1); }}><option value="">All dispatchers</option>{dispatcherOptions.map(v => <option key={v}>{v}</option>)}</select>
          <select aria-label="Status filter" className={controlClass} value={filters.status} onChange={e => { setFilters({ ...filters, status: e.target.value }); setPage(1); }}><option value="">All statuses</option>{statusOptions.map(v => <option value={v} key={v}>{statusLabel(v)}</option>)}</select>
          <label className="block space-y-1">Pickup from<input aria-label="Pickup from" type="date" className={controlClass} value={filters.pickupFrom} onChange={e => { setFilters({ ...filters, pickupFrom: e.target.value }); setPage(1); }} /></label><label className="block space-y-1">To<input aria-label="Pickup to" type="date" className={controlClass} value={filters.pickupTo} onChange={e => { setFilters({ ...filters, pickupTo: e.target.value }); setPage(1); }} /></label>
        </TableFilters>

      <div className="ml-auto flex flex-wrap items-center gap-2"><button
          type="button"
          hidden={!canSync}
          onClick={() => void handleSync()}
          disabled={isSyncing}
          className="inline-flex items-center justify-center gap-2 rounded-lg border border-zinc-700/80 bg-zinc-900/60 px-3.5 py-2 text-[13px] font-medium text-zinc-300 transition hover:border-zinc-600 hover:bg-zinc-800/70 hover:text-zinc-100 disabled:cursor-wait disabled:opacity-60"
        >
          <RefreshCw className={`h-4 w-4 ${isSyncing ? "animate-spin" : ""}`} />
          {isSyncing ? "Syncing loads…" : "Sync DataTruck"}
        </button></div></div>

      <LoadsTable
        loads={loads}
        isLoading={isLoading}
        sortKey={sortKey}
        sortDir={sortDir}
        onSort={handleSort}

      />
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
