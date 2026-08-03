"use client";

import { useEffect, useState } from "react";
import {
  AlertCircle,
  CalendarDays,
  CircleDollarSign,
  Fuel,
  Gauge,
  Info,
  Landmark,
  PackageCheck,
  Receipt,
  Route,
  TrendingUp,
  Wrench,
} from "lucide-react";
import { Cell, Pie, PieChart, ResponsiveContainer, Tooltip } from "recharts";
import { fetchFinancialDashboard } from "../lib/api";
import type { FinancialDashboard } from "../lib/types";

const money = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  maximumFractionDigits: 0,
});

const preciseMoney = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

const number = new Intl.NumberFormat("en-US", { maximumFractionDigits: 0 });

const PIE_COLORS: Record<string, string> = {
  "Driver pay / shares": "#3b82f6",
  Fuel: "#f59e0b",
  Tolls: "#a78bfa",
};

function localDate(value: string) {
  const [year, month, day] = value.split("-").map(Number);
  return new Date(year, month - 1, day);
}

function weekLabel(value: string) {
  const start = localDate(value);
  const end = new Date(start);
  end.setDate(start.getDate() + 6);
  return `${start.toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
  })} – ${end.toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
    year: "numeric",
  })}`;
}

function KpiCard({
  label,
  value,
  detail,
  icon: Icon,
  tone = "text-zinc-400",
}: {
  label: string;
  value: string;
  detail: string;
  icon: typeof CircleDollarSign;
  tone?: string;
}) {
  return (
    <div className="rounded-xl border border-zinc-800/60 bg-card p-4 transition-colors hover:border-zinc-700/70">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="text-[10px] font-semibold uppercase tracking-[0.14em] text-zinc-600">{label}</div>
          <div className="mt-2 truncate font-mono text-2xl font-semibold tracking-tight text-zinc-100">{value}</div>
          <div className="mt-1 text-[11px] text-zinc-600">{detail}</div>
        </div>
        <div className="rounded-lg border border-zinc-800 bg-zinc-950/70 p-2">
          <Icon className={`h-4 w-4 ${tone}`} />
        </div>
      </div>
    </div>
  );
}

function LoadingDashboard() {
  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }).map((_, index) => (
          <div key={index} className="h-28 animate-pulse rounded-xl border border-zinc-800/60 bg-zinc-900/30" />
        ))}
      </div>
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-5">
        <div className="h-80 animate-pulse rounded-xl border border-zinc-800/60 bg-zinc-900/30 xl:col-span-2" />
        <div className="h-80 animate-pulse rounded-xl border border-zinc-800/60 bg-zinc-900/30 xl:col-span-3" />
      </div>
    </div>
  );
}

function ExpenseTooltip({
  active,
  payload,
}: {
  active?: boolean;
  payload?: ReadonlyArray<{ name?: string; value?: number }>;
}) {
  if (!active || !payload?.length) return null;
  const item = payload[0];
  return (
    <div className="rounded-lg border border-zinc-700 bg-zinc-950/95 px-3 py-2 shadow-xl">
      <div className="text-[11px] text-zinc-500">{item.name}</div>
      <div className="mt-0.5 font-mono text-sm font-semibold text-zinc-100">{money.format(Number(item.value ?? 0))}</div>
    </div>
  );
}

function ExpenseBreakdown({ dashboard }: { dashboard: FinancialDashboard }) {
  const chartData = dashboard.expenses
    .filter((item) => item.available && Number(item.amount) > 0)
    .map((item) => ({ name: item.category, value: item.amount ?? 0 }));

  return (
    <section className="rounded-xl border border-zinc-800/60 bg-card p-4 sm:p-5 xl:col-span-2">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 className="text-sm font-medium text-zinc-200">Expense breakdown</h2>
          <p className="mt-1 text-[11px] text-zinc-600">Company-borne costs only; percentage owner-operator deductions are excluded.</p>
        </div>
        <Receipt className="h-4 w-4 text-zinc-600" />
      </div>

      <div className="mt-3 grid items-center gap-2 sm:grid-cols-[150px_1fr]">
        <div className="relative mx-auto h-36 w-36">
          {chartData.length > 0 ? (
            <>
              <ResponsiveContainer width="100%" height="100%">
                <PieChart>
                  <Pie data={chartData} dataKey="value" nameKey="name" innerRadius={44} outerRadius={64} paddingAngle={3} stroke="none">
                    {chartData.map((entry) => <Cell key={entry.name} fill={PIE_COLORS[entry.name] ?? "#71717a"} />)}
                  </Pie>
                  <Tooltip content={<ExpenseTooltip />} />
                </PieChart>
              </ResponsiveContainer>
              <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
                <span className="font-mono text-sm font-semibold text-zinc-100">{money.format(dashboard.totals.knownExpenses)}</span>
                <span className="text-[9px] uppercase tracking-wider text-zinc-600">known</span>
              </div>
            </>
          ) : (
            <div className="flex h-full items-center justify-center rounded-full border border-dashed border-zinc-800 text-[11px] text-zinc-600">No costs</div>
          )}
        </div>

        <div className="space-y-1">
          {dashboard.expenses.map((item) => (
            <div key={item.category} className="flex items-center justify-between gap-3 rounded-lg px-2.5 py-2 hover:bg-zinc-800/20" title={item.note}>
              <div className="flex min-w-0 items-center gap-2">
                <span className="h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: item.available ? PIE_COLORS[item.category] ?? "#71717a" : "#3f3f46" }} />
                <span className="truncate text-[12px] text-zinc-400">{item.category}</span>
              </div>
              <span className={`font-mono text-[12px] tabular-nums ${item.available ? "text-zinc-200" : "text-zinc-700"}`}>
                {item.available && item.amount != null ? money.format(item.amount) : "Not tracked"}
              </span>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

function PerformanceSummary({ dashboard }: { dashboard: FinancialDashboard }) {
  const expenseRatio = dashboard.totals.gross > 0 ? (dashboard.totals.knownExpenses / dashboard.totals.gross) * 100 : 0;
  const fuelRatio = dashboard.totals.gross > 0 ? (dashboard.totals.fuel / dashboard.totals.gross) * 100 : 0;
  const insights = [
    { label: "Known expense ratio", value: `${expenseRatio.toFixed(1)}%`, width: Math.min(expenseRatio, 100), color: "bg-blue-500" },
    { label: "Company fuel to gross", value: `${fuelRatio.toFixed(1)}%`, width: Math.min(fuelRatio, 100), color: "bg-amber-500" },
    { label: "Revenue per mile", value: `${preciseMoney.format(dashboard.totals.revenuePerMile)} / mi`, width: Math.min((dashboard.totals.revenuePerMile / 5) * 100, 100), color: "bg-emerald-500" },
  ];

  return (
    <section className="rounded-xl border border-zinc-800/60 bg-card p-4 sm:p-5 xl:col-span-3">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 className="text-sm font-medium text-zinc-200">Operating snapshot</h2>
          <p className="mt-1 text-[11px] text-zinc-600">A quick read on efficiency and report completeness.</p>
        </div>
        <Gauge className="h-4 w-4 text-zinc-600" />
      </div>
      <div className="mt-5 space-y-5">
        {insights.map((item) => (
          <div key={item.label}>
            <div className="mb-2 flex items-center justify-between gap-4">
              <span className="text-[12px] text-zinc-500">{item.label}</span>
              <span className="font-mono text-[12px] font-medium tabular-nums text-zinc-200">{item.value}</span>
            </div>
            <div className="h-1.5 overflow-hidden rounded-full bg-zinc-800">
              <div className={`h-full rounded-full ${item.color}`} style={{ width: `${item.width}%` }} />
            </div>
          </div>
        ))}
      </div>
      <div className="mt-6 flex items-start gap-2 rounded-lg border border-amber-500/15 bg-amber-500/5 px-3 py-2.5">
        <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-amber-400" />
        <p className="text-[11px] leading-relaxed text-amber-200/70">
          Driver-paid deductions: <span className="font-mono text-amber-200">{money.format(dashboard.totals.deductedFuel)} fuel + {money.format(dashboard.totals.deductedTolls)} tolls</span>. Dispatcher pay is reported separately under Accounting and is not deducted from this overview. Maintenance is not tracked.
        </p>
      </div>
    </section>
  );
}

export default function DashboardPage() {
  const [weekStart, setWeekStart] = useState("");
  const [dashboard, setDashboard] = useState<FinancialDashboard | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    fetchFinancialDashboard({})
      .then((result) => {
        if (cancelled) return;
        setDashboard(result);
        setWeekStart(result.period.dateFrom ?? "");
        setError("");
      })
      .catch((reason) => {
        if (cancelled) return;
        setError(reason instanceof Error ? reason.message : "The financial dashboard could not be loaded.");
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const selectWeek = async (value: string) => {
    if (!value || value === weekStart) return;
    const previousWeek = weekStart;
    setIsLoading(true);
    setWeekStart(value);
    try {
      const result = await fetchFinancialDashboard({ weekStart: value });
      setDashboard(result);
      setError("");
    } catch (reason) {
      setWeekStart(previousWeek);
      setError(reason instanceof Error ? reason.message : "The weekly financial report could not be loaded.");
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="space-y-5 animate-fade-in">
      <header className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <div className="flex items-center gap-2.5">
            <Landmark className="h-5 w-5 text-blue-400" />
            <h1 className="text-lg font-semibold text-zinc-100">Weekly financial dashboard</h1>
          </div>
          <p className="mt-1.5 text-[12px] text-zinc-600">Reliable Monday–Sunday reporting for qualified invoiced loads.</p>
        </div>
        {dashboard && dashboard.availableWeeks.length > 0 && (
          <label className="flex items-center gap-2 rounded-lg border border-zinc-800 bg-zinc-950 px-2.5 py-1.5">
            <CalendarDays className="h-3.5 w-3.5 text-zinc-600" />
            <select value={weekStart} onChange={(event) => void selectWeek(event.target.value)} className="bg-transparent text-[12px] text-zinc-300 outline-none" aria-label="Select weekly report">
              {dashboard.availableWeeks.map((week) => <option key={week} value={week}>{weekLabel(week)}</option>)}
            </select>
          </label>
        )}
      </header>

      {error && (
        <div className="flex items-center gap-2 rounded-lg border border-red-500/20 bg-red-500/5 px-3 py-2.5 text-[12px] text-red-300" role="alert">
          <AlertCircle className="h-4 w-4 shrink-0" />
          {error}
        </div>
      )}

      {isLoading && !dashboard ? (
        <LoadingDashboard />
      ) : dashboard ? (
        <div className={`space-y-4 ${isLoading ? "opacity-60" : ""}`}>
          <div className="flex items-center gap-2 text-[11px] text-zinc-600">
            <CalendarDays className="h-3.5 w-3.5" />
            {dashboard.period.dateFrom ? `Weekly report · ${weekLabel(dashboard.period.dateFrom)} · Monday–Sunday` : "No qualifying report weeks yet"}
          </div>

          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <KpiCard label="Total gross" value={money.format(dashboard.totals.gross)} detail={`${dashboard.totals.loadCount.toLocaleString()} invoiced loads`} icon={CircleDollarSign} tone="text-blue-400" />
            <KpiCard label="Estimated profit" value={money.format(dashboard.totals.estimatedProfit)} detail={`${dashboard.totals.estimatedProfitMargin.toFixed(1)}% before untracked costs`} icon={TrendingUp} tone={dashboard.totals.estimatedProfit >= 0 ? "text-emerald-400" : "text-red-400"} />
            <KpiCard label="Known expenses" value={money.format(dashboard.totals.knownExpenses)} detail="Pay/shares + company-paid diesel and tolls" icon={Receipt} tone="text-amber-400" />
            <KpiCard label="Revenue per mile" value={`${preciseMoney.format(dashboard.totals.revenuePerMile)}/mi`} detail={`${number.format(dashboard.totals.miles)} total miles`} icon={Route} tone="text-violet-400" />
          </div>

          <div className="grid grid-cols-1 gap-4 xl:grid-cols-5">
            <ExpenseBreakdown dashboard={dashboard} />
            <PerformanceSummary dashboard={dashboard} />
          </div>

          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <div className="flex items-center gap-3 rounded-xl border border-zinc-800/50 bg-zinc-950/25 px-4 py-3">
              <PackageCheck className="h-4 w-4 text-zinc-600" />
              <div><div className="font-mono text-sm font-semibold text-zinc-200">{dashboard.totals.loadCount.toLocaleString()}</div><div className="text-[10px] text-zinc-600">Loads</div></div>
            </div>
            <div className="flex items-center gap-3 rounded-xl border border-zinc-800/50 bg-zinc-950/25 px-4 py-3">
              <Fuel className="h-4 w-4 text-amber-500" />
              <div><div className="font-mono text-sm font-semibold text-zinc-200">{money.format(dashboard.totals.fuel)}</div><div className="text-[10px] text-zinc-600">Company diesel{dashboard.totals.deductedFuel > 0 ? ` · ${money.format(dashboard.totals.deductedFuel)} deducted` : ""}</div></div>
            </div>
            <div className="flex items-center gap-3 rounded-xl border border-zinc-800/50 bg-zinc-950/25 px-4 py-3">
              <Receipt className="h-4 w-4 text-violet-400" />
              <div><div className="font-mono text-sm font-semibold text-zinc-200">{money.format(dashboard.totals.tolls)}</div><div className="text-[10px] text-zinc-600">Company tolls{dashboard.totals.deductedTolls > 0 ? ` · ${money.format(dashboard.totals.deductedTolls)} deducted` : ""}{dashboard.totals.unattributedTolls > 0 ? ` · ${money.format(dashboard.totals.unattributedTolls)} unassigned` : ""}</div></div>
            </div>
            <div className="flex items-center gap-3 rounded-xl border border-zinc-800/50 bg-zinc-950/25 px-4 py-3">
              <Wrench className="h-4 w-4 text-zinc-700" />
              <div><div className="font-mono text-sm font-semibold text-zinc-600">Not tracked</div><div className="text-[10px] text-zinc-700">Maintenance</div></div>
            </div>
          </div>

          <details className="group rounded-xl border border-zinc-800/60 bg-zinc-950/20 px-4 py-3">
            <summary className="flex cursor-pointer list-none items-center gap-2 text-[11px] font-medium text-zinc-400">
              <Info className="h-3.5 w-3.5 text-zinc-600" />
              How these numbers are calculated
            </summary>
            <div className="mt-3 grid gap-2 text-[11px] leading-relaxed text-zinc-600 sm:grid-cols-2">
              <p><span className="text-zinc-400">Gross:</span> {dashboard.methodology.gross}</p>
              <p><span className="text-zinc-400">Driver pay:</span> {dashboard.methodology.driverPay}</p>
              <p><span className="text-zinc-400">Fuel:</span> {dashboard.methodology.fuel}</p>
              <p><span className="text-zinc-400">Tolls:</span> {dashboard.methodology.tolls}</p>
              <p><span className="text-zinc-400">Profit:</span> {dashboard.methodology.profit}</p>
              <p><span className="text-zinc-400">Weekly reports:</span> {dashboard.methodology.week}</p>
            </div>
          </details>
        </div>
      ) : null}
    </div>
  );
}
