import type { ExpenseInput, ExpenseSetting } from "@/app/lib/types";

export const activeExpenseCategories = (settings: ExpenseSetting[]) => settings.filter(item => item.kind === "category" && item.active);

export function expenseNames(settings: ExpenseSetting[], category: string) {
  const parent = activeExpenseCategories(settings).find(item => item.name === category);
  return parent ? settings.filter(item => item.kind === "name" && item.active && item.categoryId === parent.id).map(item => item.name) : [];
}

export function changeExpenseCategory(value: ExpenseInput, category: string, settings: ExpenseSetting[]): ExpenseInput {
  const wasDefault = expenseNames(settings, value.category).some(name => name.toLowerCase() === value.expenseType.trim().toLowerCase());
  return { ...value, category, expenseType: category !== value.category && wasDefault ? "" : value.expenseType };
}
