"use client";

import type { AccessData, AccessRole, ManagedUser, PermissionOverride } from "../lib/types";
import { PermissionMatrix } from "./PermissionMatrix";
import { ExpenseCategoryMatrix } from "./ExpenseCategoryMatrix";

const actions = ["canView", "canCreate", "canEdit", "canDelete"] as const;
const allowed = (inherited: boolean, override?: PermissionOverride) => override ? override === "allow" : inherited;

export function UserPermissions({ user, data, onChange }: { user: ManagedUser; data: AccessData; onChange: (user: ManagedUser) => void }) {
  const role = data.roles.find(item => item.id === user.roleId);
  const overrides = user.permissionOverrides ?? {};
  const categories = user.expenseCategoryOverrides ?? {};
  let effective = data.permissions.filter(p => allowed(role?.permissions.includes(p.key) ?? false, overrides[p.key])).map(p => p.key);
  for (let i = 0; i < data.permissions.length; i++) effective = effective.filter(key => data.permissions.find(p => p.key === key)?.requires.every(required => effective.includes(required)));
  const hasOverrides = Object.keys(overrides).length > 0 || Object.keys(categories).length > 0;
  const effectiveRole: AccessRole | undefined = role && {
    ...role,
    permissions: effective,
    expenseCategoryAccess: data.expenseCategories.map(category => {
      const base = role.expenseCategoryAccess.find(item => item.categoryId === category.id);
      const values = categories[category.id] ?? {};
      const canView = allowed(base?.canView ?? false, values.canView);
      return {
        categoryId: category.id,
        canView,
        canCreate: canView && allowed(base?.canCreate ?? false, values.canCreate),
        canEdit: canView && allowed(base?.canEdit ?? false, values.canEdit),
        canDelete: canView && allowed(base?.canDelete ?? false, values.canDelete),
      };
    }),
  };

  function changePermissions(next: AccessRole) {
    const permissionOverrides = { ...overrides };
    // Store only checkbox changes. Untouched permissions keep following the role,
    // and existing explicit exceptions survive changes to other boxes.
    for (const { key } of data.permissions) {
      const checked = next.permissions.includes(key);
      if (checked === effective.includes(key)) continue;
      if (checked === role?.permissions.includes(key)) delete permissionOverrides[key];
      else permissionOverrides[key] = checked ? "allow" : "deny";
    }
    onChange({ ...user, permissionOverrides });
  }

  function changeCategories(next: AccessRole) {
    const expenseCategoryOverrides = { ...categories };
    for (const category of data.expenseCategories) {
      const before = effectiveRole?.expenseCategoryAccess.find(item => item.categoryId === category.id);
      const after = next.expenseCategoryAccess.find(item => item.categoryId === category.id);
      const base = role?.expenseCategoryAccess.find(item => item.categoryId === category.id);
      const values = { ...categories[category.id] };
      for (const key of actions) {
        const checked = after?.[key] ?? false;
        if (checked === (before?.[key] ?? false)) continue;
        if (checked === (base?.[key] ?? false)) delete values[key];
        else values[key] = checked ? "allow" : "deny";
      }
      if (Object.keys(values).length) expenseCategoryOverrides[category.id] = values;
      else delete expenseCategoryOverrides[category.id];
    }
    onChange({ ...user, expenseCategoryOverrides });
  }

  return <section aria-label="User permissions" className="mt-2 min-w-0 border-t border-zinc-800 pt-4">
    <div className="flex flex-wrap items-center justify-between gap-2"><h3 className="text-sm font-medium">User permissions</h3>{!role?.system && hasOverrides && <button type="button" className="ui-button" onClick={() => onChange({ ...user, permissionOverrides: {}, expenseCategoryOverrides: {} })}>Reset to role</button>}</div>
    {role?.system ? <p className="mt-2 text-xs text-zinc-400">Administrator accounts always have full access. Select another role to customize permissions.</p> : !effectiveRole ? <p className="mt-2 text-xs text-zinc-400">Select a role to customize this user’s permissions.</p> : <>
      <p className="mt-2 text-xs text-zinc-400">Starts with the role’s permissions. Check or uncheck permissions to change access for this user only.</p>
      <PermissionMatrix role={effectiveRole} catalog={data.permissions} onChange={changePermissions} />
      <ExpenseCategoryMatrix role={effectiveRole} categories={data.expenseCategories} onChange={changeCategories} />
    </>}
  </section>;
}
