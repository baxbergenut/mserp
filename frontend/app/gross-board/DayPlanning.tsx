"use client";

import { useContext, useState, useSyncExternalStore } from "react";
import { CurrentUserIDContext } from "@/app/lib/access";
import { fetchGrossBoard, saveGrossBoardPlanningEntries } from "@/app/lib/api";
import type { GrossBoardPlanningEntry, GrossBoardPlanningKind } from "@/app/lib/types";
import { BoardDialog } from "./BoardDialog";
import { acknowledgePlanningDraft, mergePlanningEntries, planningLength, readPlanningDrafts, writePlanningDrafts, type PlanningDrafts } from "./planning";

type PlanningSession = { drafts: PlanningDrafts; receipts: GrossBoardPlanningEntry[]; saving: boolean; error: string; notice: string; storageError: boolean };
const emptySession: PlanningSession = { drafts: {}, receipts: [], saving: false, error: "", notice: "", storageError: false };
// One state/request owner per user/day, even if filters, load counts or navigation
// unmount a row. A late acknowledgement always reconciles the latest draft.
const sessions = new Map<string, { value: PlanningSession; listeners: Set<() => void> }>();
function planningSession(key: string, driverId: string, date: string) {
  let session = sessions.get(key);
  if (!session) { session = { value: { ...emptySession, drafts: readPlanningDrafts(key, driverId, date) }, listeners: new Set() }; sessions.set(key, session); }
  return session;
}

function updatePlanningSession(key: string, value: Partial<PlanningSession>) {
  const session = sessions.get(key);
  if (!session) return;
  session.value = { ...session.value, ...value };
  session.listeners.forEach(listener => listener());
}

const kinds = ["note", "destination"] as const;
const names = { note: "Status / note", destination: "Planned destination" };

// State belongs to DaySummaryCell, not its single/grouped load subtree.
// Changing the load count must not remount drafts or an in-flight save.
export function useDayPlanning({ entries, driverId, driverName, date, week, readOnly, onAddLoad, onSaved }: {
  entries: GrossBoardPlanningEntry[]; driverId: string; driverName: string; date: string; week: string; readOnly: boolean;
  onAddLoad: () => void; onSaved: (entries: GrossBoardPlanningEntry[]) => void;
}) {
  const userId = useContext(CurrentUserIDContext);
  const storageKey = ["gross-board-planning-drafts:v1", userId, week, driverId, date].join(":");
  const session = planningSession(storageKey, driverId, date);
  const { drafts, receipts, saving, error, notice, storageError } = useSyncExternalStore(
    callback => { session.listeners.add(callback); return () => session.listeners.delete(callback); },
    () => session.value, () => emptySession);
  const patch = (value: Partial<PlanningSession>) => updatePlanningSession(storageKey, value);
  const [open, setOpen] = useState(false);
  const [kind, setKind] = useState<GrossBoardPlanningKind | "">("");
  const forThisDay = (entry: GrossBoardPlanningEntry) => entry.driverId === driverId && entry.date === date;
  const allSaved = mergePlanningEntries(entries.filter(forThisDay), receipts.filter(forThisDay));
  const saved = (type: GrossBoardPlanningKind): GrossBoardPlanningEntry => allSaved.find(entry => entry.kind === type) ?? { driverId, date, kind: type, text: "", version: 0 };
  const current = kind ? (readOnly ? saved(kind) : drafts[kind] ?? saved(kind)) : null;
  const latest = kind ? saved(kind) : null;
  const conflict = !!current && !!latest && current.version !== latest.version && current.text.trim() !== latest.text;
  const count = planningLength(current?.text ?? "");
  const putDrafts = (next: PlanningDrafts) => {
    patch({ drafts: next, storageError: !writePlanningDrafts(storageKey, next) });
  };
  const edit = (text: string) => {
    if (!kind || readOnly) return;
    putDrafts({ ...session.value.drafts, [kind]: { ...(session.value.drafts[kind] ?? saved(kind)), text } });
    patch({ notice: "", error: "" });
  };
  const cancel = () => {
    if (session.value.saving) return;
    if (kind) { const next = { ...session.value.drafts }; delete next[kind]; putDrafts(next); }
    patch({ error: "", notice: "" }); setOpen(false);
  };
  const save = async (replace = false) => {
    if (readOnly || !kind || session.value.saving || count > 1000) return;
    const pending = session.value.drafts[kind];
    if (!pending) return;
    const sent = { ...pending, driverId, date, kind, version: replace ? saved(kind).version : pending.version };
    if (replace) putDrafts({ ...session.value.drafts, [kind]: sent });
    patch({ saving: true, error: "", notice: "" });
    try {
      const committed = await saveGrossBoardPlanningEntries(week, [sent]);
      const receipt = committed.find(entry => entry.driverId === driverId && entry.date === date && entry.kind === sent.kind);
      if (!receipt) throw new Error("The save could not be verified. Your draft is kept; retry to check it.");
      patch({ receipts: mergePlanningEntries(session.value.receipts.filter(forThisDay), committed.filter(forThisDay)) });
      onSaved(committed);
      const next = acknowledgePlanningDraft(session.value.drafts, sent, receipt);
      putDrafts(next);
      patch({ notice: next[sent.kind] ? "Earlier text saved. Your newer edits still need Save." : "Saved" });
    } catch (err) {
      patch({ error: err instanceof Error ? err.message : "Unable to save. Your draft is kept." });
      // Refresh only planning state: never replace pending load autosaves.
      try { const board = await fetchGrossBoard(week); patch({ receipts: mergePlanningEntries(session.value.receipts.filter(forThisDay), (board.planningEntries ?? []).filter(forThisDay)) }); onSaved(board.planningEntries ?? []); } catch { /* Retry remains available. */ }
    } finally { patch({ saving: false }); }
  };
  const choose = (type: GrossBoardPlanningKind) => { setKind(type); setOpen(true); patch({ error: "", notice: "" }); };

  return <div data-gross-planning className="border-t border-zinc-800/70 px-1 py-1 text-left font-sans text-xs" onContextMenu={event => event.stopPropagation()}>
    {kinds.map(type => {
      const entry = readOnly ? saved(type) : drafts[type] ?? saved(type);
      if (!entry.text && !drafts[type]) return null;
      return <button type="button" key={type} aria-label={names[type] + " for " + driverName + ", " + date} title={entry.text}
        className="block w-full rounded px-1 py-1 text-left text-zinc-300 hover:bg-zinc-800 focus-visible:outline-2 focus-visible:outline-blue-400"
        onClick={() => choose(type)}>
        <span className="block text-[11px] text-zinc-400">{type === "note" ? "Note" : "Destination"}{!readOnly && drafts[type] ? " · Unsaved" : ""}</span>
        <span className="line-clamp-2 whitespace-pre-wrap break-words">{entry.text || "Clear entry"}</span>
      </button>;
    })}
    {!readOnly && <button type="button" aria-label={"Add entry for " + driverName + ", " + date}
      className="min-h-8 w-full rounded px-1 text-left text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100 focus-visible:outline-2 focus-visible:outline-blue-400"
      onClick={() => { setKind(""); patch({ error: "", notice: "" }); setOpen(true); }}>+ Add entry</button>}
    {open && <BoardDialog title="Day entry" onClose={() => setOpen(false)}>
      <div data-gross-editor-day={driverId + ":" + date}><p className="mb-4 text-xs text-zinc-400">{driverName} · {date}</p>
      {!readOnly && <fieldset className="mb-4"><legend className="mb-2 text-xs text-zinc-400">Entry type</legend>
        <div className="flex flex-wrap gap-2">
          <button type="button" className="ui-button" onClick={() => { setOpen(false); onAddLoad(); }}>Load</button>
          {kinds.map(type => <button type="button" key={type} aria-pressed={kind === type} className={"ui-button " + (kind === type ? "border-blue-500 text-blue-300" : "")} onClick={() => choose(type)}>{names[type]}</button>)}
        </div>
      </fieldset>}
      {!kind && <p className="text-xs text-zinc-400">Choose a type before entering text. Loads join the dispatch queue; notes and destinations stay on Gross Board.</p>}
      {kind && current && <>
        <label className="mb-2 block text-xs text-zinc-400" htmlFor={storageKey + kind}>{names[kind]}</label>
        <textarea id={storageKey + kind} aria-label={names[kind]} value={current.text} readOnly={readOnly} rows={4} aria-invalid={count > 1000}
          className="ui-input w-full resize-y whitespace-pre-wrap" onChange={event => edit(event.target.value)}
          placeholder={kind === "note" ? "Available after 3 PM" : "Dallas, TX"} />
        <div className="mt-2 flex justify-between gap-4 text-xs text-zinc-400"><span>Gross Board only. Does not change loads, dispatch, gross or payroll.</span><span className={count > 1000 ? "text-red-300" : ""}>{count}/1000</span></div>
        {conflict && !readOnly && <div role="alert" className="mt-4 rounded-lg border border-amber-500/40 p-3 text-xs text-amber-300">
          <p>This entry changed elsewhere. Review the saved text before replacing it.</p><p className="mt-2 whitespace-pre-wrap break-words">Saved: {latest?.text || "(cleared)"}</p>
        </div>}
        {error && <p role="alert" className="mt-3 text-xs text-red-300">{error} Your draft is kept.</p>}
        {storageError && <p role="alert" className="mt-3 text-xs text-amber-300">This browser cannot keep a recovery copy. Save before leaving this page.</p>}
        {notice && <p role="status" className="mt-3 text-xs text-zinc-400">{notice}</p>}
        <div className="mt-4 flex flex-wrap justify-end gap-2">
          {!readOnly && !!current.text && <button type="button" className="ui-button mr-auto" disabled={saving} onClick={() => edit("")}>Clear text</button>}
          <button type="button" className="ui-button" disabled={saving} onClick={cancel}>{!readOnly && drafts[kind] ? "Cancel" : "Close"}</button>
          {!readOnly && <button type="button" className="ui-button ui-button-primary" disabled={saving || count > 1000 || !drafts[kind]} onClick={() => void save(conflict)}>{saving ? "Saving…" : conflict ? "Replace with my draft" : error ? "Retry save" : "Save"}</button>}
        </div>
        {!readOnly && drafts[kind] && <p className="mt-3 text-xs text-zinc-400">Closing keeps your draft in this tab. Cancel discards this draft.</p>}
      </>}
    </div></BoardDialog>}
  </div>;
}
