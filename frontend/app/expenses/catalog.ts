import type { ExpenseInput, ExpenseSetting } from "@/app/lib/types";

export const activeExpenseCategories = (settings: ExpenseSetting[], allowedIds?: string[]) => settings.filter(item => item.kind === "category" && item.active && (!allowedIds || allowedIds.includes(item.id)));

export function expenseNames(settings: ExpenseSetting[], categoryId: string) {
  return settings.filter(item => item.kind === "name" && item.active && item.categoryId === categoryId).map(item => item.name);
}

export function isDriverOnlyExpense(settings: ExpenseSetting[], categoryId: string) {
  return settings.some(item => item.kind === "category" && item.id === categoryId && item.driverOnly);
}

export function isBonusCategory(settings: ExpenseSetting[], categoryId: string) {
  return settings.some(item => item.kind === "category" && item.id === categoryId && !item.driverOnly && !item.accessorials && item.name.trim().toLowerCase() === "bonuses");
}

export function isPayrollBonus(category: string, paymentType?: string | null) {
  return category.trim().toLowerCase() === "bonuses" && paymentType?.trim().toLowerCase() === "reimburse";
}

function canRouteBonusToPayroll(value: ExpenseInput) {
  return !value.updatedAt || (value.savedPaymentType ?? value.paymentType).trim().toLowerCase() === "reimburse";
}

export function applyExpenseCategoryRules(value: ExpenseInput, settings: ExpenseSetting[]): ExpenseInput {
  if (settings.some(item => item.id === value.categoryId && item.accessorials) && (!value.updatedAt || value.paymentType === "Driver payroll")) {
    return { ...value, paymentType: "Driver payroll", applicantId: null, coveredBy: value.coveredBy === "Truck Owner" ? "Truck Owner" : "Company", brokerLoadNumber: "", paidBy: "" };
  }
  if (isBonusCategory(settings, value.categoryId) && canRouteBonusToPayroll(value)) {
    return { ...value, paymentType: "Reimburse", paidBy: "", applicantId: null };
  }
  return isDriverOnlyExpense(settings, value.categoryId)
    ? { ...value, coveredBy: "Driver", brokerLoadNumber: "", ownerId: null, paymentType: "", paidBy: "" }
    : value;
}

export function changeExpenseCategory(value: ExpenseInput, categoryId: string, settings: ExpenseSetting[]): ExpenseInput {
  const wasDefault = expenseNames(settings, value.categoryId).some(name => name.toLowerCase() === value.expenseType.trim().toLowerCase());
  const changed = categoryId !== value.categoryId;
  const accessorial = settings.some(item => item.id === categoryId && item.accessorials);
  if (changed && isBonusCategory(settings, categoryId) && canRouteBonusToPayroll(value)) {
    return applyExpenseCategoryRules({ ...value, categoryId, paymentType: "Reimburse" }, settings);
  }
  let paymentType = value.paymentType;
  if (changed) {
    if (accessorial) paymentType = "Driver payroll";
    else if (paymentType === "Driver payroll" || (!value.updatedAt && isBonusCategory(settings, value.categoryId))) paymentType = "";
  }
  return applyExpenseCategoryRules({ ...value, categoryId, paymentType, expenseType: changed && wasDefault ? "" : value.expenseType }, settings);
}
