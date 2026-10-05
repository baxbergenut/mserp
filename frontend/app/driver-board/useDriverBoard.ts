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
  const [error, setError] = useState("");
  const [refreshError, setRefreshError] = useState("");
  const [eldRefreshing, setELDRefreshing] = useState(false);
  const [pendingLink, setPendingLink] = useState<string | null>(null);
  const activity = useRef(0);
  const savingRef = useRef(false);
  const idle = useRef(false);
  const savedRef = useRef<Record<string, DriverBoardEntry>>({});
  const changesRef = useRef(changes);
  const undoStack = useRef<{ id: number; driverId: string }[]>([]);
  const draftUndo = useRef<{ id: string; field: keyof DriverBoardEntry; before?: DriverBoardEntry }[]>([]);
  const pendingUndo = useRef(false);
  const undoLatestRef = useRef<() => Promise<void>>(async () => {});
  const dirty = Object.keys(changes).length > 0;
  const saved = useMemo(() => Object.fromEntries((board?.entries ?? []).map(e => [e.driverId, e])), [board]);
  useLayoutEffect(() => { changesRef.current = changes; savedRef.current = saved; idle.current = !dirty && !saving && !loading && !eldRefreshing && !pauseRefresh; }, [saved, changes, dirty, saving, loading, eldRefreshing, pauseRefresh]);

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
    const last = draftUndo.current.at(-1);
    if (!last || last.id !== id || last.field !== field) draftUndo.current.push({ id, field, before: changesRef.current[id] });
    const current = changesRef.current;
    const baseline = savedRef.current[id];
    const next = { ...(current[id] ?? baseline), [field]: value };
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
    if (savingRef.current || !dirty || loading) return;
    savingRef.current = true; activity.current += 1;
    const snapshot = { ...changes };
    const draftActions = draftUndo.current;
    draftUndo.current = [];
    setSaving(true); setError("");
    try {
      const committed = await saveDriverBoard(Object.values(snapshot));
      const editOrder = draftActions.map(a => a.id);
      for (const e of [...committed].sort((a,b) => editOrder.lastIndexOf(a.driverId) - editOrder.lastIndexOf(b.driverId))) if (e.undoId) undoStack.current.push({ id: e.undoId, driverId: e.driverId });
      setBoard(current => {
        if (!current) return current;
        const entries = Object.fromEntries(current.entries.map(e => [e.driverId, e]));
        committed.forEach(e => { entries[e.driverId] = e; });
        return { ...current, entries: Object.values(entries) };
      });
      setChanges(current => reconcileDriverBoard(current, snapshot, committed));
      await refreshLoads(committed.map(e => e.driverId));
    } catch (err) { draftUndo.current = [...draftActions, ...draftUndo.current]; setError(err instanceof Error ? err.message : "Unable to save. Your edits are still here."); }
    finally { activity.current += 1; savingRef.current = false; setSaving(false); }
  }, [changes, dirty, loading, refreshLoads]);

  useEffect(() => {
    if (!dirty || saving || error || loading) return;
    const timer = setTimeout(() => void save(), pendingLink ? 0 : 5000);
    return () => clearTimeout(timer);
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
    setLoading(true); setError(""); setPendingLink(null); setChanges({}); draftUndo.current = [];
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
    savingRef.current = true; idle.current = false; activity.current += 1; setSaving(true);
    try {
      const committed = await undoDriverBoardEvent(eventId, entry, personal);
      undoStack.current = undoStack.current.filter(e => e.id !== eventId);
      setBoard(current => current ? { ...current, entries: current.entries.map(e => e.driverId === driverId ? committed : e) } : current);
      await refreshLoads([driverId]);
    } finally { activity.current += 1; savingRef.current = false; setSaving(false); }
  }

  async function changeLoads(driverId: string, view: BoardLoads, action: BoardLoadAction) {
    if (dirty || savingRef.current || loading) throw new Error("Wait for your board changes to save first.");
    const entry = savedRef.current[driverId];
    if (!entry) throw new Error("This driver is no longer on the active board.");
    savingRef.current = true; idle.current = false; activity.current += 1; setSaving(true);
    try {
      const result = await changeBoardLoads(entry, view, action);
      if (result.entry.undoId) undoStack.current.push({ id: result.entry.undoId, driverId });
      setBoard(current => current ? { ...current, entries: current.entries.map(e => e.driverId === driverId ? result.entry : e), loads: { ...current.loads, [driverId]: result.loads } } : current);
      return result.loads;
    } finally { activity.current += 1; savingRef.current = false; setSaving(false); }
  }

  useLayoutEffect(() => {
    undoLatestRef.current = async () => {
      if (!canEdit || loading || pendingLink) return;
      if (savingRef.current) { pendingUndo.current = true; return; }
      setError("");
      if (dirty) {
        const action = draftUndo.current.pop();
        if (!action) return;
        const result = { ...changesRef.current };
        const base = savedRef.current[action.id];
        if (action.before) result[action.id] = { ...action.before, version: base.version, homeVersion: base.homeVersion, undoId: base.undoId };
        else delete result[action.id];
        if (JSON.stringify(result[action.id]) === JSON.stringify(base)) delete result[action.id];
        changesRef.current = result; setChanges(result); activity.current += 1;
        return;
      }
      const action = undoStack.current.at(-1);
      if (action) await undo(action.id, action.driverId, true);
    };
  });

  useEffect(() => {
    const keyboard = (event: KeyboardEvent) => {
      if (!canEdit || !(event.ctrlKey || event.metaKey) || event.altKey || event.shiftKey || event.key.toLowerCase() !== "z" || event.repeat) return;
      const target = event.target as HTMLElement;
      // Keep native text undo while typing, and leave other dialogs' drafts alone.
      const dialog = target.closest('[role="dialog"]');
      if ((dialog && !dialog.hasAttribute('data-board-undo')) || (dirty && target.closest('input,textarea,[contenteditable="true"]'))) return;
      if (!dirty && !savingRef.current && !undoStack.current.length) return;
      event.preventDefault();
      void undoLatestRef.current().catch(err => setError(err instanceof Error ? err.message : "Unable to undo this change."));
    };
    document.addEventListener("keydown", keyboard);
    return () => document.removeEventListener("keydown", keyboard);
  }, [canEdit, dirty]);

  useEffect(() => {
    if (saving || !pendingUndo.current) return;
    pendingUndo.current = false;
    void undoLatestRef.current().catch(err => setError(err instanceof Error ? err.message : "Unable to undo this change."));
  }, [saving]);

  const entries = Object.fromEntries(Object.entries({ ...saved, ...changes }).map(([id, entry]) => [id, { ...entry, status: progressStatus(entry.status, board?.loads[id]) }]));
  return { board, entries, loading, saving, dirty, error, refreshError, eldRefreshing, edit, save, reload, refreshELD, undo, changeLoads, leaving: !!pendingLink };
}
