"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { fetchDrivers, fetchDriverTeam, saveDriverTeam } from "@/app/lib/api";
import type { Driver, DriverTeam } from "@/app/lib/types";
import { currentChargeWeek, validChargeWeek } from "@/app/accounting/driver-charges/charges";
import { controlClass, ErrorBanner, Field } from "@/app/components/management/ManagementUI";
import { usePermissions } from "@/app/lib/access";

export function DriverTeamPanel({ driver, onSaved }: { driver: Driver; onSaved: () => void }) {
  const permissions = usePermissions();
  const canWrite = permissions.includes("payroll.write");
  const [team, setTeam] = useState<DriverTeam | null>(null);
  const [drivers, setDrivers] = useState<Driver[]>([]);
  const [week, setWeek] = useState(currentChargeWeek);
  const [selected, setSelected] = useState("");
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let cancelled = false;
    fetchDriverTeam(driver.id).then(value => { if (!cancelled) { setTeam(value); setError(""); } }).catch(e => { if (!cancelled) setError(e.message); });
    return () => { cancelled = true; };
  }, [driver.id, driver.teamRole, attempt]);
  useEffect(() => {
    if (!notice) return;
    const timer = setTimeout(() => setNotice(""), 5000);
    return () => clearTimeout(timer);
  }, [notice]);
  const todayWeek = currentChargeWeek();
  const current = team?.periods.find(t => t.startWeek <= todayWeek && (!t.endWeek || todayWeek < t.endWeek));
  const isMember = current?.teamDriverId === driver.id;
  async function begin() {
    setBusy(true); setError("");
    try {
      setDrivers(await fetchDrivers());
      setWeek(todayWeek); setSelected(current?.teamDriverId ?? ""); setEditing(true);
    } catch (e) { setError(e instanceof Error ? e.message : "Unable to load drivers"); }
    finally { setBusy(false); }
  }
  async function save() {
    if (!team || !validChargeWeek(week)) { setError("Select an effective Monday."); return; }
    setBusy(true); setError("");
    try { const value = await saveDriverTeam(driver.id, { teamDriverId: selected, weekStart: week, version: team.version }); setTeam(value); setEditing(false); setNotice("Team saved"); onSaved(); }
    catch (e) { setError(e instanceof Error ? e.message : "Unable to save team"); }
    finally { setBusy(false); }
  }
  return <section className="space-y-3 rounded-lg border border-zinc-800 p-4">
    <div className="flex items-center justify-between gap-3"><h2 className="text-[13px] font-semibold text-zinc-100">Driver team</h2>{canWrite && !isMember && !editing && <button type="button" disabled={!team || busy} className="ui-button" onClick={() => void begin()}>{current ? "Change team" : "Set up team"}</button>}</div>
    {error && <><ErrorBanner message={error} />{!team && <button type="button" className="text-xs text-blue-400" onClick={() => setAttempt(v => v + 1)}>Retry team</button>}</>}
    {notice && <p role="status" className="text-xs text-emerald-400">{notice}</p>}
    {!team && !error && <p className="text-xs text-zinc-500">Loading team…</p>}
    {team && !editing && <p className="text-xs text-zinc-400">{current ? <>{isMember ? "Team driver · Main driver: " : "Main driver · Team driver: "}<Link className="text-blue-400" href={`/drivers/detail?id=${isMember ? current.mainDriverId : current.teamDriverId}`}>{isMember ? current.mainDriverName : current.teamDriverName}</Link></> : "Solo driver"}</p>}
    {current && (!current.mainActive || !current.teamActive) && <p role="alert" className="text-xs text-amber-300">A team member is terminated. Review the team end week from the main driver’s profile.</p>}
    {editing && <form onSubmit={e => { e.preventDefault(); void save(); }} className="space-y-3">
      <div className="grid gap-3 sm:grid-cols-2"><Field label="Team driver"><select aria-label="Team driver" className={controlClass} value={selected} disabled={busy} onChange={e => setSelected(e.target.value)}><option value="">Solo driver / End team</option>{drivers.filter(d => d.active && d.id !== driver.id).map(d => <option key={d.id} value={d.id}>{d.fullName}</option>)}</select></Field>
      <Field label="Effective Monday"><input aria-label="Team effective Monday" type="date" step={7} required className={controlClass} value={week} disabled={busy} onChange={e => { setWeek(e.target.value); const period = team?.periods.find(t => t.startWeek <= e.target.value && (!t.endWeek || e.target.value < t.endWeek)); setSelected(period?.mainDriverId === driver.id ? period.teamDriverId : ""); }} /></Field></div>
      <p className="text-xs text-zinc-500">Loads, truck assignment, tariff, escrow and recurring charges use the main driver. Relay fuel from either driver is included in that statement. Selecting a week replaces later planned team changes.</p>
      <div className="flex gap-2"><button type="submit" disabled={busy} className="ui-button ui-button-primary">{busy ? "Saving…" : "Save team"}</button><button type="button" disabled={busy} className="ui-button" onClick={() => setEditing(false)}>Cancel</button></div>
    </form>}
    {(team?.periods.length ?? 0) > 0 && <details className="text-xs text-zinc-400"><summary className="cursor-pointer">Team history</summary><ul className="mt-2 space-y-2">{team!.periods.map(t => <li key={t.id}>{t.mainDriverName} (main) + {t.teamDriverName} · {t.startWeek} – {t.endWeek ? `before ${t.endWeek}` : "ongoing"}</li>)}</ul></details>}
  </section>;
}
