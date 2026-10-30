"use client";

import { useLayoutEffect, useRef, type ReactNode } from "react";
import { pinnedHeaderClearance } from "@/app/components/PinnablePageHeader";

// Main scrolls only far enough to put the table 8px below navigation. A bounded
// table viewport then owns the remaining scroll, with a native sticky header.
export function BoardScrollArea({ children, headerPinned }: { children: ReactNode; headerPinned: boolean }) {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const area = ref.current;
    const main = area?.closest("main");
    const table = area?.querySelector("table");
    const header = table?.tHead;
    if (!area || !main || !table || !header) return;
    let frame = 0;
    let outerLimit = 0;
    const update = () => {
      frame = 0;
      const pinned = outerLimit > 0 && main.scrollTop >= outerLimit - 1;
      const bottomPadding = main.firstElementChild ? parseFloat(getComputedStyle(main.firstElementChild).paddingBottom) || 0 : 0;
      area.style.maxHeight = `${Math.max(header.offsetHeight + 32, main.clientHeight - pinnedHeaderClearance(main) + area.clientTop - bottomPadding)}px`;
      area.style.setProperty("--board-header-clearance", `${header.offsetHeight + 8}px`);
      outerLimit = main.scrollHeight - main.clientHeight;
      if (pinned) main.scrollTop = outerLimit;
    };
    const schedule = () => { if (!frame) frame = requestAnimationFrame(update); };
    const scroll = (delta: number) => {
      if (delta > 0) {
        const outer = Math.min(delta, Math.max(0, main.scrollHeight - main.clientHeight - main.scrollTop));
        main.scrollTop += outer;
        area.scrollTop += delta - outer;
      } else {
        const inner = Math.min(-delta, area.scrollTop);
        area.scrollTop -= inner;
        main.scrollTop += delta + inner;
      }
    };
    // Leave dialogs, suggestion menus and other nested scrollers in control.
    const nested = (target: EventTarget | null) => {
      if (!(target instanceof Element) || target.closest("dialog,[role=dialog],[popover],[role=listbox],textarea,select")) return true;
      for (let node = target instanceof HTMLElement ? target : target.parentElement; node && node !== area && node !== main; node = node.parentElement) {
        if (node.scrollHeight > node.clientHeight + 1 && /auto|scroll/.test(getComputedStyle(node).overflowY)) return true;
      }
      return false;
    };
    const wheel = (event: WheelEvent) => {
      if (event.ctrlKey || event.shiftKey || Math.abs(event.deltaX) > Math.abs(event.deltaY) || !event.deltaY || nested(event.target)) return;
      event.preventDefault();
      scroll(event.deltaY * (event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? area.clientHeight : 1));
    };
    const keydown = (event: KeyboardEvent) => {
      if (event.target !== area && event.target !== main || event.ctrlKey || event.metaKey || event.altKey) return;
      const step = Math.max(40, area.clientHeight - header.offsetHeight);
      const full = main.scrollHeight + area.scrollHeight;
      const delta = event.key === "ArrowDown" ? 40 : event.key === "ArrowUp" ? -40
        : event.key === "PageDown" || event.key === " " && !event.shiftKey ? step
        : event.key === "PageUp" || event.key === " " && event.shiftKey ? -step
        : event.key === "Home" ? -full : event.key === "End" ? full : 0;
      if (!delta) return;
      event.preventDefault();
      scroll(delta);
    };
    let touch: { x: number; y: number } | null = null;
    const touchstart = (event: TouchEvent) => {
      touch = event.touches.length === 1 ? { x: event.touches[0].clientX, y: event.touches[0].clientY } : null;
    };
    const touchmove = (event: TouchEvent) => {
      if (!touch || event.touches.length !== 1) return;
      const next = { x: event.touches[0].clientX, y: event.touches[0].clientY };
      const delta = touch.y - next.y;
      const horizontal = Math.abs(touch.x - next.x) > Math.abs(delta);
      touch = next;
      if (horizontal || !delta || nested(event.target)) return;
      event.preventDefault();
      scroll(delta);
    };
    const observer = new ResizeObserver(schedule);
    observer.observe(area);
    observer.observe(main);
    const pageHeader = main.querySelector("[data-pinned-page-header]");
    if (pageHeader) observer.observe(pageHeader);
    if (main.firstElementChild) observer.observe(main.firstElementChild);
    main.addEventListener("scroll", schedule, { passive: true });
    main.addEventListener("wheel", wheel, { passive: false });
    main.addEventListener("keydown", keydown);
    main.addEventListener("touchstart", touchstart, { passive: true });
    main.addEventListener("touchmove", touchmove, { passive: false });
    main.addEventListener("animationend", schedule, true);
    window.addEventListener("resize", schedule);
    update();
    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
      main.removeEventListener("scroll", schedule);
      main.removeEventListener("wheel", wheel);
      main.removeEventListener("keydown", keydown);
      main.removeEventListener("touchstart", touchstart);
      main.removeEventListener("touchmove", touchmove);
      main.removeEventListener("animationend", schedule, true);
      window.removeEventListener("resize", schedule);
    };
  }, [headerPinned]);

  return <div ref={ref} data-scroll-key="gross-board" className="weekly-content-enter overflow-auto rounded-xl border border-zinc-800 [&_td]:scroll-mt-[var(--board-header-clearance)] [&_th]:scroll-mt-[var(--board-header-clearance)] [&_[data-grid-cell]]:scroll-mt-[var(--board-header-clearance)]"
    role="region" aria-label="Weekly gross board" tabIndex={0}>{children}</div>;
}
