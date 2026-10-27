"use client";
import type { ExpenseDriverChoice as DriverOption, ExpenseTruckChoice as TruckOption, ExpenseOwnerChoice as InvestorOption } from "@/app/lib/types";

import { ExpensePayerSelect, ExpensePaymentMethodSelect, ExpenseResponsibilitySelect } from "./ExpenseFields";
import { ExpenseDriverSelect } from "./ExpenseDriverSelect";
import { expenseAssignment } from "./assignments";
import { ExpenseNameSelect } from "./ExpenseNameSelect";
import { ExpenseMaintenanceFields } from "./ExpenseMaintenanceFields";
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
  applicantId: null,
  unitNumber: "",
  driverName: "",
  amount: "",
  paymentType: "",
  expenseType: "",
  referenceNumber: "",
  description: "",
  coveredBy: "Company",
  paidBy: "",
  accountingVerified: false,
};

export function expenseToInput(expense: Expense): ExpenseInput {
  return {
    costBreakdown: [],
    maintenance: expense.maintenance,
    legacyPaymentTotal: expense.baseAmount == null && expense.paymentType?.toUpperCase() === "EFS" ? expense.amount ?? undefined : undefined,
    updatedAt: expense.updatedAt,
    savedAmount: expense.baseAmount ?? expense.amount ?? "",
    ownerId: expense.ownerId,
    categoryId: expense.categoryId,
    expenseDate: expense.expenseDate ?? "",
    truckId: expense.truckId,
    driverId: expense.driverId,
    applicantId: expense.applicantId ?? null,
    unitNumber: expense.unitNumber ?? "",
    driverName: expense.driverName ?? "",
    amount: expense.baseAmount ?? expense.amount ?? "",
    paymentType: expense.paymentType ?? "",
    expenseType: expense.expenseType ?? "",
    referenceNumber: expense.referenceNumber ?? "",
    description: expense.maintenance ? Array.from(new Set([expense.workOrderDescription?.trim(), expense.description?.trim()].filter(Boolean))).join("\n\n") : expense.description ?? "",
    coveredBy: expense.coveredBy ?? "",
    paidBy: expense.paidBy ?? "",
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
  const change = (next: ExpenseInput) => {
    if (next.truckId !== value.truckId && next.maintenance) next = { ...next, maintenance: { ...next.maintenance, odometer: null } };
    onChange(applyExpenseCategoryRules(next.categoryId === options.workOrdersCategoryId || options.settings.some(item => item.id === next.categoryId && item.kind === "category" && item.name === "Maintenance") ? next : { ...next, maintenance: undefined }, options.settings));
  };
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
          {!(value.updatedAt && value.legacyPaymentTotal === value.amount && value.paymentType.trim().toUpperCase() === "EFS") && <ExpenseFeePreview amount={value.amount} paymentType={value.paymentType} />}
        </Field>
      </FormSection>
      {(value.categoryId === options.workOrdersCategoryId || options.settings.some(item => item.id === value.categoryId && item.kind === "category" && item.name === "Maintenance")) && <ExpenseMaintenanceFields value={value} onChange={change} />}

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
          <ExpenseDriverSelect value={value} drivers={drivers} applicants={options.applicants} trucks={trucks} owners={owners} onChange={change} required={driverOnly} className={controlClass} />

        </Field>
        {driverOnly ? <p className="text-xs text-zinc-400 sm:col-span-2">Charged to the selected driver.</p> : <>
        <Field label="Who will cover" wide>
          <ExpenseResponsibilitySelect value={value} trucks={trucks} owners={owners} onChange={change} className={controlClass} />
        </Field>
        <Field label="Payment method">
          <ExpensePaymentMethodSelect value={value.paymentType} methods={options.paymentTypes} onChange={next => set("paymentType", next)} saved={!!value.updatedAt} className={controlClass} />
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
