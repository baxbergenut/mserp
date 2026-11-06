import type { ExpenseInput, ExpenseSetting } from "@/app/lib/types";

export const activeExpenseCategories = (settings: ExpenseSetting[], allowedIds?: string[]) => settings.filter(item => item.kind === "category" && item.active && (!allowedIds || allowedIds.includes(item.id)));

export function expenseNames(settings: ExpenseSetting[], categoryId: string) {
  return settings.filter(item => item.kind === "name" && item.active && item.categoryId === categoryId).map(item => item.name);
}

export function isDriverOnlyExpense(settings: ExpenseSetting[], categoryId: string) {
  return settings.some(item => item.kind === "category" && item.id === categoryId && item.driverOnly);
}

export function applyExpenseCategoryRules(value: ExpenseInput, settings: ExpenseSetting[]): ExpenseInput {
  if (settings.some(item => item.id === value.categoryId && item.accessorials) && (!value.updatedAt || value.paymentType === "Driver payroll")) {
    return { ...value, paymentType: "Driver payroll", applicantId: null, coveredBy: value.coveredBy === "Truck Owner" ? "Truck Owner" : "Company", brokerLoadNumber: "", paidBy: "" };
  }
  return isDriverOnlyExpense(settings, value.categoryId)
    ? { ...value, coveredBy: "Driver", brokerLoadNumber: "", ownerId: null, paymentType: "", paidBy: "" }
    : value;
}

export function changeExpenseCategory(value: ExpenseInput, categoryId: string, settings: ExpenseSetting[]): ExpenseInput {
  const wasDefault = expenseNames(settings, value.categoryId).some(name => name.toLowerCase() === value.expenseType.trim().toLowerCase());
  const changed = categoryId !== value.categoryId;
  const accessorial = settings.some(item => item.id === categoryId && item.accessorials);
  return applyExpenseCategoryRules({ ...value, paymentType: changed ? (accessorial ? "Driver payroll" : value.paymentType === "Driver payroll" ? "" : value.paymentType) : value.paymentType, categoryId, expenseType: categoryId !== value.categoryId && wasDefault ? "" : value.expenseType }, settings);
}
