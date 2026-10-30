"use client";

import { useLayoutEffect, useRef, type ReactNode } from "react";
import { pinnedHeaderClearance } from "@/app/components/PinnablePageHeader";

// Size the table to one main viewport. This makes main's natural
// scroll limit land at the table header, without changing the shared shell.
export function StatusBoardScrollArea({ children, loading, headerPinned }: {
  children: ReactNode; loading: boolean; headerPinned: boolean;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const tableRef = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const area = ref.current, table = tableRef.current;
    const main = area?.closest("main");
    if (!area || !table || !main) return;
    const resize = () => {
      const bottom = main.firstElementChild ? parseFloat(getComputedStyle(main.firstElementChild).paddingBottom) || 0 : 0;
      area.style.height = `${Math.max(1, main.clientHeight - bottom - pinnedHeaderClearance(main))}px`;
    };
    const observer = new ResizeObserver(resize);
    observer.observe(main);
    const pageHeader = main.querySelector<HTMLElement>("[data-pinned-page-header]");
    if (pageHeader) observer.observe(pageHeader);
    if (main.firstElementChild) observer.observe(main.firstElementChild);
    resize();

    const ignored = (target: EventTarget | null) => target instanceof Element && (
      !!target.closest('textarea,select,[contenteditable="true"],[popover],[role="dialog"],[data-grid-ignore]')
      || !!(pageHeader?.contains(target) && pageHeader.scrollHeight > pageHeader.clientHeight + 1));
    const scroll = (delta: number) => {
      const dock = Math.max(0, Math.min(main.scrollHeight - main.clientHeight,
        main.scrollTop + area.getBoundingClientRect().top - main.getBoundingClientRect().top - pinnedHeaderClearance(main)));
      let remaining = delta;
      if (remaining > 0) {
        const page = Math.min(remaining, Math.max(0, dock - main.scrollTop));
        main.scrollTop += page; remaining -= page;
        table.scrollTop += remaining;
      } else {
        const rows = Math.min(-remaining, table.scrollTop);
        table.scrollTop -= rows; remaining += rows;
        main.scrollTop += remaining;
      }
    };
    const wheel = (event: WheelEvent) => {
      if (event.ctrlKey || event.metaKey || event.shiftKey || ignored(event.target) || Math.abs(event.deltaX) >= Math.abs(event.deltaY)) return;
      event.preventDefault();
      scroll(event.deltaY * (event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? main.clientHeight : 1));
    };
    let touch: { x: number; y: number } | null = null;
    const start = (event: TouchEvent) => {
      touch = event.touches.length === 1 && !ignored(event.target) ? { x: event.touches[0].clientX, y: event.touches[0].clientY } : null;
    };
    const move = (event: TouchEvent) => {
      if (!touch || event.touches.length !== 1) return;
      const next = event.touches[0], delta = touch.y - next.clientY, horizontal = touch.x - next.clientX;
      touch = { x: next.clientX, y: next.clientY };
      if (Math.abs(horizontal) >= Math.abs(delta)) return;
      event.preventDefault(); scroll(delta);
    };
    const end = () => { touch = null; };
    main.addEventListener("wheel", wheel, { passive: false });
    main.addEventListener("touchstart", start, { passive: true });
    main.addEventListener("touchmove", move, { passive: false });
    main.addEventListener("touchend", end); main.addEventListener("touchcancel", end);
    return () => {
      observer.disconnect();
      main.removeEventListener("wheel", wheel); main.removeEventListener("touchstart", start);
      main.removeEventListener("touchmove", move); main.removeEventListener("touchend", end); main.removeEventListener("touchcancel", end);
    };
  }, [headerPinned]);
  return <div ref={ref} data-status-board-scroll className="flex min-h-0 flex-col">
    <div ref={tableRef} data-scroll-key="driver-board" className="min-h-0 flex-1 overflow-auto overscroll-contain rounded-xl border border-zinc-800" aria-busy={loading}>
      {children}
    </div>
  </div>;
}
