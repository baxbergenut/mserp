"use client";
import { useMemo, useState } from "react";
import { CircleAlert } from "lucide-react";
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import type { DashboardPeriod, DashboardPerformance } from "@/app/lib/dashboardTypes";
import { addDays } from "@/app/gross-board/board";
import { money, number, range, Panel, Change } from "./display";

export function PerformanceCards({ report, period, open }: { report: DashboardPerformance; period: DashboardPeriod; open: (dispatcher?: string, inclusion?: string) => void }) {
  return <><div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">{([
    ["Original Gross", money(report.current.gross), report.current.gross, report.previous.gross, "Customer/load amount recorded on Gross Board"],
    ["Counted loads", number(report.current.loads), report.current.loads, report.previous.loads, "Resolved loads counted once"],
    ["Recorded miles", number(report.current.miles), report.current.miles, report.previous.miles, `${report.current.missingMiles} loads without positive mileage`],
    ["RPM", money(report.current.rpm), report.current.rpm, report.previous.rpm, `${report.current.pairedLoads} loads with paired gross and miles`],
  ] as const).map(([label, value, current, previous, note], i) => <button key={label} onClick={() => open("", "included")} className={`rounded-xl border p-4 text-left transition-colors hover:border-blue-400/60 ${i === 0 ? "border-blue-500/40 bg-blue-500/5" : "border-zinc-800 bg-card"}`}><span className="text-xs text-zinc-400">{label}</span><strong className="mt-2 block text-2xl font-semibold tabular-nums text-zinc-100">{value}</strong><div className="mt-2"><Change current={current} previous={previous} /></div><span className="mt-2 block text-[11px] text-zinc-500">{note}</span></button>)}</div>
    <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-zinc-500"><span>Compared with {range(period.compareFrom, period.compareTo)}</span><span>Recorded gross includes plans; it is not collected revenue.</span><button className="text-blue-400 hover:underline" onClick={() => open()}>Reconcile totals →</button></div>
    {(report.current.provisional + report.current.conflicts + report.current.repeats + report.current.missingGross > 0) && <div className="flex flex-wrap gap-3 rounded-lg border border-amber-500/20 bg-amber-500/5 px-3 py-2 text-xs text-amber-400"><CircleAlert size={15} />
      {report.current.provisional > 0 && <button className="hover:underline" onClick={() => open("", "provisional")}>{report.current.provisional} provisional rows · {money(report.current.provisionalGross)}</button>}
      {report.current.conflicts > 0 && <button className="hover:underline" onClick={() => open("", "conflict")}>{report.current.conflicts} conflicting rows excluded</button>}
      {report.current.repeats > 0 && <button className="hover:underline" onClick={() => open("", "repeat")}>{report.current.repeats} repeated rows excluded</button>}
      {report.current.missingGross > 0 && <button className="hover:underline" onClick={() => open("", "included")}>Partial totals: {report.current.missingGross} missing original gross</button>}
    </div>}
  </>;
}
export function PerformanceCharts({ report, period, open }: { report: DashboardPerformance; period: DashboardPeriod; open: (dispatcher?: string) => void }) {
  const [metric, setMetric] = useState<"gross" | "miles" | "rpm">("gross");
  const metricLabel = { gross: "Original Gross", miles: "Recorded miles", rpm: "RPM" }[metric];
  const formatMetric = (value: string | number | null) => metric === "miles" ? number(value) : money(value === null ? null : String(value));
  const [sort, setSort] = useState<{ key: "name" | "gross" | "loads" | "miles" | "rpm"; descending: boolean }>({ key: "gross", descending: true });
  const dispatchers = [...report.dispatchers].sort((a, b) => { const diff = sort.key === "name" ? a.name.localeCompare(b.name) : Number(a[sort.key] ?? -Infinity) - Number(b[sort.key] ?? -Infinity); return (sort.descending ? -diff : diff) || a.id.localeCompare(b.id); });
  const trend = useMemo(() => {
    const rows = new Map(report.trend.map(row => [row.id, row]));
    let day = period.dateFrom;
    if (period.bucket === "week") day = addDays(day, -(new Date(`${day}T12:00:00Z`).getUTCDay() + 6) % 7);
    const values = [];
    while (day <= period.dateTo) { const row = rows.get(day); values.push({ date: day, gross: Number(row?.gross ?? 0), miles: Number(row?.miles ?? 0), rpm: row?.rpm == null ? null : Number(row.rpm) }); day = addDays(day, period.bucket === "week" ? 7 : 1); }
    return values;
  }, [period, report.trend]);
  return <div className="grid gap-4 xl:grid-cols-2"><Panel title={`${metricLabel} trend`} subtitle={`${period.bucket === "week" ? "Weekly" : "Daily"} totals · Resolved loads counted once`}><div className="mt-3 flex gap-1" role="group" aria-label="Trend metric">{(["gross", "miles", "rpm"] as const).map(value => <button key={value} aria-pressed={metric === value} onClick={() => setMetric(value)} className={`rounded px-2 py-1 text-xs ${metric === value ? "bg-blue-500/15 text-blue-400" : "text-zinc-500 hover:bg-zinc-800/40"}`}>{({gross:"Gross",miles:"Miles",rpm:"RPM"})[value]}</button>)}</div><div className="mt-2 h-60" role="img" aria-label={`${metricLabel} trend, ${formatMetric(report.current[metric])} for the selected period`}><ResponsiveContainer width="100%" height="100%"><AreaChart data={trend} margin={{ left: 2, right: 10, top: 10, bottom: 0 }}><CartesianGrid vertical={false} stroke="#71717a" strokeOpacity={0.2} /><XAxis dataKey="date" tickFormatter={date => `${date.slice(5, 7)}/${date.slice(8, 10)}`} tick={{ fontSize: 11, fill: "#71717a" }} minTickGap={30} /><YAxis tickFormatter={n => metric === "gross" ? `$${number(Number(n) / 1000)}k` : metric === "rpm" ? money(String(n)) : number(n)} tick={{ fontSize: 11, fill: "#71717a" }} width={60} /><Tooltip formatter={value => [formatMetric(value == null ? null : String(value)), metricLabel]} labelFormatter={label => String(label)} contentStyle={{ background: "var(--background)", border: "1px solid #71717a", borderRadius: 8 }} /><Area type="linear" dataKey={metric} stroke="#3b82f6" strokeWidth={2} fill="#3b82f6" fillOpacity={0.12} isAnimationActive={false} /></AreaChart></ResponsiveContainer></div></Panel>
    <Panel title="Dispatcher breakdown" subtitle="Historical assignment on each Gross Board date"><div className="mt-3 max-h-64 overflow-auto"><table className="w-full text-xs"><thead><tr>{([["name", "Dispatcher"], ["gross", "Original Gross"], ["loads", "Loads"], ["miles", "Miles"], ["rpm", "RPM"]] as const).map(([field, label]) => <th key={field} aria-sort={sort.key === field ? sort.descending ? "descending" : "ascending" : "none"} className="border-b border-zinc-800 px-2 py-2 text-left font-medium text-zinc-500"><button onClick={() => setSort({ key: field, descending: sort.key === field ? !sort.descending : field !== "name" })}>{label}{sort.key === field ? sort.descending ? " ↓" : " ↑" : ""}</button></th>)}</tr></thead><tbody>{dispatchers.map(row => <tr key={row.id} className="border-b border-zinc-800/40"><td className="px-2 py-3"><button className="text-left text-blue-400 hover:underline" onClick={() => open(row.id || "unassigned")}>{row.name}</button>{row.conflicts + row.provisional + row.missingGross + row.missingMiles > 0 && <span className="block text-[10px] text-amber-400">Incomplete / review</span>}</td><td className="px-2 tabular-nums">{money(row.gross)}</td><td className="px-2 tabular-nums">{row.loads}</td><td className="px-2 tabular-nums">{number(row.miles)}</td><td className="px-2 tabular-nums">{money(row.rpm)}</td></tr>)}</tbody></table>{dispatchers.length === 0 && <p className="py-6 text-center text-sm text-zinc-500">No Gross Board entries in this period.</p>}</div></Panel>
  </div>;
}
