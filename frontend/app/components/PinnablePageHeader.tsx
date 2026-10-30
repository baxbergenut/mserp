"use client";

import { useLayoutEffect, useRef, type ReactNode } from "react";
import { Pin, PinOff } from "lucide-react";

// Boards reserve this space above their independently scrolling table headers.
export function pinnedHeaderClearance(main: HTMLElement) {
  const header = main.querySelector<HTMLElement>('[data-pinned-page-header="true"]');
  if (!header) return 8;
  const style = getComputedStyle(header);
  return (parseFloat(style.top) || 0) + header.offsetHeight + (parseFloat(style.marginBottom) || 0);
}

export function PinnablePageHeader({ pinned, children }: { pinned: boolean; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const header = ref.current, main = header?.closest("main");
    if (!header || !main) return;
    // Wrapped cards/filters must leave usable space for rows on small screens.
    const resize = () => {
      const inset = main.firstElementChild ? parseFloat(getComputedStyle(main.firstElementChild).paddingTop) || 0 : 0;
      // Stick at the original page inset, with no initial slide toward navigation.
      header.style.top = pinned ? `${inset}px` : "";
      header.style.boxShadow = pinned ? `0 -${inset}px 0 var(--background)` : "";
      header.style.maxHeight = pinned ? `${Math.floor(main.clientHeight / 2)}px` : "";
    };
    const observer = new ResizeObserver(resize);
    observer.observe(main);
    resize();
    return () => observer.disconnect();
  }, [pinned]);
  return <div ref={ref} data-pinned-page-header={pinned} className={`space-y-5 ${pinned ? "sticky z-40 overflow-y-auto bg-background pb-2" : ""}`}>{children}</div>;
}

export function HeaderPinButton({ pinned, onToggle }: { pinned: boolean; onToggle: () => void }) {
  const Icon = pinned ? PinOff : Pin;
  return <button type="button" className="ui-button inline-flex shrink-0 items-center gap-2"
    aria-label={pinned ? "Unpin header cards and filters" : "Pin header cards and filters"}
    aria-pressed={pinned} title={pinned ? "Let header cards and filters scroll with the page" : "Keep header cards and filters visible while scrolling"}
    onClick={onToggle}><Icon className="h-4 w-4" /><span>{pinned ? "Unpin header" : "Pin header"}</span></button>;
}
