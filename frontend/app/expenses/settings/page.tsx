"use client";

import { PageHeader } from "@/app/components/PageHeader";

import { useCallback, useEffect, useState } from "react";
import { Archive, Pencil, Plus, RotateCcw, Settings } from "lucide-react";
import { IntentLink } from "@/app/components/IntentLink";
import { ErrorBanner, Modal, controlClass } from "@/app/components/management/ManagementUI";
import { fetchExpenseSettings, saveExpenseSetting } from "@/app/lib/api";
import type { ExpenseSetting, ExpenseSettingKind } from "@/app/lib/types";
import { useViewState } from "@/app/lib/viewMemory";

const buttonClass = "inline-flex items-center justify-center gap-2 rounded-lg border border-zinc-700 px-3 py-2 text-xs text-zinc-300 hover:bg-zinc-800 disabled:opacity-40";
const labels: Record<ExpenseSettingKind, string> = { category: "category", name: "expense name", payment_method: "payment method", payer: "payer" };
type Draft = Omit<ExpenseSetting, "id"> & { id?: string };

export default function ExpenseSettingsPage() {
  const [items, setItems] = useState<ExpenseSetting[]>([]);
  const [tab, setTab] = useViewState<"category" | "payment_method" | "payer">("tab", "category");
  const [selectedId, setSelectedId] = useViewState("category", "");
  const [showArchived, setShowArchived] = useViewState("archived", false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [draft, setDraft] = useState<Draft | null>(null);
  const [archive, setArchive] = useState<ExpenseSetting | null>(null);
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const settings = await fetchExpenseSettings();
      setItems(settings); setError("");
    }
    catch (reason) { setError(reason instanceof Error ? reason.message : "Could not load Expenses & Charges settings"); }
    finally { setLoading(false); }
  }, []);
  useEffect(() => { const timer = setTimeout(() => void load(), 0); return () => clearTimeout(timer); }, [load]);
  const categories = items.filter(item => item.kind === "category" && (showArchived || item.active));
  const selected = categories.find(item => item.id === selectedId) ?? categories[0];
  const visible = items.filter(item => item.kind === (tab === "category" ? "name" : tab) && (showArchived || item.active) && (tab !== "category" || item.categoryId === selected?.id));

  function create(kind: ExpenseSettingKind) {
    setError("");
    setDraft({ kind, categoryId: kind === "name" ? selected?.id ?? null : null, name: "", active: true, version: 0 });
  }
  async function save(value: Draft) {
    setSaving(true); setError("");
    try {
      const saved = await saveExpenseSetting(value);
      setItems(current => [...current.filter(item => item.id !== saved.id), saved].sort((a, b) => a.name.localeCompare(b.name)));
      if (saved.kind === "category") setSelectedId(saved.id);
      setDraft(null); setArchive(null);
    } catch (reason) { setError(reason instanceof Error ? reason.message : "Could not save Expenses & Charges setting"); }
    finally { setSaving(false); }
  }
  function actions(item: ExpenseSetting) {
    return <div className="flex shrink-0 gap-1">
      <button className={buttonClass} disabled={saving} aria-label={`Edit ${labels[item.kind]} ${item.name}`} onClick={() => { setError(""); setDraft(item); }}><Pencil className="h-3.5 w-3.5" /></button>
      <button className={buttonClass} disabled={saving} aria-label={`${item.active ? "Archive" : "Restore"} ${labels[item.kind]} ${item.name}`} onClick={() => item.active ? (setError(""), setArchive(item)) : void save({ ...item, active: true })}>{item.active ? <Archive className="h-3.5 w-3.5" /> : <RotateCcw className="h-3.5 w-3.5" />}</button>
    </div>;
  }
  return <div className="space-y-5">
    <div className="flex flex-wrap items-start justify-between gap-3"><PageHeader><div><h1 className="flex items-center gap-2 text-lg font-semibold text-zinc-100"><Settings className="h-5 w-5 text-zinc-500" />Expenses & Charges settings</h1><p className="mt-1 text-sm text-zinc-500">Manage categories, default names, and payment details. Saved entries keep their original values.</p></div></PageHeader><IntentLink href="/expenses" className={buttonClass}>Expenses & Charges</IntentLink></div>
    {error && !draft && !archive && <ErrorBanner message={error} />}
    <div className="flex flex-wrap items-center justify-between gap-3"><div role="tablist" aria-label="Expenses & Charges settings" className="flex flex-wrap gap-1">{([["category", "Categories & names"], ["payment_method", "Payment methods"], ["payer", "Paid by"]] as const).map(([key, label]) => <button key={key} role="tab" aria-selected={tab === key} onClick={() => setTab(key)} className={`${buttonClass} ${tab === key ? "border-blue-500/50 bg-blue-500/10 text-blue-300" : ""}`}>{label}</button>)}</div><div className="flex items-center gap-4"><label className="flex items-center gap-2 text-xs text-zinc-400"><input type="checkbox" checked={showArchived} onChange={event => setShowArchived(event.target.checked)} />Show archived</label><button className={buttonClass} disabled={loading || saving} onClick={() => void load()}>Reload</button></div></div>
    {loading ? <p role="status" className="text-sm text-zinc-500">Loading Expenses & Charges settings…</p> : <div className={tab === "category" ? "grid gap-4 lg:grid-cols-[minmax(260px,1fr)_2fr]" : ""}>
      {tab === "category" && <section className="rounded-xl border border-zinc-800 bg-zinc-950/30"><div className="flex items-center justify-between border-b border-zinc-800 p-3"><h2 className="text-sm font-medium text-zinc-200">Categories</h2><button className={buttonClass} disabled={saving} onClick={() => create("category")}><Plus className="h-3.5 w-3.5" />Add category</button></div>{categories.map(item => <div key={item.id} className={`flex items-center gap-2 border-b border-zinc-800/60 p-2 ${selected?.id === item.id ? "bg-blue-500/10" : ""}`}><button aria-pressed={selected?.id === item.id} className="min-w-0 flex-1 px-1 text-left text-sm text-zinc-300" onClick={() => setSelectedId(item.id)}><span className="block truncate">{item.name}</span>{!item.active && <span className="text-xs text-zinc-500">Archived</span>}</button>{actions(item)}</div>)}{!categories.length && <p className="p-4 text-sm text-zinc-500">No categories. Add one to start.</p>}</section>}
      <section className="rounded-xl border border-zinc-800 bg-zinc-950/30"><div className="flex flex-wrap items-center justify-between gap-3 border-b border-zinc-800 p-3"><div><h2 className="text-sm font-medium text-zinc-200">{tab === "category" ? `${selected?.name ?? "Category"} · Default names` : tab === "payment_method" ? "Payment methods" : "Common payer names"}</h2><p className="mt-1 text-xs text-zinc-500">{tab === "category" ? "Suggestions for this category. Custom names remain available on each expense." : "Suggestions available when entering expenses. One-off entries do not change this list."}</p></div><button className={buttonClass} disabled={saving || (tab === "category" && !selected?.active)} onClick={() => create(tab === "category" ? "name" : tab)}><Plus className="h-3.5 w-3.5" />Add {labels[tab === "category" ? "name" : tab]}</button></div>{visible.map(item => <div key={item.id} className="flex items-center justify-between gap-3 border-b border-zinc-800/60 px-4 py-2"><div className="min-w-0"><p className="truncate text-sm text-zinc-300">{item.name}</p>{!item.active && <span className="text-xs text-zinc-500">Archived</span>}</div>{actions(item)}</div>)}{!visible.length && <p className="p-4 text-sm text-zinc-500">No {tab === "category" ? "default names for this category" : "saved options"}.</p>}</section>
    </div>}
    {draft && <Modal title={`${draft.id ? "Edit" : "Add"} ${labels[draft.kind]}`} description="Changes affect suggestions for future entry. Existing expenses are preserved." isSaving={saving} submitLabel="Save" onClose={() => setDraft(null)} onSubmit={event => { event.preventDefault(); void save(draft); }}><div className="space-y-4">{error && <ErrorBanner message={error} />}<label className="block text-sm text-zinc-400">Name<input required autoFocus maxLength={100} pattern=".*\S.*" aria-label="Setting name" className={`${controlClass} mt-1`} value={draft.name} onChange={event => setDraft({ ...draft, name: event.target.value })} /></label>{draft.kind === "name" && <label className="block text-sm text-zinc-400">Category<select required aria-label="Name category" className={`${controlClass} mt-1`} value={draft.categoryId ?? ""} onChange={event => setDraft({ ...draft, categoryId: event.target.value })}><option value="">Select category</option>{items.filter(item => item.kind === "category" && (item.active || item.id === draft.categoryId)).map(item => <option key={item.id} value={item.id}>{item.name}{item.active ? "" : " (archived)"}</option>)}</select></label>}</div></Modal>}
    {archive && <Modal title={`Archive ${archive.name}?`} description={archive.kind === "category" ? "This category and its default names will stop appearing for new expenses. Existing expenses stay unchanged. You can restore it later." : "This option will stop appearing in suggestions. Existing expenses stay unchanged. You can restore it later."} isSaving={saving} submitLabel="Archive" onClose={() => setArchive(null)} onSubmit={event => { event.preventDefault(); void save({ ...archive, active: false }); }}>{error && <ErrorBanner message={error} />}</Modal>}
  </div>;
}
