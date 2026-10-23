"use client";

import { useEffect, useState } from "react";
import { fetchMaintenanceOptions } from "@/app/lib/api";
import type { ExpenseInput, ExpenseMaintenance, MaintenanceVendor } from "@/app/lib/types";
import { controlClass, Field, FormSection } from "@/app/components/management/ManagementUI";

const emptyDetails: ExpenseMaintenance = { odometer: null, vendorId: null, vendorName: "", vendorAddress: "", vendorPhone: "", vendorContact: "", oilType: "" };

export function ExpenseMaintenanceFields({ value, onChange }: { value: ExpenseInput; onChange: (next: ExpenseInput) => void }) {
  const [vendors, setVendors] = useState<MaintenanceVendor[]>([]);
  const [error, setError] = useState("");
  const details = value.maintenance ?? emptyDetails;
  const set = (next: Partial<ExpenseMaintenance>) => onChange({ ...value, maintenance: { ...details, ...next } });
  useEffect(() => {
    let active = true;
    fetchMaintenanceOptions().then(options => { if (active) setVendors(options.vendors.filter(vendor => vendor.active)); })
      .catch(reason => { if (active) setError(reason instanceof Error ? reason.message : "Unable to load shops"); });
    return () => { active = false; };
  }, []);
  return <FormSection title="Maintenance details">
    <Field label="Odometer">
      <input aria-label="Odometer" type="number" min="0" max="2000000000" step="1" placeholder="Unknown" value={details.odometer ?? ""} onChange={event => set({ odometer: event.target.value === "" ? null : Number(event.target.value) })} className={controlClass} />
    </Field>
    <Field label="Oil type">
      <input aria-label="Oil type" value={details.oilType} maxLength={100} onChange={event => set({ oilType: event.target.value })} className={controlClass} placeholder="If applicable" />
    </Field>
    <Field label="Shop">
      <select aria-label="Shop" value={details.vendorId ?? ""} className={controlClass} onChange={event => {
        const vendor = vendors.find(item => item.id === event.target.value);
        set(vendor ? { vendorId: vendor.id, vendorName: vendor.name, vendorAddress: vendor.address, vendorPhone: vendor.phone, vendorContact: vendor.contact } : { vendorId: null });
      }}>
        <option value="">Enter shop details</option>
        {details.vendorId && !vendors.some(v => v.id === details.vendorId) && <option value={details.vendorId}>{details.vendorName} (saved)</option>}
        {vendors.map(vendor => <option key={vendor.id} value={vendor.id}>{vendor.name}</option>)}
      </select>
      {error && <p role="alert" className="text-xs text-amber-400">{error}</p>}
    </Field>
    <Field label="Shop name"><input aria-label="Shop name" value={details.vendorName} maxLength={200} onChange={event => set({ vendorName: event.target.value })} className={controlClass} /></Field>
    <Field label="Shop address" wide><input aria-label="Shop address" value={details.vendorAddress} maxLength={1000} onChange={event => set({ vendorAddress: event.target.value })} className={controlClass} /></Field>
    <Field label="Shop phone"><input aria-label="Shop phone" type="tel" value={details.vendorPhone} maxLength={100} onChange={event => set({ vendorPhone: event.target.value })} className={controlClass} /></Field>
    <Field label="Shop contact"><input aria-label="Shop contact" value={details.vendorContact} maxLength={200} onChange={event => set({ vendorContact: event.target.value })} className={controlClass} /></Field>
  </FormSection>;
}
