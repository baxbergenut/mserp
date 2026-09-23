"use client";

import type { Driver, Expense, ExpenseCategory, ExpenseInput, Truck } from "../lib/types";
import {
  controlClass,
  Field,
  FormSection,
  Toggle,
} from "../components/management/ManagementUI";

export const EXPENSE_CATEGORIES: ExpenseCategory[] = [
  "Maintenance",
  "Other",
  "Safety",
  "HR",
  "Administrative",
];

export const emptyExpenseInput: ExpenseInput = {
  company: "MS Express",
  category: "Maintenance",
  expenseDate: new Date().toISOString().slice(0, 10),
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
  drivers,
  trucks,
  onChange,
}: {
  value: ExpenseInput;
  options: {
    companies: string[];
    paymentTypes: string[];
    expenseTypes: string[];
    paidBy: string[];
    coveredBy: string[];
  };
  drivers: Driver[];
  trucks: Truck[];
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
          <select
            required
            value={value.category}
            onChange={(event) => set("category", event.target.value as ExpenseCategory)}
            className={controlClass}
          >
            {EXPENSE_CATEGORIES.map((category) => <option key={category}>{category}</option>)}
          </select>
        </Field>
        <Field label="Expense date">
          <input
            required
            type="date"
            value={value.expenseDate}
            onChange={(event) => set("expenseDate", event.target.value)}
            className={controlClass}
          />
        </Field>
        <Field label="Amount" hint="Enter dollars and cents without a currency symbol.">
          <input
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
          <select
            value={value.truckId ?? ""}
            onChange={(event) => {
              const truckId = event.target.value || null;
              const unitNumber = trucks.find((truck) => truck.id === truckId)?.unitNumber ?? value.unitNumber;
              onChange({ ...value, truckId, unitNumber });
            }}
            className={controlClass}
          >
            <option value="">No linked truck</option>
            {trucks.map((truck) => (
              <option key={truck.id} value={truck.id}>{truck.unitNumber}</option>
            ))}
          </select>
          {!value.truckId && value.unitNumber && (
            <p className="mt-1 text-[11px] text-amber-400">Imported unit {value.unitNumber} is not linked.</p>
          )}
        </Field>
        <Field label="Driver">
          <select
            value={value.driverId ?? ""}
            onChange={(event) => {
              const driverId = event.target.value || null;
              const driverName = drivers.find((driver) => driver.id === driverId)?.fullName ?? value.driverName;
              onChange({ ...value, driverId, driverName });
            }}
            className={controlClass}
          >
            <option value="">No linked driver</option>
            {drivers.map((driver) => (
              <option key={driver.id} value={driver.id}>{driver.fullName}</option>
            ))}
          </select>
          {!value.driverId && value.driverName && (
            <p className="mt-1 text-[11px] text-amber-400">Imported driver {value.driverName} is not linked.</p>
          )}
        </Field>
        <Field label="Expense type">
          <input
            list="expense-types"
            value={value.expenseType}
            onChange={(event) => set("expenseType", event.target.value)}
            className={controlClass}
            placeholder="e.g. Tire issue"
          />
          <datalist id="expense-types">
            {options.expenseTypes.map((option) => <option key={option} value={option} />)}
          </datalist>
        </Field>
        <Field label="Payment type">
          <input
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
        <Field label="Who will cover">
          <input
            list="expense-covered-by"
            value={value.coveredBy}
            onChange={(event) => set("coveredBy", event.target.value)}
            className={controlClass}
            placeholder="Company, Driver, Truck Owner…"
          />
          <datalist id="expense-covered-by">
            {options.coveredBy.map((option) => <option key={option} value={option} />)}
          </datalist>
        </Field>
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
