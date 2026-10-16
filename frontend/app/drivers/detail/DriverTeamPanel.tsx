"use client";

import { useEffect, useState } from "react";
import Link from "@/app/components/AccessLink";
import { fetchDriverTeam } from "@/app/lib/api";
import type { Driver, DriverTeam } from "@/app/lib/types";
import { ErrorBanner } from "@/app/components/management/ManagementUI";
import { currentChargeWeek } from "@/app/accounting/driver-charges/charges";

export function DriverTeamPanel({ driver }: { driver: Driver }) {
  const [team, setTeam] = useState<DriverTeam | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    let cancelled = false;
    fetchDriverTeam(driver.id).then(value => { if (!cancelled) { setTeam(value); setError(""); } }).catch(e => { if (!cancelled) setError(e.message); });
    return () => { cancelled = true; };
  }, [driver.id, driver.teamVersion]);
  if (driver.teamRole !== "main" || !driver.teamPartnerId) return null;
  const period = team?.periods.find(p => p.mainDriverId === driver.id && p.teamDriverId === driver.teamPartnerId && p.startWeek === driver.teamStartWeek);
  return <section className="space-y-3 rounded-lg border border-zinc-800 p-4">
    <h2 className="text-[13px] font-semibold text-zinc-100">Driver team</h2>
    {error && <ErrorBanner message={error} />}
    <p className="text-xs text-zinc-400">Team driver · <Link className="text-blue-400" href={`/drivers/detail?id=${driver.teamPartnerId}`}>{driver.teamPartnerName}</Link></p>
    {driver.teamStartWeek && driver.teamStartWeek > currentChargeWeek() && <p className="text-xs text-zinc-500">Starts {driver.teamStartWeek}</p>}
    {period && (!period.mainActive || !period.teamActive) && <p role="status" className="text-xs text-amber-300">A team member is terminated. Review the team assignment from the team driver’s profile.</p>}
    {(team?.periods.length ?? 0) > 0 && <details className="text-xs text-zinc-400"><summary className="cursor-pointer">Team history</summary><ul className="mt-2 space-y-2">{team!.periods.map(t => <li key={t.id}>{t.mainDriverName} (main) + {t.teamDriverName} · {t.startWeek} – {t.endWeek ? `before ${t.endWeek}` : "ongoing"}</li>)}</ul></details>}
  </section>;
}
