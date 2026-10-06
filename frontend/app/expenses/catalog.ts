import type { ExpenseInput, ExpenseSetting } from "@/app/lib/types";

export const activeExpenseCategories = (settings: ExpenseSetting[], allowedIds?: string[]) => settings.filter(item => item.kind === "category" && item.active && (!allowedIds || allowedIds.includes(item.id)));

export function expenseNames(settings: ExpenseSetting[], categoryId: string) {
  return settings.filter(item => item.kind === "name" && item.active && item.categoryId === categoryId).map(item => item.name);
}

export function changeExpenseCategory(value: ExpenseInput, categoryId: string, settings: ExpenseSetting[]): ExpenseInput {
  const wasDefault = expenseNames(settings, value.categoryId).some(name => name.toLowerCase() === value.expenseType.trim().toLowerCase());
  return { ...value, categoryId, expenseType: categoryId !== value.categoryId && wasDefault ? "" : value.expenseType };
}
