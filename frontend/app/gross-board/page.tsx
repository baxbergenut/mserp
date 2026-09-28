"use client";

import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Banknote, CalendarRange, ChevronLeft, ChevronRight, Gauge, RefreshCw, Route, CloudCheck } from "lucide-react";
import { fetchGrossBoard, saveGrossBoard } from "@/app/lib/api";
import type { GrossBoard, GrossBoardEntry } from "@/app/lib/types";
import { MetricCard } from "@/app/components/MetricCard";
import { controlClass } from "@/app/components/management/ManagementUI";
import { DayCell } from "./DayCell";
import { addDays, decimalDisplay, emptyEntry, entryKey, monday, reconcileAutosave, rpmDisplay, shortDate, totals, validDecimal } from "./board";

const weekdays = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
const columnWidths = [170, 65, 82, ...weekdays.map(() => 145), 112, 112, 112, 112];
const minimumBoardWidth = columnWidths.reduce((sum, width) => sum + width, 0);
// Match the sticky truck offset to the expanding driver column, including
// when a narrow viewport scrolls the table at its minimum readable width.
const truckColumnStyle = { left: `max(${columnWidths[0]}px, ${columnWidths[0] / minimumBoardWidth * 100}%)` };
const buttonClass = "inline-flex items-center justify-center gap-2 rounded-lg border border-zinc-700/70 px-3 py-2 text-xs text-zinc-300 hover:bg-zinc-800 focus-visible:outline-2 focus-visible:outline-blue-500 disabled:cursor-not-allowed disabled:opacity-40";

export default function GrossBoardPage() {
  const [week, setWeek] = useState(() => monday());
  const [board, setBoard] = useState<GrossBoard | null>(null);
  const [changes, setChanges] = useState<Record<string, GrossBoardEntry>>({});
  const [dispatcher, setDispatcher] = useState("all");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [pendingWeek, setPendingWeek] = useState<string | null>(null);
  const [pendingLink, setPendingLink] = useState<string | null>(null);
  const savingRef = useRef(false);
  const dirty = Object.keys(changes).length > 0;
  const dates = useMemo(() => weekdays.map((_, index) => addDays(week, index)), [week]);

  const load = useCallback(async () => {
    setLoading(true); setError("");
    try { setBoard(await fetchGrossBoard(week)); }
    catch (err) { setBoard(null); setError(err instanceof Error ? err.message : "Unable to load board"); }
    finally { setLoading(false); }
  }, [week]);

  useEffect(() => {
    let cancelled = false;
    fetchGrossBoard(week).then((value) => { if (!cancelled) setBoard(value); })
      .catch((err) => { if (!cancelled) setError(err instanceof Error ? err.message : "Unable to load board"); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [week]);

  useEffect(() => {
    if (!dirty) return;
    const beforeUnload = (event: BeforeUnloadEvent) => { event.preventDefault(); };
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

  const saved = useMemo(() => Object.fromEntries((board?.entries ?? []).map((entry) => [entryKey(entry.driverId, entry.date), entry])), [board]);
  const allEntries = useMemo(() => ({ ...saved, ...changes }), [saved, changes]);
  const drivers = useMemo(() => (board?.drivers ?? []).filter((driver) => dispatcher === "all" || driver.dispatcherId === dispatcher), [board, dispatcher]);
  const dispatchers = useMemo(() => Array.from(new Map((board?.drivers ?? []).map((driver) => [driver.dispatcherId, driver.dispatcherName])).entries()), [board]);
  const shownEntries = useMemo(() => {
    const ids = new Set(drivers.map((driver) => driver.id));
    return Object.values(allEntries).filter((entry) => ids.has(entry.driverId));
  }, [allEntries, drivers]);
  const summary = totals(shownEntries);
  const invalid = Object.values(changes).some((entry) => !validDecimal(entry.originalRate) || !validDecimal(entry.driverRate) || !validDecimal(entry.miles, true));

  function switchWeek(next: string) {
    if (next === week || loading) return;
    setPendingWeek(next);
  }

  useEffect(() => {
    if (dirty || saving || loading || error || (!pendingWeek && !pendingLink)) return;
    if (pendingLink) { window.location.assign(pendingLink); return; }
    if (pendingWeek) {
      const timer = setTimeout(() => {
        setBoard(null); setMessage(""); setLoading(true); setWeek(pendingWeek); setPendingWeek(null);
      }, 0);
      return () => clearTimeout(timer);
    }
  }, [pendingWeek, pendingLink, dirty, saving, loading, error]);

  function edit(driverId: string, date: string, update: (entry: GrossBoardEntry) => GrossBoardEntry) {
    const key = entryKey(driverId, date);
    setChanges((current) => {
      const previous = current[key] ?? saved[key] ?? emptyEntry(driverId, date);
      const next = update(previous);
      if (next === previous) return current;
      const result = { ...current, [key]: next };
      // A revert during an in-flight save is a new edit; the old saved value
      // will be replaced by that response, so it must remain queued.
      if (!savingRef.current && JSON.stringify(next) === JSON.stringify(saved[key] ?? emptyEntry(driverId, date))) delete result[key];
      return result;
    });
    setMessage("");
  }

  const save = useCallback(async () => {
    if (savingRef.current || invalid || !dirty || loading) return;
    savingRef.current = true;
    const snapshot = { ...changes };
    setSaving(true); setError(""); setMessage("");
    try {
      const committed = await saveGrossBoard(week, Object.values(snapshot));
      setBoard((current) => {
        if (!current) return current;
        const entries = Object.fromEntries(current.entries.map((entry) => [entryKey(entry.driverId, entry.date), entry]));
        committed.forEach((entry) => { entries[entryKey(entry.driverId, entry.date)] = entry; });
        return { ...current, entries: Object.values(entries) };
      });
      setChanges((current) => reconcileAutosave(current, snapshot, committed));
      setMessage("All changes saved");
    } catch (err) { setError(err instanceof Error ? err.message : "Unable to save. Your edits are still here."); }
    finally { savingRef.current = false; setSaving(false); }
  }, [changes, dirty, invalid, loading, week]);

  useEffect(() => {
    if (!dirty || saving || error || invalid || loading) return;
    const timer = setTimeout(() => { void save(); }, pendingWeek || pendingLink ? 0 : 650);
    return () => clearTimeout(timer);
  }, [dirty, saving, error, invalid, loading, save, pendingWeek, pendingLink]);

  async function reload() {
    if (dirty && !window.confirm("Discard unsaved changes and reload the saved board?")) return;
    setChanges({}); setMessage(""); setPendingWeek(null); setPendingLink(null); await load();
  }

  return (
    <div className="space-y-5 animate-fade-in">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <div className="flex items-center gap-3">
            <CalendarRange className="h-5 w-5 text-zinc-500" />
            <h1 className="text-lg font-semibold text-zinc-100">Gross Board</h1>
            <span className="rounded-full bg-zinc-800/60 px-2.5 py-0.5 text-[12px] font-medium text-zinc-400">{drivers.length}</span>
          </div>
          <p className="mt-1.5 text-[13px] text-zinc-500">Weekly load planning, driver rates, and gross totals by dispatcher.</p>
        </div>
        <div className="flex items-center gap-2">
          <span role="status" className={`flex items-center gap-1.5 text-xs ${error && dirty ? "text-red-300" : "text-zinc-400"}`}><CloudCheck className="h-4 w-4" />{error && dirty ? "Not saved" : saving ? "Saving…" : dirty ? "Waiting to save…" : message || "Saved automatically"}</span>
          <button className={buttonClass} onClick={reload} disabled={saving || loading} title="Reload saved board"><RefreshCw className="h-4 w-4" />Reload</button>
        </div>
      </div>

      <div className="grid gap-3 sm:grid-cols-3">
        <MetricCard compact label="Original total gross" value={loading || !board ? "—" : decimalDisplay(summary.original, true)} icon={Banknote} />
        <MetricCard compact label="Total miles" value={loading || !board ? "—" : decimalDisplay(summary.miles)} icon={Route} />
        <MetricCard compact label="Original RPM" value={loading || !board ? "—" : rpmDisplay(summary.original, summary.miles)} icon={Gauge} />
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <div className="flex items-center gap-1 rounded-lg border border-zinc-800 bg-card p-1">
          <button aria-label="Previous week" className={buttonClass} disabled={loading || week <= "2000-01-03"} onClick={() => switchWeek(addDays(week, -7))}><ChevronLeft className="h-4 w-4" /></button>
          <span className="min-w-36 px-2 text-center font-mono text-sm text-zinc-100" title={`${week} to ${dates[6]}`}>{shortDate(week)}–{shortDate(dates[6])}</span>
          <button aria-label="Next week" className={buttonClass} disabled={loading || week >= "2100-12-27"} onClick={() => switchWeek(addDays(week, 7))}><ChevronRight className="h-4 w-4" /></button>
        </div>
        <span className="text-xs text-zinc-500">{week.slice(0, 4)}{week.slice(0, 4) !== dates[6].slice(0, 4) ? ` / ${dates[6].slice(0, 4)}` : ""}</span>
        <button className={buttonClass} disabled={loading} onClick={() => switchWeek(monday())}>This week</button>
        <label className="ml-auto flex items-center gap-2 text-xs text-zinc-400">Dispatcher
          <select aria-label="Dispatcher" className={`${controlClass} !w-48`} value={dispatcher} onChange={(event) => setDispatcher(event.target.value)} disabled={saving}>
            <option value="all">All dispatchers</option>
            {dispatchers.map(([id, name]) => <option key={id} value={id}>{name}</option>)}
          </select>
        </label>
      </div>

      {error && <div role="alert" className="rounded-lg border border-red-500/20 bg-red-500/10 p-3 text-sm text-red-300">{error} {dirty && "Your unsaved edits are still on this page."}{dirty && <button className={`${buttonClass} ml-3`} onClick={save} disabled={saving || invalid}>Retry save</button>}{!board && <button className={`${buttonClass} ml-3`} onClick={load}>Retry</button>}</div>}
      {(pendingWeek || pendingLink) && dirty && <p role="status" className="text-xs text-amber-300">Saving edits before leaving this week…{invalid || error ? " Resolve the highlighted issue to continue." : ""}<button className="ml-2 underline" onClick={() => { setPendingWeek(null); setPendingLink(null); }}>Stay here</button></p>}
      {invalid && <p role="alert" className="text-xs text-red-300">Correct the highlighted fields before saving. Use numbers with at most two decimal places; miles cannot be negative.</p>}
      <div className="flex flex-wrap gap-4 text-[11px] text-zinc-500">
        <span className="flex items-center gap-1.5"><span className="h-2 w-2 rounded-full bg-emerald-400" />Green = confirmed load; original rate and miles are locked</span>
        <span>Cut = original gross − driver gross</span>
        <span>{drivers.length} drivers · One load or plan per driver per day</span>
      </div>

      {loading ? <div role="status" className="rounded-xl border border-zinc-800 p-12 text-center text-sm text-zinc-500">Loading gross board…</div> : board && drivers.length === 0 ? <div className="rounded-xl border border-zinc-800 p-12 text-center text-sm text-zinc-500">No drivers for this dispatcher.</div> : board && <div className="max-h-[70vh] overflow-auto rounded-xl border border-zinc-800" role="region" aria-label="Weekly gross board" tabIndex={0}>
        <table className="w-full table-fixed border-separate border-spacing-0 text-center text-xs" style={{ minWidth: minimumBoardWidth }}>
          <colgroup>{columnWidths.map((width, index) => <col key={index} style={{ width: `${width / minimumBoardWidth * 100}%` }} />)}</colgroup>
          <thead className="sticky top-0 z-30 bg-zinc-900 text-zinc-300">
            <tr>
              <th className="sticky left-0 z-30 border-b border-r border-zinc-700 bg-zinc-900 px-3 py-3">Driver</th>
              <th className="sticky z-30 border-b border-r border-zinc-700 bg-zinc-900 px-2" style={truckColumnStyle}>Truck</th>
              <th className="border-b border-r border-zinc-700" aria-label="Field" />
              {dates.map((date, i) => <th key={date} className="border-b border-r border-zinc-700 py-2"><div className="font-medium">{weekdays[i]}</div><div className="mt-1 font-mono text-[11px] font-normal text-zinc-500">{shortDate(date)}</div></th>)}
              {["Original gross", "Driver gross", "Cut", "Total miles"].map((title) => <th key={title} className="border-b border-r border-zinc-700 bg-blue-500/5 px-2 font-medium">{title}</th>)}
            </tr>
          </thead>
          <tbody>
            {drivers.map((driver, index) => {
              const entries = dates.map((date) => allEntries[entryKey(driver.id, date)] ?? emptyEntry(driver.id, date));
              const sum = totals(entries);
              const groupStart = index === 0 || drivers[index - 1].dispatcherId !== driver.dispatcherId;
              return <Fragment key={driver.id}>
                {groupStart && <tr><th colSpan={14} scope="rowgroup" className="border-b border-zinc-700 bg-blue-500/10 py-2 text-left font-medium text-blue-300"><span className="sticky left-3">{driver.dispatcherName}</span></th></tr>}
                <tr>
                  <th scope="row" className="sticky left-0 z-20 border-b border-r border-zinc-800 bg-zinc-950 px-3 font-medium text-zinc-200">{driver.fullName}{!driver.active && <div className="mt-1 text-[10px] text-zinc-500">Inactive</div>}</th>
                  <td className="sticky z-20 border-b border-r border-zinc-800 bg-zinc-950 px-2 font-mono text-zinc-400" style={truckColumnStyle}>{driver.truckUnit || "—"}</td>
                  <td className="border-b border-r border-zinc-800 bg-zinc-900/60 p-0 text-[10px] text-zinc-500">{["Load #", "Original", "Driver", "Miles"].map((field) => <div key={field} className="flex h-8 items-center justify-center border-b border-zinc-800/70 px-2">{field}</div>)}</td>
                  {entries.map((entry) => <DayCell key={entry.date} entry={entry} driverName={driver.fullName} disabled={false} onChange={(update) => edit(driver.id, entry.date, update)} />)}
                  {[decimalDisplay(sum.original, true), decimalDisplay(sum.driver, true), decimalDisplay(sum.original - sum.driver, true), decimalDisplay(sum.miles)].map((value, i) => <td key={i} className={`border-b border-r border-zinc-800 bg-blue-500/[0.03] px-2 font-mono ${i === 0 ? "text-blue-200" : "text-zinc-300"}`}>{value}</td>)}
                </tr>
              </Fragment>;
            })}
          </tbody>
          <tfoot><tr className="bg-zinc-900 font-mono text-zinc-200"><th colSpan={10} className="border-t border-zinc-700 p-3 text-left"><span className="sticky left-3">Week total</span></th>{[decimalDisplay(summary.original, true), decimalDisplay(summary.driver, true), decimalDisplay(summary.original - summary.driver, true), decimalDisplay(summary.miles)].map((value, i) => <td key={i} className="border-t border-zinc-700 px-2 py-3">{value}</td>)}</tr></tfoot>
        </table>
      </div>}
    </div>
  );
}
