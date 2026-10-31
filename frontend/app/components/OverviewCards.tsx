import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";

export function ChartCard({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: ReactNode;
}) {
  return (
    <section className="ui-card min-w-0">
      <h2 className="text-sm font-medium text-zinc-200">{title}</h2>
      {description && <p className="mt-1 text-[11px] leading-relaxed text-zinc-600">{description}</p>}
      <div className="mt-4 h-68">{children}</div>
    </section>
  );
}

export function KpiCard({
  label,
  value,
  detail,
}: {
  label: string;
  value: string;
  detail?: string;
  icon: LucideIcon;
  accent: string;
}) {
  return (
    <div className="ui-card ui-metric min-w-0">
      <div className="text-xs text-zinc-400">{label}</div>
      <div className="ui-metric-value mt-2 text-zinc-100">{value}</div>
      {detail && <div className="mt-1 text-xs text-zinc-500">{detail}</div>}
    </div>
  );
}

