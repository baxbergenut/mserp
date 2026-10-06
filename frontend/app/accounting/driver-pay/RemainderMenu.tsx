"use client";

import { useEffect, useRef } from "react";
import { createPortal } from "react-dom";

export function RemainderMenu({ x, y, waived, onChange, onClose }: { x: number; y: number; waived: boolean; onChange: (waive: boolean) => void; onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    ref.current?.querySelector<HTMLButtonElement>("button")?.focus();
    const dismiss = (event: PointerEvent) => { if (!ref.current?.contains(event.target as Node)) onClose(); };
    const key = (event: KeyboardEvent) => { if (event.key === "Escape") { event.preventDefault(); onClose(); } };
    document.addEventListener("pointerdown", dismiss);
    document.addEventListener("keydown", key);
    window.addEventListener("resize", onClose);
    return () => { document.removeEventListener("pointerdown", dismiss); document.removeEventListener("keydown", key); window.removeEventListener("resize", onClose); previous?.focus(); };
  }, [onClose]);
  return createPortal(<div ref={ref} role="menu" aria-label="Recurring charge remainder" style={{ left: Math.max(8, Math.min(x, window.innerWidth - 288)), top: Math.max(8, Math.min(y, window.innerHeight - 108)) }} className="fixed z-[100] w-[280px] rounded-lg border border-zinc-700 bg-zinc-900 p-1 shadow-xl" onKeyDown={event => {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") { event.preventDefault(); const buttons = Array.from(ref.current?.querySelectorAll("button") ?? []); const at = buttons.indexOf(document.activeElement as HTMLButtonElement); buttons[(at + 1) % buttons.length]?.focus(); }
    if (event.key === "Tab") onClose();
  }}>
    <button role="menuitemradio" aria-checked={!waived} className="w-full rounded px-3 py-2 text-left text-xs text-zinc-200 hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none" onClick={() => { onChange(false); onClose(); }}>Carry unpaid amount to next week</button>
    <button role="menuitemradio" aria-checked={waived} className="w-full rounded px-3 py-2 text-left text-xs text-zinc-200 hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none" onClick={() => { onChange(true); onClose(); }}>Use this amount for this week only<span className="mt-1 block text-[10px] text-zinc-500">No remainder; future weekly rate stays the same</span></button>
  </div>, document.body);
}
