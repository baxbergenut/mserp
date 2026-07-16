import type { LucideIcon } from "lucide-react";
import { TrendingUp, TrendingDown } from "lucide-react";

interface MetricCardProps {
  label: string;
  value: string;
  icon: LucideIcon;
  delta?: number | null;
  delay?: number;
}

export function MetricCard({
  label,
  value,
  icon: Icon,
  delta,
  delay = 0,
}: MetricCardProps) {
  return (
    <div
      className="animate-fade-in rounded-xl border border-zinc-800/60 bg-card p-5 transition-colors hover:border-zinc-700/60 hover:bg-card-hover"
      style={{ animationDelay: `${delay}ms` }}
    >
      <div className="flex items-start justify-between">
        <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-zinc-800/50">
          <Icon className="h-[18px] w-[18px] text-zinc-400" />
        </div>
        {delta != null && delta !== 0 && (
          <span
            className={`inline-flex items-center gap-0.5 rounded-full px-2 py-0.5 text-[11px] font-medium ${
              delta > 0
                ? "bg-emerald-500/10 text-emerald-400"
                : "bg-red-500/10 text-red-400"
            }`}
          >
            {delta > 0 ? (
              <TrendingUp className="h-3 w-3" />
            ) : (
              <TrendingDown className="h-3 w-3" />
            )}
            {Math.abs(delta).toFixed(1)}%
          </span>
        )}
      </div>

      <div className="mt-4">
        <p className="font-mono text-2xl font-semibold tracking-tight text-zinc-100">
          {value}
        </p>
        <p className="mt-1 text-[13px] text-zinc-500">{label}</p>
      </div>
    </div>
  );
}
