import { SkeletonBar } from "./WeeklyTableSkeleton";
import type { LucideIcon } from "lucide-react";
import { TrendingUp, TrendingDown } from "lucide-react";

interface MetricCardProps {
  label: string;
  value: string;
  icon: LucideIcon;
  delta?: number | null;
  delay?: number;
  compact?: boolean;
  loading?: boolean;
  progress?: { value: number; description: string };
}

export function MetricCard({
  label,
  value,
  icon: Icon,
  delta,
  delay = 0,
  loading = false,
  progress,
}: MetricCardProps) {
  return (
    <div
      className="ui-card ui-metric flex items-center justify-between gap-3 animate-fade-in"
      style={{ animationDelay: `${delay}ms` }}
    >
      <div className="order-2 flex items-center gap-2">
        <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-zinc-800/50">
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

      <div className={`order-1 ${progress ? "min-w-0 flex-1" : ""}`}>
        <p className="ui-metric-value text-zinc-100">
          {loading ? <SkeletonBar className="h-5 w-24 align-middle" /> : value}
        </p>
        <p className="mt-1 text-xs text-zinc-400">{label}</p>
        {progress && <div className="mt-2">
          <div role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={loading ? undefined : progress.value} aria-valuetext={loading ? "Loading" : progress.description} className="h-1.5 overflow-hidden rounded-full bg-zinc-800">
            <div className="h-full rounded-full bg-emerald-500 transition-[width] duration-300 motion-reduce:transition-none" style={{ width: `${loading ? 0 : progress.value}%` }} />
          </div>
          <p className="mt-1 text-xs text-zinc-400">{loading ? "Loading…" : progress.description}</p>
        </div>}
      </div>
    </div>
  );
}
