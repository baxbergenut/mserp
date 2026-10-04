"use client";

import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { GrossBoardEntry } from "@/app/lib/types";
import { addDays, shortDate } from "@/app/gross-board/board";

export function CurrentLoadInput({ value, label, disabled, entries, week, onChange }: {
  value: string; label: string; disabled: boolean; entries: GrossBoardEntry[]; week: string;
  onChange: (value: string) => void;
}) {
  const id = useId();
  const input = useRef<HTMLInputElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const [focused, setFocused] = useState(false);
  const [active, setActive] = useState(0);
  const [position, setPosition] = useState({ left: 0, top: 0, width: 280, height: 240 });
  const query = value.trim().toLowerCase();
  const seen = new Set<string>();
  const options = entries.filter(e => {
    const number = e.loadNumber.trim().toLowerCase();
    if (e.deleted || e.dayStatus || e.date < week || e.date > addDays(week, 6) || !number || !number.includes(query) || seen.has(number)) return false;
    seen.add(number); return true;
  }).slice(0, 10);
  const open = focused && !disabled && !!query && options.length > 0;
  const selected = Math.min(active, options.length - 1);
  useEffect(() => {
    if (!open) return;
    const place = () => {
      const rect = input.current?.getBoundingClientRect();
      if (!rect) return;
      const width = Math.min(280, window.innerWidth - 16);
      const below = window.innerHeight - rect.bottom - 8;
      const above = rect.top - 8;
      const down = below >= 240 || below >= above;
      const height = Math.max(60, Math.min(280, down ? below : above));
      setPosition({ width, height, left: Math.max(8, Math.min(rect.left, window.innerWidth - width - 8)), top: down ? rect.bottom + 4 : Math.max(8, rect.top - height - 4) });
    };
    place();
    const scroll = (event: Event) => { if (!menu.current?.contains(event.target as Node)) place(); };
    window.addEventListener("resize", place); window.addEventListener("scroll", scroll, true);
    return () => { window.removeEventListener("resize", place); window.removeEventListener("scroll", scroll, true); };
  }, [open]);
  useEffect(() => {
    if (open) document.getElementById(`${id}-${selected}`)?.scrollIntoView({ block: "nearest" });
  }, [id, selected, open]);
  function choose(number: string) { setFocused(false); onChange(number); }
  return <>
    <input ref={input} type="text" role="combobox" aria-autocomplete="list" aria-expanded={open} aria-controls={open ? id : undefined} aria-activedescendant={open ? `${id}-${selected}` : undefined}
      aria-label={label} title={value} maxLength={300} disabled={disabled} autoComplete="off" value={value}
      onFocus={() => { setFocused(true); setActive(0); }} onBlur={() => setFocused(false)}
      onChange={event => { setFocused(true); setActive(0); onChange(event.target.value); }}
      onKeyDown={event => {
        if (!open) return;
        if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); setFocused(false); }
        if (event.key === "ArrowDown" || event.key === "ArrowUp") { event.preventDefault(); setActive((selected + (event.key === "ArrowDown" ? 1 : options.length - 1)) % options.length); }
        if (event.key === "Enter") { event.preventDefault(); choose(options[selected].loadNumber); }
      }}
      className="block h-8 w-full min-w-0 bg-transparent px-1.5 text-[11px] leading-4 text-zinc-200 outline-none focus:bg-zinc-800 focus:ring-1 focus:ring-inset focus:ring-blue-500" />
    {open && createPortal(<div ref={menu} role="listbox" id={id} aria-label={`${label} suggestions`} style={{ position: "fixed", left: position.left, top: position.top, width: position.width, maxHeight: position.height }} className="z-50 overflow-y-auto rounded-lg border border-zinc-700 bg-zinc-950 p-1 shadow-xl">
      <div className="px-2 py-1.5 text-[10px] text-zinc-500">Gross Board · this driver · current week</div>
      {options.map((e, i) => <div key={e.loadNumber} id={`${id}-${i}`} role="option" aria-selected={i === selected} onMouseDown={event => event.preventDefault()} onClick={() => choose(e.loadNumber)} className={`flex cursor-pointer items-center justify-between gap-3 rounded px-2 py-2 text-xs text-zinc-200 hover:bg-zinc-800 ${i === selected ? "bg-blue-500/15" : ""}`}><span className="truncate">{e.loadNumber}</span><span className="shrink-0 text-zinc-500">{shortDate(e.date)}</span></div>)}
    </div>, document.body)}
  </>;
}
