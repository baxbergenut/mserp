"use client";

import { useCallback, useEffect, useState } from "react";
import { Settings, ShieldCheck, Users, KeyRound, Pencil, RefreshCw, ListChecks, Palette } from "lucide-react";
import { Appearance } from "./Appearance";
import { usePermissions } from "@/app/lib/access";
import { SystemTasks } from "./SystemTasks";
import { fetchAccess, saveUser, saveRole, revokeUserAccess } from "@/app/lib/api";
import type { AccessData, ManagedUser, AccessRole, ExpenseCategoryAccess } from "@/app/lib/types";
import { useViewState } from "@/app/lib/viewMemory";
import { ManagementHeader, ManagementSearch, Modal, Field, controlClass } from "@/app/components/management/ManagementUI";

const button = "inline-flex items-center gap-2 rounded-lg border border-zinc-800 px-3 py-2 text-xs text-zinc-300 hover:bg-zinc-800 disabled:opacity-40";
const newUser = (): ManagedUser => ({ id: "", username: "", email: "", roleId: "", active: true, password: "", version: 0 });
const newRole = (): AccessRole => ({ id: "", name: "", permissions: [], expenseCategoryAccess: [], system: false, version: 0 });

function updateCategoryAccess(role: AccessRole, categoryId: string, key: keyof Omit<ExpenseCategoryAccess, "categoryId">, checked: boolean): AccessRole {
  const current = role.expenseCategoryAccess.find(item => item.categoryId === categoryId) ?? { categoryId, canView: false, canCreate: false, canEdit: false, canDelete: false };
  const next = { ...current, [key]: checked };
  const remaining = role.expenseCategoryAccess.filter(item => item.categoryId !== categoryId);
  return { ...role, expenseCategoryAccess: next.canView || next.canCreate || next.canEdit || next.canDelete ? [...remaining, next] : remaining };
}

export default function SettingsPage() {
  const canManage = usePermissions().includes("access.manage");
  const [data, setData] = useState<AccessData | null>(null);
  const [savedTab, setTab] = useViewState<"appearance" | "users" | "roles" | "tasks">("access:tab", "appearance");
  const tab = canManage ? savedTab : "appearance";
  const [search, setSearch] = useViewState("access:search", "");
  const [user, setUser] = useState<ManagedUser | null>(null);
  const [role, setRole] = useState<AccessRole | null>(null);
  const [revoke, setRevoke] = useState<ManagedUser | null>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const refresh = useCallback(async () => { setLoading(true); try { setData(await fetchAccess()); } catch (e) { setError(e instanceof Error ? e.message : "Could not load access settings"); } finally { setLoading(false); } }, []);
  useEffect(() => { if (!canManage) return; let active = true; fetchAccess().then(value => { if (active) setData(value); }).catch(e => { if (active) setError(e instanceof Error ? e.message : "Could not load access settings"); }).finally(() => { if (active) setLoading(false); }); return () => { active = false; }; }, [canManage]);
  async function perform(action: () => Promise<void>) {
    setBusy(true); setError(""); setNotice("");
    try { await action(); } catch (e) { setError(e instanceof Error ? e.message : "Could not save access settings"); }
    finally { setBusy(false); }
  }
  const users = data?.users.filter(u => `${u.username} ${u.email}`.toLowerCase().includes(search.toLowerCase())) ?? [];

  return <div className="space-y-5 animate-fade-in">
    <ManagementHeader icon={Settings} title="Settings"  count={tab === "appearance" ? undefined : tab === "tasks" ? 4 : tab === "users" ? data?.users.length ?? 0 : data?.roles.length ?? 0} actionLabel={tab === "appearance" || tab === "tasks" ? undefined : tab === "users" ? "Add user" : "Create role"} onAction={tab === "appearance" || tab === "tasks" ? undefined : () => { if (busy || !data) return; setError(""); if (tab === "users") setUser(newUser()); else setRole(newRole()); }} secondaryAction={tab !== "appearance" && <button className={button} disabled={busy || loading} onClick={() => void refresh()}><RefreshCw className="h-3.5 w-3.5" />Refresh</button>} />
    <div className="flex flex-wrap gap-2 border-b border-zinc-800 pb-3">{([['appearance', Palette, 'Appearance'], ['users', Users, 'Users'], ['roles', ShieldCheck, 'Roles & permissions'], ['tasks', ListChecks, 'System tasks']] as const).filter(([value]) => value === "appearance" || canManage).map(([value, Icon, label]) => <button key={value} onClick={() => setTab(value)} className={`${button} ${tab === value ? 'border-blue-500/40 bg-blue-500/10 text-blue-300' : ''}`}><Icon className="h-4 w-4" />{label}</button>)}</div>
    {tab !== "appearance" && error && <p role="alert" className="rounded-lg border border-red-500/20 bg-red-500/10 p-3 text-sm text-red-300">{error}</p>}
    {tab !== "appearance" && notice && <p role="status" className="rounded-lg border border-emerald-500/20 bg-emerald-500/10 p-3 text-sm text-emerald-300">{notice}</p>}
    {tab === "appearance" ? <Appearance /> : loading && !data ? <p role="status" className="text-sm text-zinc-500">Loading access settings…</p> : data && <>
      {tab === "tasks" ? <SystemTasks users={data.users} /> : tab === "users" ? <>
        <div className="flex flex-wrap items-center justify-between gap-3"><ManagementSearch value={search} onChange={setSearch} placeholder="Search names or emails…" /><p className="text-xs text-zinc-500">Administrator-created accounts · No public sign-up</p></div>
        <div className="overflow-x-auto rounded-xl border border-zinc-800"><table className="w-full min-w-[850px] text-left text-sm"><thead className="bg-zinc-900 text-xs text-zinc-500"><tr>{['User', 'Role', 'Status', 'Actions'].map(h => <th key={h} className="px-4 py-3 font-medium">{h}</th>)}</tr></thead><tbody>{users.map(u => <tr key={u.id} className="border-t border-zinc-800/70"><td className="px-4 py-3"><p className="text-zinc-200">{u.username}</p><p className="text-xs text-zinc-500">{u.email || 'Email required'}</p></td><td className="px-4 py-3 text-zinc-400">{data.roles.find(r => r.id === u.roleId)?.name ?? 'Unassigned'}</td><td className="px-4 py-3"><span className={u.active ? 'text-emerald-400' : 'text-zinc-500'}>{u.active ? 'Active' : 'Disabled'}</span></td><td className="px-4 py-3"><div className="flex gap-2"><button className={button} disabled={busy} onClick={() => { setError(""); setUser({ ...u }); }}><Pencil className="h-3.5 w-3.5" />Edit</button><button className={button} disabled={busy} onClick={() => setRevoke(u)}><KeyRound className="h-3.5 w-3.5" />Revoke access</button></div></td></tr>)}</tbody></table>{users.length === 0 && <p className="p-8 text-center text-sm text-zinc-500">No users match this search.</p>}</div>
      </> : <div className="grid gap-4 xl:grid-cols-2">{data.roles.map(r => <section key={r.id} className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-5"><div className="flex items-center justify-between gap-3"><div><h2 className="font-medium text-zinc-100">{r.name}</h2><p className="mt-1 text-xs text-zinc-500">{data.users.filter(u => u.roleId === r.id).length} users · {r.permissions.length} general permissions · {r.expenseCategoryAccess.length} categories</p></div>{!r.system && <button className={button} disabled={busy} onClick={() => { setError(""); setRole({ ...r, permissions: [...r.permissions], expenseCategoryAccess: r.expenseCategoryAccess.map(item => ({ ...item })) }); }}><Pencil className="h-3.5 w-3.5" />Edit role</button>}</div><p className="mt-4 text-xs leading-6 text-zinc-400">{r.system ? 'Full access. This built-in role cannot be edited. At least one active administrator must remain.' : r.permissions.map(p => data.permissions.find(v => v.key === p)?.label ?? p).join(' · ') || 'No general permissions assigned.'}</p></section>)}</div>}
    </>}
    {user && <Modal title={user.id ? "Edit user" : "Add user"} description="Email and role are required. Set a password for new accounts or enter a replacement to reset it. Saving signs the user out on every device." isSaving={busy} submitLabel={user.id ? "Save changes" : "Create user"} onClose={() => setUser(null)} onSubmit={e => { e.preventDefault(); void perform(async () => { const created = !user.id; await saveUser(user); setUser(null); await refresh(); setNotice(created ? "User created. Share the credentials with them securely." : "User updated. Existing sessions revoked."); }); }}>
      <div className="grid gap-4"><Field label="Name"><input className={controlClass} required maxLength={200} value={user.username} onChange={e => setUser({ ...user, username: e.target.value })} /></Field><Field label="Email"><input className={controlClass} required type="email" maxLength={254} value={user.email} onChange={e => setUser({ ...user, email: e.target.value })} /></Field><Field label={user.id ? "New password (optional)" : "Password"}><input className={controlClass} type="password" autoComplete="new-password" required={!user.id} minLength={12} maxLength={72} value={user.password ?? ""} onChange={e => setUser({ ...user, password: e.target.value })} /></Field><Field label="Role"><select aria-label="Role" className={controlClass} required value={user.roleId} onChange={e => setUser({ ...user, roleId: e.target.value })}><option value="">Select role</option>{data?.roles.map(r => <option key={r.id} value={r.id}>{r.name}</option>)}</select></Field><label className="flex gap-2 text-sm text-zinc-300"><input type="checkbox" checked={user.active} onChange={e => setUser({ ...user, active: e.target.checked })} />Active account</label>{error && <p role="alert" className="text-sm text-red-300">{error}</p>}</div>
    </Modal>}
    {role && <Modal title={role.id ? "Edit role" : "Create role"} description="Permissions apply to every user assigned this role. Changes take effect on their next API request. Grant fleet read access for pages with fleet selectors." isSaving={busy} submitLabel="Save role" onClose={() => setRole(null)} onSubmit={e => { e.preventDefault(); void perform(async () => { await saveRole(role); setRole(null); await refresh(); setNotice("Role saved. Permissions take effect immediately."); }); }}>
      <Field label="Role name"><input className={controlClass} required maxLength={80} value={role.name} onChange={e => setRole({ ...role, name: e.target.value })} /></Field><div className="mt-5 grid gap-2 sm:grid-cols-2">{data?.permissions.map(p => <label key={p.key} className="flex items-start gap-3 rounded-lg border border-zinc-800 p-3 text-xs text-zinc-300"><input type="checkbox" checked={role.permissions.includes(p.key)} onChange={e => setRole({ ...role, permissions: e.target.checked ? [...role.permissions, p.key] : role.permissions.filter(k => k !== p.key) })} className="mt-0.5 accent-blue-500" /><span>{p.label}<span className="mt-1 block text-[11px] text-zinc-600">{p.key}</span></span></label>)}</div><section className="mt-6"><h3 className="text-sm font-medium text-zinc-200">Expenses & Charges categories</h3><p className="mt-1 text-xs text-zinc-500">Users see only categories with View access. Add, Edit and Delete are enforced independently.</p><div className="mt-3 max-h-72 overflow-auto rounded-lg border border-zinc-800"><table className="w-full min-w-[520px] text-left text-xs"><thead className="sticky top-0 bg-zinc-900 text-zinc-500"><tr><th className="px-3 py-2 font-medium">Category</th>{["View","Add","Edit","Delete"].map(label => <th key={label} className="px-3 py-2 text-center font-medium">{label}</th>)}</tr></thead><tbody>{data?.expenseCategories.map(category => { const access = role.expenseCategoryAccess.find(item => item.categoryId === category.id); return <tr key={category.id} className="border-t border-zinc-800/70"><td className="px-3 py-2 text-zinc-300">{category.name}{!category.active && <span className="ml-2 text-zinc-600">Archived</span>}</td>{([["canView","View"],["canCreate","Add"],["canEdit","Edit"],["canDelete","Delete"]] as const).map(([key, label]) => <td key={key} className="px-3 py-2 text-center"><input aria-label={`${category.name} ${label}`} type="checkbox" checked={access?.[key] ?? false} onChange={event => setRole(updateCategoryAccess(role, category.id, key, event.target.checked))} className="accent-blue-500" /></td>)}</tr>; })}</tbody></table></div></section>{error && <p role="alert" className="mt-3 text-sm text-red-300">{error}</p>}
    </Modal>}
    {revoke && <Modal title={`Revoke access for ${revoke.username}?`} description="This signs the user out on every device, including 30-day sessions. They can sign in again with their password. Disable their account to block login." isSaving={busy} submitLabel="Revoke all sessions" onClose={() => setRevoke(null)} onSubmit={e => { e.preventDefault(); void perform(async () => { await revokeUserAccess(revoke.id); setRevoke(null); await refresh(); setNotice("All sessions revoked."); }); }}>{error && <p role="alert" className="text-sm text-red-300">{error}</p>}</Modal>}
  </div>;
}
