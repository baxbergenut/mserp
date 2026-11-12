"use client";

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useBoardNavigation } from "@/app/lib/useBoardNavigation";
import { fetchDriverBoard, saveDriverBoard, reorderBoardDrivers, updateBoardAutoOrder, undoDriverBoardEvent, fetchBoardLoads, changeBoardLoads, syncFiveELD } from "@/app/lib/api";
import type { DriverBoard, DriverBoardEntry, BoardLoads, BoardLoadAction } from "@/app/lib/types";
import { currentChargeWeek } from "@/app/accounting/driver-charges/charges";
import { applyCommittedFleetStatus, progressStatus, reconcileDriverBoard } from "./board";
import { usePermissions } from "@/app/lib/access";
import { useBoardLive } from "./useBoardLive";
import { selectedBoardCell } from "./boardCells";
import { orderDriverRows, transferDriverRows } from "./driverOrder";

export function useDriverBoard(lockedDriverId?: string) {
  const canEdit = usePermissions().includes("driver_board.write");
  const [board, setBoard] = useState<DriverBoard | null>(null);
  const [changes, setChanges] = useState<Record<string, DriverBoardEntry>>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [actionBusy, setActionBusy] = useState(false);
  const [orderSaving, setOrderSaving] = useState(false);
  const orderSavingRef = useRef(false);
  const [error, setError] = useState("");
  const [refreshError, setRefreshError] = useState("");
  const [eldRefreshing, setELDRefreshing] = useState(false);
  const activity = useRef(0);
  const savingRef = useRef(false);
  const idle = useRef(false);
  const refreshWanted = useRef<(board?: DriverBoard) => void>(() => {});
  const resumeLive = useRef<() => void>(() => {});
  const busyDrivers = useRef<string[]>([]);
  const lockedDriver = useRef(lockedDriverId);
  const live = useBoardLive(useCallback((value?: DriverBoard) => refreshWanted.current(value), []));
  const connectedRef = useRef(false);
  useLayoutEffect(() => { connectedRef.current = live.connected; }, [live.connected]);
  const savedRef = useRef<Record<string, DriverBoardEntry>>({});
  const changesRef = useRef(changes);
  const undoStack = useRef<{ id: number; driverId: string; group: number }[]>([]);
  const draftUndo = useRef<{ id: string; field: keyof DriverBoardEntry; before?: DriverBoardEntry; group: number }[]>([]);
  const editSession = useRef<{ id: string; field: keyof DriverBoardEntry; group: number } | null>(null);
  const editSequence = useRef(0);
  const pendingUndo = useRef(false);
  const undoLatestRef = useRef<() => Promise<void>>(async () => {});
  const dirty = canEdit && Object.keys(changes).length > 0;
  const { pendingLink, cancelNavigation, leave } = useBoardNavigation(dirty || saving || orderSaving);
  const saved = useMemo(() => Object.fromEntries((board?.entries ?? []).map(e => [e.driverId, e])), [board]);
  useLayoutEffect(() => { savedRef.current = saved; }, [saved]);
  useLayoutEffect(() => {
    idle.current = !loading && !eldRefreshing;
    lockedDriver.current = lockedDriverId;
    resumeLive.current();
  }, [dirty, saving, loading, eldRefreshing, lockedDriverId]);

  useEffect(() => {
    let cancelled = false;
    fetchDriverBoard(currentChargeWeek()).then(value => { if (!cancelled) setBoard(value); })
      .catch(err => { if (!cancelled) setError(err instanceof Error ? err.message : "Unable to load Status Board"); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, []);

  useEffect(() => {
    let cancelled = false, inFlight = false, pending = false;
    let latest: DriverBoard | undefined;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let blurTimer: ReturnType<typeof setTimeout> | undefined;
    let revision = 0;
    const accept = (value: DriverBoard) => {
      if (cancelled) return;
      latest = value;
      if (!idle.current || document.hidden) return;
      setBoard(current => {
        if (!current) return value;
        const retained = new Set([...Object.keys(changesRef.current), ...busyDrivers.current]);
        const focused = document.activeElement?.matches('input,textarea,select') ? selectedBoardCell(document.activeElement) : null;
        if (focused) retained.add(focused.driverId);
        if (lockedDriver.current) retained.add(lockedDriver.current);
        for (const entry of value.entries) {
          if ((savedRef.current[entry.driverId]?.version ?? 0) > entry.version) retained.add(entry.driverId);
        }
        const previous = Object.fromEntries(current.entries.map(e => [e.driverId, e]));
        const entries = value.entries.map(e => retained.has(e.driverId) ? previous[e.driverId] ?? e : e);
        const loads = { ...value.loads };
        for (const id of retained) if (current.loads[id]) loads[id] = current.loads[id];
        const incomingVersions = new Map(value.entries.map(e => [e.driverId, e.version]));
        const newerCommits = current.entries.filter(e => e.version > (incomingVersions.get(e.driverId) ?? 0));
        return { ...value, entries, loads, drivers: applyCommittedFleetStatus(value.drivers, newerCommits) };
      });
      setRefreshError("");
    };
    const refresh = async () => {
      if (!idle.current || document.visibilityState !== "visible" || inFlight) return;
      pending = false;
      inFlight = true;
      const started = revision;
      try {
        const value = await fetchDriverBoard(currentChargeWeek());
        if (started === revision) accept(value);
      } catch { if (!cancelled) setRefreshError("Live refresh is unavailable. Retry to update totals and assignments."); }
      finally { inFlight = false; if (pending && !cancelled) schedule(); }
    };
    const schedule = () => { pending = true; clearTimeout(timer); timer = setTimeout(() => void refresh(), 0); };
    refreshWanted.current = value => { if (value) { revision++; accept(value); } else schedule(); };
    resumeLive.current = () => { if (latest) accept(latest); };
    const fallback = setInterval(() => { if (!connectedRef.current) schedule(); }, 30000);
    const focus = () => schedule();
    window.addEventListener("focus", focus); document.addEventListener("visibilitychange", focus);
    // Release the focused row from the latest stream snapshot without fetching
    // the whole board every time a dispatcher moves to another input.
    const blur = () => { clearTimeout(blurTimer); blurTimer = setTimeout(() => { if (latest) accept(latest); }, 0); };
    document.addEventListener("focusout", blur);
    return () => { cancelled = true; refreshWanted.current = () => {}; resumeLive.current = () => {}; clearTimeout(timer); clearTimeout(blurTimer); clearInterval(fallback); window.removeEventListener("focus", focus); document.removeEventListener("visibilitychange", focus); document.removeEventListener("focusout", blur); };
  }, []);

  const edit = useCallback((id: string, field: keyof DriverBoardEntry, value: string, patch?: Partial<DriverBoardEntry>) => {
    if (!canEdit) return;
    activity.current += 1;
    if (!editSession.current || editSession.current.id !== id || editSession.current.field !== field) editSession.current = { id, field, group: ++editSequence.current };
    const last = draftUndo.current.at(-1);
    if (!last || last.group !== editSession.current.group) draftUndo.current.push({ id, field, before: changesRef.current[id], group: editSession.current.group });
    const current = changesRef.current;
    const baseline = savedRef.current[id];
    const next = { ...(current[id] ?? baseline), [field]: value, ...patch };
    if (field === "trailerNumber" && value !== (current[id] ?? baseline).trailerNumber) next.trailerNote = "";
    if (field === "currentLoad") { next.destination = ""; next.eta = ""; next.resolveCurrentLoad = true; next.statusEdited = false; next.status = value.trim() ? "DISPATCHED" : "READY"; }
    if (field === "status") next.statusEdited = true;
    const result = { ...current, [id]: next };
    // While saving, a revert must still be sent after the first write finishes.
    if (!savingRef.current && JSON.stringify(next) === JSON.stringify(baseline)) delete result[id];
    changesRef.current = result;
    setChanges(result);
  }, [canEdit]);

  async function setAutoOrder(dispatcherId: string, enabled: boolean) {
    if (!canEdit || loading || savingRef.current || orderSavingRef.current || dirty || pendingLink || actionBusy) throw new Error("Finish the current save before changing Auto-order.");
    orderSavingRef.current = true;
    setOrderSaving(true);
    try {
      await updateBoardAutoOrder(dispatcherId, enabled);
      try {
        refreshWanted.current(await fetchDriverBoard(currentChargeWeek()));
      } catch {
        refreshWanted.current();
        throw new Error("Auto-order was saved. Reload the board to see the current order.");
      }
    } finally { orderSavingRef.current = false; setOrderSaving(false); }
  }

  async function reorderDrivers(driverId: string, targetDriverId: string, after: boolean, revision: string, destination?: string) {
    if (!canEdit || loading || savingRef.current || orderSavingRef.current || dirty || pendingLink || actionBusy) throw new Error("Finish the current save before moving a driver.");
    const driver = board?.drivers.find(d => d.id === driverId);
    const target = board?.drivers.find(d => d.id === targetDriverId);
    const targetDispatcherId = destination ?? target?.dispatcherId;
    if (!driver || targetDispatcherId === undefined || driverId === targetDriverId) throw new Error("The driver list changed. Reload the board and try again.");
    const transfer = driver.dispatcherId !== targetDispatcherId;
    const autoOrder = board?.autoOrderDispatchers?.[targetDispatcherId];
    if (!transfer && autoOrder) throw new Error("Turn off Auto-order before reordering within this group.");
    const anchor = autoOrder ? "" : targetDriverId;
    orderSavingRef.current = true;
    setOrderSaving(true);
    try {
      const result = await reorderBoardDrivers({ dispatcherId: driver.dispatcherId, driverId, targetDriverId: anchor, after, revision,
        ...(transfer ? { targetDispatcherId, targetRevision: board?.driverOrderRevisions[targetDispatcherId] } : {}) });
      if (transfer) {
        setBoard(current => {
          // A newer SSE snapshot wins over this response. Otherwise apply the
          // committed placement immediately, without a second blocking request.
          if (!current || current.driverOrderRevisions[driver.dispatcherId] !== revision ||
              current.driverOrderRevisions[targetDispatcherId] !== board?.driverOrderRevisions[targetDispatcherId]) return current;
          const moved = transferDriverRows(current.drivers, driverId, targetDispatcherId,
            anchor, after, current.autoOrderGroupNames?.[targetDispatcherId]);
          return { ...current,
            drivers: orderDriverRows(moved, targetDispatcherId, result.driverIds),
            dispatcherOrderRevision: "",
            driverOrderRevisions: { ...current.driverOrderRevisions,
              [driver.dispatcherId]: result.sourceRevision ?? "", [targetDispatcherId]: result.revision },
          };
        });
        refreshWanted.current();
        return;
      }
      setBoard(current => {
        if (!current || current.driverOrderRevisions?.[result.dispatcherId] !== revision) return current;
        return { ...current, drivers: orderDriverRows(current.drivers, result.dispatcherId, result.driverIds), driverOrderRevisions: { ...current.driverOrderRevisions, [result.dispatcherId]: result.revision } };
      });
    } finally { orderSavingRef.current = false; setOrderSaving(false); }
  }

  const refreshLoads = useCallback(async (ids: string[]) => {
    try {
      const versions = new Map(ids.map(id => [id, savedRef.current[id]?.version]));
      const rows = await Promise.all(ids.map(async id => [id, await fetchBoardLoads(id)] as const));
      setBoard(current => current ? { ...current, loads: { ...current.loads, ...Object.fromEntries(rows.filter(([id]) =>
        savedRef.current[id]?.version === versions.get(id) && !changesRef.current[id]
      )) } } : current);
    } catch { setRefreshError("Load details could not be refreshed. Retry to review the saved plans."); }
  }, []);

  const save = useCallback(async () => {
    if (!canEdit || savingRef.current || !Object.keys(changesRef.current).length || loading) return;
    savingRef.current = true; activity.current += 1;
    const snapshot = { ...changesRef.current };
    busyDrivers.current = Object.keys(snapshot);
    const draftActions = draftUndo.current;
    draftUndo.current = [];
    setSaving(true); setError("");
    try {
      const committed = await saveDriverBoard(Object.values(snapshot));
      const loadIds = committed.filter(e => { const base = savedRef.current[e.driverId]; return !base || base.currentLoad !== e.currentLoad || base.status !== e.status || base.destination !== e.destination || base.eta !== e.eta || snapshot[e.driverId]?.resolveCurrentLoad; }).map(e => e.driverId);
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
        const loads = { ...current.loads };
        for (const entry of committed) {
          const view = loads[entry.driverId];
          if (view && entries[entry.driverId]?.destination !== entry.destination) {
            loads[entry.driverId] = { ...view, automation: entry.status === "READY" ? view.automation : undefined, destinationSource: entry.status !== "READY" && !entry.destination.trim() && !!view.stopKey };
          }
        }
        committed.forEach(e => { entries[e.driverId] = e; });
        return { ...current, loads, entries: Object.values(entries), drivers: applyCommittedFleetStatus(current.drivers, committed) };
      });
      if (loadIds.length) void refreshLoads(loadIds);
    } catch (err) { draftUndo.current = [...draftActions, ...draftUndo.current]; setError(err instanceof Error ? err.message : "Unable to save. Your edits are still here."); }
    finally { activity.current += 1; busyDrivers.current = []; savingRef.current = false; setSaving(false); }
  }, [canEdit, loading, refreshLoads]);

  useEffect(() => {
    if (!dirty || saving || error || loading) return;
    let cancelled = false;
    queueMicrotask(() => { if (!cancelled) void save(); });
    return () => { cancelled = true; };
  }, [dirty, saving, error, loading, save, pendingLink]);

  async function reload() {
    if (savingRef.current) return;
    if (dirty && !window.confirm("Discard unsaved changes and reload the saved Status Board?")) return;
    activity.current += 1; idle.current = false;
    setLoading(true); setError(""); cancelNavigation(); changesRef.current = {}; setChanges({}); draftUndo.current = []; undoStack.current = [];
    try { setBoard(await fetchDriverBoard(currentChargeWeek())); setRefreshError(""); }
    catch (err) { setError(err instanceof Error ? err.message : "Unable to load Status Board"); }
    finally { setLoading(false); }
  }

  async function refreshELD() {
    if (!canEdit) return;
    if (dirty || savingRef.current || loading || eldRefreshing) return;
    activity.current += 1; idle.current = false; setELDRefreshing(true); setRefreshError("");
    try {
      await syncFiveELD();
      setBoard(await fetchDriverBoard(currentChargeWeek()));
    } catch (err) {
      setRefreshError(err instanceof Error ? err.message : "Telematics locations could not be refreshed.");
    } finally {
      activity.current += 1; setELDRefreshing(false);
    }
  }

  async function undo(eventId: number, driverId: string, personal = false) {
    if (!canEdit) return;
    if (dirty || savingRef.current || loading) throw new Error("Wait for your board changes to save first.");
    const entry = savedRef.current[driverId];
    if (!entry) throw new Error("This driver is no longer on the active board.");
    savingRef.current = true; idle.current = false; activity.current += 1; setSaving(true); setActionBusy(true);
    busyDrivers.current = [driverId];
    try {
      const committed = await undoDriverBoardEvent(eventId, entry, personal);
      savedRef.current[driverId] = committed;
      undoStack.current = undoStack.current.filter(e => e.id !== eventId);
      setBoard(current => current ? { ...current, entries: current.entries.map(e => e.driverId === driverId ? committed : e), drivers: applyCommittedFleetStatus(current.drivers, [committed]) } : current);
      await refreshLoads([driverId]);
    } finally { activity.current += 1; busyDrivers.current = []; savingRef.current = false; setSaving(false); setActionBusy(false); }
  }

  async function changeLoads(driverId: string, view: BoardLoads, action: BoardLoadAction) {
    if (!canEdit) return view;
    if (dirty || savingRef.current || loading) throw new Error("Wait for your board changes to save first.");
    const entry = savedRef.current[driverId];
    if (!entry) throw new Error("This driver is no longer on the active board.");
    savingRef.current = true; idle.current = false; activity.current += 1; setSaving(true); setActionBusy(true);
    busyDrivers.current = [driverId];
    try {
      const result = await changeBoardLoads(entry, view, action);
      editSession.current = null;
      if (result.entry.undoId) undoStack.current.push({ id: result.entry.undoId, driverId, group: ++editSequence.current });
      savedRef.current[driverId] = result.entry;
      setBoard(current => current ? { ...current, entries: current.entries.map(e => e.driverId === driverId ? result.entry : e), drivers: applyCommittedFleetStatus(current.drivers, [result.entry]), loads: { ...current.loads, [driverId]: result.loads } } : current);
      return result.loads;
    } finally { activity.current += 1; busyDrivers.current = []; savingRef.current = false; setSaving(false); setActionBusy(false); }
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

  const entries = useMemo(() => Object.fromEntries(Object.entries({ ...saved, ...changes }).map(([id, entry]) => [
    id, { ...entry, status: board?.statusSyncEnabled ? entry.status : progressStatus(entry.status, board?.loads[id]) },
  ])), [saved, changes, board?.statusSyncEnabled, board?.loads]);
  return { board, entries, loading, saving: saving || orderSaving, actionBusy: actionBusy || orderSaving, dirty, error, refreshError, eldRefreshing, edit, save, reload, refresh: () => refreshWanted.current(), refreshELD, undo, changeLoads, reorderDrivers, setAutoOrder, ...live, leaving: !!pendingLink, cancelNavigation, leave };
}
