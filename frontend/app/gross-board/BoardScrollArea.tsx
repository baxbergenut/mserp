"use client";

import { useLayoutEffect, useRef, type ReactNode } from "react";

// Horizontal overflow creates a CSS sticky containing block. Move the original
// header within that full-height wrapper while main owns vertical scrolling.
export function BoardScrollArea({ children }: { children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const area = ref.current;
    const main = area?.closest("main");
    const table = area?.querySelector("table");
    const header = table?.tHead;
    if (!area || !main || !table || !header) return;
    let frame = 0;
    const update = () => {
      frame = 0;
      const top = area.getBoundingClientRect().top + area.clientTop;
      const target = main.getBoundingClientRect().top + 8;
      const offset = Math.max(0, Math.min(target - top, table.offsetHeight - header.offsetHeight));
      header.style.transform = `translateY(${offset}px)`;
      area.style.setProperty("--board-header-clearance", `${header.offsetHeight + 8}px`);
    };
    const schedule = () => { if (!frame) frame = requestAnimationFrame(update); };
    const observer = new ResizeObserver(schedule);
    observer.observe(area);
    observer.observe(main);
    if (main.firstElementChild) observer.observe(main.firstElementChild);
    main.addEventListener("scroll", schedule, { passive: true });
    // Page entrance transforms can finish after Back restores its scroll offset.
    main.addEventListener("animationend", schedule, true);
    window.addEventListener("resize", schedule);
    update();
    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
      main.removeEventListener("scroll", schedule);
      main.removeEventListener("animationend", schedule, true);
      window.removeEventListener("resize", schedule);
    };
  }, []);

  return <div ref={ref} data-scroll-key="gross-board" className="weekly-content-enter overflow-x-auto rounded-xl border border-zinc-800 [&_td]:scroll-mt-[var(--board-header-clearance)] [&_th]:scroll-mt-[var(--board-header-clearance)] [&_[data-grid-cell]]:scroll-mt-[var(--board-header-clearance)]"
    role="region" aria-label="Weekly gross board" tabIndex={0}>{children}</div>;
}
