"use client";
import type { ReactNode } from "react";

import { PageHeader } from "@/app/components/PageHeader";



import { useViewState } from "@/app/lib/viewMemory";

import { useEffect, useRef, useState } from "react";
import {
  AlertCircle,
  Banknote,
  CalendarDays,
  CircleDollarSign,
  Headset,
  Route,
  UserRound,
  UsersRound,
} from "lucide-react";
import { fetchDispatcherPay, type DispatcherPayReport } from "@/app/lib/api";




const money = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
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

function DispatcherPayReport({ dashboard, controls }: { dashboard: DispatcherPayReport; controls: ReactNode }) {
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
      {controls}

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

export function WeeklyPayReport() {
  const [weekStart, setWeekStart] = useViewState("WeeklyPayReport:weekStart", "");
  const initialWeek = useRef(weekStart);
  const [dashboard, setDashboard] = useState<DispatcherPayReport | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    fetchDispatcherPay({ weekStart: initialWeek.current || undefined })
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
  }, [setWeekStart]);

  const selectWeek = async (value: string) => {
    if (!value || value === weekStart) return;
    const previousWeek = weekStart;
    setWeekStart(value);
    setIsLoading(true);
    try {
      const result = await fetchDispatcherPay({ weekStart: value });
      setDashboard(result);
      setError("");
    } catch (reason) {
      setWeekStart(previousWeek);
      setError(reason instanceof Error ? reason.message : "The weekly pay report could not be loaded.");
    } finally {
      setIsLoading(false);
    }
  };

  const isDriver = false;
  const controls = <div className="flex flex-wrap items-center gap-2">{dashboard && dashboard.availableWeeks.length > 0 && (
          <label className="flex items-center gap-2 rounded-lg border border-zinc-800 bg-zinc-950 px-2.5 py-1.5">
            <CalendarDays className="h-3.5 w-3.5 text-zinc-600" />
            <select value={weekStart} onChange={(event) => void selectWeek(event.target.value)} className="bg-transparent text-[12px] text-zinc-300 outline-none" aria-label="Select weekly report">
              {dashboard.availableWeeks.map((week) => <option key={week} value={week}>{weekLabel(week)}</option>)}
            </select>
          </label>
        )}</div>;
  return (
    <div className="space-y-5 animate-fade-in">
      <PageHeader><div>
          <div className="flex items-center gap-2.5">
            {isDriver ? <UserRound className="h-5 w-5 text-blue-400" /> : <Headset className="h-5 w-5 text-violet-400" />}
            <h1 className="text-lg font-semibold text-zinc-100">{isDriver ? "Driver pay" : "Dispatcher pay"}</h1>
          </div>

        </div></PageHeader>

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
          <DispatcherPayReport dashboard={dashboard} controls={controls} />
        </div>
      ) : null}
    </div>
  );
}
