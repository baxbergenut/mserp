"use client";


import { CurrentUserIDContext, GrossBoardManagementContext, usePermissions } from "@/app/lib/access";
import { HeaderPinButton, PinnablePageHeader } from "@/app/components/PinnablePageHeader";
import { PageHeader } from "@/app/components/PageHeader";

import { useBackHref, useMarkBack, useViewState } from "@/app/lib/viewMemory";

import { IntentLink as Link } from "@/app/components/IntentLink";
import { useRouter, useSearchParams } from "next/navigation";
import { Fragment, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { Banknote, CalendarRange, ChevronLeft, ChevronRight, Gauge, Route, CloudCheck, Lock, LockOpen } from "lucide-react";
import { APIError, fetchGrossBoard, resetGrossBoardBalance, saveGrossBoard, setGrossBoardWeekLock } from "@/app/lib/api";
import { useGrossLive } from "./useGrossLive";
import { readGrossDrafts, writeGrossDrafts, restoreGrossDrafts, grossDraftFields, grossLocationKey, reconcileGrossPatches, refreshRejectedSources, type GrossDrafts } from "./drafts";
import { mergePlanningEntries } from "./planning";
import { mergeGrossLive } from "./liveMerge";
import { GrossPresence } from "./GrossPresence";
import { spreadsheetGridEvents } from "@/app/lib/spreadsheetGrid";
import type { GrossBoard, GrossBoardEntry, GrossBoardSaveIssue, GrossBoardPlanningEntry } from "@/app/lib/types";
import { SkeletonBar, WeeklyTableSkeleton } from "@/app/components/WeeklyTableSkeleton";
import { MetricCard } from "@/app/components/MetricCard";
import { ManagementSearch } from "@/app/components/management/ManagementUI";
import { BoardFilter } from "@/app/components/management/BoardFilter";
import { parseBoardDriverTarget, parseBoardLoadTarget } from "./loadLink";
import { currentChargeWeek } from "@/app/accounting/driver-charges/charges";
import { DaySummaryCell } from "./DaySummaryCell";
import { BalanceDetails } from "./BalanceDetails";
import { BoardScrollArea } from "./BoardScrollArea";
import { SaveIssues } from "./SaveIssues";
import { entryForDispatcher, dispatcherRows, indexBoardEntries, addDays, balanceLabel, decimalDisplay, emptyEntry, entryKey, incompleteRates, rateBalance, signedMoney, rpmDisplay, shortDate, totals, validDecimal } from "./board";

const weekdays = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
const columnWidths = [170, 65, 82, ...weekdays.map(() => 145), 112, 112, 112, 80, 80, 112];
const minimumBoardWidth = columnWidths.reduce((sum, width) => sum + width, 0);
// Match the sticky truck offset to the expanding driver column, including
// when a narrow viewport scrolls the table at its minimum readable width.
const truckColumnStyle = { left: `max(${columnWidths[0]}px, ${columnWidths[0] / minimumBoardWidth * 100}%)` };
const buttonClass = "inline-flex items-center justify-center gap-2 rounded-lg border border-zinc-700/70 px-3 py-2 text-xs text-zinc-300 hover:bg-zinc-800 focus-visible:outline-2 focus-visible:outline-blue-500 disabled:cursor-not-allowed disabled:opacity-40";

export default function GrossBoardPage() {
  const hasEditPermission = usePermissions().includes("board.write");
  const canManageWeek = useContext(GrossBoardManagementContext);
  const router = useRouter();
  const searchParams = useSearchParams();
  const [loadTarget, setLoadTarget] = useState(() => parseBoardLoadTarget(searchParams.toString()));
  const [driverTarget, setDriverTarget] = useState(() => parseBoardDriverTarget(searchParams.toString()));
  const [week, setWeek] = useState(() => loadTarget?.week ?? driverTarget?.week ?? currentChargeWeek());
  // Keep the old key so remembered single-dispatcher views become checked choices.
  const [dispatcher, setDispatcher] = useViewState<string[] | null | string>("page:dispatcher", null, loadTarget || driverTarget ? null : undefined);
  const selectedDispatchers = useMemo(() => typeof dispatcher === "string" ? dispatcher === "all" ? null : [dispatcher] : dispatcher, [dispatcher]);
  const [search, setSearch] = useViewState("page:search", "", loadTarget || driverTarget ? "" : undefined);
  const [headerPinned, setHeaderPinned] = useViewState("page:headerPinned", false);
  const previousHref = useBackHref();
  const markBack = useMarkBack();
  useEffect(() => {
    const load = parseBoardLoadTarget(searchParams.toString()), driver = parseBoardDriverTarget(searchParams.toString());
    const target = load ?? driver;
    const timer = setTimeout(() => { setLoadTarget(load); setDriverTarget(driver); if (target) { setWeek(target.week); setDispatcher(null); setSearch(""); } }, 0);
    return () => clearTimeout(timer);
  }, [setDispatcher, setSearch, setWeek, searchParams]);
  const [board, setBoard] = useState<GrossBoard | null>(null);
  const weekLocked = board?.weekStart === week && board.weekLock?.locked === true;
  const canEdit = hasEditPermission && board?.weekStart === week && !board.managedInSheets && !weekLocked;
  const [lockPending, setLockPending] = useState(false);
  const [lockError, setLockError] = useState<{ week: string; message: string } | null>(null);
  const planningSaved = useCallback((entries: GrossBoardPlanningEntry[]) => {
    setBoard(current => current && current.weekStart === week ? { ...current, planningEntries: mergePlanningEntries(current.planningEntries, entries) } : current);
  }, [week]);
  const planningByDay = useMemo(() => {
    const indexed = new Map<string, GrossBoardPlanningEntry[]>();
    for (const entry of board?.planningEntries ?? []) { const key = `${entry.driverId}:${entry.date}`; indexed.set(key, [...(indexed.get(key) ?? []), entry]); }
    return indexed;
  }, [board?.planningEntries]);
  const nextDayDestinations = useMemo(() => new Map((board?.nextDayDestinations ?? [])
    .map(entry => [`${entry.driverId}:${entry.date}`, entry.text])), [board?.nextDayDestinations]);
  const userId = useContext(CurrentUserIDContext);
  const draftKey = `gross-board-drafts:v1:${userId}:${week}`;
  const baselinesRef = useRef<GrossDrafts>({});
  const [changes, setChanges] = useState<Record<string, GrossBoardEntry>>({});
  const pendingLoadDays = useMemo(() => new Set(weekLocked ? [] : Object.values(changes).map(entry => `${entry.driverId}:${entry.date}`)), [changes, weekLocked]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [issues, setIssues] = useState<GrossBoardSaveIssue[]>([]);
  const [message, setMessage] = useState("");
  const [pendingWeek, setPendingWeek] = useState<string | null>(null);
  const [pendingLink, setPendingLink] = useState<string | null>(null);
  const payReturn = loadTarget?.fromPay ? (previousHref?.startsWith("/accounting/driver-pay") ? previousHref : `/accounting/driver-pay?weekStart=${loadTarget.week}`) : null;
  const returnToPay = useCallback(() => { if (payReturn) { markBack(payReturn); setPendingLink(payReturn); } }, [payReturn, markBack]);
  const [balanceDriver, setBalanceDriver] = useState<string | null>(null);
  const [refreshError, setRefreshError] = useState("");
  const activityRef = useRef(0);
  const idleRef = useRef(false);
  const savingRef = useRef(false);
  const leavingRef = useRef(false);
  useLayoutEffect(() => { leavingRef.current = !!(pendingWeek || pendingLink); }, [pendingWeek, pendingLink]);
  const savedRef = useRef<Record<string, GrossBoardEntry>>({});
  const changesRef = useRef(changes);
  const busyKeys = useRef<string[]>([]);
  const connectedRef = useRef(false);
  const setDrafts = useCallback((value: GrossDrafts) => {
    changesRef.current = value;
    setChanges(value);
    if (!writeGrossDrafts(draftKey, value, baselinesRef.current)) setRefreshError("This browser cannot keep a recovery copy. Keep this page open until Saved appears.");
  }, [draftKey]);
  const receiveLive = useRef<(value?: GrossBoard) => void>(() => {});
  const resumeLive = useRef<() => void>(() => {});
  const live = useGrossLive(week, useCallback((value?: GrossBoard) => receiveLive.current(value), []));
  useLayoutEffect(() => { connectedRef.current = live.connected; }, [live.connected]);
  const dirty = canEdit && Object.keys(changes).length > 0;
  const hasReadyEdits = useMemo(() => {
    const blocked = new Set(issues.flatMap(issue => issue.pending.map(grossLocationKey)));
    return Object.keys(changes).some(key => !blocked.has(key));
  }, [changes, issues]);
  useLayoutEffect(() => { idleRef.current = !loading; resumeLive.current(); }, [dirty, saving, loading]);
  const dates = useMemo(() => weekdays.map((_, index) => addDays(week, index)), [week]);

  const load = useCallback(async () => {
    setLoading(true); setError("");
    try {
      const value = await fetchGrossBoard(week);
      setBoard(current => mergeGrossLive(current, value, new Set(), new Set()));
    }
    catch (err) { setBoard(null); setError(err instanceof Error ? err.message : "Unable to load board"); }
    finally { setLoading(false); }
  }, [week]);

  useEffect(() => {
    let cancelled = false;
    fetchGrossBoard(week).then((value) => {
      if (cancelled) return;
      const recovery = hasEditPermission ? readGrossDrafts(draftKey) : null;
      const restored = recovery ? restoreGrossDrafts(recovery.changes, recovery.baselines, value.entries) : null;
      baselinesRef.current = restored?.baselines ?? {};
      if (restored) setDrafts(restored.changes);
      else { changesRef.current = {}; setChanges({}); }
      if (restored && Object.keys(restored.changes).length) setMessage("Recovered unsaved edits");
      setBoard(current => mergeGrossLive(current, value, new Set(Object.keys(changesRef.current)), new Set()));
    })
      .catch((err) => { if (!cancelled) setError(err instanceof Error ? err.message : "Unable to load board"); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [week, draftKey, hasEditPermission, setDrafts]);

  // Pushes apply directly while pending field edits and open dialogs stay put;
  // GET requests remain available for recovery and explicit reloads.
  useEffect(() => {
    let cancelled = false;
    let inFlight = false;
    let pending = false;
    let latest: GrossBoard | undefined;
    let revision = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const accept = (value: GrossBoard) => {
      if (cancelled || value.weekStart !== week) return;
      latest = value;
      if (!idleRef.current) return;
      setBoard(current => {
        const keys = new Set([...Object.keys(changesRef.current), ...busyKeys.current]);
        const days = new Set(Array.from(document.querySelectorAll<HTMLElement>('[data-gross-editor-day]')).map(e => e.dataset.grossEditorDay!));
        return mergeGrossLive(current,value,keys,days);
      });
      setRefreshError("");
    };
    const refresh = async () => {
      if (!idleRef.current || document.visibilityState !== "visible" || inFlight) return;
      pending = false;
      inFlight = true;
      const started = revision;
      try {
        const value = await fetchGrossBoard(week);
        if (started === revision) accept(value);
      } catch {
        if (!cancelled) setRefreshError("Automatic load refresh is unavailable. Your edits still autosave; retry to reconnect.");
      } finally { inFlight = false; if (pending && !cancelled) schedule(); }
    };
    const schedule = () => { pending = true; clearTimeout(timer); timer = setTimeout(() => void refresh(), 0); };
    receiveLive.current = value => { if (value) { revision++; accept(value); } else schedule(); };
    resumeLive.current = () => { if (latest) accept(latest); };
    const fallback = setInterval(() => { if (!connectedRef.current) schedule(); }, 30000);
    const focus = schedule;
    window.addEventListener("focus", focus);
    document.addEventListener("visibilitychange", focus);
    const blur = () => { clearTimeout(timer); timer = setTimeout(() => { if (latest) accept(latest); }, 0); };
    document.addEventListener("focusout", blur);
    return () => { cancelled = true; receiveLive.current = () => {}; resumeLive.current = () => {}; clearTimeout(timer); clearInterval(fallback); window.removeEventListener("focus", focus); document.removeEventListener("visibilitychange", focus); document.removeEventListener("focusout", blur); };
  }, [week]);

  useEffect(() => {
    if (!dirty) return;
    const beforeUnload = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ""; };
    const leave = (event: MouseEvent) => {
      const link = (event.target as Element).closest?.("a[href]");
      if (link && !event.ctrlKey && !event.metaKey && !event.shiftKey) {
        event.preventDefault(); event.stopPropagation(); setPendingLink(link.getAttribute("href"));
      }
    };
    window.addEventListener("beforeunload", beforeUnload);
    document.addEventListener("click", leave, true);
    return () => { window.removeEventListener("beforeunload", beforeUnload); document.removeEventListener("click", leave, true); };
  }, [dirty]);

  const saved = useMemo(() => Object.fromEntries((board?.entries ?? []).map((entry) => [entryKey(entry.driverId, entry.date, entry.slot), entry])), [board]);
  useLayoutEffect(() => { savedRef.current = saved; }, [saved]);
  const allEntries = useMemo(() => weekLocked ? saved : { ...saved, ...changes }, [saved, changes, weekLocked]);
  const entryIndex = useMemo(() => indexBoardEntries(Object.values(allEntries)), [allEntries]);
  const rows = useMemo(() => dispatcherRows(board?.drivers ?? [], dates, board?.entries ?? []), [board, dates]);
  const drivers = useMemo(() => {
    const query = search.trim().toLowerCase();
    return rows.filter(driver => (!driverTarget || driver.id === driverTarget.driverId)
      && (selectedDispatchers === null || selectedDispatchers.includes(driver.dispatcherId))
      && (!query || driver.fullName.toLowerCase().includes(query)
        || (entryIndex.byDriver.get(driver.id) ?? []).some(entry => !entry.deleted && !entry.dayStatus
          && driver.dates.includes(entry.date) && entryForDispatcher(entry, driver.dispatcherId, driver)
          && entry.loadNumber.toLowerCase().includes(query))));
  }, [rows, selectedDispatchers, search, driverTarget, entryIndex]);
  const dispatchers = useMemo(() => Array.from(new Map(rows.map((driver) => [driver.dispatcherId, driver.dispatcherName || "Unassigned"])).entries()), [rows]);
  const shownEntries = useMemo(() => {
    return Object.values(allEntries).filter(entry => drivers.some(driver => driver.id === entry.driverId && driver.dates.includes(entry.date) && entryForDispatcher(entry,driver.dispatcherId,driver)));
  }, [allEntries, drivers]);
  const targetEntry = loadTarget && allEntries[entryKey(loadTarget.driverId, loadTarget.date, loadTarget.slot)];
  const targetMatches = !!targetEntry && !targetEntry.deleted && !targetEntry.dayStatus
    && targetEntry.loadNumber.trim().toLowerCase() === loadTarget?.loadNumber.trim().toLowerCase();
  const targetReady = loadTarget && board?.weekStart === loadTarget.week && week === loadTarget.week;
  const summary = totals(shownEntries);
  const openings = new Map((board?.balances ?? []).map((balance) => [balance.driverId, balance]));
  const selectedDriver = drivers.find((driver) => driver.id === balanceDriver);
  const uniqueDrivers = Array.from(new Map(drivers.map(driver => [driver.id, driver])).values());
  const balanceOwners = new Map([...drivers].sort((a, b) => (a.dates.at(-1) ?? "").localeCompare(b.dates.at(-1) ?? "")).map(driver => [driver.id, driver.dispatcherId]));
  const endingBalances = uniqueDrivers.map((driver) => rateBalance(openings.get(driver.id)?.openingBalance ?? "0",
    entryIndex.byDriver.get(driver.id) ?? []));
  const balanceTotal = endingBalances.reduce((sum, value) => sum + value, BigInt(0));
  const uncoveredTotal = endingBalances.filter((value) => value < BigInt(0)).reduce((sum, value) => sum - value, BigInt(0));
  const dispatcherTotals = new Map<string, { original: bigint; driver: bigint; miles: bigint; balance: bigint }>();
  for (const driver of drivers) {
    const entries = driver.dates.flatMap(date => (entryIndex.byDay.get(`${driver.id}:${date}`) ?? []).filter(entry => entryForDispatcher(entry,driver.dispatcherId,driver)));
    const subtotal = totals(entries);
    const opening = openings.get(driver.id);
    const group = dispatcherTotals.get(driver.dispatcherId) ?? { original: BigInt(0), driver: BigInt(0), miles: BigInt(0), balance: BigInt(0) };
    group.original += subtotal.original; group.driver += subtotal.driver; group.miles += subtotal.miles;
    if (balanceOwners.get(driver.id) === driver.dispatcherId) group.balance += rateBalance(opening?.openingBalance ?? "0", entryIndex.byDriver.get(driver.id) ?? []);
    dispatcherTotals.set(driver.dispatcherId, group);
  }
  const invalid = Object.values(changes).some((entry) => !validDecimal(entry.originalRate) || !validDecimal(entry.driverRate) || !validDecimal(entry.miles, true));

  function switchWeek(next: string) {
    if (next === week || loading) return;
    activityRef.current += 1;
    setBalanceDriver(null); setLoadTarget(null);
    setPendingWeek(next);
  }

  useEffect(() => {
    if (dirty || saving || loading || (error && !weekLocked) || (!pendingWeek && !pendingLink)) return;
    if (pendingLink) {
      if (pendingLink.startsWith("/") && !pendingLink.startsWith("//")) router.push(pendingLink, { scroll: false });
      else window.location.assign(pendingLink);
      return;
    }
    if (pendingWeek) {
      const timer = setTimeout(() => {
        setBoard(null); setMessage(""); setError(""); setIssues([]); setLockError(null); setLoading(true); setWeek(pendingWeek); setPendingWeek(null);
      }, 0);
      return () => clearTimeout(timer);
    }
  }, [pendingWeek, pendingLink, dirty, saving, loading, error, weekLocked, setWeek, router]);

  const edit = useCallback((driverId: string, date: string, slot: number, update: (entry: GrossBoardEntry) => GrossBoardEntry) => {
    if (!canEdit) return;
    activityRef.current += 1;
    const key = entryKey(driverId, date, slot);
    const current = changesRef.current;
    const baseline = savedRef.current[key] ?? emptyEntry(driverId, date, slot);
    const previous = current[key] ?? baseline;
    const next = update(previous);
    if (next === previous) return;
    if (!current[key]) baselinesRef.current[key] = baseline;
    const result = { ...current, [key]: next };
    // A revert during an in-flight save is a new edit; the old saved value
    // will be replaced by that response, so it must remain queued.
    if (!(slot > 0 && baseline.version === 0) && !savingRef.current && JSON.stringify(next) === JSON.stringify(baseline)) delete result[key];
    setDrafts(result);
    setIssues(currentIssues => currentIssues.filter(issue => !issue.pending.some(location => grossLocationKey(location) === key)));
    setMessage("");
  }, [canEdit, setDrafts]);

  const save = useCallback(async () => {
    if (!canEdit || savingRef.current || invalid || !Object.keys(changesRef.current).length || loading) return;
    savingRef.current = true;
    activityRef.current += 1;
    const saveRevision = activityRef.current;
    const snapshot = { ...changesRef.current };
    busyKeys.current = Object.keys(snapshot);
    setSaving(true); setError(""); setMessage("");
    try {
      const { entries: committed, issues: rejected } = await saveGrossBoard(week, Object.entries(snapshot).map(([key, entry]) => ({
        ...entry, fields: grossDraftFields(entry, baselinesRef.current[key]), previousLoadNumber: baselinesRef.current[key]?.loadNumber,
      })));
      committed.forEach(entry => { savedRef.current[entryKey(entry.driverId,entry.date,entry.slot)] = entry; });
      const remaining = reconcileGrossPatches(changesRef.current,snapshot,committed);
      // New typing may already have fixed a rejected group while it was in flight.
      // Include blocked drafts again when new edits arrive so a later half of a
      // move can still be saved atomically with the first half.
      setIssues(rejected.filter(issue => issue.pending.every(location => {
        const key = grossLocationKey(location);
        return snapshot[key] === changesRef.current[key];
      })));
      refreshRejectedSources(remaining, snapshot, rejected);
      for (const entry of committed) baselinesRef.current[entryKey(entry.driverId, entry.date, entry.slot)] = entry;
      changesRef.current = remaining;
      setBoard((current) => {
        if (!current) return current;
        const entries = Object.fromEntries(current.entries.map((entry) => [entryKey(entry.driverId, entry.date, entry.slot), entry]));
        committed.forEach((entry) => { entries[entryKey(entry.driverId, entry.date, entry.slot)] = entry; });
        return { ...current, entries: Object.values(entries) };
      });
      setDrafts(remaining);
      setMessage("Saved");
      // The stream refreshes carry and matching metadata. Recover by GET when
      // disconnected, preserving newer edits and navigation flushes.
      if (committed.length > 0 && !live.connected && activityRef.current === saveRevision && !leavingRef.current) {
        try {
          const fresh = await fetchGrossBoard(week);
          if (activityRef.current === saveRevision) receiveLive.current(fresh);
        } catch { setRefreshError("Saved. Automatic load refresh is unavailable; retry to reconnect."); }
      }
    } catch (err) {
      if (err instanceof APIError && err.code === "gross_board_week_locked") {
        try {
          const current = await fetchGrossBoard(week);
          receiveLive.current(current);
          if (!current.weekLock?.locked) setError(err.message);
        }
        catch { setError("Unable to refresh the week lock. Your edits are still here."); }
      } else setError(err instanceof Error ? err.message : "Unable to save. Your edits are still here.");
    }
    finally { activityRef.current += 1; busyKeys.current = []; savingRef.current = false; setSaving(false); }
  }, [canEdit, live.connected, invalid, loading, week, setDrafts]);

  useEffect(() => {
    if (!dirty || !hasReadyEdits || saving || error || invalid || loading) return;
    const timer = setTimeout(() => { void save(); }, pendingWeek || pendingLink ? 0 : 300);
    return () => clearTimeout(timer);
  }, [changes, dirty, hasReadyEdits, saving, error, invalid, loading, save, pendingWeek, pendingLink]);

  async function reload() {
    if (weekLocked) { receiveLive.current(); return; }
    if (dirty && !window.confirm("Discard unsaved changes and reload the saved board?")) return;
    activityRef.current += 1;
    setIssues([]);
    baselinesRef.current = {}; setDrafts({}); setMessage(""); setRefreshError(""); setPendingWeek(null); setPendingLink(null); await load();
  }

  async function toggleWeekLock() {
    if (!canManageWeek || lockPending || loading || board?.weekStart !== week) return;
    setLockPending(true);
    setLockError(null);
    try {
      const state = await setGrossBoardWeekLock(week, !weekLocked);
      setBoard(current => current?.weekStart === week && state.version >= (current.weekLock?.version ?? 0)
        ? { ...current, weekLock: state } : current);
    } catch (err) {
      setLockError({ week, message: err instanceof Error ? err.message : "Unable to update the week lock" });
    } finally {
      setLockPending(false);
    }
  }

  function discardIssue(issue: GrossBoardSaveIssue) {
    if (savingRef.current || !window.confirm("Discard the unsaved edits for this issue?")) return;
    activityRef.current += 1;
    const remaining = { ...changesRef.current };
    for (const location of issue.pending) {
      const key = grossLocationKey(location);
      delete remaining[key];
      delete baselinesRef.current[key];
    }
    setDrafts(remaining);
    setIssues(currentIssues => currentIssues.filter(current => current !== issue));
    resumeLive.current();
    receiveLive.current();
  }

  return (
    <div className="space-y-5 animate-fade-in">
      <PageHeader><div>
          <div className="flex items-center gap-3">
            <CalendarRange className="h-5 w-5 text-zinc-500" />
            <h1 className="text-lg font-semibold text-zinc-100">Gross Board</h1>
            <span className="rounded-full bg-zinc-800/60 px-2.5 py-0.5 text-[12px] font-medium text-zinc-400">{loading ? <SkeletonBar className="h-3 w-4" /> : uniqueDrivers.length}</span>
          </div>

        </div></PageHeader>

      <PinnablePageHeader pinned={headerPinned}>
      <div className="grid gap-3 sm:grid-cols-3 xl:grid-cols-6">
        <MetricCard loading={loading} compact label="Total gross" value={loading || !board ? "—" : decimalDisplay(summary.original, true)} icon={Banknote} />
        <MetricCard loading={loading} compact label="Driver gross" value={loading || !board ? "-" : decimalDisplay(summary.driver, true)} icon={Banknote} />
        <MetricCard loading={loading} compact label="Total miles" value={loading || !board ? "—" : decimalDisplay(summary.miles)} icon={Route} />
        <MetricCard loading={loading} compact label="Total RPM" value={loading || !board ? "—" : rpmDisplay(summary.original, summary.miles)} icon={Gauge} />
        <MetricCard loading={loading} compact label="Driver RPM" value={loading || !board ? "-" : rpmDisplay(summary.driver, summary.miles)} icon={Gauge} />
        <MetricCard loading={loading} compact label="Rate balance" value={loading || !board ? "-" : signedMoney(balanceTotal)} icon={Banknote} />
      </div>

      <div className="flex flex-wrap items-center gap-3">
      <HeaderPinButton pinned={headerPinned} onToggle={() => setHeaderPinned(value => !value)} />
        <div className="flex items-center gap-1 rounded-lg border border-zinc-800 bg-card p-1">
          <button aria-label="Previous week" className={buttonClass} disabled={loading || week <= "2000-01-03"} onClick={() => switchWeek(addDays(week, -7))}><ChevronLeft className="h-4 w-4" /></button>
          <span className="min-w-36 px-2 text-center font-mono text-sm text-zinc-100" title={`${week} to ${dates[6]}`}>{shortDate(week)} → {shortDate(dates[6])}</span>
          <button aria-label="Next week" className={buttonClass} disabled={loading || week >= "2100-12-27"} onClick={() => switchWeek(addDays(week, 7))}><ChevronRight className="h-4 w-4" /></button>
        </div>
        <span className="text-xs text-zinc-500">{week.slice(0, 4)}{week.slice(0, 4) !== dates[6].slice(0, 4) ? ` / ${dates[6].slice(0, 4)}` : ""}</span>
        <button className={buttonClass} disabled={loading} onClick={() => switchWeek(currentChargeWeek())}>This week</button>
        <div className="min-w-48 flex-1"><ManagementSearch value={search} onChange={setSearch} placeholder="Search drivers or load numbers…" /></div>
        {canManageWeek && <button type="button" aria-pressed={weekLocked} className={`${buttonClass} aria-pressed:border-blue-500/50 aria-pressed:text-blue-300`}
          disabled={lockPending || loading || board?.weekStart !== week} onClick={toggleWeekLock}>
          {weekLocked ? <Lock className="h-4 w-4" /> : <LockOpen className="h-4 w-4" />} Lock week
        </button>}
        {driverTarget && <button className={buttonClass} onClick={() => { setDriverTarget(null); router.replace("/gross-board", { scroll: false }); }}>All drivers</button>}
        <BoardFilter label="Dispatcher" options={dispatchers} selected={selectedDispatchers} onChange={setDispatcher} />

      <div className="ml-auto flex flex-wrap items-center gap-2"><div className="flex items-center gap-2">
          <span className="flex items-center gap-1.5 text-xs text-zinc-400" title={live.connected ? "Updates are live" : "Reconnecting to live updates"}><span className={`h-1.5 w-1.5 rounded-full ${live.connected ? "bg-emerald-400" : "bg-amber-400"}`} />{live.connected ? "Live" : "Connecting…"}</span>
          <span role="status" className={`flex items-center gap-1.5 text-xs ${(error || issues.length > 0) && dirty ? "text-red-300" : "text-zinc-400"}`}><CloudCheck className="h-4 w-4" />{weekLocked ? "Week locked" : board?.managedInSheets ? "Managed in Sheets · read only" : loading ? "Loading…" : !canEdit ? "Read only" : error && dirty ? "Not saved" : saving ? "Saving…" : issues.length > 0 ? "Edits need review" : dirty ? "Waiting to save…" : message || "Saved"}</span>
        </div></div></div>

    </PinnablePageHeader>

      {lockError?.week === week && <p role="alert" className="text-xs text-red-300">{lockError.message}</p>}
      {!weekLocked && error && <div role="alert" className="rounded-lg border border-red-500/20 bg-red-500/10 p-3 text-sm text-red-300">{error} {dirty && "Your unsaved edits are still on this page."}{dirty && <><button className={`${buttonClass} ml-3`} onClick={save} disabled={saving || invalid}>Retry save</button><button className={`${buttonClass} ml-3`} onClick={reload} disabled={saving}>Discard unsaved changes</button></>}{!board && <button className={`${buttonClass} ml-3`} onClick={load}>Retry</button>}</div>}
      {!weekLocked && !error && issues.length > 0 && <SaveIssues issues={issues} changes={changes} drivers={board?.drivers ?? []}
        saving={saving} onDiscard={discardIssue}
        actions={<><button className={buttonClass} onClick={save} disabled={saving || invalid}>Retry save</button><button className={buttonClass} onClick={reload} disabled={saving}>Discard unsaved changes</button></>} />}
      {(pendingWeek || pendingLink) && dirty && <p role="status" className="text-xs text-amber-300">{invalid || error || issues.length ? "Resolve the unsaved edits before leaving this week." : "Saving edits before leaving this week…"}<button className="ml-2 underline" onClick={() => { setPendingWeek(null); setPendingLink(null); }}>Stay here</button></p>}
      {dirty && invalid && <p role="alert" className="text-xs text-red-300">Correct the highlighted fields before saving. Use numbers with at most two decimal places; miles cannot be negative.</p>}

      {targetReady && !targetMatches && <p role="status" className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-200">Load {loadTarget.loadNumber} is no longer in this driver’s {loadTarget.date} slot {loadTarget.slot + 1}. It may have been changed or removed since payroll was recorded.</p>}
      {refreshError && <p role="status" className="text-xs text-amber-300">{refreshError}<button className={`${buttonClass} ml-2`} onClick={reload} disabled={loading || saving}>Retry</button></p>}
      {loading ? <WeeklyTableSkeleton board /> : board && drivers.length === 0 ? <div className="rounded-xl border border-zinc-800 p-12 text-center text-sm text-zinc-500">No drivers match the selected filters.</div> : board && <BoardScrollArea headerPinned={headerPinned}>
        <table {...spreadsheetGridEvents} data-spreadsheet-grid data-gross-board-grid className="w-full table-fixed border-separate border-spacing-0 text-center text-xs" style={{ minWidth: minimumBoardWidth }}>
          <colgroup>{columnWidths.map((width, index) => <col key={index} style={{ width: `${width / minimumBoardWidth * 100}%` }} />)}</colgroup>
          <thead className="sticky top-0 z-30 bg-zinc-900 text-zinc-300">
            <tr>
              <th className="sticky left-0 z-30 border-b border-r border-zinc-700 bg-zinc-900 px-3 py-3">Driver</th>
              <th className="sticky z-30 border-b border-r border-zinc-700 bg-zinc-900 px-2" style={truckColumnStyle}>Truck</th>
              <th className="border-b border-r border-zinc-700" aria-label="Field" />
              {dates.map((date, i) => <th key={date} className="border-b border-r border-zinc-700 py-2"><div className="font-medium">{weekdays[i]}</div><div className="mt-1 font-mono text-[11px] font-normal text-zinc-500">{shortDate(date)}</div></th>)}
              {["Total gross", "Driver gross", "Total miles", "Total RPM", "Driver RPM", "Rate balance"].map((title) => <th key={title} className="border-b border-r border-zinc-700 bg-blue-500/5 px-2 font-medium">{title}</th>)}
            </tr>
          </thead>
          <tbody>
            {drivers.map((driver, index) => {
              const dayEntries = dates.map(date => (entryIndex.byDay.get(`${driver.id}:${date}`) ?? [emptyEntry(driver.id, date)]).filter(entry => entryForDispatcher(entry,driver.dispatcherId,driver)));
              const entries = dayEntries.filter((_, i) => driver.dates.includes(dates[i])).flat();
              const sum = totals(entries);
              const driverEntries = entryIndex.byDriver.get(driver.id) ?? [];
              const balance = rateBalance(openings.get(driver.id)?.openingBalance ?? "0", driverEntries);
              const incomplete = (openings.get(driver.id)?.openingIncomplete ?? 0) + incompleteRates(driverEntries);

              const groupStart = index === 0 || drivers[index - 1].dispatcherId !== driver.dispatcherId;
              const group = dispatcherTotals.get(driver.dispatcherId)!;
              return <Fragment key={`${driver.id}:${driver.dispatcherId}`}>
                {groupStart && <tr className="bg-blue-500/10 font-sans text-xs font-medium leading-4" aria-label={`${driver.dispatcherName} totals`}>
                  <th colSpan={10} scope="rowgroup" className="border-b border-zinc-700 py-2 text-left font-medium text-blue-300"><span className="sticky left-3">{driver.dispatcherName}</span></th>
                  <td className="border-b border-r border-zinc-700 px-2 py-2 text-blue-200">{decimalDisplay(group.original, true)}</td>
                  <td className="border-b border-r border-zinc-700 px-2 py-2 text-zinc-200">{decimalDisplay(group.driver, true)}</td>
                  <td className="border-b border-r border-zinc-700 px-2 py-2 text-zinc-200">{decimalDisplay(group.miles)}</td>
                  <td className="border-b border-r border-zinc-700 px-2 py-2 text-zinc-200">{rpmDisplay(group.original, group.miles)}</td>
                  <td className="border-b border-r border-zinc-700 px-2 py-2 text-zinc-200">{rpmDisplay(group.driver, group.miles)}</td>
                  <td className={`border-b border-zinc-700 px-2 py-2 ${group.balance < BigInt(0) ? "text-red-300" : group.balance > BigInt(0) ? "text-emerald-300" : "text-zinc-400"}`}>{signedMoney(group.balance)}</td>
                </tr>}
                <tr>
                  <th data-gross-driver={driver.id} data-gross-field="driver" scope="row" className="sticky left-0 z-20 border-b border-r border-zinc-800 bg-zinc-950 px-3 font-medium text-zinc-200"><Link href={`/drivers/detail?id=${driver.id}`} className="underline-offset-2 hover:text-blue-300 hover:underline focus-visible:outline-2 focus-visible:outline-blue-500">{driver.fullName}</Link>{!driver.active && <div className="mt-1 text-[10px] text-zinc-500">Inactive</div>}</th>
                  <td data-gross-driver={driver.id} data-gross-field="truck" className="sticky z-20 border-b border-r border-zinc-800 bg-zinc-950 px-2 font-mono text-zinc-400" style={truckColumnStyle}>{driver.truckId && driver.truckUnit ? <Link href={`/trucks/detail?id=${driver.truckId}`} className="underline-offset-2 hover:text-blue-300 hover:underline focus-visible:outline-2 focus-visible:outline-blue-500">{driver.truckUnit}</Link> : driver.truckUnit || "—"}</td>
                  <td className="border-b border-r border-zinc-800 bg-zinc-900/60 p-0 align-top text-[10px] text-zinc-500"><div className="h-6 border-b border-zinc-800/70" />{["Load #", "Total gross", "Driver", "Miles"].map(field => <div key={field} className="flex h-8 items-center justify-center border-b border-zinc-800/70 px-2">{field}</div>)}</td>
                  {dayEntries.map((entries, i) => !driver.dates.includes(dates[i]) ? <td key={dates[i]} className="border-b border-r border-zinc-800 bg-zinc-900/40 px-2 text-center text-xs text-zinc-500"><span>{driver.assignments?.some(a => a.date === dates[i]) ? "Changed dispatcher" : "Not started"}</span><span className="mt-1 block text-[10px]">{driver.assignments?.find(a => a.date === dates[i])?.dispatcherName}</span></td> : <DaySummaryCell pendingLoadSave={pendingLoadDays.has(`${driver.id}:${dates[i]}`)} week={week} suggestedDestination={nextDayDestinations.get(`${driver.id}:${dates[i]}`)} planningEntries={planningByDay.get(`${driver.id}:${dates[i]}`) ?? []} onPlanningSaved={planningSaved} readOnly={!canEdit} weekLocked={weekLocked} onReturn={payReturn ? returnToPay : undefined} key={dates[i]} focusSlot={targetReady && targetMatches && loadTarget.driverId === driver.id && loadTarget.date === dates[i] ? loadTarget.slot : undefined} entries={entries} driverId={driver.id} driverName={driver.fullName} date={dates[i]} onChange={edit} />)}
                  <td data-gross-driver={driver.id} data-gross-field="originalTotal" className="border-b border-r border-zinc-800 bg-blue-500/[0.03] px-2 font-mono text-blue-200">{decimalDisplay(sum.original, true)}</td>
                  <td data-gross-driver={driver.id} data-gross-field="driverTotal" className="border-b border-r border-zinc-800 bg-blue-500/[0.03] px-2 font-mono text-zinc-300">{decimalDisplay(sum.driver, true)}</td>
                  <td data-gross-driver={driver.id} data-gross-field="milesTotal" className="border-b border-r border-zinc-800 bg-blue-500/[0.03] px-2 font-mono text-zinc-300">{decimalDisplay(sum.miles)}</td>
                  <td data-gross-driver={driver.id} data-gross-field="rpm" aria-label={`Total gross RPM for ${driver.fullName}`} className="border-b border-r border-zinc-800 bg-blue-500/[0.03] px-2 font-mono text-zinc-300">{rpmDisplay(sum.original, sum.miles)}</td>
                  <td data-gross-driver={driver.id} data-gross-field="driverRpm" aria-label={`Driver RPM for ${driver.fullName}`} className="border-b border-r border-zinc-800 bg-blue-500/[0.03] px-2 font-mono text-zinc-300">{rpmDisplay(sum.driver, sum.miles)}</td>
                  <td data-gross-driver={driver.id} data-gross-field="balance" className="border-b border-r border-zinc-800 bg-blue-500/[0.03] px-1">
                    <button aria-label={`Rate balance for ${driver.fullName}`} className={`w-full rounded py-3 hover:bg-zinc-800 focus-visible:outline-2 focus-visible:outline-blue-500 ${balance < BigInt(0) ? "text-red-300" : balance > BigInt(0) ? "text-emerald-300" : "text-zinc-400"}`} onClick={() => setBalanceDriver(driver.id)}>
                      <span className="font-mono">{signedMoney(balance)}</span><span className="mt-1 block text-[10px]">{balanceLabel(balance)}</span>
                      {incomplete > 0 && <span className="mt-1 block text-[10px] text-amber-300">{incomplete} incomplete</span>}
                      {entries.some((entry) => entry.duplicate) && <span className="mt-1 block text-[10px] text-amber-300">Repeated load</span>}
                    </button>
                  </td>
                </tr>
              </Fragment>;
            })}
          </tbody>
          <tfoot><tr className="bg-zinc-900 font-mono text-zinc-200"><th colSpan={10} className="border-t border-zinc-700 p-3 text-left"><span className="sticky left-3">Totals · balance through week end</span></th>
            <td className="border-t border-zinc-700 px-2 py-3">{decimalDisplay(summary.original, true)}</td>
            <td className="border-t border-zinc-700 px-2 py-3">{decimalDisplay(summary.driver, true)}</td>
            <td className="border-t border-zinc-700 px-2 py-3">{decimalDisplay(summary.miles)}</td>
            <td className="border-t border-zinc-700 px-2 py-3">{rpmDisplay(summary.original, summary.miles)}</td>
            <td className="border-t border-zinc-700 px-2 py-3">{rpmDisplay(summary.driver, summary.miles)}</td>
            <td className="border-t border-zinc-700 px-2 py-3">{signedMoney(balanceTotal)}{uncoveredTotal > BigInt(0) && <span className="mt-1 block text-[10px] text-red-300">{decimalDisplay(uncoveredTotal, true)} uncovered</span>}</td>
          </tr></tfoot>
        </table>
      </BoardScrollArea>}
      <GrossPresence peers={live.peers} />
      {selectedDriver && <BalanceDetails key={selectedDriver.id + week} driverName={selectedDriver.fullName}
        resetThisWeek={openings.get(selectedDriver.id)?.resetThisWeek}
        onReset={canManageWeek && !weekLocked && !board?.managedInSheets && week === currentChargeWeek() ? async (amount) => {
          const opening = await resetGrossBoardBalance(selectedDriver.id, week, amount);
          setBoard(current => current?.weekStart === week ? {
            ...current, balances: [...current.balances.filter(balance => balance.driverId !== opening.driverId), opening],
          } : current);
          receiveLive.current();
        } : undefined}
        resetDisabled={dirty || saving || loading}
        openingReference={openings.get(selectedDriver.id)?.openingReference} opening={openings.get(selectedDriver.id)?.openingBalance ?? "0"} openingIncomplete={openings.get(selectedDriver.id)?.openingIncomplete ?? 0}
        entries={entryIndex.byDriver.get(selectedDriver.id) ?? []} onClose={() => setBalanceDriver(null)} />}
    </div>
  );
}
