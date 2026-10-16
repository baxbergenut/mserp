"use client";

import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { DriverBoardEntry } from "@/app/lib/types";

export function TrailerCell({ entry, name, disabled, edit, onHistory }: {
  entry: DriverBoardEntry; name: string; disabled: boolean;
  edit: (id: string, field: keyof DriverBoardEntry, value: string) => void;
  onHistory: () => void;
}) {
  const cell = useRef<HTMLTableCellElement>(null);
  const popup = useRef<HTMLDivElement>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [open, setOpen] = useState<{ kind: "menu" | "note"; focus: boolean; left: number; top: number } | null>(null);
  const cancel = () => { if (timer.current) clearTimeout(timer.current); };
  function show(kind: "menu" | "note", focus = false) {
    cancel();
    const rect = cell.current!.getBoundingClientRect();
    setOpen({ kind, focus, left: Math.max(8, Math.min(rect.right + 4, innerWidth - 308)), top: Math.max(8, Math.min(rect.top, innerHeight - 240)) });
  }
  function leave() {
    cancel(); timer.current = setTimeout(() => {
      if (!popup.current?.contains(document.activeElement)) setOpen(current => current?.kind === "menu" ? current : null);
    }, 250);
  }
  useEffect(() => () => { if (timer.current) clearTimeout(timer.current); }, []);
  const isOpen = !!open;
  useEffect(() => {
    if (!isOpen) return;
    const outside = (event: PointerEvent) => { if (!popup.current?.contains(event.target as Node) && !cell.current?.contains(event.target as Node)) setOpen(null); };
    const reposition = (event: Event) => {
      if (event.target instanceof Node && popup.current?.contains(event.target)) return;
      const rect = cell.current?.getBoundingClientRect();
      if (rect) setOpen(current => current && { ...current, left: Math.max(8, Math.min(rect.right + 4, innerWidth - 308)), top: Math.max(8, Math.min(rect.top, innerHeight - 240)) });
    };
    document.addEventListener("pointerdown", outside); window.addEventListener("scroll", reposition, true); window.addEventListener("resize", reposition);
    return () => { document.removeEventListener("pointerdown", outside); window.removeEventListener("scroll", reposition, true); window.removeEventListener("resize", reposition); };
  }, [isOpen]);
  useEffect(() => { if (open?.focus) popup.current?.querySelector<HTMLElement>(open.kind === "menu" ? "button" : "textarea")?.focus({ preventScroll: true }); }, [open?.kind, open?.focus]);
  const menuButton = "block min-h-8 w-full rounded px-2 text-left hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none";
  return <td ref={cell} className="relative overflow-hidden text-ellipsis whitespace-nowrap border-b border-r border-zinc-800/80"
    onContextMenu={event => { event.preventDefault(); event.stopPropagation(); show("menu", true); }}
    onKeyDown={event => { if (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10")) { event.preventDefault(); event.stopPropagation(); show("menu", true); } }}
    onMouseEnter={() => { cancel(); if (!open && entry.trailerNote?.trim() && entry.trailerNumber.trim()) show("note"); }} onMouseLeave={leave}>
    <input type="text" aria-label={`${name} · Trailer`} title={entry.trailerNumber} maxLength={100} disabled={disabled} value={entry.trailerNumber}
      onChange={event => edit(entry.driverId, "trailerNumber", event.target.value)}
      className="block h-8 w-full min-w-0 bg-transparent px-1.5 text-[11px] leading-4 text-zinc-200 outline-none focus:bg-zinc-800 focus:ring-1 focus:ring-inset focus:ring-blue-500" />
    {entry.trailerNumber.trim() && entry.trailerNote?.trim() && <span role="img" aria-label="Has trailer note" className="pointer-events-none absolute right-0 top-0 z-10 h-0 w-0 border-l-[9px] border-t-[9px] border-l-transparent border-t-[#18181b] drop-shadow-[0_0_1px_#a1a1aa]" />}
    {open && createPortal(<div ref={popup} data-grid-ignore data-board-undo role={open.kind === "menu" ? "menu" : "dialog"} aria-label={open.kind === "menu" ? "Trailer actions" : "Trailer note"}
      style={{ left: open.left, top: open.top, width: "min(300px, calc(100vw - 16px))" }}
      className={`mserp-ui fixed z-[100] text-left text-sm text-zinc-200 ${open.kind === "menu" ? "rounded-lg border border-zinc-700 bg-zinc-900 p-2 shadow-xl" : ""}`}
      onMouseEnter={cancel} onMouseLeave={leave} onBlur={leave} onContextMenu={event => event.stopPropagation()}
      onKeyDown={event => {
        if ((event.ctrlKey || event.metaKey) && !event.shiftKey && event.key.toLowerCase() === "z") return;
        event.stopPropagation();
        if (event.key === "Escape") { event.preventDefault(); setOpen(null); cell.current?.focus({ preventScroll: true }); }
        if (open.kind === "menu" && ["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
          event.preventDefault(); const buttons = Array.from(popup.current?.querySelectorAll<HTMLButtonElement>("button") ?? []);
          const index = buttons.indexOf(document.activeElement as HTMLButtonElement);
          buttons[event.key === "Home" ? 0 : event.key === "End" ? buttons.length - 1 : (index + (event.key === "ArrowUp" ? -1 : 1) + buttons.length) % buttons.length]?.focus();
        }
      }}>
      {open.kind === "menu" ? <>
        {!disabled && entry.trailerNumber.trim() && <button role="menuitem" className={menuButton} onClick={() => show("note", true)}>{entry.trailerNote?.trim() ? "Edit note" : "Add note"}</button>}
        {!disabled && entry.trailerNote?.trim() && <button role="menuitem" className={`${menuButton} text-red-300`} onClick={() => { edit(entry.driverId, "trailerNote", ""); setOpen(null); }}>Delete note</button>}
        <button role="menuitem" className={menuButton} onClick={() => { setOpen(null); onHistory(); }}>View trailer history</button>
      </> : <textarea aria-label={`Note for trailer ${entry.trailerNumber}`} readOnly={disabled} maxLength={5000} rows={4} value={entry.trailerNote ?? ""} placeholder="Add a note…"
        onChange={event => edit(entry.driverId, "trailerNote", event.target.value)} className="block w-full resize-y rounded border border-zinc-700 bg-zinc-950 p-2 text-sm text-zinc-200 outline-none focus:border-blue-500" />}
    </div>, document.body)}
  </td>;
}
