"use client";

import { useState } from "react";
import { createUpdater, updateUpdater, deleteUpdater } from "../lib/api";
import type { Updater, UpdaterInput } from "../lib/types";
import { ConfirmDialog, EmptyState, ErrorBanner, Field, FormSection, Modal, RowActions, TableShell, controlClass } from "../components/management/ManagementUI";

const empty: UpdaterInput = { fullName: "", shift: "main", extension: null };
export function UpdaterPanel({ updaters, onChanged }: { updaters: Updater[]; onChanged: () => Promise<void> }) {
  const [editing, setEditing] = useState<Updater | null | undefined>();
  const [form, setForm] = useState<UpdaterInput>(empty);
  const [pendingDelete, setPendingDelete] = useState<Updater | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const save = async () => {
    setSaving(true); setError("");
    try {
      if (editing) await updateUpdater(editing.id, form); else await createUpdater(form);
      setEditing(undefined); await onChanged();
    } catch (e) { setError(e instanceof Error ? e.message : "Failed to save updater"); }
    finally { setSaving(false); }
  };
  const remove = async () => {
    if (!pendingDelete) return;
    setSaving(true); setError("");
    try { await deleteUpdater(pendingDelete.id); setPendingDelete(null); await onChanged(); }
    catch (e) { setError(e instanceof Error ? e.message : "Failed to delete updater"); setPendingDelete(null); }
    finally { setSaving(false); }
  };
  return <section className="space-y-4" aria-label="Updaters">
    <div className="flex items-center justify-between"><h2 className="text-sm font-semibold text-zinc-200">Updaters <span className="ml-2 text-zinc-500">{updaters.length}</span></h2><button type="button" className="rounded-lg bg-blue-600 px-3.5 py-2 text-[13px] font-medium text-white hover:bg-blue-500" onClick={() => { setError(""); setForm(empty); setEditing(null); }}>Add updater</button></div>
    {error && <ErrorBanner message={error} />}
    <TableShell>{updaters.length === 0 ? <EmptyState message="No updaters yet. Add an updater, then assign them in a dispatcher profile." /> : <table className="w-full min-w-[680px] text-left text-[13px]">
      <thead><tr className="border-b border-zinc-800/50 text-zinc-500">{["Updater", "Shift", "Phone extension", "Dispatchers", "Actions"].map(label => <th key={label} className="px-4 py-3 font-medium">{label}</th>)}</tr></thead>
      <tbody>{updaters.map(u => <tr key={u.id} className="border-b border-zinc-900/70 text-zinc-300 last:border-0 hover:bg-zinc-800/15">
        <td className="px-4 py-3 font-medium">{u.fullName}</td><td className="px-4 py-3">{u.shift === "main" ? "Main" : "After hours"}</td><td className="px-4 py-3 font-mono">{u.extension ?? "—"}</td><td className="px-4 py-3 text-zinc-400">{u.dispatcherNames.join(", ") || "Unassigned"}</td><td className="px-4 py-3"><RowActions onEdit={() => { setError(""); setForm({ fullName: u.fullName, shift: u.shift, extension: u.extension, version: u.version }); setEditing(u); }} onDelete={() => setPendingDelete(u)} /></td>
      </tr>)}</tbody>
    </table>}</TableShell>
    {editing !== undefined && <Modal title={editing ? `Edit ${editing.fullName}` : "Add updater"} isSaving={saving} submitLabel={editing ? "Save changes" : "Create updater"} onClose={() => setEditing(undefined)} onSubmit={event => { event.preventDefault(); void save(); }}>
      {error && <ErrorBanner message={error} />}
      <FormSection title="Updater profile">
        <Field label="Full name" wide><input autoFocus required value={form.fullName} onChange={event => setForm({ ...form, fullName: event.target.value })} className={controlClass} /></Field>
        <Field label="Shift" hint={editing?.dispatcherNames.length ? "Remove dispatcher assignments before changing shift." : undefined}><select aria-label="Shift" className={controlClass} disabled={!!editing?.dispatcherNames.length} value={form.shift} onChange={event => setForm({ ...form, shift: event.target.value as UpdaterInput["shift"] })}><option value="main">Main</option><option value="after_hours">After hours</option></select></Field>
        <Field label="Phone extension"><input type="number" min="0" max="999999" step="1" value={form.extension ?? ""} onChange={event => setForm({ ...form, extension: event.target.value === "" ? null : Number(event.target.value) })} className={controlClass} /></Field>
      </FormSection>
    </Modal>}
    {pendingDelete && <ConfirmDialog title="Delete updater?" message={`This permanently deletes ${pendingDelete.fullName} and removes their assignments from ${pendingDelete.dispatcherNames.length} dispatcher(s).`} isDeleting={saving} onCancel={() => setPendingDelete(null)} onConfirm={() => void remove()} />}
  </section>;
}
