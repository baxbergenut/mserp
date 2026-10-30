import { CircleCheck } from "lucide-react";
import { MetricCard } from "@/app/components/MetricCard";

export function FinalizationCard({ finalized, total, loading }: { finalized: number; total: number; loading: boolean }) {
  const percent = total > 0 ? Math.min(100, Math.max(0, finalized / total * 100)) : 0;
  const description = total > 0 ? `${finalized} of ${total} statements finalized` : "No statements to finalize";
  return <MetricCard label="Finalization" value={`${Number(percent.toFixed(1))}%`} icon={CircleCheck} loading={loading} progress={{ value: percent, description }} />;
}
