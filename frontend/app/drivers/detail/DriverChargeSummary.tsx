"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { fetchDriverCharges } from "@/app/lib/api";
import type { ChargeData } from "@/app/lib/types";
import { decimalDisplay, hundredths } from "@/app/gross-board/board";

export function DriverChargeSummary({ driverId, expanded = false }: { driverId: string; expanded?: boolean }) {
  const [data, setData] = useState<ChargeData | null>(null);
  const [error, setError] = useState("");
  useEffect(() => { let cancelled = false; fetchDriverCharges(driverId).then(data => { if (!cancelled) setData(data); }).catch(err => { if (!cancelled) setError(err.message); }); return () => { cancelled = true; }; }, [driverId]);
  const recurring = data?.schedules.filter(s => s.kind === "recurring" && s.status === "active") ?? [];
  const remaining = (data?.schedules ?? []).reduce((sum, s) => sum + hundredths(s.remaining), BigInt(0));
  return <section className="rounded-xl border border-zinc-800 p-5"><div className="flex flex-wrap items-center justify-between gap-3"><h2 className="text-sm font-medium text-zinc-200">Driver charges</h2><Link className="text-sm text-blue-400" href={`/accounting/driver-charges?driverId=${driverId}`}>Manage charges</Link></div>{error ? <p role="alert" className="mt-3 text-sm text-red-300">{error}</p> : <p className="mt-3 text-sm text-zinc-400">{data ? `${recurring.length} active recurring assignments · ${decimalDisplay(remaining, true)} installment balance remaining` : "Loading charges…"}</p>}{expanded && data && <div className="mt-4 overflow-x-auto"><table className="w-full min-w-[750px] text-left text-xs"><thead><tr className="border-b border-zinc-800 text-zinc-500">{["Charge", "Schedule", "Total", "Confirmed paid", "Remaining", "Status"].map(label => <th className="px-3 py-2 font-medium" key={label}>{label}</th>)}</tr></thead><tbody>{data.schedules.map(s => <tr key={s.id} className="border-b border-zinc-800/60 text-zinc-300"><td className="px-3 py-3"><Link className="text-blue-400" href={`/accounting/driver-charges?driverId=${driverId}&tab=${s.kind}&scheduleId=${s.id}`}>{s.name}</Link></td><td className="px-3 py-3">{s.kind}<span className="mt-1 block text-zinc-500">From {s.startWeek}{s.endWeek ? ` through ${s.endWeek}` : ""}</span></td><td className="px-3 py-3 font-mono">{s.total ? decimalDisplay(hundredths(s.total), true) : "Recurring"}</td><td className="px-3 py-3 font-mono">{s.kind === "installment" ? decimalDisplay(hundredths(s.confirmed), true) : "See weekly pay"}</td><td className="px-3 py-3 font-mono">{s.kind === "installment" ? decimalDisplay(hundredths(s.remaining), true) : "Ongoing"}</td><td className="px-3 py-3 capitalize">{s.status}</td></tr>)}</tbody></table></div>}</section>;
}
