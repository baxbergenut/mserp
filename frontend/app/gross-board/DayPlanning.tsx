"use client";

import { useContext, useState, useSyncExternalStore } from "react";
import { CurrentUserIDContext } from "@/app/lib/access";
import { fetchGrossBoard, saveGrossBoardPlanningEntries } from "@/app/lib/api";
import type { GrossBoardPlanningEntry, GrossBoardPlanningKind } from "@/app/lib/types";
import { acknowledgePlanningDraft, mergePlanningEntries, planningLength, readPlanningDrafts, writePlanningDrafts, type PlanningDrafts } from "./planning";

type PlanningSession = { drafts: PlanningDrafts; receipts: GrossBoardPlanningEntry[]; saving: boolean; error: string; storageError: boolean };
const emptySession: PlanningSession = { drafts: {}, receipts: [], saving: false, error: "", storageError: false };
// One state/request owner per user/day. Load layout changes and late replies must
// not lose an inline edit or let an older acknowledgement erase newer typing.
const sessions = new Map<string, { value: PlanningSession; listeners: Set<() => void> }>();
const requests = new Map<string, Promise<void>>();
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

export function useDayPlanning({ entries, driverId, driverName, date, week, readOnly, fullDayEligible, onSaved }: {
  entries: GrossBoardPlanningEntry[]; driverId: string; driverName: string; date: string; week: string; readOnly: boolean; fullDayEligible: boolean;
  onSaved: (entries: GrossBoardPlanningEntry[]) => void;
}) {
  const userId = useContext(CurrentUserIDContext);
  const storageKey = ["gross-board-planning-drafts:v1", userId, week, driverId, date].join(":");
  const session = planningSession(storageKey, driverId, date);
  const { drafts, saving, error, storageError } = useSyncExternalStore(
    callback => { session.listeners.add(callback); return () => session.listeners.delete(callback); },
    () => session.value, () => emptySession);
  const patch = (value: Partial<PlanningSession>) => updatePlanningSession(storageKey, value);
  const [editing, setEditing] = useState<GrossBoardPlanningKind | null>(null);
  const forThisDay = (entry: GrossBoardPlanningEntry) => entry.driverId === driverId && entry.date === date;
  const saved = (kind: GrossBoardPlanningKind): GrossBoardPlanningEntry =>
    mergePlanningEntries(entries.filter(forThisDay), session.value.receipts.filter(forThisDay)).find(entry => entry.kind === kind)
    ?? { driverId, date, kind, text: "", version: 0 };
  const putDrafts = (next: PlanningDrafts) => patch({ drafts: next, storageError: !writePlanningDrafts(storageKey, next) });
  const edit = (kind: GrossBoardPlanningKind, text: string) => {
    if (readOnly) return;
    const latest = saved(kind);
    const recovered = session.value.drafts[kind];
    // A reload may retain a draft whose save committed before its acknowledgement.
    // Identical saved text is already accepted; start the new edit at that revision.
    const baseline = recovered && (recovered.text.trim() !== latest.text || recovered.version > latest.version) ? recovered : latest;
    putDrafts({ ...session.value.drafts, [kind]: { ...baseline, text } });
    patch({ error: "" });
  };
  const cancel = (kind: GrossBoardPlanningKind) => {
    if (session.value.saving) return;
    const next = { ...session.value.drafts }; delete next[kind]; putDrafts(next);
    patch({ error: "" }); setEditing(null);
  };
  const save = async (kind: GrossBoardPlanningKind, replace = false): Promise<void> => {
    if (readOnly) return;
    // Serialize requests across both legacy kinds without dropping blur saves.
    const active = requests.get(storageKey);
    if (active) { await active; return save(kind, replace); }
    const run = async () => {
      let replaceNext = replace;
      while (session.value.drafts[kind]) {
        const pending = session.value.drafts[kind]!;
        if (planningLength(pending.text) > 1000) throw new Error("Custom status must be 1,000 characters or fewer.");
        const latest = saved(kind);
        if (!replaceNext && pending.version !== latest.version && pending.text.trim() !== latest.text)
          throw new Error("This custom status changed elsewhere. Review the saved text below.");
        const sent = { ...pending, driverId, date, kind, version: replaceNext ? latest.version : pending.version };
        replaceNext = false;
        if (sent.version !== pending.version) putDrafts({ ...session.value.drafts, [kind]: sent });
        const committed = await saveGrossBoardPlanningEntries(week, [sent]);
        const receipt = committed.find(entry => forThisDay(entry) && entry.kind === kind);
        if (!receipt) throw new Error("The save could not be verified. Your draft is kept; retry to check it.");
        patch({ receipts: mergePlanningEntries(session.value.receipts.filter(forThisDay), committed.filter(forThisDay)) });
        onSaved(committed);
        putDrafts(acknowledgePlanningDraft(session.value.drafts, sent, receipt));
        // If typing continued during the request, save that newer draft using
        // the acknowledged version instead of falsely marking it saved.
      }
    };
    patch({ saving: true, error: "" });
    const request = run().catch(async (err: unknown) => {
      patch({ error: err instanceof Error ? err.message : "Unable to save. Your draft is kept." });
      // Only refresh planning state, never pending load autosaves.
      try {
        const board = await fetchGrossBoard(week);
        patch({ receipts: mergePlanningEntries(session.value.receipts.filter(forThisDay), (board.planningEntries ?? []).filter(forThisDay)) });
        onSaved(board.planningEntries ?? []);
      } catch { /* Keep the draft and retry control. */ }
      throw err;
    }).finally(() => { requests.delete(storageKey); patch({ saving: false }); });
    requests.set(storageKey, request);
    return request;
  };
  const finish = (kind: GrossBoardPlanningKind, replace = false) => {
    void save(kind, replace).then(() => setEditing(current => current === kind ? null : current)).catch(() => { /* Inline error and retained draft. */ });
  };
  const createCustomStatus = async (text: string): Promise<void> => {
    if (readOnly) return;
    if (!fullDayEligible) throw new Error("Clear this day’s loads before adding a custom status.");
    const value = text.trim();
    if (!value) return;
    const current = session.value.drafts.note ?? saved("note");
    if (current.text.trim() && current.text.trim() !== value) {
      setEditing("note");
      throw new Error("This day already has a custom status. Edit it inline to replace it.");
    }
    edit("note", value);
    await save("note");
  };
  const fullDay = fullDayEligible && kinds.some(kind => !!saved(kind).text || (!readOnly && !!drafts[kind]));
  const hasPlanning = kinds.some(kind => !!saved(kind).text || (!readOnly && !!drafts[kind]));
  const planningContent = hasPlanning ? <div data-gross-planning data-gross-planning-full-day={fullDay || undefined}
    className={fullDay ? "flex h-full min-h-[152px] flex-col px-1 py-1 font-sans text-xs" : "border-t border-zinc-800/70 px-1 py-1 text-left font-sans text-xs"}
    onContextMenu={event => event.stopPropagation()}>
    <div className={fullDay ? "flex flex-1 flex-col" : undefined}>
      {kinds.map(kind => {
        const latest = saved(kind);
        const current = readOnly ? latest : drafts[kind] ?? latest;
        if (!current.text && (readOnly || !drafts[kind])) return null;
        const conflict = current.version !== latest.version && current.text.trim() !== latest.text;
        const count = planningLength(current.text);
        const label = `Custom status for ${driverName}, ${date}${kind === "destination" ? ", existing destination" : ""}`;
        if (editing === kind && !readOnly) return <div key={kind} data-grid-ignore data-gross-editor-day={`${driverId}:${date}`} className="flex min-w-0 flex-1 flex-col gap-1 py-1">
          <textarea autoFocus aria-label={label} rows={fullDay ? 4 : 2} value={current.text} aria-invalid={count > 1000}
            className="w-full min-w-0 flex-1 resize-none rounded border border-blue-500 bg-zinc-900 p-2 text-xs text-zinc-100 outline-none"
            placeholder="Status or destination" onChange={event => edit(kind, event.target.value)}
            onBlur={event => { if (!event.currentTarget.parentElement?.contains(event.relatedTarget)) finish(kind); }}
            onKeyDown={event => {
              if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); cancel(kind); }
              if (event.key === "Enter" && !event.shiftKey) { event.preventDefault(); event.stopPropagation(); finish(kind); }
            }} />
          <span className={count > 1000 ? "text-red-300" : "text-[10px] text-zinc-500"}>{count > 1000 ? `${count}/1000 characters` : "Enter to save · Esc to cancel"}</span>
          {conflict && <div role="alert" className="min-w-0 text-xs text-amber-300">
            <p>Changed elsewhere. Saved: <span className="whitespace-pre-wrap break-words [overflow-wrap:anywhere]">{latest.text || "(cleared)"}</span></p>
            <button type="button" disabled={saving || count > 1000} className="mr-2 underline" onClick={() => finish(kind, true)}>Replace with my text</button>
            <button type="button" disabled={saving} className="underline" onClick={() => cancel(kind)}>Use saved text</button>
          </div>}
        </div>;
        return <button type="button" key={kind} data-grid-action={fullDay || undefined} aria-label={label} title={current.text} disabled={readOnly}
          className={`w-full min-w-0 rounded text-zinc-300 enabled:hover:bg-zinc-800 focus-visible:outline-2 focus-visible:outline-blue-400 ${fullDay ? "flex min-h-16 flex-1 flex-col items-center justify-center gap-1 px-2 py-2 text-center" : "block px-1 py-1 text-left"}`}
          onClick={() => { setEditing(kind); patch({ error: "" }); }}>
          <span className={`whitespace-pre-wrap break-words [overflow-wrap:anywhere] ${fullDay ? "line-clamp-4 w-full font-medium" : "line-clamp-2"}`}>{current.text || "Clear custom status"}</span>
          {!readOnly && drafts[kind] && <span className="block text-[10px] text-amber-300">Unsaved</span>}
        </button>;
      })}
    </div>
    {!readOnly && saving && <p role="status" className="text-[10px] text-zinc-400">Saving…</p>}
    {!readOnly && error && <div role="alert" className="text-xs text-red-300">{error} Your draft is kept. <button type="button" disabled={saving} className="underline" onClick={() => {
      const kind = kinds.find(type => session.value.drafts[type]); if (kind) { setEditing(kind); finish(kind); }
    }}>Retry</button></div>}
    {!readOnly && storageError && <p role="alert" className="text-xs text-amber-300">Keep this page open; draft recovery is unavailable.</p>}
  </div> : null;
  const planning = hasPlanning && !fullDayEligible ? <details className="border-t border-amber-500/30 p-1 text-[10px] text-amber-300">
    <summary className="cursor-pointer">Review saved status</summary>
    <p className="py-1">This older day contains both load data and a saved status. Clear the saved status, or clear the loads before using it. Nothing has been removed.</p>
    {planningContent}
  </details> : planningContent;
  return { planning, fullDay, dialog: null, createCustomStatus };
}
