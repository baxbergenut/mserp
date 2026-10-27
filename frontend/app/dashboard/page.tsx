"use client";
import { useEffect, useMemo, useState } from "react";
import { ArrowRight, ChevronLeft, ChevronRight } from "lucide-react";
import Link from "@/app/components/AccessLink";
import { PageHeader } from "@/app/components/PageHeader";
import { controlClass } from "@/app/components/management/ManagementUI";
import { fetchCompanyDashboard } from "@/app/lib/api";
import type { CompanyDashboard, DashboardQuery } from "@/app/lib/dashboardTypes";
import { useViewState } from "@/app/lib/viewMemory";
import { money, number, range, Change, Panel, SectionError } from "./display";
import { PerformanceCards, PerformanceCharts } from "./Performance";
import { CostReview } from "./CostReview";
import { LoadDetails } from "./LoadDetails";
import { FleetStatusPie } from "./FleetStatusPie";
import { ProfitWaterfall } from "./ProfitWaterfall";
import { addDays } from "@/app/gross-board/board";

export default function DashboardPage() {
  const [view, setView] = useViewState<"weekly" | "current">("dashboard:view", "weekly");
  const [week, setWeek] = useViewState("dashboard:week", "");
  const query = useMemo<DashboardQuery>(() => ({ preset: "week", dateFrom: week || undefined, view }), [week, view]);
  const key = JSON.stringify(query);
  const [snapshot, setSnapshot] = useState<{ key: string; data: CompanyDashboard } | null>(null);
  const [failure, setFailure] = useState<{ key: string; message: string } | null>(null);
  const [retry, setRetry] = useState(0);
  const [costReview, setCostReview] = useState<"fuel" | "tolls" | "ownership" | null>(null);
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
  const fleet = (["trucks", "drivers", "trailers"] as const).flatMap(id => data?.sections[id] ? [{ id, section: data.sections[id] }] : []);
  const costs = sections.filter(([, s]) => s.kind === "cost").sort(([a], [b]) => ["diesel", "tolls", "scale", "expenses"].indexOf(a) - ["diesel", "tolls", "scale", "expenses"].indexOf(b));
  const attention = sections.filter(([, s]) => s.kind === "attention").sort(([a], [b]) => a.localeCompare(b));
  const open = (dispatcherId = "", inclusion = "") => setDetails({ dispatcherId, inclusion });
  const chooseWeek = (value: string) => { setWeek(value); setDetails(null); setCostReview(null); };
  const latest = sections.map(([, s]) => s.updatedAt).sort().at(-1);
  const hasErrors = !!error || sections.some(([, s]) => !!s.error);
  return <div className="space-y-5">
    <PageHeader><h1 className="text-lg font-semibold">Dashboard</h1></PageHeader>
    <div role="tablist" aria-label="Dashboard view" className="flex gap-5 border-b border-zinc-800">
      {([["weekly", "Weekly"], ["current", "Right now"]] as const).map(([id, label]) => <button key={id} role="tab" aria-selected={view === id} aria-controls={`dashboard-${id}`} id={`dashboard-tab-${id}`} onClick={() => { setView(id); setDetails(null); setCostReview(null); }} className="pb-3 text-sm">{label}</button>)}
    </div>
    {view === "weekly" && <header className="flex flex-wrap items-center justify-between gap-3"><h2 className="text-sm font-semibold">{data ? range(data.period.dateFrom, data.period.dateTo) : "Weekly overview"}</h2><div className="flex flex-wrap items-center gap-2">
      <button aria-label="Previous week" className="ui-button" disabled={!data} onClick={() => data && chooseWeek(addDays(data.period.dateFrom, -7))}><ChevronLeft size={16} /></button>
      <input aria-label="Week of" type="date" min="2000-01-03" max="2099-12-28" value={data?.period.dateFrom ?? week} onChange={e => { if (/^\d{4}-\d{2}-\d{2}$/.test(e.target.value)) chooseWeek(e.target.value); }} className={`${controlClass} !w-auto`} />
      <button aria-label="Next week" className="ui-button" disabled={!data} onClick={() => data && chooseWeek(addDays(data.period.dateFrom, 7))}><ChevronRight size={16} /></button>
      <button className="ui-button whitespace-nowrap" onClick={() => chooseWeek("")}>This week</button>
    </div></header>}
    <div role="tabpanel" id={`dashboard-${view}`} aria-labelledby={`dashboard-tab-${view}`} className="space-y-4">
    {error && <p role="alert" className="text-sm text-amber-400">{error}{data ? " · Showing the last available results." : ""}</p>}
    {hasErrors && <button onClick={() => setRetry(v => v + 1)} className="ui-button">Retry unavailable sections</button>}
    {!data && !error && <div role="status" aria-label="Loading dashboard" className="grid animate-pulse gap-3 sm:grid-cols-2 xl:grid-cols-4">{Array.from({ length: 4 }, (_, i) => <div key={i} className="h-28 rounded-xl bg-zinc-800/40" />)}</div>}
    {data && sections.length === 0 && <Panel title="Your dashboard is ready"><p className="mt-2 text-sm text-zinc-400">No dashboard sections are available with your access.</p></Panel>}
    {view === "weekly" && performance && <SectionError section={performance} />}
    {view === "weekly" && data?.sections.waterfall && <ProfitWaterfall section={data.sections.waterfall} reconcile={() => open()} reviewCosts={setCostReview} />}
    {view === "weekly" && report && data && <PerformanceCards report={report} period={data.period} open={open} />}
    {view === "current" && (fleet.length > 0 || attention.length > 0) && <div className="space-y-4">{fleet.length > 0 && <div className="grid items-start gap-3 md:grid-cols-2 xl:grid-cols-3">{fleet.map(({id, section}) => <FleetStatusPie key={id} kind={id} section={section} />)}</div>}
      {attention.length > 0 && <Panel title="Needs attention"><div className="mt-3 space-y-1">{attention.map(([id, section]) => <div key={id}><SectionError section={section} />{section.items.filter(item => item.count > 0).map(item => <Link key={item.id} href={item.href} title={item.note} className="flex items-center justify-between gap-3 border-b border-zinc-800/50 py-2 text-xs hover:text-blue-400"><span>{item.label}</span><span className="flex shrink-0 items-center gap-2"><strong className="rounded bg-amber-500/10 px-2 py-0.5 tabular-nums text-amber-400">{number(item.count)}</strong><ArrowRight size={12} /></span></Link>)}</div>)}{attention.length > 0 && attention.every(([, s]) => !s.error && s.items.every(i => i.count === 0)) && <p className="py-4 text-sm text-zinc-500">Nothing needs attention.</p>}{attention.length === 0 && <p className="text-xs text-zinc-500">No attention sections available with your access.</p>}</div></Panel>}
    </div>}
    {view === "weekly" && report && data && <PerformanceCharts report={report} period={data.period} open={open} />}
    {view === "weekly" && costs.length > 0 && <Panel title="Recorded costs" subtitle="All trucks"><div className="mt-3 grid gap-4 sm:grid-cols-2 xl:grid-cols-4">{costs.map(([id, section]) => <div key={id}><h3 className="text-xs font-medium text-zinc-500">{section.title}</h3><SectionError section={section} />{section.items.map(item => <Link key={item.id} href={item.href} title={item.note} className="mt-2 block rounded-lg border border-zinc-800/60 p-3 hover:border-blue-500/40"><div className="flex items-center justify-between gap-2"><span className="text-xs text-zinc-400">{item.label}</span><ArrowRight size={12} /></div><strong className="my-1 block text-lg tabular-nums">{money(item.amount)}</strong><Change current={item.amount ?? null} previous={item.previousAmount ?? null} /><span className="mt-2 block text-[11px] text-zinc-500">{item.count} records{item.incomplete ? ` · ${item.incomplete} incomplete` : ""}</span></Link>)}{!section.error && section.items.length === 0 && <p className="mt-3 text-xs text-zinc-500">No expenses.</p>}</div>)}</div></Panel>}
    </div>
    {latest && <p className="text-[11px] text-zinc-500">Updated {new Date(latest).toLocaleTimeString()}</p>}
    {costReview && data?.sections.waterfall && <CostReview key={`${key}:${costReview}`} kind={costReview} period={data.period} onClose={() => setCostReview(null)} />}
    {details && data && <LoadDetails key={`${key}:${details.dispatcherId}:${details.inclusion}`} period={data.period} selection={details} current={report?.current} onClose={() => setDetails(null)} />}
  </div>;
}
