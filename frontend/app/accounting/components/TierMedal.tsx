import { Medal } from "lucide-react";
import type { DispatcherCommissionRow, DispatcherCommissionRuleVersion } from "@/app/lib/types";

const money = (value: number) => value.toLocaleString("en-US", { style: "currency", currency: "USD", maximumFractionDigits: 2 });

export function TierMedal({ kind, row, rule }: { kind: "gross" | "rpm"; row: DispatcherCommissionRow; rule: DispatcherCommissionRuleVersion | null }) {
  const grossIndex = rule?.rules.tiers.findIndex(tier => tier.minGross === row.selectedTier) ?? -1;
  const tier = grossIndex >= 0 ? rule!.rules.tiers[grossIndex] : undefined;
  const rpmIndex = tier?.brackets.findLastIndex(bracket => row.rpm >= bracket.minRPM) ?? -1;
  const index = kind === "gross" ? grossIndex : rpmIndex;
  let requirement = "No commission rules apply to this week.";
  if (rule && kind === "gross") {
    const next = rule.rules.tiers[grossIndex + 1];
    requirement = tier
      ? `Minimum adjusted gross ${money(tier.minGross)}.${next ? ` Next tier starts at ${money(next.minGross)}.` : ""} Home-day promotion limit applies.`
      : `Adjusted gross below ${money(rule.rules.tiers[0].minGross)}.`;
  } else if (tier && kind === "rpm") {
    const bracket = tier.brackets[rpmIndex], next = tier.brackets[rpmIndex + 1];
    requirement = bracket
      ? `RPM ${money(bracket.minRPM)}${next ? ` to below ${money(next.minRPM)}` : " or more"} for gross tier ${grossIndex + 1}. Commission ${bracket.percent}%.`
      : `RPM below ${money(tier.brackets[0].minRPM)} for gross tier ${grossIndex + 1}. Commission 0%.`;
  } else if (rule && kind === "rpm") {
    requirement = "A qualifying gross tier is required.";
  }
  const basis = kind === "gross" ? `Full-week gross ${money(row.gross)}; adjusted gross ${money(row.adjustedGross)}.` : `Full-week RPM $${row.rpm.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 6 })}.`;
  const label = `${kind === "gross" ? "Gross" : "RPM"} tier ${index >= 0 ? index + 1 : "not qualified"}: ${requirement} ${basis}`;
  const color = ["text-gray-400", "text-orange-500", "text-yellow-500"][index] ?? "text-blue-400";
  return <span title={label} aria-label={label} tabIndex={0} className="inline-flex items-center justify-center gap-1 rounded px-1 outline-none focus-visible:ring-2 focus-visible:ring-blue-500">
    {index >= 0 ? <><Medal className={`h-4 w-4 ${color}`} aria-hidden="true" /><span className="font-mono tabular-nums">{index + 1}</span></> : <span className="text-zinc-500">—</span>}
  </span>;
}
