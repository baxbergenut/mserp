"use client";

import { useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";

import type { PTIGroup, PTIMatrix } from "@/app/lib/types";
type Truck = PTIMatrix["trucks"][number];
type Drop = {group: PTIGroup; target?: string; after: boolean};
type Move = (id: string, drop: Drop, version: number) => Promise<boolean>;

// Keep DOM order stable during the gesture and save. Transforms move the actual
// rows and group headers, including sticky cells, with the Status Board easing.
export function usePTIRowDrag(drivers: Truck[], move: Move) {
  const [frozen, setFrozen] = useState<Truck[] | null>(null);
  const cleanup = useRef<(() => void) | null>(null);
  useLayoutEffect(() => {
    if (!frozen) { cleanup.current?.(); cleanup.current = null; }
  }, [frozen]);
  useLayoutEffect(() => () => { cleanup.current?.(); cleanup.current = null; }, []);

  function start(event: ReactPointerEvent<HTMLButtonElement>, driver: Truck, revision: number) {
    if (event.button !== 0 || !event.isPrimary || cleanup.current) return;
    const table = event.currentTarget.closest("table");
    if (!table) return;
    event.preventDefault(); event.stopPropagation();
    const tableTop = table.getBoundingClientRect().top;
    // Animate group headers too, opening a slot when a truck crosses groups.
    const rows = Array.from(table.querySelectorAll<HTMLTableRowElement>("tr[data-pti-truck],tr[data-pti-group]")).map(element => {
      const box = element.getBoundingClientRect();
      return { id: element.dataset.ptiTruck, group: element.dataset.ptiGroup as PTIGroup | undefined, element, top: box.top - tableTop, height: box.height, style: element.getAttribute("style") };
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
    const scroller = table.parentElement;
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
      const top = Math.max(min + rows[0].height, Math.min(max, y - table.getBoundingClientRect().top - grabOffset));
      // Choose the nearest available slot, also for rows with unequal heights.
      let slot = min, distance = Infinity;
      for (let i = 0; i <= rest.length; i++) {
        if (i > 0 && Math.abs(top - slot) < distance) { destination = i; distance = Math.abs(top - slot); }
        if (rest[i]) slot += rest[i].height;
      }
      const pointerTop = y - table.getBoundingClientRect().top;
      const headerIndex = rest.findIndex(row => row.group && pointerTop >= row.top && pointerTop < row.top + row.height);
      if (headerIndex >= 0) destination = headerIndex + 1;
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
      const group = preview.slice(0, destination).findLast(row => row.group)?.group;
      const next = preview[destination + 1], previous = preview[destination - 1];
      const target = next?.id ? next : previous?.id ? previous : undefined;
      const saving = !cancel && destination !== sourceIndex && group
        ? move(driver.id, {group, target: target?.id, after: target === previous}, revision) : Promise.resolve(true);
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
    source.element.style.backgroundColor = getComputedStyle(source.element.cells[0]).backgroundColor;
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
  return { trucks: frozen ?? drivers, active: frozen !== null, start };
}
