"use client";

import { useEffect, useRef } from "react";
import { createPortal } from "react-dom";

export function StatementMenu({ x, y, label, disabled, onSelect, onClose }: { x: number; y: number; label: string; disabled?: boolean; onSelect: () => void; onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    ref.current?.querySelector<HTMLButtonElement>("button")?.focus({ preventScroll: true });
    const dismiss = (event: PointerEvent) => { if (!ref.current?.contains(event.target as Node)) onClose(); };
    const key = (event: KeyboardEvent) => { if (event.key === "Escape") { event.preventDefault(); onClose(); } };
    document.addEventListener("pointerdown", dismiss);
    document.addEventListener("keydown", key);
    window.addEventListener("resize", onClose);
    window.addEventListener("wheel", onClose, { passive: true });
    return () => {
      document.removeEventListener("pointerdown", dismiss);
      document.removeEventListener("keydown", key);
      window.removeEventListener("resize", onClose);
      window.removeEventListener("wheel", onClose);
      previous?.focus({ preventScroll: true });
    };
  }, [onClose]);
  return createPortal(<div ref={ref} role="menu" aria-label="Statement actions" className="mserp-ui fixed z-[100] w-44 rounded-lg border border-zinc-700 bg-zinc-900 p-1 shadow-xl" style={{ left: Math.max(8, Math.min(x, window.innerWidth - 184)), top: Math.max(8, Math.min(y, window.innerHeight - 48)) }} onClick={event => event.stopPropagation()} onContextMenu={event => event.preventDefault()} onKeyDown={event => { if (event.key === "Tab") onClose(); }}>
    <button type="button" role="menuitem" aria-disabled={disabled} className="w-full rounded px-3 py-2 text-left text-xs text-zinc-200 hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none aria-disabled:opacity-40" onClick={() => { if (!disabled) { onClose(); onSelect(); } }}>{label}</button>
  </div>, document.body);
}
