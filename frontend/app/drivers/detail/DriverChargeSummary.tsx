"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { fetchDriverCharges } from "@/app/lib/api";
import type { ChargeData } from "@/app/lib/types";
import { decimalDisplay, hundredths } from "@/app/gross-board/board";

export function DriverChargeSummary({ driverId }: { driverId: string }) {
  const [data, setData] = useState<ChargeData | null>(null);
  const [error, setError] = useState("");
  useEffect(() => { let cancelled = false; fetchDriverCharges(driverId).then(data => { if (!cancelled) setData(data); }).catch(err => { if (!cancelled) setError(err.message); }); return () => { cancelled = true; }; }, [driverId]);
  const recurring = data?.schedules.filter(s => s.kind === "recurring" && s.status === "active") ?? [];
  const remaining = (data?.schedules ?? []).reduce((sum, s) => sum + hundredths(s.remaining), BigInt(0));
  return <section className="rounded-xl border border-zinc-800 p-5"><div className="flex flex-wrap items-center justify-between gap-3"><h2 className="text-sm font-medium text-zinc-200">Driver charges</h2><Link className="text-sm text-blue-400" href={`/accounting/driver-charges?driverId=${driverId}`}>Manage charges</Link></div>{error ? <p role="alert" className="mt-3 text-sm text-red-300">{error}</p> : <p className="mt-3 text-sm text-zinc-400">{data ? `${recurring.length} active recurring assignments · ${decimalDisplay(remaining, true)} installment balance remaining` : "Loading charges…"}</p>}</section>;
}
