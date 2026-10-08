"use client";

import { useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import Link from "next/link";
import { Landmark, Pencil } from "lucide-react";
import { fetchInvestor, updateInvestor, fetchDrivers } from "@/app/lib/api";
import type { Investor, InvestorInput, Driver } from "@/app/lib/types";
import { usePermissions } from "@/app/lib/access";
import { PageHeader } from "@/app/components/PageHeader";
import { OwnershipPanel } from "@/app/components/fleet/OwnershipPanel";
import { ErrorBanner, Modal, StatusBadge } from "@/app/components/management/ManagementUI";
import { formatPhone } from "@/app/lib/phone";
import { InvestorForm } from "../InvestorForm";

export default function InvestorDetailPage() {
  const id = useSearchParams().get("id") ?? "";
  const permissions = usePermissions();
  const [investor, setInvestor] = useState<Investor | null>(null);
  const [form, setForm] = useState<InvestorInput | null>(null);
  const [drivers, setDrivers] = useState<Driver[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  useEffect(() => { let cancelled = false; fetchInvestor(id).then(v => { if (!cancelled) { setInvestor(v); setError(""); } }).catch(e => { if (!cancelled) setError(e.message); }); return () => { cancelled = true; }; }, [id, attempt]);
  async function edit() {
    if (!investor) return;
    try { setDrivers(await fetchDrivers()); setForm({ fullName: investor.fullName, driverId: investor.driverId, phone: investor.phone ?? "", email: investor.email ?? "", notes: investor.notes ?? "", active: investor.active }); }
    catch (e) { setError(e instanceof Error ? e.message : "Unable to open investor"); }
  }
  return <div className="space-y-6">
    {error && <div className="space-y-2"><ErrorBanner message={error} /><button className="ui-button" onClick={() => setAttempt(v => v + 1)}>Retry investor</button></div>}
    {!investor && !error && <p className="text-zinc-400">Loading investor…</p>}
    {investor && <><PageHeader><div className="flex items-center gap-3"><Landmark /><h1>{investor.fullName}</h1><StatusBadge active={investor.active} /></div></PageHeader>
      <div className="flex flex-wrap justify-end gap-2">{investor.driverId && <Link className="ui-button" href={`/drivers/detail?id=${investor.driverId}`}>Driver profile</Link>}{!investor.isCompany && permissions.includes("fleet.write") && <button className="ui-button ui-button-primary" onClick={() => void edit()}><Pencil />Edit investor</button>}</div>
      <section className="ui-card"><dl className="grid gap-4 sm:grid-cols-3">{[["Type", investor.isCompany ? "Company" : investor.driverId ? "Driver investor" : "Independent investor"], ["Phone", formatPhone(investor.phone) || "Not recorded"], ["Email", investor.email || "Not recorded"]].map(([label, value]) => <div key={label}><dt className="text-xs text-zinc-400">{label}</dt><dd className="mt-1 break-words">{value}</dd></div>)}</dl>{investor.notes && <p className="mt-4 whitespace-pre-wrap border-t border-zinc-800 pt-4 text-zinc-400">{investor.notes}</p>}</section>
      <OwnershipPanel key={investor.id} investor={investor} />
    </>}
    {form && <Modal title="Edit investor" isSaving={saving} submitLabel="Save investor" onClose={() => setForm(null)} onSubmit={e => { e.preventDefault(); setSaving(true); setError(""); void updateInvestor(id, form).then(v => { setInvestor(v); setForm(null); }).catch(e => setError(e.message)).finally(() => setSaving(false)); }}>{error && <ErrorBanner message={error} />}<InvestorForm value={form} onChange={setForm} drivers={drivers} editing /></Modal>}
  </div>;
}
