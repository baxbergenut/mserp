"use client";

import { useState } from "react";
import { Modal, controlClass } from "@/app/components/management/ManagementUI";

export type BoardView = { id: string; name: string; dispatcherIds: string[] };
const buttonClass = "rounded-lg border border-zinc-700 px-3 py-2 text-xs text-zinc-300 hover:bg-zinc-800";

export function BoardViews({ mode, myIds, saved, dispatchers, onSelect, onSave, onDelete }: {
  mode: string; myIds: string[]; saved: BoardView[]; dispatchers: [string, string][];
  onSelect: (id: string) => void;
  onSave: (ids: string[], name: string) => void;
  onDelete: (id: string) => void;
}) {
  const [draft, setDraft] = useState<string[] | null>(null);
  const [name, setName] = useState("");
  function configure() {
    setDraft(mode === "all" || mode === "my" ? myIds : saved.find(v => v.id === mode)?.dispatcherIds ?? myIds);
    setName("");
  }
  return <>
    <div className="flex flex-wrap items-center gap-2">
      <button aria-pressed={mode === "all"} className={`${buttonClass} ${mode === "all" ? "bg-blue-500/15 text-blue-200" : ""}`} onClick={() => onSelect("all")}>All drivers</button>
      <button aria-pressed={mode === "my"} className={`${buttonClass} ${mode === "my" ? "bg-blue-500/15 text-blue-200" : ""}`} onClick={() => { if (!myIds.length) configure(); else onSelect("my"); }}>My view</button>
      {saved.length > 0 && <select aria-label="Saved board view" className={`${controlClass} max-w-48`} value={saved.some(v => v.id === mode) ? mode : ""} onChange={e => { if (e.target.value) onSelect(e.target.value); }}><option value="">Saved views</option>{saved.map(v => <option key={v.id} value={v.id}>{v.name}</option>)}</select>}
      <button className={buttonClass} onClick={configure}>Customize view</button>
      {saved.some(v => v.id === mode) && <button className={buttonClass} onClick={() => onDelete(mode)}>Remove saved view</button>}
    </div>
    {draft !== null && <Modal title="Customize Driver Board view" description="Choose dispatcher groups. Views are personal and remembered in this browser tab." isSaving={false} submitLabel={name.trim() ? "Save named view" : "Use as My view"} onClose={() => setDraft(null)} onSubmit={event => { event.preventDefault(); if (!draft.length) return; onSave(draft, name.trim()); setDraft(null); }}>
      <fieldset className="space-y-2"><legend className="mb-3 text-sm text-zinc-300">Dispatcher groups</legend>{dispatchers.map(([id, label]) => <label key={id} className="flex items-center gap-3 rounded-lg border border-zinc-800 p-3 text-sm text-zinc-200"><input type="checkbox" checked={draft.includes(id)} onChange={e => setDraft(e.target.checked ? [...draft, id] : draft.filter(value => value !== id))} />{label || "Unassigned"}</label>)}</fieldset>
      {!draft.length && <p className="mt-3 text-xs text-amber-300">Choose at least one dispatcher group.</p>}
      <label className="mt-5 block text-sm text-zinc-300">Save with a name (optional)<input className={`${controlClass} mt-2`} maxLength={60} value={name} onChange={e => setName(e.target.value)} placeholder="For example, Weekend coverage" /></label>
      <p className="mt-2 text-xs text-zinc-500">Leave the name blank to update My view. Saving an existing name replaces that view’s groups.</p>
    </Modal>}
  </>;
}
