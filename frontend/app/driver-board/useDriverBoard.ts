"use client";

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { fetchDriverBoard, saveDriverBoard, undoDriverBoardEvent, fetchBoardLoads, changeBoardLoads, syncFiveELD } from "@/app/lib/api";
import type { DriverBoard, DriverBoardEntry, BoardLoads, BoardLoadAction } from "@/app/lib/types";
import { currentChargeWeek } from "@/app/accounting/driver-charges/charges";
import { progressStatus, reconcileDriverBoard } from "./board";
import { usePermissions } from "@/app/lib/access";

export function useDriverBoard(pauseRefresh = false) {
  const canEdit = usePermissions().includes("driver_board.write");
  const router = useRouter();
  const [board, setBoard] = useState<DriverBoard | null>(null);
  const [changes, setChanges] = useState<Record<string, DriverBoardEntry>>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [actionBusy, setActionBusy] = useState(false);
  const [error, setError] = useState("");
  const [refreshError, setRefreshError] = useState("");
  const [eldRefreshing, setELDRefreshing] = useState(false);
  const [pendingLink, setPendingLink] = useState<string | null>(null);
  const activity = useRef(0);
  const savingRef = useRef(false);
  const idle = useRef(false);
  const savedRef = useRef<Record<string, DriverBoardEntry>>({});
  const changesRef = useRef(changes);
  const undoStack = useRef<{ id: number; driverId: string; group: number }[]>([]);
  const draftUndo = useRef<{ id: string; field: keyof DriverBoardEntry; before?: DriverBoardEntry; group: number }[]>([]);
  const editSession = useRef<{ id: string; field: keyof DriverBoardEntry; group: number } | null>(null);
  const editSequence = useRef(0);
  const pendingUndo = useRef(false);
  const undoLatestRef = useRef<() => Promise<void>>(async () => {});
  const dirty = Object.keys(changes).length > 0;
  const saved = useMemo(() => Object.fromEntries((board?.entries ?? []).map(e => [e.driverId, e])), [board]);
  useLayoutEffect(() => { savedRef.current = saved; }, [saved]);
  useLayoutEffect(() => { idle.current = !dirty && !saving && !loading && !eldRefreshing && !pauseRefresh; }, [dirty, saving, loading, eldRefreshing, pauseRefresh]);

  useEffect(() => {
    let cancelled = false;
    fetchDriverBoard(currentChargeWeek()).then(value => { if (!cancelled) setBoard(value); })
      .catch(err => { if (!cancelled) setError(err instanceof Error ? err.message : "Unable to load Status Board"); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, []);

  useEffect(() => {
    let cancelled = false, inFlight = false;
    const refresh = async () => {
      if (!idle.current || document.visibilityState !== "visible" || inFlight) return;
      const revision = activity.current;
      inFlight = true;
      try {
        const value = await fetchDriverBoard(currentChargeWeek());
        if (!cancelled && idle.current && revision === activity.current) { setBoard(value); setRefreshError(""); }
      } catch { if (!cancelled) setRefreshError("Live refresh is unavailable. Use Reload to update totals and assignments."); }
      finally { inFlight = false; }
    };
    const timer = setInterval(() => void refresh(), 30000);
    const focus = () => void refresh();
    window.addEventListener("focus", focus); document.addEventListener("visibilitychange", focus);
    return () => { cancelled = true; clearInterval(timer); window.removeEventListener("focus", focus); document.removeEventListener("visibilitychange", focus); };
  }, []);

  const edit = useCallback((id: string, field: keyof DriverBoardEntry, value: string) => {
    activity.current += 1;
    idle.current = false;
    if (!editSession.current || editSession.current.id !== id || editSession.current.field !== field) editSession.current = { id, field, group: ++editSequence.current };
    const last = draftUndo.current.at(-1);
    if (!last || last.group !== editSession.current.group) draftUndo.current.push({ id, field, before: changesRef.current[id], group: editSession.current.group });
    const current = changesRef.current;
    const baseline = savedRef.current[id];
    const next = { ...(current[id] ?? baseline), [field]: value };
    if (field === "trailerNumber" && value !== (current[id] ?? baseline).trailerNumber) next.trailerNote = "";
    if (field === "currentLoad") { next.destination = ""; next.resolveCurrentLoad = true; next.statusEdited = false; next.status = value.trim() ? "DISPATCHED" : ""; }
    if (field === "status") next.statusEdited = true;
    const result = { ...current, [id]: next };
    // While saving, a revert must still be sent after the first write finishes.
    if (!savingRef.current && JSON.stringify(next) === JSON.stringify(baseline)) delete result[id];
    changesRef.current = result;
    setChanges(result);
  }, []);

  const refreshLoads = useCallback(async (ids: string[]) => {
    try {
      const rows = await Promise.all(ids.map(async id => [id, await fetchBoardLoads(id)] as const));
      setBoard(current => current ? { ...current, loads: { ...current.loads, ...Object.fromEntries(rows) } } : current);
    } catch { setRefreshError("Load details could not be refreshed. Use Reload to review the saved plans."); }
  }, []);

  const save = useCallback(async () => {
    if (savingRef.current || !Object.keys(changesRef.current).length || loading) return;
    savingRef.current = true; activity.current += 1;
    const snapshot = { ...changesRef.current };
    const draftActions = draftUndo.current;
    draftUndo.current = [];
    setSaving(true); setError("");
    try {
      const committed = await saveDriverBoard(Object.values(snapshot));
      const loadIds = committed.filter(e => { const base = savedRef.current[e.driverId]; return !base || base.currentLoad !== e.currentLoad || base.status !== e.status || base.destination !== e.destination || snapshot[e.driverId]?.resolveCurrentLoad; }).map(e => e.driverId);
      // Advance the authoritative refs before releasing the save lock. A new
      // keystroke can arrive before React renders this response.
      committed.forEach(e => { savedRef.current[e.driverId] = e; });
      const remaining = reconcileDriverBoard(changesRef.current, snapshot, committed);
      changesRef.current = remaining;
      setChanges(remaining);
      const editOrder = draftActions.map(a => a.id);
      for (const e of [...committed].sort((a,b) => editOrder.lastIndexOf(a.driverId) - editOrder.lastIndexOf(b.driverId))) if (e.undoId) undoStack.current.push({ id: e.undoId, driverId: e.driverId, group: draftActions.findLast(a => a.id === e.driverId)?.group ?? ++editSequence.current });
      setBoard(current => {
        if (!current) return current;
        const entries = Object.fromEntries(current.entries.map(e => [e.driverId, e]));
        committed.forEach(e => { entries[e.driverId] = e; });
        return { ...current, entries: Object.values(entries) };
      });
      if (loadIds.length) await refreshLoads(loadIds);
    } catch (err) { draftUndo.current = [...draftActions, ...draftUndo.current]; setError(err instanceof Error ? err.message : "Unable to save. Your edits are still here."); }
    finally { activity.current += 1; savingRef.current = false; setSaving(false); }
  }, [loading, refreshLoads]);

  useEffect(() => {
    if (!dirty || saving || error || loading) return;
    let cancelled = false;
    queueMicrotask(() => { if (!cancelled) void save(); });
    return () => { cancelled = true; };
  }, [dirty, saving, error, loading, save, pendingLink]);

  useEffect(() => {
    if (!dirty && !saving) return;
    const beforeUnload = (event: BeforeUnloadEvent) => event.preventDefault();
    const leave = (event: MouseEvent) => {
      const link = (event.target as Element).closest?.("a[href]");
      if (link && event.button === 0 && !event.ctrlKey && !event.metaKey && !event.shiftKey && link.getAttribute("target") !== "_blank") {
        const href = link.getAttribute("href");
        if (!href?.startsWith("/") && !href?.startsWith("http")) return;
        event.preventDefault(); event.stopPropagation(); setPendingLink(href);
      }
    };
    window.addEventListener("beforeunload", beforeUnload); document.addEventListener("click", leave, true);
    return () => { window.removeEventListener("beforeunload", beforeUnload); document.removeEventListener("click", leave, true); };
  }, [dirty, saving]);

  useEffect(() => {
    if (!pendingLink || dirty || saving || error || loading) return;
    if (pendingLink.startsWith("/") && !pendingLink.startsWith("//")) router.push(pendingLink, { scroll: false });
    else window.location.assign(pendingLink);
  }, [pendingLink, dirty, saving, error, loading, router]);

  async function reload() {
    if (savingRef.current) return;
    if (dirty && !window.confirm("Discard unsaved changes and reload the saved Status Board?")) return;
    activity.current += 1; idle.current = false;
    setLoading(true); setError(""); setPendingLink(null); changesRef.current = {}; setChanges({}); draftUndo.current = []; undoStack.current = [];
    try { setBoard(await fetchDriverBoard(currentChargeWeek())); setRefreshError(""); }
    catch (err) { setError(err instanceof Error ? err.message : "Unable to load Status Board"); }
    finally { setLoading(false); }
  }

  async function refreshELD() {
    if (dirty || savingRef.current || loading || eldRefreshing) return;
    activity.current += 1; idle.current = false; setELDRefreshing(true); setRefreshError("");
    try {
      await syncFiveELD();
      setBoard(await fetchDriverBoard(currentChargeWeek()));
    } catch (err) {
      setRefreshError(err instanceof Error ? err.message : "Five ELD locations could not be refreshed.");
    } finally {
      activity.current += 1; setELDRefreshing(false);
    }
  }

  async function undo(eventId: number, driverId: string, personal = false) {
    if (dirty || savingRef.current || loading) throw new Error("Wait for your board changes to save first.");
    const entry = savedRef.current[driverId];
    if (!entry) throw new Error("This driver is no longer on the active board.");
    savingRef.current = true; idle.current = false; activity.current += 1; setSaving(true); setActionBusy(true);
    try {
      const committed = await undoDriverBoardEvent(eventId, entry, personal);
      savedRef.current[driverId] = committed;
      undoStack.current = undoStack.current.filter(e => e.id !== eventId);
      setBoard(current => current ? { ...current, entries: current.entries.map(e => e.driverId === driverId ? committed : e) } : current);
      await refreshLoads([driverId]);
    } finally { activity.current += 1; savingRef.current = false; setSaving(false); setActionBusy(false); }
  }

  async function changeLoads(driverId: string, view: BoardLoads, action: BoardLoadAction) {
    if (dirty || savingRef.current || loading) throw new Error("Wait for your board changes to save first.");
    const entry = savedRef.current[driverId];
    if (!entry) throw new Error("This driver is no longer on the active board.");
    savingRef.current = true; idle.current = false; activity.current += 1; setSaving(true); setActionBusy(true);
    try {
      const result = await changeBoardLoads(entry, view, action);
      editSession.current = null;
      if (result.entry.undoId) undoStack.current.push({ id: result.entry.undoId, driverId, group: ++editSequence.current });
      setBoard(current => current ? { ...current, entries: current.entries.map(e => e.driverId === driverId ? result.entry : e), loads: { ...current.loads, [driverId]: result.loads } } : current);
      return result.loads;
    } finally { activity.current += 1; savingRef.current = false; setSaving(false); setActionBusy(false); }
  }

  useLayoutEffect(() => {
    undoLatestRef.current = async () => {
      if (!canEdit || loading || pendingLink) return;
      if (savingRef.current || (dirty && !error)) { pendingUndo.current = true; return; }
      setError("");
      if (dirty) {
        const action = draftUndo.current.pop();
        if (!action) return;
        const result = { ...changesRef.current };
        const base = savedRef.current[action.id];
        if (action.before) result[action.id] = { ...action.before, version: base.version, undoId: base.undoId };
        else delete result[action.id];
        if (JSON.stringify(result[action.id]) === JSON.stringify(base)) delete result[action.id];
        changesRef.current = result; setChanges(result); activity.current += 1;
        return;
      }
      const action = undoStack.current.at(-1);
      if (action) {
        editSession.current = null;
        while (undoStack.current.at(-1)?.group === action.group) {
          const latest = undoStack.current.at(-1)!;
          await undo(latest.id, latest.driverId, true);
        }
      }
    };
  });

  useEffect(() => {
    const boundary = (event: FocusEvent) => {
      const target = event.target as HTMLElement;
      if (target.matches('input,textarea,select')) editSession.current = null;
    };
    document.addEventListener("focusout", boundary);
    return () => document.removeEventListener("focusout", boundary);
  }, []);

  useEffect(() => {
    const keyboard = (event: KeyboardEvent) => {
      if (!canEdit || !(event.ctrlKey || event.metaKey) || event.altKey || event.shiftKey || event.key.toLowerCase() !== "z" || event.repeat) return;
      const target = event.target as HTMLElement;
      // Leave unrelated controls and dialog drafts to their native undo.
      const dialog = target.closest('[role="dialog"]');
      if ((dialog && !dialog.hasAttribute('data-board-undo')) || (target.closest('input,textarea,[contenteditable="true"]') && !target.closest('[data-spreadsheet-grid],[data-board-undo]'))) return;
      if (!dirty && !savingRef.current && !undoStack.current.length) return;
      event.preventDefault();
      void undoLatestRef.current().catch(err => setError(err instanceof Error ? err.message : "Unable to undo this change."));
    };
    document.addEventListener("keydown", keyboard);
    return () => document.removeEventListener("keydown", keyboard);
  }, [canEdit, dirty]);

  useEffect(() => {
    if (saving || (dirty && !error) || !pendingUndo.current) return;
    pendingUndo.current = false;
    void undoLatestRef.current().catch(err => setError(err instanceof Error ? err.message : "Unable to undo this change."));
  }, [saving, dirty, error]);

  const entries = Object.fromEntries(Object.entries({ ...saved, ...changes }).map(([id, entry]) => [id, { ...entry, status: progressStatus(entry.status, board?.loads[id]) }]));
  return { board, entries, loading, saving, actionBusy, dirty, error, refreshError, eldRefreshing, edit, save, reload, refreshELD, undo, changeLoads, leaving: !!pendingLink };
}
