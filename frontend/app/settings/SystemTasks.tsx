"use client";

import { useEffect, useState } from "react";
import { X } from "lucide-react";
import { fetchSystemTaskAssignments, saveSystemTaskAssignment } from "@/app/lib/api";
import type { AccessRole, ManagedUser, SystemTaskAssignment } from "@/app/lib/types";
import { controlClass, ErrorBanner } from "@/app/components/management/ManagementUI";

const labels = { maintenance_pm: "PM Service reminders", five_eld_truck: "Five ELD truck review", relay_setup: "Relay driver setup", weighmytruck_setup: "WeighMyTruck driver setup", driver_onboarding: "Driver onboarding", driver_offboarding: "Driver offboarding", relay_review: "Relay account review", escrow_release: "Escrow release (30 days after termination)" };
const button = "rounded-lg border border-zinc-700 px-3 py-2 text-xs text-zinc-300 hover:bg-zinc-800 disabled:opacity-40";

export function SystemTasks({ users, roles }: { users: ManagedUser[]; roles: AccessRole[] }) {
  const [items, setItems] = useState<SystemTaskAssignment[] | null>(null);
  const [drafts, setDrafts] = useState<Record<string, string[]>>({});
  const [roleDrafts, setRoleDrafts] = useState<Record<string, string[]>>({});
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    let active = true;
    fetchSystemTaskAssignments().then(value => {
      if (active) { setItems(value); setRoleDrafts(Object.fromEntries(value.map(v => [v.kind,v.assigneeRoleIds ?? []]))); setDrafts(Object.fromEntries(value.map(v => [v.kind, v.assigneeIds?.length ? v.assigneeIds : v.assigneeId ? [v.assigneeId] : []]))); setError(""); }
    }).catch(e => { if (active) setError(e instanceof Error ? e.message : "Could not load system task assignments"); });
    return () => { active = false; };
  }, [revision]);
  async function save(item: SystemTaskAssignment) {
    setBusy(true); setError(""); setNotice("");
    try {
      const next = { ...item, assigneeRoleIds: roleDrafts[item.kind] ?? [], assigneeId: drafts[item.kind]?.[0] || null, assigneeIds: drafts[item.kind] ?? [] };
      await saveSystemTaskAssignment(next);
      setItems(current => current?.map(v => v.kind === item.kind ? { ...next, version: item.version + 1 } : v) ?? null);
      setNotice(`${labels[item.kind]} assignment saved.`);
    } catch (e) { setError(e instanceof Error ? e.message : "Could not save assignment"); }
    finally { setBusy(false); }
  }
  return <section aria-label="System task assignments" className="space-y-4">
    <div className="flex items-start justify-between gap-4"><div><h2 className="text-sm font-semibold text-zinc-200">System task assignments</h2><p className="mt-1 text-xs leading-5 text-zinc-400">Assign each system task category to people or roles. Every active user in a selected role receives existing and new tasks. Administrators can see every category. Unassigned categories are visible only to Administrators.</p><p className="mt-1 text-xs text-zinc-500">The assignee’s role still needs Tasks permissions, Fleet permissions for driver onboarding and Five ELD truck reviews, and Escrow read/write permissions for escrow reviews.</p></div><button className={button} disabled={busy} onClick={() => setRevision(v => v + 1)}>Reload assignments</button></div>
    {error && <ErrorBanner message={error} />}
    {notice && <p role="status" className="text-xs text-emerald-300">{notice}</p>}
    {!items ? <p role="status" className="text-sm text-zinc-500">Loading assignments…</p> : <div className="divide-y divide-zinc-800 rounded-xl border border-zinc-800">{items.map(item => <div key={item.kind} className="flex flex-wrap items-center gap-3 p-4"><label className="min-w-48 flex-1 text-sm text-zinc-200" htmlFor={`assignee-${item.kind}`}>{labels[item.kind]}</label><div className="ml-auto flex flex-wrap items-center justify-end gap-2" role="group" aria-label={`${labels[item.kind]} assignees`}>
      {(drafts[item.kind] ?? []).map(id => { const user = users.find(u => u.id === id); const name = user?.username || "Unavailable user"; return <span key={id} className="group inline-flex items-center gap-0.5 rounded-md border border-zinc-700 bg-zinc-800/60 py-0.5 pl-2 pr-0.5 text-[11px] text-zinc-200">
        {name}{user && !user.active ? " (disabled)" : ""}<button type="button" disabled={busy} aria-label={`Remove ${name} from ${labels[item.kind]}`} className="rounded-full p-0.5 text-zinc-400 opacity-0 transition hover:bg-zinc-700 hover:text-zinc-100 focus-visible:opacity-100 group-hover:opacity-100 group-focus-within:opacity-100 [@media(hover:none)]:opacity-100 disabled:opacity-30" onClick={() => setDrafts(current => ({ ...current, [item.kind]: (current[item.kind] ?? []).filter(value => value !== id) }))}><X className="h-2.5 w-2.5" /></button>
      </span>; })}
      {(roleDrafts[item.kind] ?? []).map(id => <span key={id} className="inline-flex items-center gap-1 rounded-md border border-blue-900 bg-blue-950/30 px-2 py-1 text-[11px] text-blue-300">Role: {roles.find(r=>r.id===id)?.name ?? "Unavailable role"}<button type="button" disabled={busy} aria-label={`Remove role ${roles.find(r=>r.id===id)?.name} from ${labels[item.kind]}`} onClick={()=>setRoleDrafts(current=>({...current,[item.kind]:(current[item.kind]??[]).filter(v=>v!==id)}))}><X className="h-3 w-3" /></button></span>)}
      {(roleDrafts[item.kind] ?? []).length === 0 && (drafts[item.kind] ?? []).length === 0 && <span className="text-xs text-zinc-500">Administrators only</span>}
      <select id={`assignee-${item.kind}`} className={`${controlClass} !w-48`} disabled={busy} value="" onChange={e => { const id = e.target.value; if (id) setDrafts(current => ({ ...current, [item.kind]: [...new Set([...(current[item.kind] ?? []), id])] })); }}>
        <option value="">Add person…</option>{users.filter(u => u.active && !(drafts[item.kind] ?? []).includes(u.id)).map(u => <option key={u.id} value={u.id}>{u.username}</option>)}
      </select>
      <select aria-label={`Add role for ${labels[item.kind]}`} className={`${controlClass} !w-48`} disabled={busy} value="" onChange={e=>{const id=e.target.value;if(id)setRoleDrafts(current=>({...current,[item.kind]:[...new Set([...(current[item.kind]??[]),id])]}));}}><option value="">Add role…</option>{roles.filter(r=>!(roleDrafts[item.kind]??[]).includes(r.id)).map(r=><option key={r.id} value={r.id}>{r.name}</option>)}</select>
      </div><button className={button} aria-label={`Save ${labels[item.kind]} assignment`} disabled={busy || (JSON.stringify([...(roleDrafts[item.kind] ?? [])].sort()) === JSON.stringify([...(item.assigneeRoleIds ?? [])].sort()) && JSON.stringify([...(drafts[item.kind] ?? [])].sort()) === JSON.stringify([...(item.assigneeIds?.length ? item.assigneeIds : item.assigneeId ? [item.assigneeId] : [])].sort()))} onClick={() => void save(item)}>Save</button></div>)}</div>}
  </section>;
}
