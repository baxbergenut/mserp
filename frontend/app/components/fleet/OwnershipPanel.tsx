"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { fetchTrucks, fetchTruckCharges } from "@/app/lib/api";
import type { Investor, Truck, TruckChargeData } from "@/app/lib/types";
import { currentChargeWeek } from "@/app/accounting/driver-charges/charges";
import { usePermissions } from "@/app/lib/access";
import { EmptyState, ErrorBanner, LoadingTable, TableShell } from "../management/ManagementUI";
import { InvestorStatements } from "./InvestorStatements";

export function OwnershipPanel({ investor }: { investor: Investor }) {
  const permissions = usePermissions();
  const [trucks, setTrucks] = useState<Truck[] | null>(null);
  const [charges, setCharges] = useState<TruckChargeData | null>(null);
  const canReadCharges = permissions.includes("charges.read");
  const [error, setError] = useState("");
  useEffect(() => {
    let cancelled = false;
    fetchTrucks().then(rows => { if (!cancelled) setTrucks(rows.filter(t => t.ownerId === investor.id)); }).catch(e => { if (!cancelled) setError(e.message); });
    return () => { cancelled = true; };
  }, [investor.id]);
  useEffect(() => {
    let cancelled = false;
    if (canReadCharges) fetchTruckCharges().then(value => { if (!cancelled) setCharges(value); }).catch(e => { if (!cancelled) setError(e.message); });
    return () => { cancelled = true; };
  }, [canReadCharges]);
  function terms(truck: Truck) {
    if (investor.isCompany) return "Company";
    if (investor.driverId && truck.driverId === investor.driverId) return "Owner-operated · Driver Pay";
    if (!charges) return canReadCharges ? "Loading terms…" : "—";
    const term = charges.terms.filter(t => t.truckId === truck.id && t.weekStart <= currentChargeWeek()).sort((a,b) => b.weekStart.localeCompare(a.weekStart))[0];
    if (term && term.ownerId !== investor.id) return "Review owner agreement";
    return term ? `${Number(term.sharePercent)}% of driver gross · ${term.weekStart}` : charges.eligibleTruckIds.includes(truck.id) ? "Setup required" : "Driver Pay";
  }
  return <div className="space-y-6">
    <section className="space-y-3"><div className="flex items-center justify-between gap-3"><h2>Owned trucks <span className="text-zinc-400">{investor.trucks.length}</span></h2>{permissions.includes("charges.read") && !investor.isCompany && <Link className="ui-button" href="/accounting/driver-charges?tab=trucks">Truck terms & charges</Link>}</div>
      {error && <ErrorBanner message={error} />}
      <TableShell>{!trucks ? <LoadingTable columns={5} /> : !trucks.length ? <EmptyState message="No trucks currently owned." /> : <table className="[&_th]:px-3 [&_td]:px-3 [&_th]:py-2 [&_td]:py-2 w-full min-w-[720px] text-left"><thead><tr><th>Truck</th><th>Vehicle</th><th>Current driver</th><th>Settlement terms</th><th>Status</th></tr></thead><tbody>{trucks.map(t => <tr key={t.id}><td><Link href={`/trucks/detail?id=${t.id}`}>{t.unitNumber}</Link></td><td>{[t.year,t.make,t.model].filter(Boolean).join(" ") || "—"}</td><td>{t.driverId ? <Link href={`/drivers/detail?id=${t.driverId}`}>{t.driverName}</Link> : "Unassigned"}</td><td>{terms(t)}</td><td>{t.active ? "Active" : "Inactive"}</td></tr>)}</tbody></table>}</TableShell>
    </section>
    {!investor.isCompany && permissions.includes("payroll.read") && <InvestorStatements investorId={investor.id} />}
  </div>;
}
