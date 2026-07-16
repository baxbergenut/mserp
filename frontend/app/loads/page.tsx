"use client";

import { useEffect, useMemo, useState } from "react";
import { Package } from "lucide-react";
import { fetchLoads } from "../lib/api";
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

  useEffect(() => {
    fetchLoads()
      .then(setLoads)
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

  const handleClearAll = () => {
    setSearch("");
    setFilters(EMPTY_FILTERS);
  };

  const isFiltered = search || Object.values(filters).some(Boolean);

  return (
    <div className="space-y-5 animate-fade-in">
      {/* Header */}
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
