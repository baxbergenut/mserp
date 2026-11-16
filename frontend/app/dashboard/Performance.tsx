"use client";
import { useMemo, useState } from "react";
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import type { DashboardDispatcher, DashboardPeriod, DashboardPerformance } from "@/app/lib/dashboardTypes";
import { addDays } from "@/app/gross-board/board";
import { money, number, Panel, Change } from "./display";

const metricLabels = { gross: "Gross", miles: "Miles", rpm: "RPM", grossPerActiveTruck: "GPT" };
const trendMetrics = ["gross", "miles", "rpm"] as const;
const dispatcherMetrics = ["gross", "rpm", "miles", "grossPerActiveTruck"] as const;
type TrendMetric = (typeof trendMetrics)[number];
type DispatcherMetric = (typeof dispatcherMetrics)[number];

function MetricControls<T extends DispatcherMetric>({ label, options, selected, onSelect }: {
  label: string;
  options: readonly T[];
  selected: T;
  onSelect: (metric: T) => void;
}) {
  return <div role="group" aria-label={label} className="flex shrink-0 gap-0.5 sm:gap-1">
    {options.map(metric => <button key={metric} type="button" aria-pressed={selected === metric}
      title={metric === "grossPerActiveTruck" ? "Gross per active truck" : undefined}
      onClick={() => onSelect(metric)}
      className={`rounded px-1.5 py-1 text-xs sm:px-2 ${selected === metric ? "bg-blue-500/15 text-blue-400" : "text-zinc-500 hover:bg-zinc-800/40"}`}>
      {metricLabels[metric]}
    </button>)}
  </div>;
}

export function PerformanceCards({ report }: { report: DashboardPerformance }) {
  return <dl className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">{([
    ["Original Gross", money(report.current.gross), report.current.gross, report.previous.gross],
    ["Counted loads", number(report.current.loads), report.current.loads, report.previous.loads],
    ["Recorded miles", number(report.current.miles), report.current.miles, report.previous.miles],
    ["RPM", money(report.current.rpm), report.current.rpm, report.previous.rpm],
  ] as const).map(([label, value, current, previous], i) => <div key={label} className={`rounded-xl border p-4 ${i === 0 ? "border-blue-500/40 bg-blue-500/5" : "border-zinc-800 bg-card"}`}>
    <dt className="text-xs text-zinc-400">{label}</dt>
    <dd><strong className="mt-2 block text-2xl font-semibold tabular-nums text-zinc-100">{value}</strong><div className="mt-2"><Change current={current} previous={previous} /></div></dd>
  </div>)}</dl>;
}
function DispatcherChart({ dispatchers }: { dispatchers: DashboardDispatcher[] }) {
  const [metric, setMetric] = useState<DispatcherMetric>("grossPerActiveTruck");
  const ranked = dispatchers.map(row => ({ ...row, value: row[metric] == null ? null : Number(row[metric]) }))
    .sort((a, b) => {
      if (a.value !== null && b.value !== null && a.value !== b.value) return b.value - a.value;
      if (a.value === null && b.value !== null) return 1;
      if (a.value !== null && b.value === null) return -1;
      return a.name.localeCompare(b.name) || a.id.localeCompare(b.id);
    });
  const min = Math.min(0, ...ranked.map(row => row.value ?? 0));
  const max = Math.max(0, ...ranked.map(row => row.value ?? 0));
  const span = max - min || 1;
  const medals = ["🥇", "🥈", "🥉"];
  let rank = 0;

  return <Panel title="Dispatcher breakdown" actions={
    <MetricControls label="Dispatcher metric" options={dispatcherMetrics} selected={metric} onSelect={setMetric} />
  }>
    {ranked.length === 0 ? <p className="py-6 text-center text-sm text-zinc-500">No Gross Board entries in this period.</p> :
      <div className="mt-2 max-h-60 overflow-y-auto">
        <ol role="list" aria-label={metric === "grossPerActiveTruck" ? "Dispatchers by gross per active truck" : `Dispatchers by ${metricLabels[metric]}`}
          className="relative" style={{ height: `${ranked.length * 2.5}rem` }}>
          {ranked.map((row, index) => {
            const medal = row.id && row.value !== null ? medals[rank++] : undefined;
            const left = ((Math.min(row.value ?? 0, 0) - min) / span) * 100;
            const width = (Math.abs(row.value ?? 0) / span) * 100;
            return <li key={row.id || row.name}
              className="absolute inset-x-0 top-0 grid h-10 grid-cols-[minmax(0,1fr)_minmax(0,1.5fr)_6rem] items-center gap-2 border-b border-zinc-800/40 text-xs motion-safe:transition-transform motion-safe:duration-300"
              style={{ transform: `translateY(${index * 100}%)` }}>
              <div className="flex min-w-0 items-center gap-1">
                <span className="truncate text-zinc-300" title={row.name}>{row.name}</span>
                {medal && <span aria-hidden="true" className="shrink-0">{medal}</span>}
              </div>
              <div aria-hidden="true" className="relative h-3 overflow-hidden rounded-sm bg-zinc-800/30">
                <div className="absolute inset-y-0 rounded-sm bg-[var(--accent)] motion-safe:transition-[width,left] motion-safe:duration-300"
                  style={{ left: `${left}%`, width: `${width}%` }} />
              </div>
              <span className="text-right tabular-nums" title={row.value === null ? "Unavailable" : undefined}>
                {metric === "miles" ? number(row.miles) : money(row[metric])}
              </span>
            </li>;
          })}
        </ol>
      </div>}
  </Panel>;
}

export function PerformanceCharts({ report, period }: { report: DashboardPerformance; period: DashboardPeriod }) {
  const [metric, setMetric] = useState<TrendMetric>("gross");
  const metricLabel = { gross: "Original Gross", miles: "Recorded miles", rpm: "RPM" }[metric];
  const formatMetric = (value: string | number | null) => metric === "miles" ? number(value) : money(value === null ? null : String(value));
  const trend = useMemo(() => {
    const rows = new Map(report.trend.map(row => [row.id, row]));
    let day = period.dateFrom;
    if (period.bucket === "week") day = addDays(day, -(new Date(`${day}T12:00:00Z`).getUTCDay() + 6) % 7);
    const values = [];
    while (day <= period.dateTo) { const row = rows.get(day); values.push({ date: day, gross: Number(row?.gross ?? 0), miles: Number(row?.miles ?? 0), rpm: row?.rpm == null ? null : Number(row.rpm) }); day = addDays(day, period.bucket === "week" ? 7 : 1); }
    return values;
  }, [period, report.trend]);
  return <div className="grid gap-4 xl:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
    <Panel title={`${metricLabel} trend`} actions={
      <MetricControls label="Trend metric" options={trendMetrics} selected={metric} onSelect={setMetric} />
    }>
      <div className="mt-2 h-60" role="img" aria-label={`${metricLabel} trend, ${formatMetric(report.current[metric])} for the selected period`}>
        <ResponsiveContainer width="100%" height="100%">
          <AreaChart data={trend} margin={{ left: 2, right: 10, top: 10, bottom: 0 }}>
            <CartesianGrid vertical={false} stroke="var(--border-color)" strokeOpacity={0.2} />
            <XAxis dataKey="date" tickFormatter={date => `${date.slice(5, 7)}/${date.slice(8, 10)}`}
              tick={{ fontSize: 11, fill: "var(--muted)" }} minTickGap={30} />
            <YAxis tickFormatter={n => metric === "gross" ? `$${number(Number(n) / 1000)}k` : formatMetric(n)}
              tick={{ fontSize: 11, fill: "var(--muted)" }} width={60} />
            <Tooltip formatter={value => [formatMetric(value == null ? null : String(value)), metricLabel]}
              labelFormatter={label => String(label)}
              contentStyle={{ background: "var(--background)", border: "1px solid var(--border-color)", borderRadius: 8 }} />
            <Area type="linear" dataKey={metric} stroke="var(--accent)" strokeWidth={2} fill="var(--accent)" fillOpacity={0.12} isAnimationActive={false} />
          </AreaChart>
        </ResponsiveContainer>
      </div>
    </Panel>
    <DispatcherChart dispatchers={report.dispatchers} />
  </div>;
}
