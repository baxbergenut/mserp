"use client";

import { useEffect, useRef, useState } from "react";
import { Plus } from "lucide-react";
import { addProfileNote, fetchProfileNotes } from "@/app/lib/api";
import type { ProfileNote } from "@/app/lib/types";
import { usePermissions } from "@/app/lib/access";
import { ErrorBanner, controlClass } from "../management/ManagementUI";

export function ProfileNotes({ kind, id, legacy }: { kind: "drivers" | "trucks"; id: string; legacy: string | null }) {
  const canWrite = usePermissions().includes("fleet.write");
  const [notes, setNotes] = useState<ProfileNote[] | null>(null);
  const [body, setBody] = useState("");
  const [adding, setAdding] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const submission = useRef<{ id: string; body: string } | null>(null);
  useEffect(() => {
    let cancelled = false;
    fetchProfileNotes(kind, id).then(value => { if (!cancelled) { setNotes(value); setError(""); } }).catch(e => { if (!cancelled) setError(e.message); });
    return () => { cancelled = true; };
  }, [kind, id, attempt]);
  useEffect(() => {
    if (!body.trim()) return;
    const warn = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [body]);
  async function save() {
    if (saving || !body.trim()) return;
    if (!submission.current || submission.current.body !== body.trim()) submission.current = { id: crypto.randomUUID(), body: body.trim() };
    setSaving(true); setError("");
    try {
      const note = await addProfileNote(kind, id, submission.current);
      setNotes(current => [note, ...(current ?? []).filter(n => n.id !== note.id)]);
      setBody(""); setAdding(false); submission.current = null;
    } catch (e) { setError(e instanceof Error ? e.message : "Unable to save note"); }
    finally { setSaving(false); }
  }
  return <section className="ui-card space-y-4" aria-label="Internal notes">
    <div className="flex items-center justify-between gap-3"><h2>Internal notes</h2>{canWrite && !adding && <button className="ui-button" onClick={() => setAdding(true)}><Plus />Add note</button>}</div>
    {error && <div className="space-y-2"><ErrorBanner message={error} />{!notes && <button className="ui-button" onClick={() => setAttempt(v => v + 1)}>Retry notes</button>}</div>}
    {adding && <form className="space-y-2" onSubmit={e => { e.preventDefault(); void save(); }}>
      <textarea autoFocus aria-label="New internal note" className={controlClass} rows={3} maxLength={5000} value={body} disabled={saving} onChange={e => setBody(e.target.value)} />
      <div className="flex justify-end gap-2"><button type="button" className="ui-button" disabled={saving} onClick={() => { setAdding(false); setBody(""); submission.current = null; }}>Cancel</button><button className="ui-button ui-button-primary" disabled={saving || !body.trim()}>{saving ? "Saving…" : "Save note"}</button></div>
    </form>}
    {!notes && !error && <p className="text-zinc-400">Loading notes…</p>}
    {notes?.map(note => <article key={note.id} className="border-t border-zinc-800 pt-3"><div className="mb-2 flex flex-wrap justify-between gap-2 text-xs text-zinc-400"><span>{note.actorName}</span><time dateTime={note.createdAt}>{new Date(note.createdAt).toLocaleString("en-US", { timeZone: "America/New_York", dateStyle: "medium", timeStyle: "short" })} ET</time></div><p className="whitespace-pre-wrap break-words text-zinc-200">{note.body}</p></article>)}
    {legacy && <article className="border-t border-zinc-800 pt-3"><p className="mb-2 text-xs text-zinc-400">Existing notes · date not recorded</p><p className="whitespace-pre-wrap break-words text-zinc-300">{legacy}</p></article>}
    {notes?.length === 0 && !legacy && <p className="text-zinc-400">No notes recorded.</p>}
  </section>;
}
