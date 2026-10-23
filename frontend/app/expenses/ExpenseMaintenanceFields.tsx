"use client";

import { useEffect, useRef, useState } from "react";
import { fetchMaintenanceOdometer, fetchMaintenanceOptions } from "@/app/lib/api";
import type { ExpenseInput, ExpenseMaintenance, MaintenanceType, MaintenanceVendor } from "@/app/lib/types";
import { controlClass, Field, FormSection } from "@/app/components/management/ManagementUI";

const emptyDetails: ExpenseMaintenance = { odometer: null, vendorId: null, vendorName: "", vendorAddress: "", vendorPhone: "", vendorContact: "", oilType: "" };
const oilGrades = ["10W-30", "15W-40", "5W-40", "5W-30", "10W-40", "0W-40"];
const today = () => new Intl.DateTimeFormat("en-CA", { timeZone: "America/New_York", year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());

export function ExpenseMaintenanceFields({ value, onChange }: { value: ExpenseInput; onChange: (next: ExpenseInput) => void }) {
  const [vendors, setVendors] = useState<MaintenanceVendor[]>([]);
  const [types, setTypes] = useState<MaintenanceType[]>([]);
  const [customOil, setCustomOil] = useState(false);
  const [readingHint, setReadingHint] = useState<{ truckId: string; date: string; text: string } | null>(null);
  const [error, setError] = useState("");
  const details = value.maintenance ?? emptyDetails;
  const latest = useRef({ value, onChange });
  const automatic = useRef<{ truckId: string; date: string; mileage: number } | null>(null);
  useEffect(() => { latest.current = { value, onChange }; });
  const name = value.expenseType.trim().toLowerCase();
  const isOilChange = name === "oil change" || name === "pm service" || types.some(type => (type.code === "preventive" || type.code === "oil") && type.name.trim().toLowerCase() === name);
  const set = (next: Partial<ExpenseMaintenance>) => onChange({ ...value, maintenance: { ...details, ...next } });
  useEffect(() => {
    let active = true;
    fetchMaintenanceOptions().then(options => { if (active) { setVendors(options.vendors.filter(vendor => vendor.active)); setTypes(options.types ?? []); } })
      .catch(reason => { if (active) setError(reason instanceof Error ? reason.message : "Unable to load shops"); });
    return () => { active = false; };
  }, []);
  useEffect(() => {
    const prior = automatic.current;
    if (prior && (prior.truckId !== value.truckId || prior.date !== value.expenseDate)) {
      automatic.current = null;
      const current = latest.current;
      if (current.value.maintenance?.odometer === prior.mileage) {
        current.onChange({ ...current.value, maintenance: { ...current.value.maintenance, odometer: null } });
        return;
      }
    }
    if (value.updatedAt || !value.truckId || value.expenseDate !== today() || details.odometer != null) return;
    const controller = new AbortController();
    const truckId = value.truckId, date = value.expenseDate;
    fetchMaintenanceOdometer(truckId, controller.signal).then(result => {
      if (controller.signal.aborted) return;
      const current = latest.current;
      if (current.value.truckId !== truckId || current.value.expenseDate !== date || date !== today() || current.value.updatedAt || current.value.maintenance?.odometer != null) return;
      setReadingHint({ truckId, date, text: result.reading ? `${result.reading.source === "samsara" ? "Samsara" : result.reading.source === "manual" ? "Manual" : "Five ELD"} · ${new Date(result.reading.reportedAt).toLocaleString()}` : result.message });
      if (result.reading) {
        automatic.current = { truckId, date, mileage: result.reading.mileage };
        current.onChange({ ...current.value, maintenance: { ...(current.value.maintenance ?? emptyDetails), odometer: result.reading.mileage } });
      }
    }).catch(() => { if (!controller.signal.aborted) setReadingHint({ truckId, date, text: "Enter the odometer manually; the current reading is unavailable." }); });
    return () => controller.abort();
  }, [value.truckId, value.expenseDate, value.updatedAt, details.odometer]);
  const hint = readingHint?.truckId === value.truckId && readingHint.date === value.expenseDate ? readingHint.text : "";
  return <FormSection title="Maintenance details">
    <Field label="Odometer">
      <input aria-label="Odometer" type="number" min="0" max="2000000000" step="1" placeholder="Unknown" value={details.odometer ?? ""} onChange={event => { automatic.current = null; setReadingHint(null); set({ odometer: event.target.value === "" ? null : Number(event.target.value) }); }} className={controlClass} />
      {hint && <p role="status" className="mt-1 text-xs text-zinc-400">{hint}</p>}
    </Field>
    {isOilChange && <Field label="Oil type">
      <select aria-label="Oil type" className={controlClass} value={customOil || details.oilType && !oilGrades.includes(details.oilType) ? "custom" : details.oilType} onChange={event => { setCustomOil(event.target.value === "custom"); set({ oilType: event.target.value === "custom" ? "" : event.target.value }); }}>
        <option value="">Unknown</option>
        {oilGrades.map(grade => <option key={grade} value={grade}>{grade}</option>)}
        <option value="custom">Other / custom oil type</option>
      </select>
      {(customOil || !!details.oilType && !oilGrades.includes(details.oilType)) && <input aria-label="Custom oil type" value={details.oilType} maxLength={100} onChange={event => set({ oilType: event.target.value.toUpperCase() })} className={`${controlClass} mt-2`} placeholder="Enter oil grade" />}
    </Field>}
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
