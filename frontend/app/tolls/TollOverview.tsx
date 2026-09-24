"use client";

import { useEffect, useState } from "react";
import { CalendarDays, CircleDollarSign, Receipt, Truck } from "lucide-react";
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { fetchTollDashboard } from "../lib/api";
import type { TollDashboard, TollDashboardPoint } from "../lib/types";
import { EmptyState, ErrorBanner } from "../components/management/ManagementUI";
import { ChartCard, KpiCard } from "../components/OverviewCards";

const inputClass = "rounded-lg border border-zinc-800 bg-zinc-950 px-2.5 py-1.5 text-[13px] text-zinc-300 outline-none transition-colors focus:border-zinc-600";
const money = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD" });
const compact = new Intl.NumberFormat("en-US", { notation: "compact", maximumFractionDigits: 1 });

function todayDate() {
  const today = new Date();
  return `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, "0")}-${String(today.getDate()).padStart(2, "0")}`;
}

function periodLabel(value: string, monthly: boolean) {
  return new Intl.DateTimeFormat("en-US", monthly
    ? { month: "short", year: "2-digit" }
    : { month: "short", day: "numeric", year: "2-digit" },
  ).format(new Date(`${value}T00:00:00`));
}

function SpendingChart({ points, color, horizontal = false }: {
  points: TollDashboardPoint[];
  color: string;
  horizontal?: boolean;
}) {
  return (
    <ResponsiveContainer width="100%" height="100%">
      <BarChart data={points} layout={horizontal ? "vertical" : "horizontal"} margin={{ top: 5, right: 12, left: 0, bottom: 0 }} accessibilityLayer>
        <CartesianGrid stroke="#27272a" vertical={horizontal} horizontal={!horizontal} />
        {horizontal ? (
          <>
            <XAxis type="number" tickFormatter={(value) => `$${compact.format(Number(value))}`} tick={{ fill: "#71717a", fontSize: 10 }} tickLine={false} axisLine={false} />
            <YAxis type="category" dataKey="label" width={100} tickFormatter={(value: string) => value.length > 15 ? `${value.slice(0, 14)}…` : value} tick={{ fill: "#a1a1aa", fontSize: 10 }} tickLine={false} axisLine={false} />
          </>
        ) : (
          <>
            <XAxis dataKey="label" minTickGap={20} tick={{ fill: "#71717a", fontSize: 10 }} tickLine={false} axisLine={false} />
            <YAxis tickFormatter={(value) => `$${compact.format(Number(value))}`} tick={{ fill: "#71717a", fontSize: 10 }} tickLine={false} axisLine={false} width={55} />
          </>
        )}
        <Tooltip cursor={{ fill: "#ffffff", fillOpacity: 0.025 }} content={({ active, payload }) => {
          const point = payload?.[0]?.payload as TollDashboardPoint | undefined;
          if (!active || !point) return null;
          return (
            <div className="rounded-lg border border-zinc-700 bg-zinc-950/95 px-3 py-2 text-[12px] shadow-xl">
              <div className="text-zinc-400">{point.label}</div>
              <div className="mt-1 font-mono font-medium text-zinc-100">{money.format(point.spend)}</div>
              <div className="mt-1 text-zinc-500">{point.transactionCount.toLocaleString()} transactions</div>
            </div>
          );
        }} />
        <Bar dataKey="spend" name="Net toll spend" fill={color} radius={horizontal ? [0, 3, 3, 0] : [3, 3, 0, 0]} />
      </BarChart>
    </ResponsiveContainer>
  );
}

export function TollOverview({ refreshKey }: { refreshKey: number }) {
  const [dateFrom, setDateFrom] = useState(() => `${new Date().getFullYear()}-01-01`);
  const [dateTo, setDateTo] = useState(todayDate);
  const [retryKey, setRetryKey] = useState(0);
  const requestKey = `${dateFrom}/${dateTo}/${refreshKey}/${retryKey}`;
  const [result, setResult] = useState<{ key: string; dashboard?: TollDashboard; error?: string } | null>(null);
  const validationError = !dateFrom || !dateTo ? "Select both dates to view toll spending." : dateFrom > dateTo ? "Start date must be on or before end date." : "";

  useEffect(() => {
    if (validationError) return;
    let cancelled = false;
    fetchTollDashboard({ dateFrom, dateTo }).then((dashboard) => {
      if (!cancelled) setResult({ key: requestKey, dashboard });
    }).catch((reason) => {
      if (!cancelled) setResult({ key: requestKey, error: reason instanceof Error ? reason.message : "Failed to load toll overview." });
    });
    return () => { cancelled = true; };
  }, [dateFrom, dateTo, requestKey, validationError]);

  const current = result?.key === requestKey ? result : null;
  const dashboard = current?.dashboard;
  const error = validationError || current?.error;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <CalendarDays className="h-4 w-4 text-zinc-500" />
        <span className="text-[13px] text-zinc-400">Posting dates</span>
        <input aria-label="Overview start date" type="date" value={dateFrom} max={dateTo || undefined} onChange={(event) => setDateFrom(event.target.value)} className={inputClass} />
        <span className="text-zinc-600">–</span>
        <input aria-label="Overview end date" type="date" value={dateTo} min={dateFrom || undefined} onChange={(event) => setDateTo(event.target.value)} className={inputClass} />
        <button type="button" onClick={() => { setDateFrom(`${new Date().getFullYear()}-01-01`); setDateTo(todayDate()); }} className="rounded-lg px-2 py-1.5 text-[12px] text-zinc-400 hover:bg-zinc-800/50 hover:text-zinc-200">Year to date</button>
      </div>

      {error ? (
        <div className="space-y-2">
          <ErrorBanner message={error} />
          {!validationError && <button type="button" onClick={() => setRetryKey((value) => value + 1)} className={inputClass}>Retry overview</button>}
        </div>
      ) : !dashboard ? (
        <div role="status" aria-label="Loading toll overview" className="space-y-4">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            {Array.from({ length: 3 }, (_, index) => <div key={index} className="h-28 animate-pulse rounded-xl border border-zinc-800/60 bg-zinc-900/30" />)}
          </div>
          <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
            {Array.from({ length: 4 }, (_, index) => <div key={index} className="h-[350px] animate-pulse rounded-xl border border-zinc-800/60 bg-zinc-900/30" />)}
          </div>
        </div>
      ) : (
        <>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <KpiCard label="Toll expense" value={money.format(dashboard.totals.spend)} detail="Net charges after credits" icon={CircleDollarSign} accent="text-blue-400" />
            <KpiCard label="Transactions" value={dashboard.totals.transactionCount.toLocaleString()} detail="All charges and credits in range" icon={Receipt} accent="text-cyan-400" />
            <KpiCard label="Truck units" value={dashboard.totals.truckCount.toLocaleString()} detail="Distinct units reported by PrePass" icon={Truck} accent="text-violet-400" />
          </div>
          {dashboard.totals.transactionCount === 0 ? (
            <EmptyState message="No toll transactions in this date range. Adjust the dates or sync tolls to get started." />
          ) : (
            <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
              <ChartCard title="Monthly toll spending" description="Net posted charges by month for the selected dates.">
                <SpendingChart points={dashboard.monthly.map((point) => ({ ...point, label: periodLabel(point.label, true) }))} color="#3b82f6" />
              </ChartCard>
              <ChartCard title="Weekly toll spending" description="Weeks begin Monday; boundary weeks include only selected dates.">
                <SpendingChart points={dashboard.weekly.map((point) => ({ ...point, label: periodLabel(point.label, false) }))} color="#a78bfa" />
              </ChartCard>
              <ChartCard title="Top toll agencies" description="Up to 10 agencies ranked by net spending in this range.">
                <SpendingChart points={[...dashboard.agencies].sort((a, b) => b.spend - a.spend)} color="#22d3ee" horizontal />
              </ChartCard>
              <ChartCard title="Top truck units" description="Up to 10 source equipment units ranked by net spending.">
                <SpendingChart points={[...dashboard.trucks].sort((a, b) => b.spend - a.spend)} color="#3b82f6" horizontal />
              </ChartCard>
            </div>
          )}
          <details className="rounded-xl border border-zinc-800/60 bg-card px-4 py-3 text-[12px] text-zinc-500">
            <summary className="cursor-pointer font-medium text-zinc-400">How these numbers are calculated</summary>
            <p className="mt-3 leading-relaxed">All cards and charts use the selected posting dates, including both endpoints, without timezone conversion. Credits reduce net spend. Historical imports and production PrePass transactions are included; nonproduction transactions are excluded. Truck units come from the original toll records, including unmatched units, and do not use current driver assignments. Months and weeks without transactions show zero spending.</p>
          </details>
        </>
      )}
    </div>
  );
}
