"use client";
import { useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import Link from "@/app/components/AccessLink";
import { Banknote, Landmark, Pencil } from "lucide-react";
import { fetchInvestor, updateInvestor, fetchDriverOptions } from "@/app/lib/api";
import type { DriverOption, Investor, InvestorInput } from "@/app/lib/types";
import { usePermissions } from "@/app/lib/access";
import { PageHeader } from "@/app/components/PageHeader";
import { OwnedTrucks } from "@/app/components/fleet/OwnershipPanel";
import { ErrorBanner, Modal, StatusBadge } from "@/app/components/management/ManagementUI";
import { formatPhone } from "@/app/lib/phone";
import { latestPayrollWeek } from "@/app/accounting/payrollWeek";
import { InvestorForm } from "../InvestorForm";

export default function InvestorDetailPage() {
  const id = useSearchParams().get("id") ?? "";
  const permissions = usePermissions();
  const [investor, setInvestor] = useState<Investor | null>(null);
  const [form, setForm] = useState<InvestorInput | null>(null);
  const [drivers, setDrivers] = useState<DriverOption[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [saveError, setSaveError] = useState("");
  const [attempt, setAttempt] = useState(0);
  useEffect(() => { let cancelled = false; fetchInvestor(id).then(v => { if (!cancelled) { setInvestor(v); setError(""); } }).catch(e => { if (!cancelled) setError(e.message); }); return () => { cancelled = true; }; }, [id, attempt]);
  async function edit() {
    if (!investor) return;
    try {
      setDrivers(await fetchDriverOptions());
      setSaveError("");
      setForm({ fullName: investor.fullName, driverId: investor.driverId, phone: investor.phone ?? "", email: investor.email ?? "", notes: investor.notes ?? "", active: investor.active });
    }
    catch (e) { setError(e instanceof Error ? e.message : "Unable to open investor"); }
  }
  const profile = investor?.id === id ? investor : null;
  const canManageTerms = !profile?.isCompany && permissions.includes("charges.read");
  const canViewPay = !profile?.isCompany && permissions.includes("investor_pay.read");
  const canEdit = !profile?.isCompany && permissions.includes("investors.edit");
  const driverId = permissions.includes("drivers.read") ? profile?.driverId : null;
  return <div className="space-y-5 animate-fade-in">
    {error && <><ErrorBanner message={error} /><button className="text-sm text-blue-400" onClick={() => setAttempt(v => v + 1)}>Retry investor</button></>}
    {!profile && !error && <p className="py-12 text-center text-zinc-500">Loading investor…</p>}
    {profile && <header className="overflow-hidden rounded-2xl border border-zinc-800 bg-zinc-900/30">
      <PageHeader><div className="flex flex-wrap items-center gap-3">
        <Landmark className="h-5 w-5 shrink-0 text-zinc-500" />
        <h1 className="break-words text-xl font-semibold text-zinc-100">{profile.fullName}</h1>
        <StatusBadge active={profile.active} />
      </div></PageHeader>
      {(driverId || canViewPay || canManageTerms || canEdit) && (
        <div className="flex flex-wrap items-center justify-end gap-2 p-5 sm:p-6" role="group" aria-label="Investor actions">
          {driverId && <Link className="ui-button" href={`/drivers/detail?id=${driverId}`}>Driver profile</Link>}
          {canViewPay && <Link className="ui-button" href={`/accounting/investor-pay?investorId=${profile.id}&weekStart=${latestPayrollWeek()}`}><Banknote className="h-3.5 w-3.5" />Investor Pay</Link>}
          {canManageTerms && <Link className="ui-button" href="/accounting/driver-charges?tab=trucks">Truck terms & charges</Link>}
          {canEdit && <button type="button" className="ui-button ui-button-primary" onClick={() => void edit()}><Pencil className="h-3.5 w-3.5" />Edit investor</button>}
        </div>
      )}
      <dl className="grid grid-cols-[repeat(auto-fit,minmax(150px,1fr))] gap-px border-t border-zinc-800 bg-zinc-800">
        {[
          ["Type", profile.isCompany ? "Company" : profile.driverId ? "Driver investor" : "Independent investor"],
          ["Phone", formatPhone(profile.phone) || "Not recorded"],
          ["Email", profile.email || "Not recorded"],
        ].map(([label, value]) => <div key={label} className="min-w-0 break-words bg-zinc-950/90 px-5 py-4">
          <dt className="text-xs text-zinc-500">{label}</dt>
          <dd className="mt-1 text-sm font-medium text-zinc-200">{value}</dd>
        </div>)}
      </dl>
      {profile.notes && <p className="whitespace-pre-wrap break-words border-t border-zinc-800 px-5 py-3 text-sm text-zinc-400">{profile.notes}</p>}
    </header>}
    {profile && <OwnedTrucks key={profile.id} investor={profile} />}
    {profile && form && <Modal title="Edit investor" isSaving={saving} submitLabel="Save investor" onClose={() => setForm(null)} onSubmit={e => {
      e.preventDefault();
      setSaving(true);
      setSaveError("");
      void updateInvestor(profile.id, form).then(v => { setInvestor(v); setForm(null); }).catch(e => setSaveError(e.message)).finally(() => setSaving(false));
    }}>{saveError && <ErrorBanner message={saveError} />}<InvestorForm value={form} onChange={setForm} drivers={drivers} editing /></Modal>}
  </div>;
}
