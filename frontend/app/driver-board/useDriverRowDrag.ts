"use client";

import { useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";

type Driver = { id: string; dispatcherId: string };
type Move = (id: string, target: string, after: boolean, revision: string) => Promise<boolean>;

// Keep DOM order stable during the gesture and save. Transforms move the actual
// rows, including their sticky cells; no native drag image can escape the group.
export function useDriverRowDrag<T extends Driver>(drivers: T[], move: Move) {
  const [frozen, setFrozen] = useState<T[] | null>(null);
  const cleanup = useRef<(() => void) | null>(null);
  useLayoutEffect(() => {
    if (!frozen) { cleanup.current?.(); cleanup.current = null; }
  }, [frozen]);
  useLayoutEffect(() => () => { cleanup.current?.(); cleanup.current = null; }, []);

  function start(event: ReactPointerEvent<HTMLButtonElement>, driver: T, revision: string) {
    if (event.button !== 0 || !event.isPrimary || cleanup.current) return;
    const table = event.currentTarget.closest("table");
    if (!table) return;
    event.preventDefault(); event.stopPropagation();
    const tableTop = table.getBoundingClientRect().top;
    const elements = new Map(Array.from(table.querySelectorAll<HTMLTableRowElement>("tr[data-driver-id]")).map(row => [row.dataset.driverId, row]));
    const rows = drivers.filter(d => d.dispatcherId === driver.dispatcherId).map(d => {
      const element = elements.get(d.id)!;
      const box = element.getBoundingClientRect();
      return { id: d.id, element, top: box.top - tableTop, height: box.height, style: element.getAttribute("style") };
    });
    const sourceIndex = rows.findIndex(row => row.id === driver.id);
    const source = rows[sourceIndex];
    if (!source || rows.length < 2) return;
    const rest = rows.filter(row => row !== source);
    const min = rows[0].top, max = rows.at(-1)!.top + rows.at(-1)!.height - source.height;
    const grabOffset = event.clientY - tableTop - source.top;
    const pointerId = event.pointerId;
    const duration = matchMedia("(prefers-reduced-motion: reduce)").matches ? 0 : 180;
    const transition = `transform ${duration}ms cubic-bezier(.2,.8,.2,1)`;
    const scroller = table.closest<HTMLElement>('[data-scroll-key="driver-board"]');
    let y = event.clientY, destination = sourceIndex, pointerActive = true, disposed = false, frame = 0;
    let preview = rows;
    let previousFrame = performance.now();
    const delay = () => new Promise(resolve => setTimeout(resolve, duration));
    const paint = (dragTop?: number) => {
      let top = min;
      for (const row of preview) {
        row.element.style.transform = `translate3d(0,${(row === source && dragTop !== undefined ? dragTop : top) - row.top}px,0)`;
        top += row.height;
      }
    };
    const update = () => {
      const top = Math.max(min, Math.min(max, y - table.getBoundingClientRect().top - grabOffset));
      // Choose the nearest available slot, also for rows with unequal heights.
      let slot = min, distance = Infinity;
      for (let i = 0; i <= rest.length; i++) {
        if (Math.abs(top - slot) < distance) { destination = i; distance = Math.abs(top - slot); }
        if (rest[i]) slot += rest[i].height;
      }
      preview = [...rest]; preview.splice(destination, 0, source);
      paint(top);
    };
    const tick = (now: number) => {
      if (!pointerActive) return;
      const elapsed = Math.min(32, now - previousFrame); previousFrame = now;
      if (scroller) {
        const box = scroller.getBoundingClientRect(), top = table.getBoundingClientRect().top;
        const header = table.tHead?.getBoundingClientRect().height ?? 0;
        const upper = box.top + header + 32, lower = box.bottom - 32;
        const speed = y < upper && top + min < upper ? -Math.min(1, (upper - y) / 32)
          : y > lower && top + max + source.height > lower ? Math.min(1, (y - lower) / 32) : 0;
        scroller.scrollTop += speed * elapsed * 0.6;
      }
      update(); frame = requestAnimationFrame(tick);
    };
    const stopListening = () => {
      pointerActive = false; cancelAnimationFrame(frame);
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onCancel);
      window.removeEventListener("keydown", onKey, true);
      window.removeEventListener("blur", onCancel);
      window.removeEventListener("resize", onCancel);
      table.removeEventListener("lostpointercapture", onCancel);
      if (table.hasPointerCapture(pointerId)) table.releasePointerCapture(pointerId);
    };
    const finish = async (cancel: boolean) => {
      if (!pointerActive) return;
      if (!cancel) update();
      stopListening();
      source.element.style.transition = transition;
      if (cancel) preview = rows;
      paint();
      const target = destination < sourceIndex ? preview[destination + 1] : preview[destination - 1];
      const saving = !cancel && destination !== sourceIndex && target
        ? move(source.id, target.id, destination > sourceIndex, revision) : Promise.resolve(true);
      const [saved] = await Promise.all([saving, delay()]);
      if (disposed) return;
      if (!saved) { preview = rows; paint(); await delay(); }
      if (!disposed) setFrozen(null);
    };
    const onMove = (e: PointerEvent) => { if (e.pointerId === pointerId) { y = e.clientY; e.preventDefault(); update(); } };
    const onUp = (e: PointerEvent) => { if (e.pointerId === pointerId) { y = e.clientY; void finish(false); } };
    const onCancel = () => { void finish(true); };
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); onCancel(); } };
    cleanup.current = () => {
      disposed = true; stopListening();
      for (const row of rows) {
        if (row.style === null) row.element.removeAttribute("style"); else row.element.setAttribute("style", row.style);
        delete row.element.dataset.rowDragging;
      }
    };
    for (const row of rows) row.element.style.transition = transition;
    source.element.style.transition = "none";
    source.element.style.position = "relative";
    source.element.style.zIndex = "15";
    source.element.style.backgroundColor = "rgb(9, 9, 11)";
    source.element.style.boxShadow = "0 0 0 2px #60a5fa";
    source.element.style.cursor = "grabbing";
    source.element.dataset.rowDragging = "true";
    table.setPointerCapture(pointerId);
    window.addEventListener("pointermove", onMove, { passive: false });
    window.addEventListener("pointerup", onUp);
    window.addEventListener("pointercancel", onCancel);
    window.addEventListener("keydown", onKey, true);
    window.addEventListener("blur", onCancel);
    window.addEventListener("resize", onCancel);
    table.addEventListener("lostpointercapture", onCancel);
    setFrozen(drivers);
    update(); frame = requestAnimationFrame(tick);
  }
  return { drivers: frozen ?? drivers, active: frozen !== null, start };
}
