"use client";
import type { TruckOption, DispatcherOption } from "@/app/lib/types";
import { useEffect, useState } from "react";
import { fetchDispatcherOptions, fetchTruckOptions, updateDriver } from "@/app/lib/api";
import type { Driver } from "@/app/lib/types";
import { Modal, ErrorBanner } from "@/app/components/management/ManagementUI";
import { DriverForm, driverToInput } from "../DriverForm";

export function DriverEditor({ driver, onClose, onSaved }: { driver: Driver; onClose: () => void; onSaved: (driver: Driver) => void }) {
  const [form, setForm] = useState(() => driverToInput(driver));
  const [trucks, setTrucks] = useState<TruckOption[]>([]);
  const [dispatchers, setDispatchers] = useState<DispatcherOption[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => { let cancelled = false; Promise.all([fetchTruckOptions(), fetchDispatcherOptions()]).then(([t, d]) => { if (!cancelled) { setTrucks(t); setDispatchers(d); setLoading(false); } }).catch(e => { if (!cancelled) setError(e.message); }); return () => { cancelled = true; }; }, []);
  return <Modal title="Edit driver" description={driver.fullName} isSaving={saving || (loading && !error)} submitLabel="Save driver" onClose={onClose} onSubmit={event => {
    event.preventDefault(); if (loading) return; setSaving(true); setError("");
    void updateDriver(driver.id, form).then(onSaved).catch(e => setError(e.message)).finally(() => setSaving(false));
  }}>{error && <ErrorBanner message={error} />}{loading ? <p className="py-8 text-sm text-zinc-500">Loading assignment options…</p> : <DriverForm editingDriverId={driver.id} value={form} onChange={next => { if (!!next.isTeamDriver !== !!form.isTeamDriver) setError(""); setForm(next); }} trucks={trucks} dispatchers={dispatchers} />}</Modal>;
}
