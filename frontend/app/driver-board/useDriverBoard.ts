"use client";

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { fetchDriverBoard, saveDriverBoard, undoDriverBoardEvent, fetchBoardLoads, changeBoardLoads } from "@/app/lib/api";
import type { DriverBoard, DriverBoardEntry, BoardLoads, BoardLoadAction } from "@/app/lib/types";
import { currentChargeWeek } from "@/app/accounting/driver-charges/charges";
import { reconcileDriverBoard } from "./board";

export function useDriverBoard(pauseRefresh = false) {
  const router = useRouter();
  const [board, setBoard] = useState<DriverBoard | null>(null);
  const [changes, setChanges] = useState<Record<string, DriverBoardEntry>>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [refreshError, setRefreshError] = useState("");
  const [pendingLink, setPendingLink] = useState<string | null>(null);
  const activity = useRef(0);
  const savingRef = useRef(false);
  const idle = useRef(false);
  const savedRef = useRef<Record<string, DriverBoardEntry>>({});
  const dirty = Object.keys(changes).length > 0;
  const saved = useMemo(() => Object.fromEntries((board?.entries ?? []).map(e => [e.driverId, e])), [board]);
  useLayoutEffect(() => { savedRef.current = saved; idle.current = !dirty && !saving && !loading && !pauseRefresh; }, [saved, dirty, saving, loading, pauseRefresh]);

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
    setChanges(current => {
      const baseline = savedRef.current[id];
      const next = { ...(current[id] ?? baseline), [field]: value };
      const result = { ...current, [id]: next };
      // While saving, a revert must still be sent after the first write finishes.
      if (!savingRef.current && JSON.stringify(next) === JSON.stringify(baseline)) delete result[id];
      return result;
    });
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
    setSaving(true); setError("");
    try {
      const committed = await saveDriverBoard(Object.values(snapshot));
      setBoard(current => {
        if (!current) return current;
        const entries = Object.fromEntries(current.entries.map(e => [e.driverId, e]));
        committed.forEach(e => { entries[e.driverId] = e; });
        return { ...current, entries: Object.values(entries) };
      });
      setChanges(current => reconcileDriverBoard(current, snapshot, committed));
      await refreshLoads(committed.map(e => e.driverId));
    } catch (err) { setError(err instanceof Error ? err.message : "Unable to save. Your edits are still here."); }
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
    setLoading(true); setError(""); setPendingLink(null); setChanges({});
    try { setBoard(await fetchDriverBoard(currentChargeWeek())); setRefreshError(""); }
    catch (err) { setError(err instanceof Error ? err.message : "Unable to load Status Board"); }
    finally { setLoading(false); }
  }

  async function undo(eventId: number, driverId: string) {
    if (dirty || savingRef.current || loading) throw new Error("Wait for your board changes to save first.");
    const entry = savedRef.current[driverId];
    if (!entry) throw new Error("This driver is no longer on the active board.");
    savingRef.current = true; idle.current = false; activity.current += 1; setSaving(true);
    try {
      const committed = await undoDriverBoardEvent(eventId, entry);
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
      setBoard(current => current ? { ...current, entries: current.entries.map(e => e.driverId === driverId ? result.entry : e), loads: { ...current.loads, [driverId]: result.loads } } : current);
      return result.loads;
    } finally { activity.current += 1; savingRef.current = false; setSaving(false); }
  }

  return { board, entries: { ...saved, ...changes }, loading, saving, dirty, error, refreshError, edit, save, reload, undo, changeLoads, leaving: !!pendingLink };
}
