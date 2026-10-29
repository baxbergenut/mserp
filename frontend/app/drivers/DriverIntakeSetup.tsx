"use client";

import { useEffect, useRef, useState } from "react";
import { Modal, ErrorBanner } from "../components/management/ManagementUI";
import { canCreateDriver, usePermissions } from "../lib/access";
import { completeDriverIntake, fetchDriverIntakeById, fetchDriverSetupDefaults, fetchDriverOptions, fetchTruckOptions, fetchDispatcherOptions, fetchInvestorOptions } from "../lib/api";
import type { DriverInput, DriverServiceIntent } from "../lib/types";

async function loadSetup(id: string, canCreateTruck: boolean) {
  const [intake, defaults, drivers, trucks, dispatchers, investors, wizard, driverForm, truckForm, services] = await Promise.all([
    fetchDriverIntakeById(id), fetchDriverSetupDefaults(), fetchDriverOptions(), fetchTruckOptions(), fetchDispatcherOptions(), fetchInvestorOptions(),
    import("./DriverSetup"), import("./DriverForm"), import("../trucks/TruckForm"), import("./DriverServices"),
  ]);
  const source = intake.driver;
  const form: DriverInput = {
    ...driverForm.emptyDriverInput,
    newTruck: canCreateTruck ? { ...truckForm.emptyTruckInput } : undefined,
    escrowAmount: defaults.escrowAmount, fullName: source.fullName, hireDate: source.hireDate,
    firstName: source.firstName ?? "", lastName: source.lastName ?? "",
    dateOfBirth: source.dateOfBirth ?? "",
    phone: source.phone ?? "", email: source.email ?? "", address: source.address ?? "",
    city: source.city ?? "", state: source.state ?? "", postalCode: source.postalCode ?? "",
    licenseNumber: source.licenseNumber ?? "", licenseState: source.licenseState ?? "", licenseExpires: source.licenseExpires ?? "",
  };
  return { intake, drivers, trucks, dispatchers, investors, form, Wizard: wizard.DriverSetup, defaultServices: services.defaultDriverServices };
}

// Keep the shell available immediately; load the full wizard only when requested.
export function DriverIntakeSetup({ intakeId, name, onClose, onSaved }: {
  intakeId: string; name: string; onClose: () => void; onSaved: () => void;
}) {
  const permissions = usePermissions();
  const allowed = canCreateDriver(permissions) && permissions.includes("tasks.review");
  const canCreateTruck = permissions.includes("trucks.create");
  const [setup, setSetup] = useState<Awaited<ReturnType<typeof loadSetup>> | null>(null);
  const [form, setForm] = useState<DriverInput | null>(null);
  const [services, setServices] = useState<DriverServiceIntent[]>([]);
  const [linkId, setLinkId] = useState("");
  const [separateConfirmed, setSeparateConfirmed] = useState(false);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [saving, setSaving] = useState(false);
  const saveLatch = useRef(false);

  useEffect(() => {
    if (!allowed) return;
    let active = true;
    loadSetup(intakeId, canCreateTruck).then(value => {
      if (!active) return;
      setSetup(value); setForm(value.form); setServices(value.defaultServices());
    }).catch(reason => { if (active) setError(reason instanceof Error ? reason.message : "Unable to load driver setup."); });
    return () => { active = false; };
  }, [intakeId, canCreateTruck, allowed, attempt]);

  async function save() {
    if (!allowed || !form || saveLatch.current) return;
    saveLatch.current = true; setSaving(true); setError("");
    try {
      await completeDriverIntake(intakeId, { ...(linkId ? { linkDriverId: linkId } : { driver: form, separateConfirmed }), services });
      onSaved();
    } catch (reason) { setError(reason instanceof Error ? reason.message : "Unable to complete driver setup."); }
    finally { saveLatch.current = false; setSaving(false); }
  }

  if (!allowed || !setup || !form) return <Modal title={`Set up ${name}`} isSaving={false} hideSubmit submitLabel="Continue" onClose={onClose} onSubmit={event => event.preventDefault()}>
    {!allowed ? <ErrorBanner message="Driver setup requires Tasks Review, Add driver, private details editing, compensation editing and escrow creation." /> : error ? <>
      <ErrorBanner message={error} />
      <button type="button" className="ui-button mt-3" onClick={() => { setError(""); setAttempt(value => value + 1); }}>Retry setup</button>
    </> : <p role="status" className="text-sm text-zinc-400">Loading setup options…</p>}
  </Modal>;

  return <setup.Wizard value={form} onChange={value => { setError(""); setForm(value); }} intake={setup.intake}
    drivers={setup.drivers} trucks={setup.trucks} dispatchers={setup.dispatchers} investors={setup.investors}
    linkId={linkId} onLink={id => { setLinkId(id); setServices(setup.defaultServices()); }}
    separateConfirmed={separateConfirmed} onConfirmSeparate={setSeparateConfirmed} services={services} onServices={setServices}
    saving={saving} loading={false} error={error} onSave={save} onClose={() => { if (!saveLatch.current) onClose(); }} />;
}
