"use client";

import { useId, useState } from "react";
import { createPortal } from "react-dom";
import { MessageSquarePlus, MessageSquareText } from "lucide-react";

export function CommentButton({ label, value, disabled, onClick }: {
  label: string;
  value: string;
  disabled?: boolean;
  onClick: () => void;
}) {
  const id = useId();
  const [position, setPosition] = useState<{ top: number; left: number; above: boolean } | null>(null);
  const show = (element: HTMLButtonElement) => {
    const rect = element.getBoundingClientRect();
    const above = rect.top > 280;
    setPosition({ top: above ? rect.top - 8 : rect.bottom + 8, above, left: Math.max(8, Math.min(rect.right - 288, window.innerWidth - 296)) });
  };
  const Icon = value.trim() ? MessageSquareText : MessageSquarePlus;
  return <>
    <button type="button" aria-label={label} aria-describedby={position ? id : undefined} disabled={disabled}
      onMouseEnter={event => show(event.currentTarget)} onMouseLeave={() => setPosition(null)}
      onFocus={event => show(event.currentTarget)} onBlur={() => setPosition(null)}
      onClick={() => { setPosition(null); onClick(); }}
      className={`inline-flex h-7 w-7 items-center justify-center rounded hover:bg-zinc-700/50 focus-visible:outline-2 focus-visible:outline-blue-500 disabled:opacity-40 ${value.trim() ? "text-blue-300" : "text-zinc-600 hover:text-zinc-300"}`}>
      <Icon className="h-3.5 w-3.5" />
    </button>
    {position && createPortal(<div id={id} role="tooltip" style={{ top: position.top, left: position.left }}
      className={`pointer-events-none fixed z-[60] max-h-64 w-72 overflow-hidden whitespace-pre-wrap break-words rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2 text-xs leading-relaxed text-zinc-200 shadow-xl ${position.above ? "-translate-y-full" : ""}`}>
      {value.trim() || "Add comment"}
    </div>, document.body)}
  </>;
}
