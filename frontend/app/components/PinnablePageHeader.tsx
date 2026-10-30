"use client";

import { useLayoutEffect, useRef, type ReactNode } from "react";
import { Pin, PinOff } from "lucide-react";

// Boards reserve this space above their independently scrolling table headers.
export function pinnedHeaderClearance(main: HTMLElement) {
  const header = main.querySelector<HTMLElement>('[data-pinned-page-header="true"]');
  return (header?.offsetHeight ?? 0) + 8;
}

export function PinnablePageHeader({ pinned, children }: { pinned: boolean; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const header = ref.current, main = header?.closest("main");
    if (!header || !main) return;
    // Wrapped cards/filters must leave usable space for rows on small screens.
    const resize = () => { header.style.maxHeight = pinned ? `${Math.floor(main.clientHeight / 2)}px` : ""; };
    const observer = new ResizeObserver(resize);
    observer.observe(main);
    resize();
    return () => observer.disconnect();
  }, [pinned]);
  return <div ref={ref} data-pinned-page-header={pinned} className={`space-y-5 ${pinned ? "sticky top-0 z-40 overflow-y-auto bg-background pb-2" : ""}`}>{children}</div>;
}

export function HeaderPinButton({ pinned, onToggle }: { pinned: boolean; onToggle: () => void }) {
  const Icon = pinned ? PinOff : Pin;
  return <button type="button" className="ui-button inline-flex shrink-0 items-center gap-2"
    aria-label={pinned ? "Unpin header cards and filters" : "Pin header cards and filters"}
    aria-pressed={pinned} title={pinned ? "Let header cards and filters scroll with the page" : "Keep header cards and filters visible while scrolling"}
    onClick={onToggle}><Icon className="h-4 w-4" /><span>{pinned ? "Unpin header" : "Pin header"}</span></button>;
}
