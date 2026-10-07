"use client";

import { useEffect, useState } from "react";
import { Landmark } from "lucide-react";
import { PageHeader } from "@/app/components/PageHeader";
import { ErrorBanner, controlClass } from "@/app/components/management/ManagementUI";
import { fetchDriverEscrowSetting, saveDriverEscrowSetting } from "@/app/lib/api";
import type { DriverEscrowSetting } from "@/app/lib/types";
import { usePermissions } from "@/app/lib/access";
import { EscrowTable } from "./EscrowTable";

export default function EscrowPage() {
  const canManage = usePermissions().includes("escrow.write");
  const [setting, setSetting] = useState<DriverEscrowSetting | null>(null);
  const [amount, setAmount] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let cancelled = false;
    fetchDriverEscrowSetting().then(value => { if (!cancelled) { setSetting(value); setAmount(value.defaultAmount); setError(""); } })
      .catch(reason => { if (!cancelled) setError(reason instanceof Error ? reason.message : "Could not load the default"); });
    return () => { cancelled = true; };
  }, [attempt]);
  async function save() {
    if (!setting) return;
    setSaving(true); setError(""); setNotice("");
    try {
      const value = await saveDriverEscrowSetting({ defaultAmount: amount, version: setting.version });
      setSetting(value); setAmount(value.defaultAmount); setNotice("Default saved");
    } catch (reason) { setError(reason instanceof Error ? reason.message : "Could not save the default"); }
    finally { setSaving(false); }
  }
  return <div className="space-y-5">
    <PageHeader><div><h1 className="flex items-center gap-2 text-lg font-semibold text-zinc-100"><Landmark className="h-5 w-5 text-zinc-500" />Escrow</h1><p className="mt-1 text-sm text-zinc-500">Driver escrow balances and weekly payroll collections.</p></div></PageHeader>
    <section className="flex flex-wrap items-end justify-between gap-4 rounded-xl border border-zinc-800 bg-zinc-950/30 p-4">
      <div><h2 className="text-sm font-medium text-zinc-200">New-driver default</h2><p className="mt-1 text-xs text-zinc-500">Used during driver setup. Existing escrow targets and payments stay unchanged.</p></div>
      <form className="flex flex-wrap items-end gap-2" onSubmit={event => { event.preventDefault(); void save(); }}>
        <label className="text-xs text-zinc-400">Default amount ($)<input aria-label="Default driver escrow amount" className={`${controlClass} mt-1 w-40`} type="number" required min="0.01" step="0.01" value={amount} onChange={event => { setAmount(event.target.value); setNotice(""); }} disabled={!canManage || !setting || saving} /></label>
        {canManage && <button disabled={!setting || saving} className="rounded-lg bg-blue-600 px-3 py-2 text-xs text-white disabled:opacity-40">{saving ? "Saving…" : "Save default"}</button>}
      </form>
      {error && <div className="w-full space-y-2"><ErrorBanner message={error} /><button className="text-xs text-blue-400" disabled={saving} onClick={() => setAttempt(value => value + 1)}>Reload default</button></div>}
      {notice && <p className="w-full text-xs text-emerald-400" role="status">{notice}</p>}
    </section>
    <EscrowTable />
  </div>;
}
