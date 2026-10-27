"use client";

import { useState } from "react";
import { Cell, Pie, PieChart, ResponsiveContainer, Tooltip } from "recharts";
import Link from "@/app/components/AccessLink";
import type { DashboardSection } from "@/app/lib/dashboardTypes";
import { number, SectionError } from "./display";

const colors: Record<string, string> = {
  ready: "#000000", in_use: "#3b82f6", shop: "#f59e0b", out_of_service: "#ef4444",
  active: "#3b82f6", home: "#ef4444", vacation: "#f59e0b",
};

export function FleetStatusPie({ section, kind }: { section: DashboardSection; kind: "trucks" | "drivers" | "trailers" }) {
  const [width, setWidth] = useState(320);
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

  const height = 260, cx = width / 2, cy = height / 2;
  const radius = Math.max(36, Math.min(84, (width - 180) / 2));
  const labels = slices.map((item, index) => {
    const offset = slices.slice(0, index).reduce((sum, previous) => sum + previous.count, 0);
    const angle = (90 - (offset + item.count / 2) / total * 360) * Math.PI / 180;
    const right = Math.cos(angle) >= 0;
    return { item, right, x: cx + Math.cos(angle) * radius, y: cy - Math.sin(angle) * radius,
      elbowX: cx + Math.cos(angle) * (radius + 10), elbowY: cy - Math.sin(angle) * (radius + 10),
      labelY: cy - Math.sin(angle) * (radius + 24) };
  });
  // Spread small neighboring slices vertically without moving their anchors.
  for (const right of [false, true]) {
    const side = labels.filter(label => label.right === right).sort((a, b) => a.labelY - b.labelY);
    side.forEach((label, i) => { label.labelY = Math.max(26, label.labelY, i ? side[i - 1].labelY + 48 : 26); });
    for (let i = side.length - 1; i >= 0; i--) {
      side[i].labelY = Math.min(height - 26, side[i].labelY, i + 1 < side.length ? side[i + 1].labelY - 48 : height - 26);
    }
  }

  return <section className="min-w-0" aria-labelledby={`dashboard-${kind}-heading`}>
    <h2 id={`dashboard-${kind}-heading`} className="mb-2 text-sm font-semibold text-zinc-100">{section.title} <span className="tabular-nums">{unavailable ? "—" : number(total)}</span></h2>
    <div className="rounded-xl border border-zinc-800 bg-card p-3">
      <SectionError section={section} />
      {!unavailable && (total > 0 ? <div className="relative mx-auto h-[260px] w-full max-w-[440px]">
        <div className="absolute inset-0" role="img" aria-label={`${section.title} status distribution: ${number(total)} total`}>
          <ResponsiveContainer width="100%" height="100%" onResize={nextWidth => { if (nextWidth > 0) setWidth(nextWidth); }}>
            <PieChart margin={{ top: 0, right: 0, bottom: 0, left: 0 }}>
              <Pie data={slices} dataKey="count" nameKey="label" cx="50%" cy="50%" outerRadius={radius} startAngle={90} endAngle={-270} stroke="var(--background)" strokeWidth={2} isAnimationActive={false}>
                {slices.map(item => <Cell key={item.id} fill={colors[item.id] ?? "#71717a"} />)}
              </Pie>
              <Tooltip formatter={value => `${number(Number(value))} · ${share(Number(value))}`} contentStyle={{ background: "var(--background)", border: "1px solid #71717a", borderRadius: 8, fontSize: 12 }} />
            </PieChart>
          </ResponsiveContainer>
        </div>
        <svg aria-hidden="true" viewBox={`0 0 ${width} ${height}`} className="pointer-events-none absolute inset-0 h-full w-full">
          {labels.map(label => <polyline key={label.item.id} points={`${label.x},${label.y} ${label.elbowX},${label.elbowY} ${label.right ? width - 86 : 86},${label.labelY} ${label.right ? width - 80 : 80},${label.labelY}`} fill="none" stroke={colors[label.item.id] ?? "#71717a"} strokeWidth={1.25} />)}
        </svg>
        <div aria-label={`${section.title} status labels`} className="pointer-events-none absolute inset-0">
          {labels.map(({ item, right, labelY }) => <div key={item.id} className={`pointer-events-auto absolute w-[78px] ${right ? "right-0 text-left" : "left-0 text-right"}`} style={{ top: labelY - 20 }}>
            <Link href={item.href} aria-label={`${item.label}: ${number(item.count)}, ${share(item.count)}`} className="block rounded px-1 py-1 text-xs hover:bg-zinc-800/40 focus-visible:outline-2 focus-visible:outline-blue-500">
              <span className="block text-zinc-400">{item.label}</span>
              <span className="block whitespace-nowrap tabular-nums"><strong className="font-semibold text-zinc-200">{number(item.count)}</strong><span className="ml-1.5 text-zinc-500">{share(item.count)}</span></span>
            </Link>
          </div>)}
        </div>
      </div> : <p className="py-6 text-center text-sm text-zinc-500">No {kind} in these statuses.</p>)}
    </div>
  </section>;
}
