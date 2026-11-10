import type { ReactNode } from "react";
import { ArrowDownRight, ArrowUpRight } from "lucide-react";
import { amountDisplay } from "@/app/gross-board/board";
import type { DashboardSection } from "@/app/lib/dashboardTypes";

export const money = (v?: string | null) => v == null ? "—" : amountDisplay(v, true);
export const number = (v?: string | number | null) => v == null ? "—" : Number(v).toLocaleString("en-US", { maximumFractionDigits: 2 });
export const range = (from: string, to: string) => `${from} → ${to}`;
export function Change({ current, previous }: { current: string | number | null; previous: string | number | null }) {
  if (current == null || previous == null || Number(previous) === 0) return <span className="text-xs text-zinc-500">No comparable baseline</span>;
  const delta = (Number(current) - Number(previous)) / Math.abs(Number(previous)) * 100;
  if (delta === 0) return <span className="text-xs text-zinc-400">Unchanged</span>;
  const Icon = delta >= 0 ? ArrowUpRight : ArrowDownRight;
  return <span className="inline-flex items-center gap-1 text-xs text-zinc-400"><Icon size={13} />{Math.abs(delta).toFixed(1)}% {delta >= 0 ? "higher" : "lower"}</span>;
}
export function Panel({ title, children, subtitle }: { title: string; children: ReactNode; subtitle?: string }) {
  return <section className="min-w-0 rounded-xl border border-zinc-800 bg-card p-4"><h2 className="text-sm font-semibold text-zinc-100">{title}</h2>{subtitle && <p className="mt-1 text-xs text-zinc-500">{subtitle}</p>}{children}</section>;
}
export function SectionError({ section }: { section: DashboardSection }) {
  return section.error ? <p role="status" className="mt-2 text-xs text-amber-400">{section.error}{section.performance || section.items.length ? ` · Last result: ${new Date(section.updatedAt).toLocaleTimeString()}` : ""}</p> : null;
}
