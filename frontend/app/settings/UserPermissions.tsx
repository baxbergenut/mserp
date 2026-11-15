"use client";

import { useState } from "react";
import type { AccessData, ManagedUser, PermissionOverride } from "../lib/types";
import { controlClass } from "../components/management/ManagementUI";

const actions = [["canView", "View"], ["canCreate", "Add"], ["canEdit", "Edit"], ["canDelete", "Delete"]] as const;
const allowed = (inherited: boolean, override?: PermissionOverride) => override ? override === "allow" : inherited;

function OverrideSelect({ label, value, inherited, onChange }: { label: string; value?: PermissionOverride; inherited: boolean; onChange: (value: string) => void }) {
  return <select aria-label={label} className={`${controlClass} min-w-36`} value={value ?? "inherit"} onChange={event => onChange(event.target.value)}>
    <option value="inherit">Role: {inherited ? "Allowed" : "Denied"}</option>
    <option value="allow">Allow</option>
    <option value="deny">Deny</option>
  </select>;
}

export function UserPermissions({ user, data, onChange }: { user: ManagedUser; data: AccessData; onChange: (user: ManagedUser) => void }) {
  const [search, setSearch] = useState("");
  const role = data.roles.find(item => item.id === user.roleId);
  const overrides = user.permissionOverrides ?? {};
  const categories = user.expenseCategoryOverrides ?? {};
  let effective = data.permissions.filter(p => allowed(role?.permissions.includes(p.key) ?? false, overrides[p.key])).map(p => p.key);
  for (let i = 0; i < data.permissions.length; i++) effective = effective.filter(key => data.permissions.find(p => p.key === key)?.requires.every(required => effective.includes(required)));
  const count = Object.keys(overrides).length + Object.values(categories).reduce((total, values) => total + Object.keys(values).length, 0);
  function changePermission(key: string, value: string) {
    const next = { ...overrides };
    if (value === "inherit") delete next[key]; else next[key] = value as PermissionOverride;
    onChange({ ...user, permissionOverrides: next });
  }
  function changeCategory(id: string, key: string, value: string) {
    const next = { ...categories, [id]: { ...categories[id] } };
    if (value === "inherit") delete next[id][key]; else next[id][key] = value as PermissionOverride;
    if (!Object.keys(next[id]).length) delete next[id];
    onChange({ ...user, expenseCategoryOverrides: next });
  }
  const matches = (value: string) => value.toLowerCase().includes(search.trim().toLowerCase());
  const visible = data.permissions.filter(p => !p.key.startsWith("dispatcher_compensation.") && matches(`${p.group} ${p.label} ${p.key}`));
  const visibleCategories = data.expenseCategories.filter(category => matches(`Expenses ${category.name}`));
  return <section aria-label="User permissions" className="mt-2 min-w-0 space-y-3 border-t border-zinc-800 pt-4">
    <div className="flex flex-wrap items-center justify-between gap-2"><h3 className="text-sm font-medium">User permissions</h3>{!role?.system && count > 0 && <button type="button" className="ui-button" onClick={() => onChange({ ...user, permissionOverrides: {}, expenseCategoryOverrides: {} })}>Reset to role ({count})</button>}</div>
    {role?.system ? <p className="text-xs text-zinc-400">Administrator accounts always have full access. Select another role to customize permissions.</p> : !role ? <p className="text-xs text-zinc-400">Select a role to customize this user’s permissions.</p> : <>
      <p className="text-xs text-zinc-400">Use the role’s permissions, or allow or deny access for this user. Actions still require their View permissions. Role changes keep these overrides.</p>
      <input aria-label="Search user permissions" placeholder="Find a permission or expense category…" className={controlClass} value={search} onChange={event => setSearch(event.target.value)} />
      <div className="max-h-80 overflow-auto rounded-lg border border-zinc-800"><table className="w-full text-left text-xs">
        <thead className="sticky top-0 bg-zinc-900"><tr><th className="px-3 py-2 font-medium">Permission</th><th className="px-3 py-2 font-medium">Override</th><th className="px-3 py-2 font-medium">Effective</th></tr></thead>
        <tbody>{visible.map(p => {
          const missing = p.requires.filter(key => !effective.includes(key));
          const requested = allowed(role.permissions.includes(p.key), overrides[p.key]);
          return <tr key={p.key} className="border-t border-zinc-800"><td className="px-3 py-2"><div>{p.label}</div>{p.sensitive && <span className="text-zinc-500">Sensitive</span>}{requested && missing.length > 0 && <p className="text-amber-300">Requires {missing.map(key => data.permissions.find(item => item.key === key)?.label ?? key).join(", ")}</p>}</td><td className="px-3 py-2"><OverrideSelect label={`${p.label} override`} value={overrides[p.key]} inherited={role.permissions.includes(p.key)} onChange={value => changePermission(p.key, value)} /></td><td className="px-3 py-2">{effective.includes(p.key) ? "Allowed" : "Denied"}</td></tr>;
        })}</tbody>
      </table>{visible.length === 0 && <p className="p-3 text-xs text-zinc-500">No matching permissions.</p>}</div>
      {visibleCategories.length > 0 && <div><h4 className="mb-2 text-xs font-medium">Expense categories</h4><p className="mb-3 text-xs text-zinc-400">Add also permits editing records created by users in the same role. Edit permits all records. Denying View disables all category actions.</p><div className="max-h-72 overflow-auto rounded-lg border border-zinc-800"><table className="w-full text-left text-xs"><thead className="sticky top-0 bg-zinc-900"><tr><th className="px-3 py-2 font-medium">Category / action</th><th className="px-3 py-2 font-medium">Override</th><th className="px-3 py-2 font-medium">Effective</th></tr></thead><tbody>{visibleCategories.flatMap(category => {
        const base = role.expenseCategoryAccess.find(item => item.categoryId === category.id);
        const values = categories[category.id] ?? {};
        const canView = allowed(base?.canView ?? false, values.canView);
        return actions.map(([key, label]) => <tr key={`${category.id}:${key}`} className="border-t border-zinc-800"><td className="px-3 py-2">{category.name} · {label}{!category.active && <span className="ml-2 text-zinc-500">Archived</span>}</td><td className="px-3 py-2"><OverrideSelect label={`${category.name} ${label} override`} value={values[key]} inherited={base?.[key] ?? false} onChange={value => changeCategory(category.id, key, value)} /></td><td className="px-3 py-2">{canView && allowed(base?.[key] ?? false, values[key]) ? "Allowed" : "Denied"}</td></tr>);
      })}</tbody></table></div></div>}
    </>}
  </section>;
}
