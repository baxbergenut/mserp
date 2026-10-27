"use client";
import { usePermissions } from "../lib/access";


import { TableFilters } from "@/app/components/management/TableFilters";
import { useViewState } from "@/app/lib/viewMemory";

import { useCallback, useEffect, useState } from "react";
import {
  AlertCircle,
  CheckCircle2,
  Receipt,
  LayoutDashboard,
  List,
  RefreshCw,
  X,
} from "lucide-react";
import { PrePassFleetTab } from "./PrePassFleetTab";
import { TollOverview } from "./TollOverview";
import { fetchTollsPage, syncTolls } from "../lib/api";
import type { Toll } from "../lib/types";
import { TransactionFlagIcon } from "../components/TransactionFlagIcon";
import { useDebouncedValue } from "../lib/useDebouncedValue";
import {
  EmptyState,
  ErrorBanner,
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
  const entry = toll.entryPlazaName || toll.entryPlaza;
  const exit = toll.exitPlazaName || toll.exitPlaza;
  if (!entry) return exit;
  return `${entry} → ${exit}`;
}

export default function TollsPage() {
  const canSync=usePermissions().includes("tolls.sync");
  const [activeTab, setActiveTab] = useViewState<"overview" | "transactions" | "prepass" | "trucks" | "transponders" | "attention">("page:activeTab", "overview");
  const fleetView = activeTab === "prepass" ? "trucks" : activeTab;
  const [dashboardRefreshKey, setDashboardRefreshKey] = useState(0);
  const [tolls, setTolls] = useState<Toll[]>([]);
  const [search, setSearch] = useViewState("page:search", "");
  const [unit, setUnit] = useViewState("page:unit", "");
  const [agency, setAgency] = useViewState("page:agency", "");
  const [unlinked, setUnlinked] = useViewState("page:unlinked", false);
  const [productionOnly, setProductionOnly] = useViewState("page:productionOnly", false);
  const [postFrom, setPostFrom] = useViewState("page:postFrom", "");
  const [postTo, setPostTo] = useViewState("page:postTo", "");
  const [page, setPage] = useViewState("page:page", 1);
  const [pageSize, setPageSize] = useViewState("page:pageSize", 25);
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
        page, pageSize, search: debouncedSearch, unit, agency, postFrom, postTo, unlinked, productionOnly,
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
  }, [setPage, agency, debouncedSearch, page, pageSize, postFrom, postTo, unit, unlinked, productionOnly]);

  useEffect(() => {
    const timeout = window.setTimeout(() => void loadData(), 0);
    return () => window.clearTimeout(timeout);
  }, [loadData]);
  const hasFilters = Boolean(search || unit || agency || postFrom || postTo || unlinked || productionOnly);

  const handleSync = async () => {
    if (!canSync || isSyncing) return;
    setIsSyncing(true);
    setMessage(null);
    try {
      const result = await syncTolls();
      await loadData();
      setDashboardRefreshKey((value) => value + 1);
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
    setProductionOnly(false);
    setUnlinked(false);
    setSearch("");
    setUnit("");
    setAgency("");
    setPostFrom("");
    setPostTo("");
    setPage(1);
  };

  const syncAction = (canSync && <ManagementHeader hideTitle
        icon={Receipt}
        title="Tolls"

        count={total}
        actionLabel={isSyncing ? "Syncing…" : "Sync tolls"}
        onAction={() => void handleSync()}
        actionIcon={RefreshCw}
      />);

  return (
    <div className="space-y-5 animate-fade-in">
      <ManagementHeader icon={Receipt} title="Tolls" count={total} />

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

      <div className="flex w-fit max-w-full overflow-x-auto rounded-lg border border-zinc-800/60 bg-zinc-900/30 p-0.5" aria-label="Toll views">
        {(["overview", "transactions", "trucks", "transponders", "attention"] as const).map((tab) => (
          <button key={tab} type="button" onClick={() => setActiveTab(tab)} role="tab" aria-selected={fleetView === tab}
            className={`inline-flex shrink-0 items-center gap-2 rounded-md px-3 py-1.5 text-[13px] font-medium transition-all ${(fleetView === tab) ? "bg-zinc-800 text-zinc-100 shadow-sm" : "text-zinc-500 hover:text-zinc-300"}`}>
            {tab === "overview" ? <LayoutDashboard className="h-3.5 w-3.5" /> : <List className="h-3.5 w-3.5" />}
            {tab === "overview" ? "Overview" : tab === "trucks" ? "Trucks" : tab === "transponders" ? "Transponders" : tab === "attention" ? "Needs attention" : "Transactions"}
          </button>
        ))}
      </div>

      {(fleetView === "trucks" || fleetView === "transponders" || fleetView === "attention") ? <PrePassFleetTab key={fleetView} view={fleetView} /> : activeTab === "overview" ? <TollOverview refreshKey={dashboardRefreshKey} actions={syncAction} /> : (
        <>
      {loadError && <ErrorBanner message={loadError} />}
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
        <TableFilters>
          <label className="flex items-center gap-2 text-xs"><input type="checkbox" checked={unlinked} onChange={e => { setUnlinked(e.target.checked); setPage(1); }} />Without linked truck</label>
          {productionOnly && <button className="text-xs text-blue-400" onClick={() => { setProductionOnly(false); setPage(1); }}>Production records only ×</button>}
          <label className="block space-y-1"><input aria-label="Post date from" type="date" value={postFrom} onChange={(event) => { setPostFrom(event.target.value); setPage(1); }} className={filterClass} /></label><span aria-hidden="true">→</span><label><input aria-label="Post date to" type="date" value={postTo} onChange={(event) => { setPostTo(event.target.value); setPage(1); }} className={filterClass} /></label>
          <select aria-label="Toll truck" value={unit} onChange={(event) => { setUnit(event.target.value); setPage(1); }} className={filterClass}>
          <option value="">Truck</option>
          {units.map((value) => <option key={value} value={value}>{value}</option>)}
        </select>
          <select aria-label="Toll agency" value={agency} onChange={(event) => { setAgency(event.target.value); setPage(1); }} className={filterClass}>
          <option value="">Agency</option>
          {agencies.map((value) => <option key={value} value={value}>{value}</option>)}
        </select>
        </TableFilters>



        {hasFilters && (
          <button type="button" onClick={clearFilters} className="inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-[13px] text-zinc-500 transition hover:bg-zinc-800/50 hover:text-zinc-300">
            <X className="h-3 w-3" /> Clear
          </button>
        )}
      <div className="ml-auto">{syncAction}</div></div>

      <TableShell>
        <table className="w-full min-w-[980px] text-left text-[13px]">
            <thead>
              <tr className="border-b border-zinc-800/50 text-zinc-500">
                <th className="px-4 py-3 font-medium">Post date</th>
                <th className="px-4 py-3 font-medium">Truck</th>
                <th className="px-4 py-3 font-medium">Plaza / route</th>
                <th className="px-4 py-3 font-medium">Agency</th>
                <th className="px-4 py-3 font-medium">Class / miles</th>
                <th className="px-4 py-3 text-right font-medium">Amount</th>
                <th className="w-12 px-2 py-3"><span className="sr-only">Review flag</span></th>
              </tr>
            </thead>
            <tbody>{isLoading ? <tr><td colSpan={7} className="p-8 text-center text-zinc-500" role="status">Loading…</td></tr> : tolls.length === 0 ? <tr><td colSpan={7}>{(
          <EmptyState message={hasFilters ? "No tolls match these filters." : "No PrePass tolls have synced yet."} />
        )}</td></tr> : <>
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
                    <div className="text-zinc-300">{toll.agency}{toll.tollAgencyState ? ` · ${toll.tollAgencyState}` : ""}</div>
                    <div className="mt-0.5 text-[11px] text-zinc-600">{toll.tollAgencyName || toll.source}</div>
                  </td>
                  <td className="px-4 py-3 text-zinc-400">
                    Class {toll.tollClass}{toll.miles !== null ? ` · ${toll.miles.toLocaleString()} mi` : ""}
                  </td>
                  <td className={`px-4 py-3 text-right font-mono font-medium tabular-nums ${toll.amount < 0 ? "text-emerald-400" : "text-zinc-200"}`}>
                    {formatMoney(toll.amount)}
                  </td>
                  <td className="px-2 py-3 text-right"><TransactionFlagIcon flag={toll.flag} kind="toll" /></td>
                </tr>
              ))}
            </>}</tbody>
          </table>
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

        </>
      )}
    </div>
  );
}
