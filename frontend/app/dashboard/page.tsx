"use client";

import { useEffect, useMemo, useState } from "react";
import {
  DollarSign,
  Gauge,
  Route,
  Package,
  TrendingUp,
  BarChart3,
  Clock,
} from "lucide-react";
import { fetchLoads } from "../lib/api";
import type { Load, PeriodKey } from "../lib/types";
import {
  computeMetrics,
  computeDelta,
  computeRevenueTrend,
  formatMoney,
  formatRate,
  formatMiles,
  formatDate,
  formatCompact,
} from "../lib/utils";
import { MetricCard } from "../components/MetricCard";
import { MiniBarChart } from "../components/MiniBarChart";
import { StatusDot } from "../components/StatusDot";

const PERIODS: { key: PeriodKey; label: string }[] = [
  { key: "today", label: "Today" },
  { key: "thisWeek", label: "This Week" },
  { key: "thisMonth", label: "This Month" },
  { key: "lastMonth", label: "Last Month" },
  { key: "allTime", label: "All Time" },
];

export default function DashboardPage() {
  const [loads, setLoads] = useState<Load[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [period, setPeriod] = useState<PeriodKey>("thisMonth");

  useEffect(() => {
    fetchLoads()
      .then(setLoads)
      .finally(() => setIsLoading(false));
  }, []);

  const metrics = useMemo(
    () => computeMetrics(loads, period),
    [loads, period],
  );

  const revenueDelta = useMemo(
    () => computeDelta(loads, period),
    [loads, period],
  );

  const trendData = useMemo(
    () => computeRevenueTrend(loads, period),
    [loads, period],
  );

  // Recent loads (last 5 by pickup time)
  const recentLoads = useMemo(() => {
    return [...loads]
      .sort(
        (a, b) =>
          new Date(b.PickupTime || 0).getTime() -
          new Date(a.PickupTime || 0).getTime(),
      )
      .slice(0, 5);
  }, [loads]);

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div className="h-8 w-48 animate-pulse rounded-lg bg-zinc-800/50" />
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-5">
          {Array.from({ length: 5 }).map((_, i) => (
            <div
              key={i}
              className="h-32 animate-pulse rounded-xl border border-zinc-800/60 bg-zinc-900/30"
            />
          ))}
        </div>
        <div className="h-64 animate-pulse rounded-xl border border-zinc-800/60 bg-zinc-900/30" />
      </div>
    );
  }

  return (
    <div className="space-y-6 animate-fade-in">
      {/* Header + Period Selector */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-3">
          <BarChart3 className="h-5 w-5 text-zinc-500" />
          <h1 className="text-lg font-semibold text-zinc-100">Dashboard</h1>
        </div>

        <div className="flex rounded-lg border border-zinc-800/60 bg-zinc-900/30 p-0.5">
          {PERIODS.map((p) => (
            <button
              key={p.key}
              onClick={() => setPeriod(p.key)}
              className={`rounded-md px-3 py-1.5 text-[13px] font-medium transition-all ${
                period === p.key
                  ? "bg-zinc-800 text-zinc-100 shadow-sm"
                  : "text-zinc-500 hover:text-zinc-300"
              }`}
            >
              {p.label}
            </button>
          ))}
        </div>
      </div>

      {/* KPI Cards */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-5">
        <MetricCard
          label="Gross Revenue"
          value={formatCompact(metrics.grossRevenue)}
          icon={DollarSign}
          delta={revenueDelta}
          delay={0}
        />
        <MetricCard
          label="Average RPM"
          value={formatRate(metrics.avgRpm)}
          icon={Gauge}
          delay={50}
        />
        <MetricCard
          label="Total Miles"
          value={formatMiles(metrics.totalMiles)}
          icon={Route}
          delay={100}
        />
        <MetricCard
          label="Loads"
          value={String(metrics.loadCount)}
          icon={Package}
          delay={150}
        />
        <MetricCard
          label="Avg Pay / Load"
          value={formatMoney(metrics.avgPayPerLoad)}
          icon={TrendingUp}
          delay={200}
        />
      </div>

      {/* Revenue Trend Chart */}
      <div className="rounded-xl border border-zinc-800/60 bg-card p-5 animate-fade-in" style={{ animationDelay: "250ms" }}>
        <div className="mb-4 flex items-center gap-2">
          <BarChart3 className="h-4 w-4 text-zinc-500" />
          <h2 className="text-sm font-medium text-zinc-300">Revenue Trend</h2>
        </div>
        <MiniBarChart data={trendData} height={180} />
      </div>

      {/* Recent Loads */}
      <div className="rounded-xl border border-zinc-800/60 bg-card animate-fade-in" style={{ animationDelay: "300ms" }}>
        <div className="flex items-center gap-2 border-b border-zinc-800/40 px-5 py-3.5">
          <Clock className="h-4 w-4 text-zinc-500" />
          <h2 className="text-sm font-medium text-zinc-300">Recent Loads</h2>
        </div>

        {recentLoads.length === 0 ? (
          <div className="px-5 py-10 text-center text-[13px] text-zinc-600">
            No loads yet.
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-[13px]">
              <thead>
                <tr className="border-b border-zinc-800/40 text-zinc-500">
                  <th className="px-5 py-2.5 font-medium">Load</th>
                  <th className="px-5 py-2.5 font-medium">Customer</th>
                  <th className="px-5 py-2.5 font-medium">Driver</th>
                  <th className="px-5 py-2.5 font-medium">Status</th>
                  <th className="px-5 py-2.5 font-medium">Pickup</th>
                  <th className="px-5 py-2.5 font-medium text-right">Pay</th>
                </tr>
              </thead>
              <tbody>
                {recentLoads.map((load) => (
                  <tr
                    key={load.ID}
                    className="border-b border-zinc-900/40 text-zinc-300 transition-colors last:border-0 hover:bg-zinc-800/15"
                  >
                    <td className="px-5 py-2.5 font-mono text-zinc-200">
                      {load.LoadID}
                    </td>
                    <td className="px-5 py-2.5">{load.CustomerName}</td>
                    <td className="px-5 py-2.5">{load.DriverName}</td>
                    <td className="px-5 py-2.5">
                      <StatusDot status={load.Status} />
                    </td>
                    <td className="px-5 py-2.5 font-mono tabular-nums text-zinc-400">
                      {formatDate(load.PickupTime)}
                    </td>
                    <td className="px-5 py-2.5 text-right font-mono tabular-nums text-zinc-200">
                      {formatMoney(load.TotalPay)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
