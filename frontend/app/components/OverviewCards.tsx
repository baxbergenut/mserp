import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";

export function ChartCard({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: ReactNode;
}) {
  return (
    <section className="min-w-0 rounded-xl border border-zinc-800/60 bg-card p-4 sm:p-5">
      <h2 className="text-sm font-medium text-zinc-200">{title}</h2>
      <p className="mt-1 text-[11px] leading-relaxed text-zinc-600">{description}</p>
      <div className="mt-5 h-[270px]">{children}</div>
    </section>
  );
}

export function KpiCard({
  label,
  value,
  detail,
  icon: Icon,
  accent,
}: {
  label: string;
  value: string;
  detail: string;
  icon: LucideIcon;
  accent: string;
}) {
  return (
    <div className="rounded-xl border border-zinc-800/60 bg-card p-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-[11px] font-medium uppercase tracking-wider text-zinc-600">{label}</div>
          <div className="mt-2 font-mono text-2xl font-semibold tabular-nums text-zinc-100">{value}</div>
          <div className="mt-1 text-[11px] text-zinc-600">{detail}</div>
        </div>
        <div className="rounded-lg border border-zinc-800 bg-zinc-950/60 p-2">
          <Icon className={`h-4 w-4 ${accent}`} />
        </div>
      </div>
    </div>
  );
}

