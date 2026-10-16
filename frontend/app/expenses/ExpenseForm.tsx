"use client";
import type { DriverOption, TruckOption, InvestorOption } from "@/app/lib/types";

import { ExpensePayerSelect, ExpenseResponsibilitySelect } from "./ExpenseFields";
import { expenseAssignment } from "./assignments";
import { ExpenseCostBreakdown, ExpenseNameSelect } from "./ExpenseCostBreakdown";
import { ExpenseFeePreview } from "./ExpenseFeePreview";
import { activeExpenseCategories, applyExpenseCategoryRules, changeExpenseCategory, expenseNames, isDriverOnlyExpense } from "./catalog";

import type { Expense, ExpensePage, ExpenseInput } from "../lib/types";
import {
  controlClass,
  Field,
  FormSection,
} from "../components/management/ManagementUI";

export const emptyExpenseInput: ExpenseInput = {
  costBreakdown: [],
  categoryId: "",
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
    costBreakdown: expense.costBreakdown ?? [],
    updatedAt: expense.updatedAt,
    ownerId: expense.ownerId,
    categoryId: expense.categoryId,
    expenseDate: expense.expenseDate ?? "",
    truckId: expense.truckId,
    driverId: expense.driverId,
    unitNumber: expense.unitNumber ?? "",
    driverName: expense.driverName ?? "",
    amount: expense.baseAmount ?? expense.amount ?? "",
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
  value: initialValue,
  options,
  originalCategory,
  allowedCategoryIds,
  drivers,
  trucks,
  owners,
  onChange,
}: {
  value: ExpenseInput;
  options: ExpensePage["options"];
  originalCategory?: { id: string; name: string };
  allowedCategoryIds: string[];
  drivers: DriverOption[];
  trucks: TruckOption[];
  owners: InvestorOption[];
  onChange: (value: ExpenseInput) => void;
}) {
  const driverOnly = isDriverOnlyExpense(options.settings, initialValue.categoryId);
  const value = applyExpenseCategoryRules(initialValue, options.settings);
  const change = (next: ExpenseInput) => onChange(applyExpenseCategoryRules(next, options.settings));
  const set = <K extends keyof ExpenseInput>(key: K, next: ExpenseInput[K]) =>
    change({ ...value, [key]: next });

  return (
    <div className="space-y-6">
      <FormSection title="Expense">
        <Field label="Category">
          <select autoFocus aria-label="Category"
            required
            value={value.categoryId}
            onChange={(event) => change(changeExpenseCategory(value, event.target.value, options.settings))}
            className={controlClass}
          >
            <option value="">Select category</option>
            {originalCategory && !activeExpenseCategories(options.settings, allowedCategoryIds).some(item => item.id === originalCategory.id) && <option value={originalCategory.id}>{originalCategory.name} (saved category)</option>}
            {activeExpenseCategories(options.settings, allowedCategoryIds).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}
          </select>
        </Field>
        <Field label="Expense type">
          <ExpenseNameSelect key={value.categoryId} value={value.expenseType} names={expenseNames(options.settings, value.categoryId)} onChange={next => set("expenseType", next)} />
        </Field>
        <Field label="Description" wide>
          <textarea aria-label="Description" rows={3} value={value.description} onChange={event => set("description", event.target.value)} className={controlClass} placeholder="Describe what this expense was for" />
        </Field>
        <Field label="Expense date">
          <input aria-label="Expense date"
            required
            type="date"
            value={value.expenseDate}
            onChange={(event) => set("expenseDate", event.target.value)}
            className={controlClass}
          />
        </Field>
        <Field label="Amount">
          <input aria-label="Amount"
            required
            type="number"
            step="0.01"
            value={value.amount}
            onChange={(event) => set("amount", event.target.value)}
            className={controlClass}
            placeholder="0.00"
          />
          <ExpenseFeePreview amount={value.amount} paymentType={value.paymentType} />
        </Field>
      </FormSection>
      <ExpenseCostBreakdown value={value} names={expenseNames(options.settings, value.categoryId)} onChange={change} />

      <FormSection title="Assignment and classification">
        <Field label="Unit number">
          <select aria-label="Unit number"
            value={value.truckId ?? ""}
            onChange={(event) => {
              change(expenseAssignment(value, "truck", event.target.value, drivers, trucks, owners));
            }}
            className={controlClass}
          >
            <option value="">No linked truck</option>
            {trucks.map((truck) => (
              <option key={truck.id} value={truck.id}>{truck.unitNumber}</option>
            ))}
          </select>

        </Field>
        <Field label="Driver">
          <select aria-label="Driver"
            required={driverOnly}
            value={value.driverId ?? ""}
            onChange={(event) => {
              change(expenseAssignment(value, "driver", event.target.value, drivers, trucks, owners));
            }}
            className={controlClass}
          >
            <option value="">{driverOnly ? "Select driver" : "No linked driver"}</option>
            {drivers.map((driver) => (
              <option key={driver.id} value={driver.id}>{driver.fullName}</option>
            ))}
          </select>

        </Field>
        {driverOnly ? <p className="text-xs text-zinc-400 sm:col-span-2">Charged to the selected driver.</p> : <>
        <Field label="Who will cover" wide>
          <ExpenseResponsibilitySelect value={value} trucks={trucks} owners={owners} onChange={change} className={controlClass} />
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
        <Field label="Paid by">
          <ExpensePayerSelect value={value.paidBy} users={options.paidBy} onChange={next => set("paidBy", next)} className={controlClass} />
        </Field>
        </>}
        <Field label="Reference number" wide>
          <input
            value={value.referenceNumber}
            onChange={(event) => set("referenceNumber", event.target.value)}
            className={controlClass}
          />
        </Field>
      </FormSection>

    </div>
  );
}
