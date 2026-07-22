"use client";

import { useCallback, useEffect, useState } from "react";
import {
  AlertCircle,
  CheckCircle2,
  Fuel,
  LayoutDashboard,
  List,
  RefreshCw,
  X,
} from "lucide-react";
import {
  fetchFuelTransactionsPage,
  syncFuelTransactions,
} from "../lib/api";
import type { FuelTransaction } from "../lib/types";
import { useDebouncedValue } from "../lib/useDebouncedValue";
import {
  EmptyState,
  LoadingTable,
  ManagementSearch,
  TablePagination,
  TableShell,
} from "../components/management/ManagementUI";
import { FuelOverview } from "./FuelOverview";

const filterClass =
  "rounded-lg border border-zinc-800 bg-zinc-950 px-2.5 py-1.5 text-[13px] text-zinc-300 outline-none transition-colors focus:border-zinc-600";

const moneyFormatter = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
});

const FALLBACK_TIMEZONE = "America/New_York";
const dateFormatters = new Map<string, Intl.DateTimeFormat>();

function formatterFor(timezone: string) {
  const key = timezone || FALLBACK_TIMEZONE;
  const existing = dateFormatters.get(key);
  if (existing) return existing;
  try {
    const formatter = new Intl.DateTimeFormat("en-US", {
      timeZone: key,
      month: "short",
      day: "numeric",
      year: "numeric",
      hour: "numeric",
      minute: "2-digit",
      timeZoneName: "short",
    });
    dateFormatters.set(key, formatter);
    return formatter;
  } catch {
    return formatterFor(FALLBACK_TIMEZONE);
  }
}

function formatTransactionDate(transaction: FuelTransaction) {
  return formatterFor(transaction.timezone).format(
    new Date(transaction.purchasedAt),
  );
}

function cardLabel(value: string | null) {
  if (!value) return "No card ID";
  return `•••• ${value.slice(-4)}`;
}

function productSummary(transaction: FuelTransaction) {
  const parts: string[] = [];
  if (transaction.fuelAmount > 0) {
    parts.push(
      `${transaction.fuelVolume.toLocaleString(undefined, { maximumFractionDigits: 3 })} gal fuel`,
    );
  }
  if (transaction.defAmount > 0) {
    parts.push(
      `${transaction.defVolume.toLocaleString(undefined, { maximumFractionDigits: 3 })} gal DEF`,
    );
  }
  if (transaction.otherAmount > 0) parts.push("other products");
  if (transaction.cashAdvance && transaction.cashAdvance > 0) {
    parts.push("cash advance");
  }
  return parts.length > 0 ? parts.join(" · ") : "Transaction total";
}

export default function FuelPage() {
  const [activeTab, setActiveTab] = useState<"overview" | "transactions">("overview");
  const [dashboardRefreshKey, setDashboardRefreshKey] = useState(0);
  const [transactions, setTransactions] = useState<FuelTransaction[]>([]);
  const [search, setSearch] = useState("");
  const [driver, setDriver] = useState("");
  const [state, setState] = useState("");
  const [category, setCategory] = useState("");
  const [dateFrom, setDateFrom] = useState("");
  const [dateTo, setDateTo] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(25);
  const [total, setTotal] = useState(0);
  const [totalPages, setTotalPages] = useState(1);
  const [drivers, setDrivers] = useState<string[]>([]);
  const [states, setStates] = useState<string[]>([]);
  const [totals, setTotals] = useState({ spend: 0, saved: 0, gallons: 0 });
  const [isLoading, setIsLoading] = useState(true);
  const [isSyncing, setIsSyncing] = useState(false);
  const [message, setMessage] = useState<{
    type: "success" | "error";
    text: string;
  } | null>(null);
  const debouncedSearch = useDebouncedValue(search);

  const loadData = useCallback(async () => {
    setIsLoading(true);
    try {
      const response = await fetchFuelTransactionsPage({
        page, pageSize, search: debouncedSearch, driver, state, category, dateFrom, dateTo,
      });
      setTransactions(response.items);
      setPage(response.page);
      setTotal(response.total);
      setTotalPages(response.totalPages);
      setDrivers(response.options.drivers);
      setStates(response.options.states);
      setTotals(response.summary);
    } catch (reason) {
      setMessage({
        type: "error",
        text: reason instanceof Error ? reason.message : "Failed to load fuel transactions.",
      });
    } finally {
      setIsLoading(false);
    }
  }, [category, dateFrom, dateTo, debouncedSearch, driver, page, pageSize, state]);

  useEffect(() => {
    const timeout = window.setTimeout(() => void loadData(), 0);
    return () => window.clearTimeout(timeout);
  }, [loadData]);

  const hasFilters = Boolean(
    search || driver || state || category || dateFrom || dateTo,
  );

  const handleSync = async () => {
    setIsSyncing(true);
    setMessage(null);
    try {
      const result = await syncFuelTransactions();
      await loadData();
      setDashboardRefreshKey((value) => value + 1);
      setMessage({
        type: "success",
        text: `Checked ${result.daysFetched} missing/current day${result.daysFetched === 1 ? "" : "s"} and synced ${result.saved} fuel transaction${result.saved === 1 ? "" : "s"}${result.excluded > 0 ? `; excluded ${result.excluded} non-fuel financial entr${result.excluded === 1 ? "y" : "ies"}` : ""}. ${result.daysSkipped} completed day${result.daysSkipped === 1 ? " was" : "s were"} already up to date.`,
      });
    } catch (reason) {
      setMessage({
        type: "error",
        text:
          reason instanceof Error ? reason.message : "Relay fuel sync failed.",
      });
    } finally {
      setIsSyncing(false);
    }
  };

  const clearFilters = () => {
    setSearch("");
    setDriver("");
    setState("");
    setCategory("");
    setDateFrom("");
    setDateTo("");
    setPage(1);
  };

  return (
    <div className="space-y-5 animate-fade-in">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <div className="flex items-center gap-3">
            <Fuel className="h-5 w-5 text-zinc-500" />
            <h1 className="text-lg font-semibold text-zinc-100">Fuel</h1>
            {activeTab === "transactions" && !isLoading && (
              <span className="rounded-full bg-zinc-800/60 px-2.5 py-0.5 text-[12px] font-medium text-zinc-400">
                {hasFilters
                  ? `${total.toLocaleString()} matching`
                  : total.toLocaleString()}
              </span>
            )}
          </div>
          <p className="mt-1 text-[13px] text-zinc-600">
            Fuel performance, pricing, and Relay purchase data.
          </p>
        </div>
        <button
          type="button"
          onClick={() => void handleSync()}
          disabled={isSyncing}
          className="inline-flex items-center justify-center gap-2 rounded-lg border border-zinc-700/80 bg-zinc-900/60 px-3.5 py-2 text-[13px] font-medium text-zinc-300 transition hover:border-zinc-600 hover:bg-zinc-800/70 hover:text-zinc-100 disabled:cursor-wait disabled:opacity-60"
        >
          <RefreshCw className={`h-4 w-4 ${isSyncing ? "animate-spin" : ""}`} />
          {isSyncing ? "Syncing missing days…" : "Sync fuel"}
        </button>
      </div>

      {message && (
        <div
          className={`flex items-start gap-2 rounded-lg border px-3 py-2.5 text-[13px] ${
            message.type === "success"
              ? "border-emerald-500/20 bg-emerald-500/5 text-emerald-300"
              : "border-red-500/20 bg-red-500/5 text-red-300"
          }`}
          role={message.type === "error" ? "alert" : "status"}
        >
          {message.type === "success" ? (
            <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" />
          ) : (
            <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
          )}
          <span className="min-w-0 flex-1">{message.text}</span>
          <button
            type="button"
            onClick={() => setMessage(null)}
            className="rounded p-0.5 opacity-60 transition hover:bg-white/5 hover:opacity-100"
            aria-label="Dismiss message"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      )}

      <div className="flex w-fit rounded-lg border border-zinc-800/60 bg-zinc-900/30 p-0.5">
        <button
          type="button"
          onClick={() => setActiveTab("overview")}
          className={`inline-flex items-center gap-2 rounded-md px-3 py-1.5 text-[13px] font-medium transition-all ${
            activeTab === "overview"
              ? "bg-zinc-800 text-zinc-100 shadow-sm"
              : "text-zinc-500 hover:text-zinc-300"
          }`}
        >
          <LayoutDashboard className="h-3.5 w-3.5" /> Overview
        </button>
        <button
          type="button"
          onClick={() => setActiveTab("transactions")}
          className={`inline-flex items-center gap-2 rounded-md px-3 py-1.5 text-[13px] font-medium transition-all ${
            activeTab === "transactions"
              ? "bg-zinc-800 text-zinc-100 shadow-sm"
              : "text-zinc-500 hover:text-zinc-300"
          }`}
        >
          <List className="h-3.5 w-3.5" /> Transactions
        </button>
      </div>

      {activeTab === "overview" ? (
        <FuelOverview refreshKey={dashboardRefreshKey} />
      ) : (
        <>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <div className="rounded-xl border border-zinc-800/60 bg-card px-4 py-3">
          <div className="text-[11px] font-medium uppercase tracking-wider text-zinc-600">
            Displayed spend
          </div>
          <div className="mt-1 font-mono text-lg font-semibold tabular-nums text-zinc-200">
            {moneyFormatter.format(totals.spend)}
          </div>
        </div>
        <div className="rounded-xl border border-zinc-800/60 bg-card px-4 py-3">
          <div className="text-[11px] font-medium uppercase tracking-wider text-zinc-600">
            Savings
          </div>
          <div className="mt-1 font-mono text-lg font-semibold tabular-nums text-emerald-400">
            {moneyFormatter.format(totals.saved)}
          </div>
        </div>
        <div className="rounded-xl border border-zinc-800/60 bg-card px-4 py-3">
          <div className="text-[11px] font-medium uppercase tracking-wider text-zinc-600">
            Fuel + DEF volume
          </div>
          <div className="mt-1 font-mono text-lg font-semibold tabular-nums text-zinc-200">
            {totals.gallons.toLocaleString(undefined, {
              maximumFractionDigits: 3,
            })}{" "}
            <span className="text-sm font-normal text-zinc-600">gal</span>
          </div>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <ManagementSearch
          value={search}
          onChange={(value) => { setSearch(value); setPage(1); }}
          placeholder="Search driver, card, merchant, location…"
        />
        <select
          value={driver}
          onChange={(event) => { setDriver(event.target.value); setPage(1); }}
          className={filterClass}
          aria-label="Filter by driver"
        >
          <option value="">Driver</option>
          {drivers.map((value) => (
            <option key={value} value={value}>{value}</option>
          ))}
        </select>
        <select
          value={state}
          onChange={(event) => { setState(event.target.value); setPage(1); }}
          className={filterClass}
          aria-label="Filter by state"
        >
          <option value="">State</option>
          {states.map((value) => (
            <option key={value} value={value}>{value}</option>
          ))}
        </select>
        <select
          value={category}
          onChange={(event) => { setCategory(event.target.value); setPage(1); }}
          className={filterClass}
          aria-label="Filter by purchase category"
        >
          <option value="">Category</option>
          <option value="fuel">Fuel</option>
          <option value="def">DEF</option>
          <option value="other">Other</option>
        </select>
        <div className="flex items-center gap-1.5">
          <input
            aria-label="Purchase date from"
            type="date"
            value={dateFrom}
            onChange={(event) => { setDateFrom(event.target.value); setPage(1); }}
            className={filterClass}
          />
          <span className="text-[13px] text-zinc-700">–</span>
          <input
            aria-label="Purchase date to"
            type="date"
            value={dateTo}
            onChange={(event) => { setDateTo(event.target.value); setPage(1); }}
            className={filterClass}
          />
        </div>
        <span className="text-[11px] text-zinc-600">
          Dates use each merchant&apos;s local timezone to match Relay reports.
        </span>
        {hasFilters && (
          <button
            type="button"
            onClick={clearFilters}
            className="inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-[13px] text-zinc-500 transition hover:bg-zinc-800/50 hover:text-zinc-300"
          >
            <X className="h-3 w-3" /> Clear
          </button>
        )}
      </div>

      <TableShell>
        {isLoading ? (
          <LoadingTable columns={7} />
        ) : transactions.length === 0 ? (
          <EmptyState
            message={
              hasFilters
                ? "No fuel transactions match these filters."
                : "No Relay transactions yet. Sync fuel to get started."
            }
          />
        ) : (
          <table className="w-full min-w-[1080px] text-left text-[13px]">
            <thead>
              <tr className="border-b border-zinc-800/50 text-zinc-500">
                <th className="px-4 py-3 font-medium">Purchased</th>
                <th className="px-4 py-3 font-medium">Driver</th>
                <th className="px-4 py-3 font-medium">Merchant / location</th>
                <th className="px-4 py-3 font-medium">Products</th>
                <th className="px-4 py-3 text-right font-medium">Fuel</th>
                <th className="px-4 py-3 text-right font-medium">DEF / other</th>
                <th className="px-4 py-3 text-right font-medium">Paid</th>
              </tr>
            </thead>
            <tbody>
              {transactions.map((transaction) => (
                <tr
                  key={transaction.id}
                  className="border-b border-zinc-900/70 text-zinc-300 transition last:border-0 hover:bg-zinc-800/15"
                >
                  <td className="px-4 py-3">
                    <div className="font-mono tabular-nums text-zinc-200">
                      {formatTransactionDate(transaction)}
                    </div>
                    <div className="mt-0.5 font-mono text-[11px] text-zinc-700">
                      {transaction.relayTransactionId}
                    </div>
                  </td>
                  <td className="px-4 py-3">
                    <div className="font-medium text-zinc-200">
                      {transaction.driverName}
                    </div>
                    <div className="mt-0.5 font-mono text-[11px] text-zinc-600">
                      {cardLabel(transaction.relayIntegrationId)}
                    </div>
                  </td>
                  <td className="px-4 py-3">
                    <div className="text-zinc-300">{transaction.merchantName}</div>
                    <div className="mt-0.5 text-[11px] text-zinc-600">
                      {transaction.locationName} · {transaction.city}, {transaction.state}
                    </div>
                  </td>
                  <td className="px-4 py-3 text-zinc-400">
                    {productSummary(transaction)}
                  </td>
                  <td className="px-4 py-3 text-right font-mono tabular-nums text-zinc-300">
                    {moneyFormatter.format(transaction.fuelAmount)}
                  </td>
                  <td className="px-4 py-3 text-right font-mono tabular-nums text-zinc-400">
                    <div>{moneyFormatter.format(transaction.defAmount)}</div>
                    {transaction.otherAmount > 0 && (
                      <div className="mt-0.5 text-[11px] text-zinc-600">
                        +{moneyFormatter.format(transaction.otherAmount)} other
                      </div>
                    )}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <div className="font-mono font-medium tabular-nums text-zinc-100">
                      {moneyFormatter.format(transaction.totalAmountPaid)}
                    </div>
                    {transaction.totalAmountSaved > 0 && (
                      <div className="mt-0.5 font-mono text-[11px] tabular-nums text-emerald-500">
                        saved {moneyFormatter.format(transaction.totalAmountSaved)}
                      </div>
                    )}
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
        </>
      )}
    </div>
  );
}
