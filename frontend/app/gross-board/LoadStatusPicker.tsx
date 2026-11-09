"use client";

import { useContext, useEffect, useId, useImperativeHandle, useRef, useState, type Ref } from "react";
import { createPortal } from "react-dom";
import { AlertCircle, Check, ChevronDown } from "lucide-react";
import { CurrentUserIDContext } from "@/app/lib/access";
import { searchGrossBoardLoads } from "@/app/lib/api";
import type { GrossBoardDayStatus, GrossBoardEntry, GrossBoardLoad } from "@/app/lib/types";
import { amountDisplay, dayStatuses, matchLoad, setDayStatus, suggestedDayStatuses } from "./board";
import { BoardDialog } from "./BoardDialog";

export type DayEdit = (driverId: string, date: string, update: (current: GrossBoardEntry) => GrossBoardEntry) => void;
type Option = { kind: "status"; status: typeof dayStatuses[number] } | { kind: "load"; load: GrossBoardLoad } | { kind: "clear" } | { kind: "manual" } | { kind: "cancel" } | { kind: "custom" };

export function LoadStatusPicker({ entry, label, disabled, needsReview, onReview, onChange, onCustomStatus, resetRef, customStatusUnavailable, allowDayStatus = true }: {
  entry: GrossBoardEntry; label: string; disabled: boolean; needsReview: boolean; onReview: () => void; onChange: DayEdit; onCustomStatus?: (text: string) => Promise<void>; resetRef?: Ref<() => void>; customStatusUnavailable?: string; allowDayStatus?: boolean;
}) {
  const id = useId();
  const userId = useContext(CurrentUserIDContext);
  const draftKey = ["gross-board-load-intent:v1", userId, entry.driverId, entry.date, entry.slot].join(":");
  const [draftText, setDraftText] = useState<string | null>(() => {
    if (disabled) return null;
    try { const saved = JSON.parse(sessionStorage.getItem(draftKey) ?? "null"); return saved && Date.now() - saved.savedAt < 7 * 86400000 && typeof saved.text === "string" && saved.text.trim() ? saved.text : null; } catch { return null; }
  });
  const [recoveryError, setRecoveryError] = useState(false);
  const [customError, setCustomError] = useState("");
  const [savingCustom, setSavingCustom] = useState(false);
  const setDraft = (value: string | null) => {
    setDraftText(value);
    try { if (value === null) sessionStorage.removeItem(draftKey); else sessionStorage.setItem(draftKey, JSON.stringify({ text: value, savedAt: Date.now() })); setRecoveryError(false); }
    catch { setRecoveryError(true); }
  };
  const input = useRef<HTMLInputElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const [focused, setFocused] = useState(false);
  useImperativeHandle(resetRef, () => () => { setDraft(null); setCustomError(""); setFocused(false); });
  const [browse, setBrowse] = useState(false);
  const [active, setActive] = useState(0);
  const [results, setResults] = useState<{ query: string; loads: GrossBoardLoad[] }>({ query: "", loads: [] });
  const [searchError, setSearchError] = useState("");
  const [pendingStatus, setPendingStatus] = useState<GrossBoardDayStatus>("");
  const [position, setPosition] = useState({ left: 0, top: 0, height: 320, width: 288 });
  const query = (draftText ?? entry.loadNumber).trim();
  const status = draftText === null ? dayStatuses.find((item) => item.value === entry.dayStatus) : undefined;
  const confirmed = draftText === null && entry.loadRecordId !== null && !entry.sourceMissing;
  const unmatched = !status && !confirmed && query !== "";
  const statusOptions = allowDayStatus ? suggestedDayStatuses(browse || status ? "" : query) : [];
  const loads = results.query === query && !status ? results.loads : [];
  // Manual loads are identifiers, never free-form planning sentences.
  const loadIdentifier = /^(?=.*\d)[a-z0-9][a-z0-9._/#-]*$/i.test(query);
  const options: Option[] = [
    ...(draftText !== null && !query ? [{ kind: "manual" } as const] : []),
    ...(status ? [{ kind: "clear" } as const] : []),
    ...statusOptions.map((item): Option => ({ kind: "status", status: item })),
    ...loads.map((load): Option => ({ kind: "load", load })),
    ...(draftText !== null && query && loadIdentifier ? [{ kind: "manual" } as const] : []),
    ...(draftText !== null && query && onCustomStatus ? [{ kind: "custom" } as const] : []),
    ...(draftText !== null ? [{ kind: "cancel" } as const] : []),
  ];
  const open = focused && !disabled && (!confirmed || browse);
  const activeIndex = Math.min(active, Math.max(0, options.length - 1));
  const hasAmounts = (value: GrossBoardEntry) => value.loadRecordId !== null ||
    !!(value.originalRate || value.driverRate || value.miles || value.enteredOriginalRate || value.enteredMiles);

  useEffect(() => {
    if (!query || confirmed || status || disabled || !focused || browse) return;
    let cancelled = false;
    const timer = setTimeout(() => {
      searchGrossBoardLoads(query).then((matches) => {
        if (cancelled) return;
        setSearchError(""); setResults({ query, loads: matches });

      }).catch(() => { if (!cancelled) { setResults({ query, loads: [] }); setSearchError(query); } });
    }, 250);
    return () => { cancelled = true; clearTimeout(timer); };
  }, [query, confirmed, status, disabled, focused, browse, onChange, entry.driverId, entry.date, entry.sourceMissing]);

  useEffect(() => {
    if (!open) return;
    const place = () => {
      const rect = input.current?.getBoundingClientRect();
      if (!rect) return;
      const width = Math.min(320, window.innerWidth - 16);
      const below = window.innerHeight - rect.bottom - 8;
      const above = rect.top - 8;
      const down = below >= 240 || below >= above;
      const height = Math.max(80, Math.min(340, down ? below : above));
      setPosition({ width, height, left: Math.max(8, Math.min(rect.left, window.innerWidth - width - 8)), top: down ? rect.bottom + 4 : Math.max(8, rect.top - height - 4) });
    };
    place();
    const scroll = (event: Event) => { if (!menu.current?.contains(event.target as Node)) place(); };
    window.addEventListener("scroll", scroll, true);
    window.addEventListener("resize", place);
    return () => { window.removeEventListener("scroll", scroll, true); window.removeEventListener("resize", place); };
  }, [open, entry.dayStatus]);

  useEffect(() => {
    if (open) document.getElementById(`${id}-${activeIndex}`)?.scrollIntoView({ block: "nearest" });
  }, [open, activeIndex, id]);

  const choose = (option: Option) => {
    if (disabled || savingCustom) return;
    if (option.kind === "custom") {
      if (!onCustomStatus || customStatusUnavailable) return;
      // Retire the load intent before planning can switch this day to full-day layout.
      setDraft(null); setSavingCustom(true); setCustomError("");
      void onCustomStatus(query).then(() => { setFocused(false); setBrowse(false); })
        .catch(err => {
          // A newly created planning draft survives in its own recovery store.
          // Only restore the load input if this same picker is still mounted.
          if (input.current) { setDraft(query); setCustomError(err instanceof Error ? err.message : "Unable to save custom status. Your text is kept."); }
        })
        .finally(() => setSavingCustom(false));
      return;
    }
    setFocused(false); setBrowse(false);
    if (option.kind === "cancel") { setDraft(null); return; }
    if (option.kind === "manual") {
      onChange(entry.driverId, entry.date, current => ({ ...current, sourceMissing: false, dayStatus: "", loadNumber: query, loadRecordId: null, loadTripId: null, split: false, tripReview: false, tripReviewReason: "", trips: [],
        note: query.toLowerCase() === current.loadNumber.trim().toLowerCase() ? current.note : "",
        originalRate: current.loadRecordId !== null ? "" : current.originalRate, driverRate: current.loadRecordId !== null ? "" : current.driverRate, miles: current.loadRecordId !== null ? "" : current.miles,
        enteredOriginalRate: current.loadRecordId !== null ? "" : current.enteredOriginalRate, enteredMiles: current.loadRecordId !== null ? "" : current.enteredMiles,
        duplicate: false, systemDriverRate: "", acceptDriverGross: false, acceptSystemValues: false }));
      setDraft(null); return;
    }
    setDraft(null);
    if (option.kind === "status" && hasAmounts(entry)) { setPendingStatus(option.status.value); return; }
    onChange(entry.driverId, entry.date, (current) => option.kind === "load" ? matchLoad(current, option.load) :
      setDayStatus(current, option.kind === "status" ? option.status.value : ""));
  };
  return <>
    <div data-grid-cell className="relative">
      <input ref={input} data-unconfirmed-load={draftText !== null || undefined} aria-label={`${label}, load number or status`}
        role="combobox" aria-autocomplete="list" aria-expanded={open} aria-controls={open ? id : undefined}
        aria-activedescendant={open && options.length ? `${id}-${activeIndex}` : undefined}
        title={draftText !== null ? (loadIdentifier ? "Load number. Saves on Enter or when you leave the field." : "Choose Add as custom status to use this text.") : status ? `${status.value} · Day status. Type a load number to replace it.` : confirmed ? "Confirmed system load. Total gross and miles come from Loads." : unmatched ? "Unmatched manual load. Not linked to a system load." : "Choose a system load, explicitly add a manual load, or choose a day status"}
        maxLength={200} value={draftText ?? status?.value ?? entry.loadNumber} disabled={disabled || savingCustom}
        onFocus={(event) => { setFocused(true); setBrowse(false); setActive(status ? dayStatuses.indexOf(status) + 1 : 0); if (status) event.target.select(); }}
        onBlur={() => {
          if (draftText !== null && loadIdentifier && !savingCustom) {
            const exact = loads.filter(load => load.loadNumber.trim().toLowerCase() === query.toLowerCase());
            choose(exact.length === 1 ? { kind: "load", load: exact[0] } : { kind: "manual" });
          } else { setFocused(false); setBrowse(false); }
        }}
        onChange={(event) => {
          setCustomError("");
          if (!event.target.value.trim()) {
            setDraft(null);
            onChange(entry.driverId, entry.date, current => setDayStatus(current, ""));
            setFocused(false); setBrowse(false); return;
          }
          setDraft(event.target.value);
          setActive(0); setBrowse(false); setFocused(true); setSearchError("");
        }}
        onKeyDown={(event) => {
          if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); setDraft(null); setFocused(false); return; }
          if (event.key === "ArrowDown" || event.key === "ArrowUp") {
            event.preventDefault(); setFocused(true);
            if (confirmed) setBrowse(true);
            if (options.length) setActive((value) => open ? (Math.min(value, options.length - 1) + (event.key === "ArrowDown" ? 1 : -1) + options.length) % options.length : 0);
          }
          if (event.key === "Enter" && draftText !== null && loadIdentifier && activeIndex === 0) {
            event.preventDefault();
            const exact = loads.filter(load => load.loadNumber.trim().toLowerCase() === query.toLowerCase());
            choose(exact.length === 1 ? { kind: "load", load: exact[0] } : { kind: "manual" });
          } else if (event.key === "Enter" && open && options.length) { event.preventDefault(); choose(options[activeIndex]); }
        }}
        className={`w-full min-w-0 border-0 text-center text-xs outline-none focus:ring-1 focus:ring-inset focus:ring-blue-500 disabled:opacity-50 ${status ? `h-32 pl-2 pr-6 font-semibold tracking-wide ${status.color}` : `h-8 border-b border-zinc-800/70 px-6 ${confirmed ? "bg-emerald-500/15 text-emerald-300" : unmatched ? "bg-red-500/15 font-semibold text-red-300" : "bg-zinc-800/30 text-zinc-200"}`}`}
        placeholder="Load number" />
      {customError && <span role="alert" className="block text-[10px] text-red-300">{customError}</span>}
      {savingCustom && <span role="status" className="block text-[10px] text-zinc-400">Saving custom status...</span>}
      {recoveryError && <span role="alert" className="block text-[10px] text-amber-300">Keep this page open; draft recovery is unavailable.</span>}
      {entry.sourceMissing && <span role="status" title="The linked DataTruck record is no longer available. Reconciliation reconnects a unique replacement; entered values and finalized statements are preserved." className="block px-1 text-[10px] text-amber-400">Not found in system</span>}
      {confirmed && !needsReview && <Check aria-label="Confirmed load" className="pointer-events-none absolute left-1 top-2 h-4 w-4 text-emerald-400" />}
        <button type="button" tabIndex={-1} aria-label={`Choose day status for ${label}`} title="Choose a day status" disabled={disabled}
          onMouseDown={(event) => event.preventDefault()}
          onClick={() => { input.current?.focus(); setBrowse(true); setFocused(true); setActive(status ? dayStatuses.indexOf(status) + 1 : 0); }}
          className={`absolute right-0 flex h-8 w-6 items-center justify-center rounded text-zinc-400 hover:bg-white/10 hover:text-zinc-100 focus-visible:ring-1 focus-visible:ring-blue-500 ${status ? "top-12" : "top-0"}`}><ChevronDown className="h-3 w-3" /></button>
      {needsReview && <button type="button" aria-label={`Review rate and miles for ${label}`} title={entry.tripReviewReason || "Entered values differ from the system load"} onClick={onReview} className="absolute left-1 top-1.5 rounded text-red-400 hover:text-red-200"><AlertCircle className="h-5 w-5" /></button>}
    </div>
    {open && createPortal(<div ref={menu} style={{ position: "fixed", left: position.left, top: position.top, width: position.width, maxHeight: position.height }}
      className="z-[80] overflow-y-auto rounded-lg border border-zinc-700 bg-zinc-900 p-1 text-left ui-popover"
      onMouseDown={(event) => event.preventDefault()}>
      <div className="px-2 py-2 text-[10px] text-zinc-400">{statusOptions.length ? "DAY STATUSES" : "LOADS"} <span className="float-right">↑ ↓ choose · Enter select</span></div>
      <ul id={id} role="listbox" aria-label="Loads and day statuses" className="grid grid-cols-2 gap-0.5">
        {options.map((option, index) => <li key={option.kind === "status" ? option.status.value : option.kind === "load" ? option.load.id : option.kind}
          id={`${id}-${index}`} role="option" aria-selected={index === activeIndex} aria-disabled={option.kind === "custom" && !!customStatusUnavailable}
          onClick={() => choose(option)} className={`${option.kind !== "status" ? "col-span-2" : ""} cursor-pointer rounded-md px-2 py-2 text-xs ${index === activeIndex ? "bg-white/10 ring-1 ring-inset ring-zinc-600" : "hover:bg-zinc-800"}`}>
          {option.kind === "status" ? <span className={`inline-flex rounded px-2 py-1 font-medium ${option.status.color}`}>{option.status.value}</span> : option.kind === "clear" ?
            <span className="text-zinc-400">Clear status · Enter a load</span> : option.kind === "manual" ? <span className="text-blue-300">{query ? `Add as manual load: ${query}` : "Clear load number"}</span> : option.kind === "custom" ? <span className={customStatusUnavailable ? "text-zinc-500" : "text-blue-300"}>Add as custom status: {query}{customStatusUnavailable && <span className="mt-1 block text-[10px]">{customStatusUnavailable}</span>}</span> : option.kind === "cancel" ? <span className="text-zinc-400">Cancel typed text</span> : <>
              <div className="font-medium text-zinc-100">{option.load.loadNumber} · {amountDisplay(option.load.originalRate, true)} <span className="text-[10px] text-zinc-500">LOAD</span></div>
              <div className="mt-1 text-zinc-400">{option.load.driverName || "Unassigned"} · {option.load.pickupDate || "No pickup date"} · {amountDisplay(option.load.miles) || "—"} mi</div>
              <div className="text-zinc-500">Record #{option.load.id}</div>
            </>}
        </li>)}
      </ul>
      {!options.length && <p className="p-2 text-xs text-zinc-500">{searchError === query ? "Load lookup unavailable. Your text is kept; you can explicitly add a manual load." : "Choose a load or add its number explicitly. Type a status or destination to add a custom status."}</p>}
      {draftText !== null && <p className="border-t border-zinc-800 px-2 py-2 text-[11px] text-zinc-400">Load numbers save as loads. Choose the custom status alternative for status or destination text.</p>}
      {statusOptions.length > 0 && <p className="border-t border-zinc-800 px-2 py-2 text-[10px] text-zinc-500">Status days have no rates or miles.</p>}
    </div>, document.body)}
    {pendingStatus && <BoardDialog title={`Replace this day with ${pendingStatus}?`} onClose={() => setPendingStatus("")}>
      <p className="text-sm text-zinc-300">{label}. This removes the day’s load, rates, and miles from the board and its totals. The system load itself stays unchanged.</p>
      <div className="mt-5 flex justify-end gap-2">
        <button type="button" className="rounded-lg border border-zinc-700 px-3 py-2 text-sm" onClick={() => setPendingStatus("")}>Keep load</button>
        <button type="button" className="rounded-lg bg-blue-600 px-3 py-2 text-sm text-white" onClick={() => {
          onChange(entry.driverId, entry.date, (current) => setDayStatus(current, pendingStatus)); setPendingStatus("");
        }}>Use {pendingStatus}</button>
      </div>
    </BoardDialog>}
  </>;
}
