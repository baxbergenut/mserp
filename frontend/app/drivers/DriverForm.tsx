"use client";

import type { Dispatcher, Driver, DriverInput, Truck } from "../lib/types";
import {
  controlClass,
  Field,
  FormSection,
  Toggle,
} from "../components/management/ManagementUI";

export const emptyDriverInput: DriverInput = {
  fullName: "",
  isOwnerOperator: false,
  payType: "cpm",
  payRate: 0,
  phone: "",
  email: "",
  licenseNumber: "",
  licenseState: "",
  licenseExpires: "",
  hireDate: "",
  address: "",
  city: "",
  state: "",
  postalCode: "",
  emergencyContact: "",
  dispatcherId: null,
  truckId: null,
  active: true,
  notes: "",
};

export function driverToInput(driver: Driver): DriverInput {
  return {
    fullName: driver.fullName,
    isOwnerOperator: driver.isOwnerOperator,
    payType: driver.payType,
    payRate: driver.payRate,
    phone: driver.phone ?? "",
    email: driver.email ?? "",
    licenseNumber: driver.licenseNumber ?? "",
    licenseState: driver.licenseState ?? "",
    licenseExpires: driver.licenseExpires?.slice(0, 10) ?? "",
    hireDate: driver.hireDate?.slice(0, 10) ?? "",
    address: driver.address ?? "",
    city: driver.city ?? "",
    state: driver.state ?? "",
    postalCode: driver.postalCode ?? "",
    emergencyContact: driver.emergencyContact ?? "",
    dispatcherId: driver.dispatcherId,
    truckId: driver.truckId,
    active: driver.active,
    notes: driver.notes ?? "",
  };
}

export function DriverForm({
  value,
  dispatchers,
  trucks,
  onChange,
}: {
  value: DriverInput;
  dispatchers: Dispatcher[];
  trucks: Truck[];
  onChange: (value: DriverInput) => void;
}) {
  const set = <K extends keyof DriverInput>(key: K, next: DriverInput[K]) =>
    onChange({ ...value, [key]: next });

  return (
    <div className="space-y-6">
      <FormSection title="Driver profile">
        <Field label="Full name" wide>
          <input
            required
            autoFocus
            value={value.fullName}
            onChange={(event) => set("fullName", event.target.value)}
            className={controlClass}
            placeholder="e.g. Marcus Reed"
          />
        </Field>
        <Field label="Driver type">
          <select
            value={value.isOwnerOperator ? "owner" : "company"}
            onChange={(event) =>
              set("isOwnerOperator", event.target.value === "owner")
            }
            className={controlClass}
          >
            <option value="company">Company driver</option>
            <option value="owner">Owner-operator</option>
          </select>
        </Field>
        <Field label="Hire date">
          <input
            type="date"
            value={value.hireDate}
            onChange={(event) => set("hireDate", event.target.value)}
            className={controlClass}
          />
        </Field>
        <Field label="Phone">
          <input
            type="tel"
            value={value.phone}
            onChange={(event) => set("phone", event.target.value)}
            className={controlClass}
            placeholder="(555) 555-0123"
          />
        </Field>
        <Field label="Email">
          <input
            type="email"
            value={value.email}
            onChange={(event) => set("email", event.target.value)}
            className={controlClass}
            placeholder="driver@company.com"
          />
        </Field>
        <Field label="Emergency contact" wide>
          <input
            value={value.emergencyContact}
            onChange={(event) => set("emergencyContact", event.target.value)}
            className={controlClass}
            placeholder="Name and phone number"
          />
        </Field>
      </FormSection>

      <FormSection title="Compensation">
        <Field label="Pay basis">
          <select
            value={value.payType}
            onChange={(event) =>
              set(
                "payType",
                event.target.value as DriverInput["payType"],
              )
            }
            className={controlClass}
          >
            <option value="cpm">Cents per mile (CPM)</option>
            <option value="gross_percentage">Percentage of gross</option>
          </select>
        </Field>
        <Field
          label={value.payType === "cpm" ? "Rate per mile ($)" : "Gross percentage (%)"}
          hint={value.payType === "cpm" ? "Example: 0.65 means 65 cents per mile" : "Enter a value from 0 to 100"}
        >
          <input
            required
            type="number"
            min="0"
            max={value.payType === "gross_percentage" ? "100" : undefined}
            step="0.0001"
            value={value.payRate}
            onChange={(event) => set("payRate", Number(event.target.value))}
            className={controlClass}
          />
        </Field>
      </FormSection>

      <FormSection title="Assignments">
        <Field label="Dispatcher">
          <select
            value={value.dispatcherId ?? ""}
            onChange={(event) =>
              set(
                "dispatcherId",
                event.target.value || null,
              )
            }
            className={controlClass}
          >
            <option value="">Unassigned</option>
            {dispatchers.map((dispatcher) => (
              <option key={dispatcher.id} value={dispatcher.id}>
                {dispatcher.fullName}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Truck">
          <select
            value={value.truckId ?? ""}
            onChange={(event) =>
              set("truckId", event.target.value || null)
            }
            className={controlClass}
          >
            <option value="">Unassigned</option>
            {trucks.map((truck) => (
              <option key={truck.id} value={truck.id}>
                {truck.unitNumber}
                {truck.driverName ? ` — currently ${truck.driverName}` : ""}
              </option>
            ))}
          </select>
        </Field>
      </FormSection>

      <FormSection title="License and address">
        <Field label="CDL / license number">
          <input
            value={value.licenseNumber}
            onChange={(event) => set("licenseNumber", event.target.value)}
            className={controlClass}
          />
        </Field>
        <Field label="License state">
          <input
            value={value.licenseState}
            onChange={(event) => set("licenseState", event.target.value)}
            className={controlClass}
            maxLength={2}
            placeholder="IL"
          />
        </Field>
        <Field label="License expiration">
          <input
            type="date"
            value={value.licenseExpires}
            onChange={(event) => set("licenseExpires", event.target.value)}
            className={controlClass}
          />
        </Field>
        <div className="hidden sm:block" />
        <Field label="Street address" wide>
          <input
            value={value.address}
            onChange={(event) => set("address", event.target.value)}
            className={controlClass}
          />
        </Field>
        <Field label="City">
          <input
            value={value.city}
            onChange={(event) => set("city", event.target.value)}
            className={controlClass}
          />
        </Field>
        <Field label="State / province">
          <input
            value={value.state}
            onChange={(event) => set("state", event.target.value)}
            className={controlClass}
          />
        </Field>
        <Field label="Postal code">
          <input
            value={value.postalCode}
            onChange={(event) => set("postalCode", event.target.value)}
            className={controlClass}
          />
        </Field>
      </FormSection>

      <FormSection title="Status and notes">
        <Toggle
          checked={value.active}
          onChange={(checked) => set("active", checked)}
          label="Active driver"
          description="Inactive drivers stay in historical records but are visually marked."
        />
        <Field label="Internal notes" wide>
          <textarea
            rows={3}
            value={value.notes}
            onChange={(event) => set("notes", event.target.value)}
            className={controlClass}
            placeholder="Optional notes about this driver"
          />
        </Field>
      </FormSection>
    </div>
  );
}
