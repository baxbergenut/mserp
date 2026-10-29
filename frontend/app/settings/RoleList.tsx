"use client";

import { ChevronRight, LockKeyhole } from "lucide-react";
import type { AccessData, AccessRole } from "../lib/types";

function accessLevel(role: AccessRole, data: AccessData) {
  if (role.system) return 100;
  const catalog = data.permissions.filter(p => !p.key.startsWith("dispatcher_compensation."));
  const total = catalog.length + data.expenseCategories.length * 4;
  const granted = catalog.filter(p => role.permissions.includes(p.key)).length + data.expenseCategories.reduce((count, category) => {
    const access = role.expenseCategoryAccess.find(item => item.categoryId === category.id);
    return count + [access?.canView, access?.canCreate, access?.canEdit, access?.canDelete].filter(Boolean).length;
  }, 0);
  return total ? Math.round(granted / total * 100) : 0;
}

export function RoleList({ data, busy, onOpen }: { data: AccessData; busy: boolean; onOpen: (role: AccessRole) => void }) {
  return <div className="overflow-hidden rounded-xl border border-zinc-800">
    <div className="flex items-center justify-between gap-4 bg-zinc-900 px-4 py-2.5 text-xs text-zinc-500">
      <span>Role</span><span className="pr-6" title="Share of available permissions, including expense category actions">Access level</span>
    </div>
    <ul className="divide-y divide-zinc-800/70">
      {data.roles.map(role => {
        const percentage = accessLevel(role, data);
        const users = data.users.filter(user => user.roleId === role.id).length;
        return <li key={role.id}>
          <button type="button" disabled={busy} onClick={() => onOpen(role)} aria-label={`${role.name}, ${percentage}% access`} className="flex w-full items-center gap-3 px-4 py-3 text-left transition hover:bg-zinc-800/40 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-blue-500 disabled:opacity-40">
            <span className="min-w-0 flex-1">
              <span className="flex items-center gap-2 text-sm font-medium text-zinc-100"><span className="break-words">{role.name}</span>{role.system && <LockKeyhole aria-hidden="true" className="h-3.5 w-3.5 shrink-0 text-zinc-500" />}</span>
              <span className="mt-0.5 block text-xs text-zinc-500">{users} {users === 1 ? "user" : "users"}{role.permanent && !role.system ? " · Built-in" : ""}</span>
            </span>
            <span className="flex w-28 shrink-0 items-center gap-3 sm:w-44" title="Share of available permissions, including expense category actions">
              <span aria-hidden="true" className="h-1.5 flex-1 overflow-hidden rounded-full bg-zinc-800"><span className="block h-full rounded-full bg-blue-500" style={{ width: `${percentage}%` }} /></span>
              <span className="w-10 text-right text-sm tabular-nums text-zinc-200">{percentage}%</span>
            </span>
            <ChevronRight aria-hidden="true" className="h-4 w-4 shrink-0 text-zinc-500" />
          </button>
        </li>;
      })}
    </ul>
    {data.roles.length === 0 && <p className="px-4 py-8 text-center text-sm text-zinc-500">No roles yet.</p>}
  </div>;
}
