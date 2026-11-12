"use client";
import { useEffect, useState } from "react";
import Link from "@/app/components/AccessLink";
import { APIError, fetchTruckOptions, fetchTruckCharges, saveTruckTerm } from "@/app/lib/api";
import type { InvestorOption, TruckChargeData, TruckOption, TruckTerm } from "@/app/lib/types";
import { currentChargeWeek } from "@/app/accounting/driver-charges/charges";
import { TruckTermsDialog } from "@/app/accounting/driver-charges/TruckTermsDialog";
import { usePermissions } from "@/app/lib/access";
import { EmptyState, ErrorBanner, LoadingTable, TableShell } from "../management/ManagementUI";
import { InvestorStatements } from "./InvestorStatements";

export function OwnershipPanel({ investor }: { investor: InvestorOption }) {
  return <OwnedTrucks investor={investor} showDriverOwnership />;
}

export function OwnedTrucks({ investor, showDriverOwnership = false }: {
  investor: InvestorOption;
  showDriverOwnership?: boolean;
}) {
  const permissions = usePermissions();
  const [trucks, setTrucks] = useState<TruckOption[] | null>(null);
  const [truckError, setTruckError] = useState("");
  const [charges, setCharges] = useState<TruckChargeData | null>(null);
  const canReadCharges = !investor.isCompany && permissions.includes("charges.read") && permissions.includes("investor_terms.read");
  const canEditTerms = !showDriverOwnership && investor.active && permissions.includes("investor_terms.edit");
  const [termsError, setTermsError] = useState("");
  const [termsAttempt, setTermsAttempt] = useState(0);
  const [editing, setEditing] = useState<{ unitNumber: string; term: TruckTerm } | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");
  const week = currentChargeWeek();
  useEffect(() => {
    let cancelled = false;
    if (canReadCharges) fetchTruckCharges(week).then(value => {
      if (!cancelled) { setCharges(value); setTermsError(""); }
    }).catch(e => { if (!cancelled) { setCharges(null); setTermsError(e.message); } });
    return () => { cancelled = true; };
  }, [canReadCharges, week, termsAttempt]);
  useEffect(() => {
    let cancelled = false;
    fetchTruckOptions().then(rows => { if (!cancelled) setTrucks(rows.filter(t => t.ownerId === investor.id)); }).catch(e => { if (!cancelled) setTruckError(e.message); });
    return () => { cancelled = true; };
  }, [investor.id]);

  function reloadTerms() {
    setCharges(null);
    setTermsError("");
    setTermsAttempt(attempt => attempt + 1);
  }

  async function saveTerms() {
    if (!editing || !canReadCharges || !canEditTerms || saving) return;
    setSaving(true);
    setSaveError("");
    try {
      await saveTruckTerm(editing.term);
      setEditing(null);
      reloadTerms();
    } catch (error) {
      if (error instanceof APIError && error.status === 409) {
        reloadTerms();
        setSaveError(`${error.message} Close and reopen the editor to load the latest terms.`);
      } else {
        setSaveError(error instanceof Error ? error.message : "Unable to save settlement terms");
      }
    } finally {
      setSaving(false);
    }
  }

  const termsByTruck = new Map<string, TruckTerm>();
  if (canReadCharges) {
    for (const term of charges?.terms ?? []) {
      const latest = termsByTruck.get(term.truckId);
      if (term.weekStart <= week && (!latest || term.weekStart > latest.weekStart)) termsByTruck.set(term.truckId, term);
    }
    for (const [truckId, term] of termsByTruck) {
      if (term.endsBefore && term.endsBefore <= week) termsByTruck.delete(truckId);
    }
  }
  function renderTerms(truck: TruckOption) {
    if (!charges) return termsError ? "Unavailable" : "Loading terms…";
    const term = termsByTruck.get(truck.id);
    const eligible = charges.eligibleTruckIds.includes(truck.id);
    if (!term && !eligible) return "No current agreement";
    if (!term && investor.driverId && truck.driverId === investor.driverId) return "Owner-operated · Driver Pay";

    const matchingTerm = term?.ownerId === investor.id ? term : undefined;
    const label = matchingTerm ? `${Number(matchingTerm.sharePercent)}% of driver gross` : term ? "Review owner agreement" : "Setup required";
    const datedOwner = charges.owners?.find(owner => owner.truckId === truck.id);
    const editable = canEditTerms && eligible && (!datedOwner || datedOwner.ownerId === investor.id);
    return <>
      {editable ? <button type="button" className={`${matchingTerm ? "text-blue-400" : "text-amber-400"} hover:underline`} onClick={() => {
        const exact = charges.terms?.find(value => value.truckId === truck.id && value.weekStart === week);
        setSaveError("");
        setEditing({
          unitNumber: truck.unitNumber,
          term: { truckId: truck.id, ownerId: investor.id, weekStart: week, sharePercent: matchingTerm?.sharePercent ?? "", version: exact?.version ?? 0 },
        });
      }}>{label}</button> : <span className={matchingTerm ? undefined : "text-amber-400"}>{label}</span>}
      {matchingTerm && <>{" "}<span className="ml-1 whitespace-nowrap text-xs text-zinc-400">Since <time dateTime={matchingTerm.weekStart}>{matchingTerm.weekStart}</time></span></>}
    </>;
  }
  return <div className="space-y-6">
    <section className="space-y-3" aria-label="Owned trucks">
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-sm font-semibold text-zinc-100">Owned trucks <span className="ml-1 font-normal text-zinc-400">{trucks?.length ?? investor.trucks.length}</span></h2>
        {showDriverOwnership && permissions.includes("charges.read") && !investor.isCompany && <Link className="ui-button" href="/accounting/driver-charges?tab=trucks">Truck terms & charges</Link>}
      </div>
      {truckError && <ErrorBanner message={truckError} />}
      {canReadCharges && termsError && <>
        <ErrorBanner message={`Unable to load truck terms: ${termsError}`} />
        <button type="button" className="text-sm text-blue-400 hover:underline" onClick={reloadTerms}>Retry truck terms</button>
      </>}
      {!truckError && <TableShell>
        {!trucks ? <LoadingTable columns={canReadCharges ? 5 : 4} /> : !trucks.length ? <EmptyState message="No trucks currently owned." /> : (
          <table className="w-full min-w-[600px] text-left text-sm [&_td]:px-3 [&_td]:py-2.5 [&_th]:px-3 [&_th]:py-2" aria-label="Owned trucks">
            <thead className="bg-zinc-900 text-xs text-zinc-400"><tr><th>Truck</th><th>Vehicle</th><th>Current driver</th>{canReadCharges && <th>Settlement terms</th>}<th>Status</th></tr></thead>
            <tbody className="divide-y divide-zinc-800/60">{trucks.map(t => (
              <tr key={t.id}>
                <td><Link className="text-blue-400 hover:underline" href={`/trucks/detail?id=${t.id}`}>{t.unitNumber}</Link></td>
                <td>{[t.year, t.make, t.model].filter(Boolean).join(" ") || "—"}</td>
                <td>{t.driverId ? <Link className="text-blue-400 hover:underline" href={`/drivers/detail?id=${t.driverId}`}>{t.driverName}</Link> : "Unassigned"}</td>
                {canReadCharges && <td>{renderTerms(t)}</td>}
                <td>{t.active ? "Active" : "Inactive"}</td>
              </tr>
            ))}</tbody>
          </table>
        )}
      </TableShell>}
    </section>
    {editing && canReadCharges && canEditTerms && <TruckTermsDialog unitNumber={editing.unitNumber} term={editing.term}
      ownerField={<dl><dt className="text-xs font-medium text-zinc-400">Investor</dt><dd className="mt-1 text-sm text-zinc-200">{investor.fullName}</dd></dl>}
      saving={saving} error={saveError} onClose={() => setEditing(null)} onSave={() => void saveTerms()}
      onShareChange={sharePercent => setEditing({ ...editing, term: { ...editing.term, sharePercent } })}
    />}
    {showDriverOwnership && !investor.isCompany && permissions.includes("investor_pay.read") && <InvestorStatements investorId={investor.id} />}
  </div>;
}
