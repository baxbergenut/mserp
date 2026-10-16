"use client";

import { useState } from "react";
import type { AccessRole, PermissionDefinition } from "../lib/types";
import { controlClass } from "../components/management/ManagementUI";

const actions = ["read", "create", "edit", "delete"];
export function PermissionMatrix({ role, catalog, onChange }: { role: AccessRole; catalog: PermissionDefinition[]; onChange: (role: AccessRole) => void }) {
  const [search, setSearch] = useState("");
  const groups = [...new Set(catalog.map(p => p.group))];
  const missing = (p: PermissionDefinition) => p.requires.filter(key => !role.permissions.includes(key));
  const label = (key: string) => catalog.find(p => p.key === key)?.label ?? key;
  function toggle(permission: PermissionDefinition, checked: boolean) {
    if (checked && missing(permission).length) return;
    let keys = checked ? [...role.permissions, permission.key] : role.permissions.filter(key => key !== permission.key);
    // Removing a prerequisite removes dependent actions, never adds access.
    for (let i = 0; i < catalog.length; i++) keys = keys.filter(key => catalog.find(p => p.key === key)?.requires.every(required => keys.includes(required)));
    onChange({ ...role, permissions: keys });
  }
  function checkbox(p: PermissionDefinition, showLabel = false) {
    const needs = missing(p);
    return <label key={p.key} title={needs.length ? `Requires: ${needs.map(label).join(", ")}` : p.label} className="inline-flex items-center gap-2 text-xs">
      <input aria-label={p.label} type="checkbox" checked={role.permissions.includes(p.key)} disabled={needs.length > 0} onChange={e => toggle(p, e.target.checked)} className="accent-blue-500" />
      {showLabel && <span>{p.label.replace(`${p.group}`, "").trim()}</span>}
      {needs.length > 0 && <span className="sr-only">Requires {needs.map(label).join(", ")}</span>}
    </label>;
  }
  const setup = ["drivers.create", "drivers.private.edit", "driver_compensation.edit", "escrow.create"];
  const setupMissing = setup.filter(key => !role.permissions.includes(key));
  return <section className="mt-5 space-y-3">
    <input aria-label="Search permissions" placeholder="Find a page, action, or sensitive section…" className={controlClass} value={search} onChange={e => setSearch(e.target.value)} />
    <p className="text-xs text-zinc-400">Actions require View. Select prerequisites explicitly. Private details, documents, and financial access are separate.</p>
    <div className="max-h-[50vh] overflow-auto rounded-lg border border-zinc-800"><table className="w-full min-w-[700px] text-left text-xs">
      <thead className="sticky top-0 bg-zinc-900"><tr>{["Page / section", "View", "Add", "Edit", "Delete", "Other actions"].map(h => <th key={h} className="px-3 py-2 font-medium">{h}</th>)}</tr></thead>
      <tbody>{groups.filter(group => catalog.some(p => p.group === group && `${p.group} ${p.label} ${p.key}`.toLowerCase().includes(search.toLowerCase()))).map(group => {
        const permissions = catalog.filter(p => p.group === group);
        return <tr key={group} className="border-t border-zinc-800"><td className="px-3 py-3">{group}{permissions.some(p => p.sensitive) && <span className="ml-2 text-amber-300">Sensitive</span>}</td>
          {actions.map(action => <td key={action} className="px-3 py-3">{permissions.filter(p => p.action === action || (action === "edit" && p.action === "write")).map(p => checkbox(p))}</td>)}
          <td className="px-3 py-3"><div className="flex flex-wrap gap-3">{permissions.filter(p => !actions.includes(p.action) && p.action !== "write").map(p => checkbox(p, true))}</div></td>
        </tr>;
      })}</tbody>
    </table></div>
    <details className="rounded-lg border border-zinc-800 p-3 text-xs" open><summary className="cursor-pointer font-medium">Effective access · {role.permissions.length} capabilities</summary>
      <ul className="mt-3 space-y-1 text-zinc-400">{groups.filter(group => catalog.some(p => p.group === group && role.permissions.includes(p.key))).map(group => <li key={group}><span className="text-zinc-200">{group}:</span> {catalog.filter(p => p.group === group && role.permissions.includes(p.key)).map(p => p.label).join(" · ")}</li>)}</ul>
      {role.permissions.includes("drivers.create") && <p className="mt-3 text-amber-300">{setupMissing.length ? `Driver setup also requires: ${setupMissing.map(label).join(", ")}.` : "Standard driver setup is available."} Creating a truck, setting up a team, or enrolling services additionally requires that operation’s permissions.</p>}
      <p className="mt-3 text-zinc-500">Manager verification and cancelling another person’s verification are Administrator-only. Accounting verification also requires category View.</p>
    </details>
  </section>;
}
