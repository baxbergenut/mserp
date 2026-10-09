"use client";

import { useState } from "react";
import { changeDriverStatus } from "../lib/api";
import type { Driver, DriverInput } from "../lib/types";
import { Modal, Field, ErrorBanner, controlClass } from "../components/management/ManagementUI";
import { AssignmentWeekField } from "../components/management/AssignmentWeekField";
import { currentChargeWeek } from "../accounting/driver-charges/charges";

export const driverStatuses = ["active", "vacation", "home", "terminated"] as const;
export function DriverStatusDialog({ driver, onClose, onSaved }: { driver: Driver; onClose: () => void; onSaved: (driver: Driver) => void }) {
  const [status, setStatus] = useState<NonNullable<DriverInput["status"]>>(driver.status ?? (driver.active ? "active" : "terminated"));
  const [date, setDate] = useState(driver.terminationDate?.slice(0, 10) ?? new Intl.DateTimeFormat("en-CA", { timeZone: "America/New_York" }).format(new Date()));
  const [week, setWeek] = useState(currentChargeWeek());
  const [pauseWeek, setPauseWeek] = useState(currentChargeWeek());
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  return <Modal title={`Change status · ${driver.fullName}`} submitLabel="Save status" isSaving={saving} onClose={onClose} onSubmit={event => {
    event.preventDefault(); if (saving) return; setSaving(true); setError("");
    void changeDriverStatus(driver.id, { status, terminationDate: status === "terminated" ? date : "", assignmentWeek: week, chargePauseWeek: pauseWeek, updatedAt: driver.updatedAt }).then(onSaved).catch(e => setError(e.message)).finally(() => setSaving(false));
  }}><div className="space-y-4">
    {error && <ErrorBanner message={error} />}
    <Field label="Driver status"><select aria-label="Driver status" className={controlClass} value={status} disabled={saving} onChange={event => setStatus(event.target.value as typeof status)}>{driverStatuses.map(value => <option key={value} value={value}>{value[0].toUpperCase() + value.slice(1)}</option>)}</select></Field>
    {(status === "terminated" || !driver.active) && <AssignmentWeekField value={week} onChange={setWeek} />}
    {status === "terminated" && <>
      <Field label="Termination date"><input aria-label="Termination date" className={controlClass} required type="date" min="2000-01-01" max={new Intl.DateTimeFormat("en-CA", { timeZone: "America/New_York" }).format(new Date())} value={date} onChange={event => setDate(event.target.value)} /></Field>
      <Field label="Pause charges from week (Monday)"><input aria-label="Pause charges from week (Monday)" className={controlClass} required type="date" min="2000-01-03" step={7} value={pauseWeek} onChange={event => setPauseWeek(event.target.value)} /></Field>
      <p className="text-xs text-zinc-500">Termination releases truck and dispatcher assignments and pauses charges. Outstanding balances remain.</p>
    </>}
  </div></Modal>;
}
