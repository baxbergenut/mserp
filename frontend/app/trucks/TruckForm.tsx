"use client";

import type { Driver, Truck, TruckInput } from "../lib/types";
import {
  controlClass,
  Field,
  FormSection,
  Toggle,
} from "../components/management/ManagementUI";

export const emptyTruckInput: TruckInput = {
  unitNumber: "",
  vin: "",
  year: null,
  make: "",
  model: "",
  licensePlate: "",
  licenseState: "",
  isCompanyOwned: true,
  status: "available",
  mileage: null,
  registrationExpires: "",
  insuranceExpires: "",
  lastServiceDate: "",
  nextServiceMiles: null,
  driverId: null,
  active: true,
  notes: "",
};

export function truckToInput(truck: Truck): TruckInput {
  return {
    unitNumber: truck.unitNumber,
    vin: truck.vin ?? "",
    year: truck.year,
    make: truck.make ?? "",
    model: truck.model ?? "",
    licensePlate: truck.licensePlate ?? "",
    licenseState: truck.licenseState ?? "",
    isCompanyOwned: truck.isCompanyOwned,
    status: truck.status,
    mileage: truck.mileage,
    registrationExpires: truck.registrationExpires?.slice(0, 10) ?? "",
    insuranceExpires: truck.insuranceExpires?.slice(0, 10) ?? "",
    lastServiceDate: truck.lastServiceDate?.slice(0, 10) ?? "",
    nextServiceMiles: truck.nextServiceMiles,
    driverId: truck.driverId,
    active: truck.active,
    notes: truck.notes ?? "",
  };
}

export function TruckForm({
  value,
  drivers,
  onChange,
}: {
  value: TruckInput;
  drivers: Driver[];
  onChange: (value: TruckInput) => void;
}) {
  const set = <K extends keyof TruckInput>(key: K, next: TruckInput[K]) =>
    onChange({ ...value, [key]: next });
  const numberOrNull = (raw: string) => (raw === "" ? null : Number(raw));

  return (
    <div className="space-y-6">
      <FormSection title="Truck profile">
        <Field label="Unit number">
          <input
            required
            autoFocus
            value={value.unitNumber}
            onChange={(event) => set("unitNumber", event.target.value)}
            className={controlClass}
            placeholder="e.g. 1042"
          />
        </Field>
        <Field label="VIN">
          <input
            value={value.vin}
            onChange={(event) => set("vin", event.target.value.toUpperCase())}
            className={controlClass}
            maxLength={17}
          />
        </Field>
        <Field label="Year">
          <input
            type="number"
            min="1900"
            max="2200"
            value={value.year ?? ""}
            onChange={(event) => set("year", numberOrNull(event.target.value))}
            className={controlClass}
          />
        </Field>
        <Field label="Make">
          <input
            value={value.make}
            onChange={(event) => set("make", event.target.value)}
            className={controlClass}
            placeholder="Freightliner"
          />
        </Field>
        <Field label="Model">
          <input
            value={value.model}
            onChange={(event) => set("model", event.target.value)}
            className={controlClass}
            placeholder="Cascadia"
          />
        </Field>
        <Field label="Ownership">
          <select
            value={value.isCompanyOwned ? "company" : "leased"}
            onChange={(event) => set("isCompanyOwned", event.target.value === "company")}
            className={controlClass}
          >
            <option value="company">Company owned</option>
            <option value="leased">Owner / leased</option>
          </select>
        </Field>
      </FormSection>

      <FormSection title="Registration and insurance">
        <Field label="License plate">
          <input
            value={value.licensePlate}
            onChange={(event) => set("licensePlate", event.target.value)}
            className={controlClass}
          />
        </Field>
        <Field label="Plate state">
          <input
            value={value.licenseState}
            onChange={(event) => set("licenseState", event.target.value)}
            className={controlClass}
            maxLength={2}
            placeholder="IL"
          />
        </Field>
        <Field label="Registration expires">
          <input
            type="date"
            value={value.registrationExpires}
            onChange={(event) => set("registrationExpires", event.target.value)}
            className={controlClass}
          />
        </Field>
        <Field label="Insurance expires">
          <input
            type="date"
            value={value.insuranceExpires}
            onChange={(event) => set("insuranceExpires", event.target.value)}
            className={controlClass}
          />
        </Field>
      </FormSection>

      <FormSection title="Operations and assignment">
        <Field label="Assigned driver" hint="Selecting a driver releases any truck currently assigned to them.">
          <select
            value={value.driverId ?? ""}
            onChange={(event) => set("driverId", event.target.value || null)}
            className={controlClass}
          >
            <option value="">Unassigned</option>
            {drivers.map((driver) => (
              <option key={driver.id} value={driver.id}>
                {driver.fullName}{driver.truckUnit ? ` — currently ${driver.truckUnit}` : ""}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Status" hint="An assigned driver automatically sets the status to Assigned.">
          <select
            value={value.status}
            onChange={(event) => set("status", event.target.value as TruckInput["status"])}
            className={controlClass}
          >
            <option value="available">Available</option>
            {value.driverId !== null && <option value="assigned">Assigned</option>}
            <option value="maintenance">Maintenance</option>
            <option value="out_of_service">Out of service</option>
          </select>
        </Field>
        <Field label="Current mileage">
          <input
            type="number"
            min="0"
            value={value.mileage ?? ""}
            onChange={(event) => set("mileage", numberOrNull(event.target.value))}
            className={controlClass}
          />
        </Field>
        <Field label="Next service at (miles)">
          <input
            type="number"
            min="0"
            value={value.nextServiceMiles ?? ""}
            onChange={(event) => set("nextServiceMiles", numberOrNull(event.target.value))}
            className={controlClass}
          />
        </Field>
        <Field label="Last service date">
          <input
            type="date"
            value={value.lastServiceDate}
            onChange={(event) => set("lastServiceDate", event.target.value)}
            className={controlClass}
          />
        </Field>
      </FormSection>

      <FormSection title="Status and notes">
        <Toggle
          checked={value.active}
          onChange={(checked) => set("active", checked)}
          label="Active truck"
          description="Inactive trucks remain available in historical records."
        />
        <Field label="Internal notes" wide>
          <textarea
            rows={3}
            value={value.notes}
            onChange={(event) => set("notes", event.target.value)}
            className={controlClass}
            placeholder="Maintenance details, equipment notes, or other context"
          />
        </Field>
      </FormSection>
    </div>
  );
}
