"use client";

import { useState } from "react";
import { saveMaintenancePM } from "../lib/api";
import type { PreventativeTruck } from "../lib/types";
import { controlClass, ErrorBanner, Field, FormSection, Modal } from "../components/management/ManagementUI";
import { today } from "./maintenance";

export function MaintenancePMEditor({ truck, onClose, onSaved }: { truck: PreventativeTruck; onClose: () => void; onSaved: () => Promise<void> }) {
  const [date, setDate] = useState(truck.lastDate ?? "");
  const [mileage, setMileage] = useState(truck.lastMiles?.toString() ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  return <Modal title={`Update last PM · ${truck.unitNumber}`} isSaving={busy} submitLabel="Save PM" onClose={onClose} onSubmit={e => {
    e.preventDefault(); e.stopPropagation(); setBusy(true); setError("");
    void saveMaintenancePM({truckId: truck.truckId, recordId: truck.recordId, recordVersion: truck.recordVersion, baselineVersion: truck.baselineVersion, lastDate: date, lastMiles: mileage === "" ? null : Number(mileage)})
      .then(onSaved).then(onClose).catch(e => setError(e.message)).finally(() => setBusy(false));
  }}>
    {error && <ErrorBanner message={error} />}
    <FormSection title="Last completed PM">
      <Field label="Last PM date"><input aria-label="Last PM date" type="date" required max={today()} className={controlClass} value={date} onChange={e => setDate(e.target.value)} /></Field>
      <Field label="Service odometer (miles)"><input aria-label="PM service odometer" type="number" min="0" max="2000000000" step="1" className={controlClass} value={mileage} onChange={e => setMileage(e.target.value)} placeholder="Unknown" /></Field>
    </FormSection>
    <p className="mt-3 text-xs text-zinc-500">Updates PM due dates and reminders. Work orders and invoices keep their recorded details.</p>
  </Modal>;
}
