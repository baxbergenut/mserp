"use client";

import { expenseAssignment } from "./assignments";
import { activeExpenseCategories, changeExpenseCategory, expenseNames } from "./catalog";

import type { Driver, Expense, ExpensePage, ExpenseInput, Investor, Truck } from "../lib/types";
import {
  controlClass,
  Field,
  FormSection,
  Toggle,
} from "../components/management/ManagementUI";

export const emptyExpenseInput: ExpenseInput = {
  company: "MS Express",
  category: "",
  expenseDate: new Intl.DateTimeFormat("en-CA", { timeZone: "America/New_York", year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date()),
  truckId: null,
  driverId: null,
  unitNumber: "",
  driverName: "",
  amount: "",
  paymentType: "",
  expenseType: "",
  referenceNumber: "",
  description: "",
  coveredBy: "Company",
  paidBy: "",
  managerVerified: false,
  accountingVerified: false,
};

export function expenseToInput(expense: Expense): ExpenseInput {
  return {
    ownerId: expense.ownerId,
    company: expense.company,
    category: expense.category,
    expenseDate: expense.expenseDate ?? "",
    truckId: expense.truckId,
    driverId: expense.driverId,
    unitNumber: expense.unitNumber ?? "",
    driverName: expense.driverName ?? "",
    amount: expense.amount ?? "",
    paymentType: expense.paymentType ?? "",
    expenseType: expense.expenseType ?? "",
    referenceNumber: expense.referenceNumber ?? "",
    description: expense.description ?? "",
    coveredBy: expense.coveredBy ?? "",
    paidBy: expense.paidBy ?? "",
    managerVerified: expense.managerVerified,
    accountingVerified: expense.accountingVerified,
  };
}

export function ExpenseForm({
  value,
  options,
  originalCategory,
  drivers,
  trucks,
  owners,
  onChange,
}: {
  value: ExpenseInput;
  options: ExpensePage["options"];
  originalCategory?: string;
  drivers: Driver[];
  trucks: Truck[];
  owners: Investor[];
  onChange: (value: ExpenseInput) => void;
}) {
  const set = <K extends keyof ExpenseInput>(key: K, next: ExpenseInput[K]) =>
    onChange({ ...value, [key]: next });

  return (
    <div className="space-y-6">
      <FormSection title="Expense">
        <Field label="Company">
          <input
            required
            autoFocus
            list="expense-companies"
            value={value.company}
            onChange={(event) => set("company", event.target.value)}
            className={controlClass}
            placeholder="MS Express"
          />
          <datalist id="expense-companies">
            {options.companies.map((option) => <option key={option} value={option} />)}
          </datalist>
        </Field>
        <Field label="Category">
          <select aria-label="Category"
            required
            value={value.category}
            onChange={(event) => onChange(changeExpenseCategory(value, event.target.value, options.settings))}
            className={controlClass}
          >
            <option value="">Select category</option>
            {originalCategory && !activeExpenseCategories(options.settings).some(item => item.name === originalCategory) && <option value={originalCategory}>{originalCategory} (saved category)</option>}
            {activeExpenseCategories(options.settings).map(item => <option key={item.id}>{item.name}</option>)}
          </select>
        </Field>
        <Field label="Name" hint="Choose a default for this category or enter a custom name.">
          <input aria-label="Name"
            required
            pattern=".*\S.*"
            list="expense-names"
            value={value.expenseType}
            onChange={(event) => set("expenseType", event.target.value)}
            className={controlClass}
            placeholder="e.g. Parking violation"
          />
          <datalist id="expense-names">
            {expenseNames(options.settings, value.category).map((option) => <option key={option} value={option} />)}
          </datalist>
        </Field>
        <Field label="Expense date">
          <input aria-label="Expense date"
            required
            type="date"
            value={value.expenseDate}
            onChange={(event) => onChange({ ...value, expenseDate: event.target.value, ownerId: value.coveredBy === "Truck Owner" ? null : value.ownerId })}
            className={controlClass}
          />
        </Field>
        <Field label="Amount" hint="Enter dollars and cents without a currency symbol.">
          <input aria-label="Amount"
            required
            type="number"
            step="0.01"
            value={value.amount}
            onChange={(event) => set("amount", event.target.value)}
            className={controlClass}
            placeholder="0.00"
          />
        </Field>
      </FormSection>

      <FormSection title="Assignment and classification">
        <Field label="Unit number">
          <select aria-label="Unit number"
            value={value.truckId ?? ""}
            onChange={(event) => {
              onChange(expenseAssignment(value, "truck", event.target.value, drivers, trucks, owners));
            }}
            className={controlClass}
          >
            <option value="">No linked truck</option>
            {trucks.map((truck) => (
              <option key={truck.id} value={truck.id}>{truck.unitNumber}</option>
            ))}
          </select>
          {!value.truckId && (
            <>
              <input
                value={value.unitNumber}
                onChange={(event) => set("unitNumber", event.target.value)}
                className={controlClass}
                placeholder="Enter an unlinked unit number"
              />
              {value.unitNumber && (
                <p className="mt-1 text-[11px] text-amber-400">Unit {value.unitNumber} is not linked to a truck.</p>
              )}
            </>
          )}
        </Field>
        <Field label="Driver">
          <select aria-label="Driver"
            value={value.driverId ?? ""}
            onChange={(event) => {
              onChange(expenseAssignment(value, "driver", event.target.value, drivers, trucks, owners));
            }}
            className={controlClass}
          >
            <option value="">No linked driver</option>
            {drivers.map((driver) => (
              <option key={driver.id} value={driver.id}>{driver.fullName}</option>
            ))}
          </select>
          {!value.driverId && (
            <>
              <input
                value={value.driverName}
                onChange={(event) => set("driverName", event.target.value)}
                className={controlClass}
                placeholder="Enter an unlinked driver name"
              />
              {value.driverName && (
                <p className="mt-1 text-[11px] text-amber-400">Driver {value.driverName} is not linked.</p>
              )}
            </>
          )}
        </Field>
        <Field label="Who will cover" wide>
          <select aria-label="Who will cover" value={value.coveredBy} onChange={event => onChange({ ...value, coveredBy: event.target.value, ownerId: event.target.value === "Truck Owner" ? value.ownerId ?? trucks.find(t => t.id === value.truckId)?.ownerId ?? null : null })} className={controlClass}>
            {Array.from(new Set(["Company", "Driver", "Truck Owner", ...options.coveredBy, value.coveredBy])).filter(Boolean).map(option => <option key={option}>{option}</option>)}
          </select>
          {value.coveredBy === "Driver" && <p className="text-xs text-zinc-500">Deduct from the linked driver&apos;s pay. Unpaid amounts carry forward.</p>}
          {value.coveredBy === "Truck Owner" && <div className="mt-2 space-y-1">
            <select aria-label="Responsible truck owner" required value={value.ownerId ?? ""} onChange={event => onChange({...value, ownerId:event.target.value || null})} className={controlClass}>
              <option value="">Select the responsible owner</option>
              {owners.filter(o => !o.isCompany && (o.active || o.id === value.ownerId)).map(o => <option key={o.id} value={o.id}>{o.fullName}{o.driverId ? " · Driver" : " · Investor"}</option>)}
            </select>
            <p className="text-xs text-zinc-500">{owners.find(o => o.id === value.ownerId)?.driverId ? "Deduct from this owner's Driver Pay, regardless of who operates the truck." : "Investor responsibility. This will not be deducted from the operating driver's pay."}</p>
            <p className="text-xs text-amber-300">Confirm the owner responsible on the expense date. For historical expenses, select the owner explicitly.</p>
          </div>}
        </Field>
        <Field label="Payment method">
          <input
            aria-label="Payment method"
            list="payment-types"
            value={value.paymentType}
            onChange={(event) => set("paymentType", event.target.value)}
            className={controlClass}
            placeholder="e.g. Card 1456"
          />
          <datalist id="payment-types">
            {options.paymentTypes.map((option) => <option key={option} value={option} />)}
          </datalist>
        </Field>
        <Field label="Reference number" wide>
          <input
            value={value.referenceNumber}
            onChange={(event) => set("referenceNumber", event.target.value)}
            className={controlClass}
            placeholder="Invoice, receipt, or transaction reference"
          />
        </Field>
        <Field label="Description" wide>
          <textarea
            rows={3}
            value={value.description}
            onChange={(event) => set("description", event.target.value)}
            className={controlClass}
            placeholder="What was this expense for?"
          />
        </Field>
      </FormSection>

      <FormSection title="Responsibility and verification">
        <Field label="Paid by">
          <input
            list="expense-paid-by"
            value={value.paidBy}
            onChange={(event) => set("paidBy", event.target.value)}
            className={controlClass}
            placeholder="Name"
          />
          <datalist id="expense-paid-by">
            {options.paidBy.map((option) => <option key={option} value={option} />)}
          </datalist>
        </Field>
        <Toggle
          checked={value.managerVerified}
          onChange={(checked) => set("managerVerified", checked)}
          label="Manager verified"
          description="The manager has reviewed this expense."
        />
        <Toggle
          checked={value.accountingVerified}
          onChange={(checked) => set("accountingVerified", checked)}
          label="Accounting verified"
          description="Accounting has reviewed this expense."
        />
      </FormSection>
    </div>
  );
}
