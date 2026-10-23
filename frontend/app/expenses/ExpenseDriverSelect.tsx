"use client";

import type { DriverOption, ExpenseApplicant, ExpenseInput, InvestorOption, TruckOption } from "../lib/types";
import { expenseAssignment } from "./assignments";

export function ExpenseDriverSelect({ value, drivers, applicants = [], trucks, owners, onChange, className, required = false, label = "Driver" }: {
  value: ExpenseInput;
  drivers: DriverOption[];
  applicants?: ExpenseApplicant[];
  trucks: TruckOption[];
  owners: InvestorOption[];
  onChange: (value: ExpenseInput) => void;
  className: string;
  required?: boolean;
  label?: string;
}) {
  const choices = [
    ...drivers.map(driver => ({ id: driver.id, fullName: driver.fullName, name: driver.fullName })),
    ...(!required ? applicants.map(applicant => ({
      id: `applicant:${applicant.id}`,
      fullName: applicant.fullName,
      name: `${applicant.fullName} (${applicant.status === "hired" ? "Pending setup" : applicant.status === "rejected" ? "Rejected applicant" : applicant.status === "withdrawn" ? "Withdrawn applicant" : "Applicant"})${applicants.filter(other => other.fullName === applicant.fullName).length > 1 ? ` · ${applicant.id.slice(-6)}` : ""}`,
    })) : []),
  ].sort((a, b) => a.fullName.localeCompare(b.fullName, "en", { sensitivity: "base" }) || a.id.localeCompare(b.id));
  const selected = value.driverId || (value.applicantId ? `applicant:${value.applicantId}` : value.driverName ? "__unlinked" : "");

  return <select
    aria-label={label}
    required={required}
    value={selected}
    className={className}
    ref={select => { select?.setCustomValidity(required && !value.driverId ? "Select an existing driver for a driver-covered expense." : !value.updatedAt && !!value.driverName && !value.driverId && !value.applicantId ? "Select a driver or applicant from the dropdown, or clear the driver field." : ""); }}
    onChange={event => {
      const id = event.target.value;
      const applicant = id.startsWith("applicant:") ? applicants.find(person => `applicant:${person.id}` === id) : null;
      onChange(applicant
        ? { ...value, applicantId: applicant.id, driverId: null, driverName: applicant.fullName, truckId: null, unitNumber: "", coveredBy: "Company", ownerId: null }
        : expenseAssignment(value, "driver", id, drivers, trucks, owners));
    }}
  >
    <option value="">{required ? "Select driver" : "No driver"}</option>
    {selected && !choices.some(choice => choice.id === selected) && <option value={selected} disabled>{value.driverName} ({value.updatedAt ? "saved" : "unlinked"})</option>}
    {choices.map(choice => <option key={choice.id} value={choice.id}>{choice.name}</option>)}
  </select>;
}
