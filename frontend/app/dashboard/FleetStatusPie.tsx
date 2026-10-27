"use client";

import { Cell, Pie, PieChart, Tooltip } from "recharts";
import Link from "@/app/components/AccessLink";
import type { DashboardSection } from "@/app/lib/dashboardTypes";
import { number, Panel, SectionError } from "./display";

const colors: Record<string, string> = {
  ready: "#10b981", in_use: "#3b82f6", shop: "#f59e0b", out_of_service: "#ef4444",
  active: "#10b981", home: "#3b82f6", vacation: "#8b5cf6",
};

export function FleetStatusPie({ section, kind }: { section: DashboardSection; kind: "trucks" | "drivers" }) {
  const visibleStatuses = kind === "drivers" ? ["active", "home", "vacation"] : ["ready", "in_use", "shop", "out_of_service"];
  const statuses = section.items.filter(item => visibleStatuses.includes(item.id)).map(item => {
    // Accept the previous API shape during deployment without dropping new drivers.
    if (kind !== "drivers" || item.id !== "active") return item;
    const count = item.count + (section.items.find(other => other.id === "new_driver")?.count ?? 0);
    const href = "/drivers?dashboard=1&employment=active&availability=active_or_new";
    return { ...item, count, href };
  });
  const total = statuses.reduce((sum, item) => sum + item.count, 0);
  const slices = statuses.filter(item => item.count > 0);
  const share = (count: number) => `${total ? (count / total * 100).toLocaleString("en-US", { maximumFractionDigits: 1 }) : 0}%`;
  const unavailable = !!section.error && section.items.length === 0;

  return <Panel title={section.title}>
    <SectionError section={section} />
    {!unavailable && <>
      <p className="mt-1 text-xs text-zinc-500"><strong className="font-semibold tabular-nums text-zinc-200">{number(total)}</strong> {kind === "drivers" ? "drivers · Active, Home or Vacation" : "trucks"}</p>
      {total > 0 ? <div className="my-3 flex justify-center" role="img" aria-label={`${section.title} status distribution: ${number(total)} total`}>
        <PieChart width={180} height={180}>
          <Pie data={slices} dataKey="count" nameKey="label" cx="50%" cy="50%" outerRadius={84} startAngle={90} endAngle={-270} stroke="var(--background)" strokeWidth={2} isAnimationActive={false}>
            {slices.map(item => <Cell key={item.id} fill={colors[item.id] ?? "#71717a"} />)}
          </Pie>
          <Tooltip formatter={value => `${number(Number(value))} · ${share(Number(value))}`} contentStyle={{ background: "var(--background)", border: "1px solid #71717a", borderRadius: 8, fontSize: 12 }} />
        </PieChart>
      </div> : <p className="py-6 text-center text-sm text-zinc-500">{kind === "drivers" ? "No drivers in these statuses." : "No trucks in these statuses."}</p>}
      <div className="grid grid-cols-2 gap-1" aria-label={`${section.title} status legend`}>
        {statuses.map(item => <Link key={item.id} href={item.href} aria-label={`${item.label}: ${number(item.count)}, ${share(item.count)}`} className="flex min-w-0 items-start gap-2 rounded-lg px-2 py-1.5 hover:bg-zinc-800/40">
          <span aria-hidden="true" className="mt-1 h-2.5 w-2.5 shrink-0 rounded-full" style={{ backgroundColor: colors[item.id] ?? "#71717a" }} />
          <span className="min-w-0 text-xs"><span className="block text-zinc-400">{item.label}</span><strong className="font-semibold tabular-nums text-zinc-200">{number(item.count)}</strong><span className="ml-2 tabular-nums text-zinc-500">{share(item.count)}</span></span>
        </Link>)}
      </div>
    </>}
  </Panel>;
}
