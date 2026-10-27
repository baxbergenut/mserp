"use client";

import Link from "@/app/components/AccessLink";
import type { DashboardSection } from "@/app/lib/dashboardTypes";
import { hundredths } from "@/app/gross-board/board";
import { centsAmount } from "@/app/accounting/driver-pay/pay";
import { money, Panel, SectionError } from "./display";

export function ProfitWaterfall({ section, reconcile, reviewCosts }: { section: DashboardSection; reconcile: () => void; reviewCosts: (kind: "fuel" | "tolls") => void }) {
  const value = section.waterfall;
  if (!value) return <Panel title="Weekly profit"><SectionError section={section} /></Panel>;
  const entries = [
    ["Gross", value.gross], ["Company fuel", value.fuel], ["Company tolls", value.tolls],
    ["Driver / owner pay", value.driverPay], ["Investor pay", value.investorPay],
  ] as const;
  let remaining = BigInt(0);
  const steps: {label: string; amount: string; start: number; end: number; total: boolean}[] = entries.map(([label, amount], i) => {
    const before = remaining;
    remaining += (i === 0 ? BigInt(1) : -BigInt(1)) * hundredths(amount);
    return { label, amount, start: Number(before) / 100, end: Number(remaining) / 100, total: i === 0 };
  });
  if (value.profit !== null) steps.push({ label: "Profit", amount: value.profit, start: 0, end: Number(hundredths(value.profit)) / 100, total: true });
  const low = Math.min(0, ...steps.flatMap(s => [s.start, s.end]));
  const high = Math.max(0, ...steps.flatMap(s => [s.start, s.end]));
  const span = high - low || 1;
  const y = (v: number) => 210 - (v - low) / span * 170;
  const width = 840, cell = width / 6, bar = 82;
  return <Panel title="Weekly profit" subtitle="Before other costs">
    <SectionError section={section} />
    <div className="mt-4">
      <div role="img" aria-label="Gross to profit waterfall" className="space-y-3 sm:hidden">
        {steps.map((step, i) => <div key={step.label}><div className="mb-1 flex justify-between gap-2 text-xs"><span>{step.label}</span><strong className="tabular-nums">{money(step.total ? step.amount : centsAmount(-hundredths(step.amount)))}</strong></div><div className="relative h-3 rounded bg-zinc-500/10"><span className="absolute h-3 rounded" style={{ left: `${(Math.min(step.start, step.end) - low) / span * 100}%`, width: `${Math.max(0.5, Math.abs(step.start - step.end) / span * 100)}%`, backgroundColor: step.total ? i === 0 ? "#3b82f6" : step.end < 0 ? "#ef4444" : "#10b981" : "#f59e0b" }} /></div></div>)}
        {value.profit === null && <p className="text-xs text-amber-500">Profit · Needs review</p>}
      </div>
      <svg role="img" aria-label="Gross to profit waterfall" viewBox={`0 0 ${width} 250`} className="hidden w-full sm:block">
        <line x1={0} x2={width} y1={y(0)} y2={y(0)} stroke="currentColor" opacity={0.2} />
        {steps.map((step, i) => {
          const x = cell * i + (cell - bar) / 2;
          const color = step.total ? i === 0 ? "#3b82f6" : step.end < 0 ? "#ef4444" : "#10b981" : step.end > step.start ? "#10b981" : "#f59e0b";
          return <g key={step.label}>
            <title>{step.label}: {money(step.amount)}</title>
            {i > 0 && <line x1={cell * (i - 1) + (cell + bar) / 2} x2={x} y1={y(steps[i - 1].end)} y2={y(steps[i - 1].end)} stroke="currentColor" opacity={0.3} strokeDasharray="3 3" />}
            <rect x={x} y={Math.min(y(step.start), y(step.end))} width={bar} height={Math.max(2, Math.abs(y(step.start) - y(step.end)))} rx={4} fill={color} />
            <text x={x + bar / 2} y={Math.min(y(step.start), y(step.end)) - 10} textAnchor="middle" fill="currentColor" fontSize={12}>
              {money(step.total ? step.amount : centsAmount(-hundredths(step.amount)))}
            </text>
            <text x={x + bar / 2} y={238} textAnchor="middle" fill="currentColor" fontSize={12}>{step.label}</text>
          </g>;
        })}
        {value.profit === null && <text x={cell * 5.5} y={130} textAnchor="middle" fill="currentColor" fontSize={13}>Needs review</text>}
      </svg>
    </div>
    {value.review.length > 0 && <div className="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-amber-500" role="status">{value.review.map(item => item.id === "gross" ? <button key={item.id} onClick={reconcile} className="hover:underline">{item.count} {item.label.toLowerCase()}</button> : item.id === "fuel" || item.id === "tolls" ? <button key={item.id} onClick={() => reviewCosts(item.id as "fuel" | "tolls")} className="hover:underline">{item.count} {item.label.toLowerCase()}</button> : <Link key={item.id} href={item.href} className="hover:underline">{item.count} {item.label.toLowerCase()}</Link>)}</div>}
    <details className="mt-3 text-xs text-zinc-500"><summary className="cursor-pointer">Calculation details</summary><p className="mt-2">Whole-fleet gross less company-owned truck fuel and tolls, driver/owner earnings, and investor shares after hired-driver earnings. Uses dated ownership and recorded weekly payroll. Owner fuel/toll recoveries, recurring collections, escrow, and carry are excluded. Fuel includes DEF. Other operating costs and taxes are not deducted. Unresolved amounts leave profit unavailable.</p></details>
  </Panel>;
}
