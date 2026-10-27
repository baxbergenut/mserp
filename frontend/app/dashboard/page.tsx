"use client";
import { useEffect, useMemo, useState } from "react";
import { ArrowRight, BarChart3, Truck } from "lucide-react";
import Link from "@/app/components/AccessLink";
import { PageHeader } from "@/app/components/PageHeader";
import { controlClass } from "@/app/components/management/ManagementUI";
import { fetchCompanyDashboard } from "@/app/lib/api";
import type { CompanyDashboard, DashboardPreset, DashboardQuery } from "@/app/lib/dashboardTypes";
import { useViewState } from "@/app/lib/viewMemory";
import { money, number, range, Change, Panel, SectionError } from "./display";
import { PerformanceCards, PerformanceCharts } from "./Performance";
import { LoadDetails } from "./LoadDetails";

const presets: [DashboardPreset, string][] = [["thisWeek", "This week"], ["lastWeek", "Last week"], ["thisMonth", "This month"], ["lastMonth", "Last month"], ["custom", "Custom dates"]];
const colors: Record<string, string> = { ready: "text-emerald-400", in_use: "text-blue-400", shop: "text-amber-400", out_of_service: "text-red-400", terminated: "text-zinc-500", home: "text-violet-400", vacation: "text-violet-400", unassigned: "text-amber-400" };

export default function DashboardPage() {
  const [preset, setPreset] = useViewState<DashboardPreset>("dashboard:preset", "thisWeek");
  const [from, setFrom] = useViewState("dashboard:from", "");
  const [to, setTo] = useViewState("dashboard:to", "");
  const [custom, setCustom] = useViewState("dashboard:custom", { dateFrom: "", dateTo: "" });
  const query = useMemo<DashboardQuery>(() => preset === "custom" ? { preset, ...custom } : { preset }, [preset, custom]);
  const key = JSON.stringify(query);
  const [snapshot, setSnapshot] = useState<{ key: string; data: CompanyDashboard } | null>(null);
  const [failure, setFailure] = useState<{ key: string; message: string } | null>(null);
  const [retry, setRetry] = useState(0);
  const [details, setDetails] = useState<{ dispatcherId: string; inclusion: string } | null>(null);
  const data = snapshot?.key === key ? snapshot.data : null;
  const error = failure?.key === key ? failure.message : "";
  useEffect(() => {
    let active = true, running = false;
    const controller = new AbortController();
    const load = async () => {
      if (running) return;
      running = true;
      try {
        const next = await fetchCompanyDashboard(JSON.parse(key), controller.signal);
        if (active) {
          setSnapshot(old => {
            const sections = { ...next.sections };
            // Only preserve stale values for the same period and still-authorized sections.
            if (old?.key === key && old.data.period.dateFrom === next.period.dateFrom && old.data.period.dateTo === next.period.dateTo) {
              for (const [id, section] of Object.entries(sections)) if (section.error && old.data.sections[id]) sections[id] = { ...old.data.sections[id], error: section.error };
            }
            return { key, data: { ...next, sections } };
          });
          setFailure(null);
        }
      } catch (e) { if (active) setFailure({ key, message: e instanceof Error ? e.message : "Dashboard could not be loaded" }); }
      finally { running = false; }
    };
    void load();
    const visible = () => { if (document.visibilityState === "visible") void load(); };
    const timer = setInterval(visible, 60000);
    window.addEventListener("focus", visible); document.addEventListener("visibilitychange", visible);
    return () => { active = false; controller.abort(); clearInterval(timer); window.removeEventListener("focus", visible); document.removeEventListener("visibilitychange", visible); };
  }, [key, retry]);
  const performance = data?.sections.performance;
  const report = performance?.performance;
  const sections = Object.entries(data?.sections ?? {});
  const fleet = sections.filter(([, s]) => s.kind === "fleet").sort(([a], [b]) => ["trucks", "trailers", "drivers"].indexOf(a) - ["trucks", "trailers", "drivers"].indexOf(b));
  const costs = sections.filter(([, s]) => s.kind === "cost").sort(([a], [b]) => ["diesel", "tolls", "scale", "expenses"].indexOf(a) - ["diesel", "tolls", "scale", "expenses"].indexOf(b));
  const attention = sections.filter(([, s]) => s.kind === "attention").sort(([a], [b]) => a.localeCompare(b));
  const open = (dispatcherId = "", inclusion = "") => setDetails({ dispatcherId, inclusion });
  const choosePreset = (value: DashboardPreset) => {
    if (value === "custom" && data) { setFrom(data.period.dateFrom); setTo(data.period.dateTo); setCustom({ dateFrom: data.period.dateFrom, dateTo: data.period.dateTo }); }
    setPreset(value); setDetails(null);
  };
  const datesValid = /^\d{4}-\d{2}-\d{2}$/.test(from) && /^\d{4}-\d{2}-\d{2}$/.test(to) && from <= to && (Date.parse(to) - Date.parse(from)) / 86400000 < 366;
  const latest = sections.map(([, s]) => s.updatedAt).sort().at(-1);
  const hasErrors = !!error || sections.some(([, s]) => !!s.error);
  return <div className="space-y-5">
    <PageHeader><h1 className="text-lg font-semibold">Dashboard</h1></PageHeader>
    <header className="flex flex-wrap items-center justify-between gap-3"><div><h1 className="text-xl font-semibold text-zinc-100">Company overview</h1><p className="mt-1 text-xs text-zinc-500">{data ? `${range(data.period.dateFrom, data.period.dateTo)} · New York business dates` : "Gross, operations and the next things to address"}</p></div><div className="flex flex-wrap items-center gap-2">
      <select aria-label="Reporting period" value={preset} onChange={e => choosePreset(e.target.value as DashboardPreset)} className={`${controlClass} !w-auto max-w-full`}>{presets.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select>
      {preset === "custom" && <form className="flex flex-wrap items-center gap-2" onSubmit={e => { e.preventDefault(); if (datesValid) { setCustom({ dateFrom: from, dateTo: to }); setDetails(null); } }}><input aria-label="Dashboard start date" type="date" required min="2000-01-01" max="2100-12-31" value={from} onChange={e => setFrom(e.target.value)} className={`${controlClass} !w-auto max-w-full`} /><span aria-hidden="true">→</span><input aria-label="Dashboard end date" type="date" required min="2000-01-01" max="2100-12-31" value={to} onChange={e => setTo(e.target.value)} className={`${controlClass} !w-auto max-w-full`} /><button className="ui-button" disabled={!datesValid}>Apply</button>{!datesValid && <span className="text-xs text-amber-400">Choose up to 366 days.</span>}</form>}
    </div></header>
    {error && <p role="alert" className="text-sm text-amber-400">{error}{data ? " · Showing the last available results." : ""}</p>}
    {hasErrors && <button onClick={() => setRetry(v => v + 1)} className="ui-button">Retry unavailable sections</button>}
    {!data && !error && <div role="status" aria-label="Loading dashboard" className="grid animate-pulse gap-3 sm:grid-cols-5">{Array.from({ length: 5 }, (_, i) => <div key={i} className="h-28 rounded-xl bg-zinc-800/40" />)}</div>}
    {data && sections.length === 0 && <Panel title="Your dashboard is ready"><p className="mt-2 text-sm text-zinc-400">No dashboard sections are available with your current access. Your administrator can grant access to the relevant areas.</p></Panel>}
    {performance && <SectionError section={performance} />}
    {report && data && <PerformanceCards report={report} period={data.period} open={open} />}
    {(fleet.length > 0 || attention.length > 0) && <div className={`grid items-start gap-4 ${fleet.length > 0 && attention.length > 0 ? "xl:grid-cols-[minmax(0,2fr)_minmax(280px,1fr)]" : ""}`}>{fleet.length > 0 && <div><h2 className="mb-3 flex items-center gap-2 text-sm font-semibold text-zinc-200"><Truck size={16} />Right now <span className="font-normal text-zinc-500">Current fleet, independent of reporting dates</span></h2><div className="grid gap-3 md:grid-cols-3">{fleet.map(([id, section]) => <Panel key={id} title={section.title}><SectionError section={section} /><div className="mt-3 space-y-1">{section.items.map(item => <Link key={item.id} href={item.href} className="flex items-center justify-between gap-2 rounded px-1 py-1.5 text-xs hover:bg-zinc-800/40"><span className="text-zinc-400">{item.label}</span><strong className={`tabular-nums ${colors[item.id] ?? "text-zinc-200"}`}>{number(item.count)}</strong></Link>)}</div></Panel>)}</div></div>}
      {attention.length > 0 && <Panel title="Needs attention" subtitle="Current items you can access"><div className="mt-3 space-y-1">{attention.map(([id, section]) => <div key={id}><SectionError section={section} />{section.items.filter(item => item.count > 0).map(item => <Link key={item.id} href={item.href} title={item.note} className="flex items-center justify-between gap-3 border-b border-zinc-800/50 py-2 text-xs hover:text-blue-400"><span>{item.label}</span><span className="flex shrink-0 items-center gap-2"><strong className="rounded bg-amber-500/10 px-2 py-0.5 tabular-nums text-amber-400">{number(item.count)}</strong><ArrowRight size={12} /></span></Link>)}</div>)}{attention.length > 0 && attention.every(([, s]) => !s.error && s.items.every(i => i.count === 0)) && <p className="py-4 text-sm text-zinc-500">No current attention items in the sections you can access.</p>}{attention.length === 0 && <p className="text-xs text-zinc-500">No attention sections available with your access.</p>}</div></Panel>}
    </div>}
    {report && data && <PerformanceCharts report={report} period={data.period} open={open} />}
    {costs.length > 0 && <Panel title="Recorded costs" subtitle="Separate source totals. These can overlap and do not establish company liability or payment."><div className="mt-3 grid gap-4 sm:grid-cols-2 xl:grid-cols-4">{costs.map(([id, section]) => <div key={id}><h3 className="text-xs font-medium text-zinc-500">{section.title}</h3><SectionError section={section} />{section.items.map(item => <Link key={item.id} href={item.href} className="mt-2 block rounded-lg border border-zinc-800/60 p-3 hover:border-blue-500/40"><div className="flex items-center justify-between gap-2"><span className="text-xs text-zinc-400">{item.label}</span><ArrowRight size={12} /></div><strong className="my-1 block text-lg tabular-nums">{money(item.amount)}</strong><Change current={item.amount ?? null} previous={item.previousAmount ?? null} /><span className="mt-2 block text-[11px] text-zinc-500">{item.count} records{item.incomplete ? ` · ${item.incomplete} incomplete` : ""}</span><span className="mt-1 block text-[11px] text-zinc-500">{item.note}</span></Link>)}{!section.error && section.items.length === 0 && <p className="mt-3 text-xs text-zinc-500">No recorded expenses in the selected or comparison period.</p>}</div>)}</div></Panel>}
    {latest && <p className="flex items-center gap-2 text-[11px] text-zinc-500"><BarChart3 size={13} />Last refreshed {new Date(latest).toLocaleString()} · Local records and cached provider data · Updates every minute while visible</p>}
    {details && data && <LoadDetails key={`${key}:${details.dispatcherId}:${details.inclusion}`} period={data.period} selection={details} current={report?.current} onClose={() => setDetails(null)} />}
  </div>;
}
