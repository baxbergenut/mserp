"use client";

import { useEffect, useRef, useState, type KeyboardEvent, type MouseEvent } from "react";
import { createPortal } from "react-dom";
import { usePermissions } from "@/app/lib/access";
import type { GrossBoardEntry } from "@/app/lib/types";

type Edit = (driverId: string, date: string, slot: number, update: (entry: GrossBoardEntry) => GrossBoardEntry) => void;
type OpenNote = { kind: "menu" | "note"; slot?: number; slots?: number[]; focus: boolean; top: number; left: number };

function notePosition(element: HTMLElement) {
  const rect = element.getBoundingClientRect();
  const width = Math.min(300, window.innerWidth - 16);
  const fitsRight = rect.right + width + 12 <= window.innerWidth;
  const fitsLeft = rect.left - width - 12 >= 0;
  const left = fitsRight ? rect.right + 4 : fitsLeft ? rect.left - width - 4 : rect.left;
  const top = fitsRight || fitsLeft ? Math.min(rect.top, window.innerHeight - 280)
    : rect.bottom + 200 < window.innerHeight ? rect.bottom + 4 : rect.top - 240;
  return { left: Math.max(8, Math.min(left, window.innerWidth - width - 8)), top: Math.max(8, Math.min(top, window.innerHeight - 80)) };
}

export function useLoadNotes(entries: GrossBoardEntry[], disabled: boolean, onChange: Edit) {
  const canEdit = usePermissions().includes("board.write") && !disabled;
  const [open, setOpen] = useState<OpenNote | null>(null);
  const anchor = useRef<HTMLElement | null>(null);
  const popup = useRef<HTMLDivElement | null>(null);
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const eligible = entries.filter(entry => !entry.deleted && !entry.dayStatus && entry.loadNumber.trim());
  const noted = eligible.filter(entry => entry.note?.trim());
  const cancelClose = () => { if (closeTimer.current) clearTimeout(closeTimer.current); };
  const close = () => { cancelClose(); setOpen(null); };
  const leave = () => {
    cancelClose();
    closeTimer.current = setTimeout(() => {
      if (!popup.current?.contains(document.activeElement)) setOpen(current => current?.kind === "menu" ? current : null);
    }, 250);
  };
  useEffect(() => () => { if (closeTimer.current) clearTimeout(closeTimer.current); }, []);

  const place = (element: HTMLElement, kind: OpenNote["kind"], slot?: number, focus = false) => {
    cancelClose(); anchor.current = element;
    setOpen({ kind, slot, slots: noted.map(entry => entry.slot), focus, ...notePosition(element) });
  };
  const isOpen = !!open;
  useEffect(() => {
    if (!isOpen) return;
    const outside = (event: PointerEvent) => {
      if (!popup.current?.contains(event.target as Node) && !anchor.current?.contains(event.target as Node)) setOpen(null);
    };
    const reposition = (event: Event) => {
      if (event.target instanceof Node && popup.current?.contains(event.target)) return;
      const element = anchor.current;
      if (element) setOpen(current => current && { ...current, ...notePosition(element) });
    };
    document.addEventListener("pointerdown", outside);
    window.addEventListener("scroll", reposition, true); window.addEventListener("resize", reposition);
    return () => {
      document.removeEventListener("pointerdown", outside);
      window.removeEventListener("scroll", reposition, true); window.removeEventListener("resize", reposition);
    };
  }, [isOpen]);
  useEffect(() => {
    if (open?.focus) popup.current?.querySelector<HTMLElement>(open.kind === "menu" ? "button" : "textarea")?.focus();
  }, [open?.kind, open?.slot, open?.focus]);

  const write = (entry: GrossBoardEntry, note: string) => onChange(entry.driverId, entry.date, entry.slot, current =>
    current.loadNumber === entry.loadNumber && current.loadRecordId === entry.loadRecordId ? { ...current, note } : current);
  const contextMenu = (event: MouseEvent<HTMLElement>) => {
    if (!eligible.length || !canEdit) return;
    event.preventDefault(); event.stopPropagation(); place(event.currentTarget, "menu", undefined, true);
  };
  const keyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (!eligible.length || !canEdit || !(event.key === "ContextMenu" || (event.shiftKey && event.key === "F10"))) return;
    event.preventDefault(); event.stopPropagation(); place(event.currentTarget, "menu", undefined, true);
  };
  const shown = eligible.filter(entry => open?.slot === undefined ? open?.slots?.includes(entry.slot) : entry.slot === open.slot);
  return {
    cellProps: {
      onContextMenu: contextMenu, onKeyDown: keyDown,
      onMouseEnter: (event: MouseEvent<HTMLElement>) => { cancelClose(); if (!open && noted.length) place(event.currentTarget, "note"); },
      onMouseLeave: leave,
    },
    marker: noted.length > 0 ? <span role="img" aria-label="Has load note" className="pointer-events-none absolute right-0 top-0 z-10 h-0 w-0 border-l-[9px] border-t-[9px] border-l-transparent border-t-[#18181b] drop-shadow-[0_0_1px_#a1a1aa]" /> : null,
    popup: open && (open.kind === "menu" || shown.length > 0) ? createPortal(
      <div ref={popup} data-grid-ignore role={open.kind === "menu" ? "menu" : "dialog"} aria-label={open.kind === "menu" ? "Load notes" : "Load note"}
        style={{ top: open.top, left: open.left, width: "min(300px, calc(100vw - 16px))", maxHeight: `calc(100vh - ${open.top + 8}px)` }}
        className={`mserp-ui fixed z-[100] overflow-y-auto text-left text-sm text-zinc-200 ${open.kind === "menu" ? "rounded-lg border border-zinc-700 bg-zinc-900 p-2 shadow-xl" : "space-y-1"}`}
        onMouseEnter={cancelClose} onMouseLeave={leave} onBlur={leave} onContextMenu={event => event.stopPropagation()}
        onKeyDown={event => {
          event.stopPropagation();
          if (event.key === "Escape") { event.preventDefault(); close(); anchor.current?.focus({ preventScroll: true }); }
          if (open.kind === "menu" && ["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
            event.preventDefault();
            const buttons = Array.from(popup.current?.querySelectorAll<HTMLButtonElement>("button") ?? []);
            const index = buttons.indexOf(document.activeElement as HTMLButtonElement);
            buttons[event.key === "Home" ? 0 : event.key === "End" ? buttons.length - 1 : (index + (event.key === "ArrowUp" ? -1 : 1) + buttons.length) % buttons.length]?.focus();
          }
        }}>
        {open.kind === "menu" ? eligible.map(entry => <div key={entry.slot}>
          <button type="button" role="menuitem" className="block min-h-8 w-full rounded px-2 text-left hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none"
            onClick={() => place(anchor.current!, "note", entry.slot, true)}>{entry.note?.trim() ? "Edit note" : "Add note"}{eligible.length > 1 ? ` · ${entry.loadNumber} (Load ${entry.slot + 1})` : ""}</button>
          {entry.note?.trim() && <button type="button" role="menuitem" className="block min-h-8 w-full rounded px-2 text-left text-red-300 hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none"
            onClick={() => { write(entry, ""); close(); }}>Delete note{eligible.length > 1 ? ` · ${entry.loadNumber} (Load ${entry.slot + 1})` : ""}</button>}
        </div>) : shown.map(entry => <textarea key={entry.slot} aria-label={`Note for ${entry.loadNumber}, load ${entry.slot + 1}`} readOnly={!canEdit} maxLength={5000} rows={4}
              value={entry.note ?? ""} placeholder="Add a note…" onChange={event => write(entry, event.target.value)}
              className="block w-full resize-y rounded border border-zinc-700 bg-zinc-950 p-2 text-sm text-zinc-200 outline-none focus:border-blue-500" />)}
      </div>, document.body) : null,
  };
}
