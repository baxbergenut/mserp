"use client";

import type { AccessData, AccessRole, ExpenseCategoryAccess } from "../lib/types";

function updateCategoryAccess(role: AccessRole, categoryId: string, key: keyof Omit<ExpenseCategoryAccess, "categoryId">, checked: boolean): AccessRole {
  const current = role.expenseCategoryAccess.find(item => item.categoryId === categoryId) ?? { categoryId, canView: false, canCreate: false, canEdit: false, canDelete: false };
  if(key!=="canView" && checked && !current.canView)return role;
  const next = { ...current, [key]: checked };
  if(key==="canView" && !checked) {next.canCreate=false;next.canEdit=false;next.canDelete=false;}
  const remaining = role.expenseCategoryAccess.filter(item => item.categoryId !== categoryId);
  return { ...role, expenseCategoryAccess: next.canView || next.canCreate || next.canEdit || next.canDelete ? [...remaining, next] : remaining };
}

export function ExpenseCategoryMatrix({ role, categories, onChange }: { role: AccessRole; categories: AccessData["expenseCategories"]; onChange: (role: AccessRole) => void }) {
  return <section className="mt-6"><h3 className="text-sm font-medium text-zinc-200">Expenses categories</h3><p className="mt-1 text-xs text-zinc-500">View shows a category. Add also allows editing records created by users in the same role. Edit allows editing all records in the category. Delete is separate.</p><div className="mt-3 max-h-72 overflow-auto rounded-lg border border-zinc-800"><table className="w-full min-w-[520px] text-left text-xs"><thead className="sticky top-0 bg-zinc-900 text-zinc-500"><tr><th className="px-3 py-2 font-medium">Category</th>{["View","Add","Edit","Delete"].map(label => <th key={label} className="px-3 py-2 text-center font-medium">{label}</th>)}</tr></thead><tbody>{categories.map(category => { const access = role.expenseCategoryAccess.find(item => item.categoryId === category.id); return <tr key={category.id} className="border-t border-zinc-800/70"><td className="px-3 py-2 text-zinc-300">{category.name}{!category.active && <span className="ml-2 text-zinc-600">Archived</span>}</td>{([["canView","View"],["canCreate","Add"],["canEdit","Edit"],["canDelete","Delete"]] as const).map(([key, label]) => <td key={key} className="px-3 py-2 text-center"><input aria-label={`${category.name} ${label}`} type="checkbox" disabled={key!=="canView" && !access?.canView} checked={access?.[key] ?? false} onChange={event => onChange(updateCategoryAccess(role, category.id, key, event.target.checked))} className="accent-blue-500" /></td>)}</tr>; })}</tbody></table></div></section>;
}
