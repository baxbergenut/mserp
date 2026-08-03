"use client";

import { useEffect, useMemo, useState } from "react";
import {
  AlertCircle,
  Banknote,
  CalendarDays,
  ChevronDown,
  CircleDollarSign,
  Headset,
  ReceiptText,
  Route,
  Search,
  UserRound,
  UsersRound,
} from "lucide-react";
import { fetchFinancialDashboard } from "@/app/lib/api";
import type { FinancialDashboard } from "@/app/lib/types";

type PayReportKind = "driver" | "dispatcher";

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

const number = new Intl.NumberFormat("en-US", {
  maximumFractionDigits: 0,
});

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

function payPlan(
  type: "cpm" | "gross_percentage",
  rate: number,
  isOwnerOperator: boolean,
) {
  return type === "cpm"
    ? `${preciseMoney.format(rate)} / mile`
    : `${rate.toLocaleString(undefined, { maximumFractionDigits: 2 })}% ${
        isOwnerOperator ? "gross share" : "of gross"
      }`;
}

function KpiCard({
  label,
  value,
  detail,
  icon: Icon,
  tone,
}: {
  label: string;
  value: string;
  detail: string;
  icon: typeof CircleDollarSign;
  tone: string;
}) {
  return (
    <div className="rounded-xl border border-zinc-800/60 bg-card p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="text-[10px] font-semibold uppercase tracking-[0.14em] text-zinc-600">
            {label}
          </div>
          <div className="mt-2 truncate font-mono text-2xl font-semibold tracking-tight text-zinc-100">
            {value}
          </div>
          <div className="mt-1 text-[11px] text-zinc-600">{detail}</div>
        </div>
        <div className="rounded-lg border border-zinc-800 bg-zinc-950/70 p-2">
          <Icon className={`h-4 w-4 ${tone}`} />
        </div>
      </div>
    </div>
  );
}

function LoadingReport() {
  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }).map((_, index) => (
          <div
            key={index}
            className="h-28 animate-pulse rounded-xl border border-zinc-800/60 bg-zinc-900/30"
          />
        ))}
      </div>
      <div className="h-96 animate-pulse rounded-xl border border-zinc-800/60 bg-zinc-900/30" />
    </div>
  );
}

function DriverPayReport({ dashboard }: { dashboard: FinancialDashboard }) {
  const [search, setSearch] = useState("");
  const drivers = useMemo(() => {
    const query = search.trim().toLowerCase();
    if (!query) return dashboard.drivers;
    return dashboard.drivers.filter(
      (driver) =>
        driver.driverName.toLowerCase().includes(query) ||
        driver.loadNumbers.some((load) => load.toLowerCase().includes(query)),
    );
  }, [dashboard.drivers, search]);
  const totalSettlements = dashboard.drivers.reduce(
    (sum, driver) => sum + driver.settlement,
    0,
  );
  const totalContribution = dashboard.drivers.reduce(
    (sum, driver) => sum + driver.contribution,
    0,
  );

  return (
    <>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <KpiCard
          label="Pay / gross shares"
          value={money.format(dashboard.totals.driverPay)}
          detail="Before owner-operator deductions"
          icon={Banknote}
          tone="text-blue-400"
        />
        <KpiCard
          label="Net settlements"
          value={money.format(totalSettlements)}
          detail="After applicable fuel and toll deductions"
          icon={ReceiptText}
          tone="text-emerald-400"
        />
        <KpiCard
          label="Company contribution"
          value={money.format(totalContribution)}
          detail="After driver-specific pay and company costs"
          icon={CircleDollarSign}
          tone={totalContribution >= 0 ? "text-emerald-400" : "text-red-400"}
        />
        <KpiCard
          label="Miles"
          value={number.format(dashboard.totals.miles)}
          detail={`${dashboard.totals.loadCount.toLocaleString()} qualified loads`}
          icon={Route}
          tone="text-violet-400"
        />
      </div>

      <section className="overflow-hidden rounded-xl border border-zinc-800/60 bg-card">
        <div className="flex flex-col gap-3 border-b border-zinc-800/50 px-4 py-4 sm:flex-row sm:items-center sm:justify-between sm:px-5">
          <div>
            <div className="flex items-center gap-2">
              <UserRound className="h-4 w-4 text-blue-400" />
              <h2 className="text-sm font-medium text-zinc-200">Weekly driver pay</h2>
              <span className="rounded-full bg-zinc-800/70 px-2 py-0.5 text-[10px] font-medium text-zinc-500">
                {drivers.length}
              </span>
            </div>
            <p className="mt-1 text-[11px] text-zinc-600">
              Percentage owner-operators pay their fuel and tolls; CPM owner-operators follow the company-paid model.
            </p>
          </div>
          <label className="flex items-center gap-2 rounded-lg border border-zinc-800 bg-zinc-950 px-2.5 py-1.5 focus-within:border-zinc-600">
            <Search className="h-3.5 w-3.5 text-zinc-600" />
            <input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Driver or load…"
              className="w-40 bg-transparent text-[12px] text-zinc-300 outline-none placeholder:text-zinc-700"
            />
          </label>
        </div>

        {drivers.length === 0 ? (
          <div className="px-5 py-12 text-center text-[12px] text-zinc-600">
            {search ? "No drivers match this search." : "No driver activity in this week."}
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[1280px] text-left text-[12px]">
              <thead>
                <tr className="border-b border-zinc-800/50 text-zinc-600">
                  <th className="px-4 py-3 font-medium sm:px-5">Driver / loads</th>
                  <th className="px-4 py-3 font-medium">Pay plan</th>
                  <th className="px-4 py-3 text-right font-medium">Gross</th>
                  <th className="px-4 py-3 text-right font-medium">Pay / share</th>
                  <th className="px-4 py-3 text-right font-medium">Fuel</th>
                  <th className="px-4 py-3 text-right font-medium">Tolls</th>
                  <th className="px-4 py-3 text-right font-medium">Net settlement</th>
                  <th className="px-4 py-3 text-right font-medium">Miles</th>
                  <th className="px-4 py-3 text-right font-medium">RPM</th>
                  <th className="px-4 py-3 text-right font-medium sm:pr-5">Company contribution</th>
                </tr>
              </thead>
              <tbody>
                {drivers.map((driver) => (
                  <tr
                    key={driver.driverId}
                    className="border-b border-zinc-900/80 text-zinc-400 transition-colors last:border-0 hover:bg-zinc-800/15"
                  >
                    <td className="px-4 py-3 sm:px-5">
                      <div className="flex items-center gap-2 font-medium text-zinc-200">
                        <span>{driver.driverName}</span>
                        {driver.isOwnerOperator && (
                          <span className="rounded-full border border-blue-500/20 bg-blue-500/5 px-1.5 py-0.5 text-[8px] font-semibold uppercase tracking-wider text-blue-400">
                            Owner-op
                          </span>
                        )}
                      </div>
                      <details className="group mt-1">
                        <summary className="flex w-fit cursor-pointer list-none items-center gap-1 text-[10px] text-zinc-600 hover:text-zinc-400">
                          <ChevronDown className="h-3 w-3 transition-transform group-open:rotate-180" />
                          {driver.loadCount.toLocaleString()} load{driver.loadCount === 1 ? "" : "s"}
                        </summary>
                        <div className="mt-2 flex max-w-72 flex-wrap gap-1">
                          {driver.loadNumbers.map((load) => (
                            <span
                              key={load}
                              className="rounded border border-zinc-800 bg-zinc-950 px-1.5 py-0.5 font-mono text-[9px] text-zinc-500"
                            >
                              {load}
                            </span>
                          ))}
                        </div>
                      </details>
                    </td>
                    <td className="px-4 py-3 text-zinc-500">
                      {payPlan(driver.payType, driver.payRate, driver.isOwnerOperator)}
                    </td>
                    <td className="px-4 py-3 text-right font-mono tabular-nums text-zinc-200">{money.format(driver.gross)}</td>
                    <td className="px-4 py-3 text-right font-mono tabular-nums">{money.format(driver.pay)}</td>
                    <td className="px-4 py-3 text-right font-mono tabular-nums">
                      <div>{money.format(driver.fuel)}</div>
                      {driver.deductsExpenses && driver.fuel > 0 && <div className="mt-0.5 text-[9px] text-blue-500">deducted</div>}
                    </td>
                    <td className="px-4 py-3 text-right font-mono tabular-nums">
                      <div>{money.format(driver.tolls)}</div>
                      {driver.deductsExpenses && driver.tolls > 0 && <div className="mt-0.5 text-[9px] text-blue-500">deducted</div>}
                    </td>
                    <td className="px-4 py-3 text-right font-mono font-medium tabular-nums text-zinc-200">{money.format(driver.settlement)}</td>
                    <td className="px-4 py-3 text-right font-mono tabular-nums">{number.format(driver.miles)}</td>
                    <td className="px-4 py-3 text-right font-mono tabular-nums">{preciseMoney.format(driver.revenuePerMile)}</td>
                    <td className={`px-4 py-3 text-right font-mono font-medium tabular-nums sm:pr-5 ${driver.contribution >= 0 ? "text-emerald-400" : "text-red-400"}`}>
                      {money.format(driver.contribution)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <details className="group rounded-xl border border-zinc-800/60 bg-zinc-950/20 px-4 py-3">
        <summary className="cursor-pointer list-none text-[11px] font-medium text-zinc-400">How driver pay is calculated</summary>
        <div className="mt-3 grid gap-2 text-[11px] leading-relaxed text-zinc-600 sm:grid-cols-2">
          <p><span className="text-zinc-400">Driver pay:</span> {dashboard.methodology.driverPay}</p>
          <p><span className="text-zinc-400">Week:</span> {dashboard.methodology.week}</p>
          <p><span className="text-zinc-400">Fuel:</span> {dashboard.methodology.fuel}</p>
          <p><span className="text-zinc-400">Tolls:</span> {dashboard.methodology.tolls}</p>
        </div>
      </details>
    </>
  );
}

function DispatcherPayReport({ dashboard }: { dashboard: FinancialDashboard }) {
  const configured = dashboard.dispatchers.filter((dispatcher) => dispatcher.pay != null);
  const totalPay = configured.reduce((sum, dispatcher) => sum + (dispatcher.pay ?? 0), 0);
  const totalGross = dashboard.dispatchers.reduce((sum, dispatcher) => sum + dispatcher.gross, 0);
  const totalLoads = dashboard.dispatchers.reduce((sum, dispatcher) => sum + dispatcher.loadCount, 0);

  return (
    <>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <KpiCard label="Dispatcher pay" value={money.format(totalPay)} detail="Calculated from configured commission rates" icon={Banknote} tone="text-violet-400" />
        <KpiCard label="Managed gross" value={money.format(totalGross)} detail="Qualified weekly load gross" icon={CircleDollarSign} tone="text-blue-400" />
        <KpiCard label="Dispatchers configured" value={`${configured.length} / ${dashboard.dispatchers.length}`} detail="With a commission percentage" icon={UsersRound} tone="text-emerald-400" />
        <KpiCard label="Loads" value={totalLoads.toLocaleString()} detail="Monday–Sunday by pickup date" icon={Route} tone="text-amber-400" />
      </div>

      <section className="overflow-hidden rounded-xl border border-zinc-800/60 bg-card">
        <div className="flex items-start justify-between gap-3 border-b border-zinc-800/50 px-4 py-4 sm:px-5">
          <div>
            <div className="flex items-center gap-2">
              <Headset className="h-4 w-4 text-violet-400" />
              <h2 className="text-sm font-medium text-zinc-200">Weekly dispatcher pay</h2>
            </div>
            <p className="mt-1 text-[11px] text-zinc-600">Pay equals each dispatcher’s configured commission percentage multiplied by managed gross.</p>
          </div>
          <Banknote className="h-4 w-4 text-zinc-600" />
        </div>

        {dashboard.dispatchers.length === 0 ? (
          <div className="px-5 py-12 text-center text-[12px] text-zinc-600">No dispatcher activity in this week.</div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[760px] text-left text-[12px]">
              <thead>
                <tr className="border-b border-zinc-800/50 text-zinc-600">
                  <th className="px-4 py-3 font-medium sm:px-5">Dispatcher</th>
                  <th className="px-4 py-3 text-right font-medium">Drivers</th>
                  <th className="px-4 py-3 text-right font-medium">Loads</th>
                  <th className="px-4 py-3 text-right font-medium">Managed gross</th>
                  <th className="px-4 py-3 text-right font-medium">Commission</th>
                  <th className="px-4 py-3 text-right font-medium sm:pr-5">Weekly pay</th>
                </tr>
              </thead>
              <tbody>
                {dashboard.dispatchers.map((dispatcher) => (
                  <tr key={dispatcher.dispatcherId ?? dispatcher.dispatcherName} className="border-b border-zinc-900/80 text-zinc-400 last:border-0 hover:bg-zinc-800/15">
                    <td className="px-4 py-3 font-medium text-zinc-200 sm:px-5">{dispatcher.dispatcherName}</td>
                    <td className="px-4 py-3 text-right font-mono tabular-nums">{dispatcher.driverCount}</td>
                    <td className="px-4 py-3 text-right font-mono tabular-nums">{dispatcher.loadCount}</td>
                    <td className="px-4 py-3 text-right font-mono tabular-nums text-zinc-200">{money.format(dispatcher.gross)}</td>
                    <td className="px-4 py-3 text-right font-mono tabular-nums">
                      {dispatcher.payPercentage == null ? "Not set" : `${dispatcher.payPercentage.toLocaleString(undefined, { maximumFractionDigits: 2 })}%`}
                    </td>
                    <td className={`px-4 py-3 text-right font-mono font-medium tabular-nums sm:pr-5 ${dispatcher.pay == null ? "text-amber-400" : "text-zinc-100"}`}>
                      {dispatcher.pay == null ? "Configure rate" : money.format(dispatcher.pay)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <div className="flex items-start gap-2 rounded-lg border border-blue-500/15 bg-blue-500/5 px-3 py-2.5">
        <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-blue-400" />
        <p className="text-[11px] leading-relaxed text-blue-200/70">
          Dispatcher commission rates are managed on the Dispatchers page. Unassigned loads and dispatchers without a rate are shown but excluded from total pay.
        </p>
      </div>
    </>
  );
}

export function WeeklyPayReport({ kind }: { kind: PayReportKind }) {
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
        setError(reason instanceof Error ? reason.message : "The weekly pay report could not be loaded.");
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
    setWeekStart(value);
    setIsLoading(true);
    try {
      const result = await fetchFinancialDashboard({ weekStart: value });
      setDashboard(result);
      setError("");
    } catch (reason) {
      setWeekStart(previousWeek);
      setError(reason instanceof Error ? reason.message : "The weekly pay report could not be loaded.");
    } finally {
      setIsLoading(false);
    }
  };

  const isDriver = kind === "driver";
  return (
    <div className="space-y-5 animate-fade-in">
      <header className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <div className="flex items-center gap-2.5">
            {isDriver ? <UserRound className="h-5 w-5 text-blue-400" /> : <Headset className="h-5 w-5 text-violet-400" />}
            <h1 className="text-lg font-semibold text-zinc-100">{isDriver ? "Driver pay" : "Dispatcher pay"}</h1>
          </div>
          <p className="mt-1.5 text-[12px] text-zinc-600">
            {isDriver ? "Weekly driver settlements and company contribution." : "Weekly dispatcher commission from managed load gross."}
          </p>
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
        <LoadingReport />
      ) : dashboard ? (
        <div className={`space-y-4 ${isLoading ? "opacity-60" : ""}`}>
          <div className="flex items-center gap-2 text-[11px] text-zinc-600">
            <CalendarDays className="h-3.5 w-3.5" />
            {dashboard.period.dateFrom ? `${weekLabel(dashboard.period.dateFrom)} · Monday–Sunday` : "No qualifying report weeks yet"}
          </div>
          {isDriver ? <DriverPayReport dashboard={dashboard} /> : <DispatcherPayReport dashboard={dashboard} />}
        </div>
      ) : null}
    </div>
  );
}
