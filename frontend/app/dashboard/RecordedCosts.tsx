import { ArrowRight } from "lucide-react";
import Link from "@/app/components/AccessLink";
import type { DashboardItem, DashboardSection } from "@/app/lib/dashboardTypes";
import { Change, money, Panel, SectionError } from "./display";

function CostCard({ item }: { item: DashboardItem }) {
  return <Link href={item.href} title={item.note} className="block min-w-0 rounded-lg border border-zinc-800/60 p-3 hover:border-blue-500/40">
    <div className="flex items-center justify-between gap-2">
      <span className="text-xs text-zinc-400">{item.label}</span>
      <ArrowRight size={12} className="shrink-0" aria-hidden="true" />
    </div>
    <strong className="my-1 block text-lg tabular-nums">{money(item.amount)}</strong>
    <Change current={item.amount ?? null} previous={item.previousAmount ?? null} />
    <span className="mt-2 block text-[11px] text-zinc-500">{item.count} records{item.incomplete ? ` · ${item.incomplete} incomplete` : ""}</span>
  </Link>;
}

export function RecordedCosts({ sections }: { sections: Record<string, DashboardSection> }) {
  const sources = ["diesel", "tolls", "scale"].flatMap(id => sections[id] ? [[id, sections[id]] as const] : []);
  const expenses = sections.expenses;
  if (!sources.length && !expenses) return null;

  return <Panel title="Recorded costs" subtitle="All trucks">
    {sources.length > 0 && <div className="mt-3 grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
      {sources.map(([id, section]) => <div key={id} className="min-w-0 space-y-2">
        <SectionError section={section} />
        {section.items.map(item => <CostCard key={item.id} item={item} />)}
        {!section.error && section.items.length === 0 && <p className="text-xs text-zinc-500">{section.title}: no records.</p>}
      </div>)}
    </div>}
    {expenses && <div className="mt-4">
      <h3 id="dashboard-expense-categories" className="text-xs font-medium text-zinc-400">{expenses.title}</h3>
      <SectionError section={expenses} />
      {expenses.items.length > 0 && <div role="region" aria-labelledby="dashboard-expense-categories"
        className="mt-2 grid max-h-56 grid-cols-1 gap-2 overflow-y-auto sm:grid-cols-2 xl:grid-cols-3">
        {expenses.items.map(item => <Link key={item.id} href={item.href} title={item.note}
          className="min-w-0 rounded-md border border-zinc-800/60 px-3 py-2 hover:border-blue-500/40 focus-visible:outline-offset-[-2px]">
          <div className="flex items-start justify-between gap-2 text-xs">
            <span className="min-w-0 break-words text-zinc-400">{item.label}</span>
            <strong className="shrink-0 tabular-nums">{money(item.amount)}</strong>
          </div>
          <div className="mt-1 flex flex-wrap items-center justify-between gap-x-2 gap-y-1">
            <Change current={item.amount ?? null} previous={item.previousAmount ?? null} />
            <span className="text-[11px] text-zinc-500">{item.count} records{item.incomplete ? ` · ${item.incomplete} incomplete` : ""}</span>
          </div>
        </Link>)}
      </div>}
      {!expenses.error && expenses.items.length === 0 && <p className="mt-2 text-xs text-zinc-500">No expenses.</p>}
    </div>}
  </Panel>;
}
