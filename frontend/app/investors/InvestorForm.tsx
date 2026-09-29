"use client";

import type { Driver, InvestorInput } from "../lib/types";
import { controlClass, Field, FormSection, Toggle } from "../components/management/ManagementUI";

export const emptyInvestorInput: InvestorInput = { fullName: "", driverId: null, email: "", phone: "", notes: "", active: true };

export function InvestorForm({ value, onChange, drivers, editing }: {
  value: InvestorInput; onChange: (value: InvestorInput) => void; drivers: Driver[]; editing: boolean;
}) {
  const set = <K extends keyof InvestorInput>(key: K, next: InvestorInput[K]) => onChange({ ...value, [key]: next });
  return <div className="space-y-6">
    <FormSection title="Investor profile">
      <Field label="Linked driver" wide hint={editing ? "The linked identity is fixed. Update driver contact details from Drivers." : "Select an existing driver to share their profile, or create an independent investor."}>
        <select className={controlClass} disabled={editing} value={value.driverId ?? ""} onChange={(event) => {
          const driver = drivers.find((d) => d.id === event.target.value);
          onChange({ ...value, driverId: driver?.id ?? null, fullName: driver?.fullName ?? "", email: driver?.email ?? "", phone: driver?.phone ?? "" });
        }}>
          <option value="">Independent investor</option>
          {drivers.map((d) => <option key={d.id} value={d.id}>{d.fullName}</option>)}
        </select>
      </Field>
      <Field label="Full name" wide><input autoFocus required maxLength={200} disabled={!!value.driverId} value={value.fullName} onChange={(e) => set("fullName", e.target.value)} className={controlClass} /></Field>
      <Field label="Phone"><input type="tel" maxLength={100} disabled={!!value.driverId} value={value.phone} onChange={(e) => set("phone", e.target.value)} className={controlClass} /></Field>
      <Field label="Email"><input type="email" maxLength={254} disabled={!!value.driverId} value={value.email} onChange={(e) => set("email", e.target.value)} className={controlClass} /></Field>
    </FormSection>
    <FormSection title="Status and notes">
      <Toggle checked={value.active} onChange={(v) => set("active", v)} label="Active investor" description="Inactive investors retain their trucks and history, but cannot receive additional trucks." />
      <Field label="Internal notes" wide><textarea rows={3} maxLength={5000} value={value.notes} onChange={(e) => set("notes", e.target.value)} className={controlClass} /></Field>
    </FormSection>
  </div>;
}
