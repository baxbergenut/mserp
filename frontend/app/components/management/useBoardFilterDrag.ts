"use client";

import { useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";

type Option = [string, string];
type Move = (id: string, target: string, after: boolean) => Promise<boolean>;

export function useBoardFilterDrag(options: Option[], move?: Move, pinnedFirst?: string) {
  const [frozen, setFrozen] = useState<Option[] | null>(null);
  const cleanup = useRef<(() => void) | null>(null);
  useLayoutEffect(() => {
    if (!frozen) { cleanup.current?.(); cleanup.current = null; }
  }, [frozen]);
  useLayoutEffect(() => () => { cleanup.current?.(); cleanup.current = null; }, []);

  function start(event: ReactPointerEvent<HTMLButtonElement>, id: string) {
    if (!move || event.button !== 0 || !event.isPrimary || cleanup.current) return;
    const list = event.currentTarget.closest<HTMLElement>("[data-filter-options]");
    if (!list) return;
    const elements = Array.from(list.querySelectorAll<HTMLElement>("[data-filter-option]"));
    const box = list.getBoundingClientRect();
    const rows = options.map(([key], index) => {
      const element = elements[index];
      const rect = element.getBoundingClientRect();
      return { id: key, element, top: rect.top - box.top, height: rect.height, style: element.getAttribute("style") };
    }).filter(row => row.id !== pinnedFirst);
    const sourceIndex = rows.findIndex(row => row.id === id);
    const source = rows[sourceIndex];
    if (!source || rows.length < 2) return;
    event.preventDefault(); event.stopPropagation();
    const rest = rows.filter(row => row !== source);
    const min = rows[0].top, max = rows.at(-1)!.top + rows.at(-1)!.height - source.height;
    const grabOffset = event.clientY - box.top - source.top;
    const duration = matchMedia("(prefers-reduced-motion: reduce)").matches ? 0 : 180;
    const transition = `transform ${duration}ms cubic-bezier(.2,.8,.2,1)`;
    const pointerId = event.pointerId;
    let y = event.clientY, destination = sourceIndex, active = true, disposed = false, frame = 0;
    let preview = rows;
    const delay = () => new Promise(resolve => setTimeout(resolve, duration));
    const paint = (dragTop?: number) => {
      let top = min;
      for (const row of preview) {
        row.element.style.transform = `translate3d(0,${(row === source && dragTop !== undefined ? dragTop : top) - row.top}px,0)`;
        top += row.height;
      }
    };
    const update = () => {
      const top = Math.max(min, Math.min(max, y - list.getBoundingClientRect().top - grabOffset));
      let slot = min, distance = Infinity;
      for (let i = 0; i <= rest.length; i++) {
        if (Math.abs(top - slot) < distance) { destination = i; distance = Math.abs(top - slot); }
        if (rest[i]) slot += rest[i].height;
      }
      preview = [...rest]; preview.splice(destination, 0, source);
      paint(top);
    };
    const tick = () => {
      if (!active) return;
      const panel = list.closest<HTMLElement>("[popover]");
      if (panel) {
        const rect = panel.getBoundingClientRect();
        if (y < rect.top + 30) panel.scrollTop -= 8;
        else if (y > rect.bottom - 30) panel.scrollTop += 8;
      }
      update(); frame = requestAnimationFrame(tick);
    };
    const stopListening = () => {
      active = false; cancelAnimationFrame(frame);
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onCancel);
      window.removeEventListener("keydown", onKey, true);
      window.removeEventListener("blur", onCancel);
      list.removeEventListener("lostpointercapture", onCancel);
      if (list.hasPointerCapture(pointerId)) list.releasePointerCapture(pointerId);
    };
    const finish = async (cancel: boolean) => {
      if (!active) return;
      if (!cancel) update();
      stopListening();
      source.element.style.transition = transition;
      if (cancel) preview = rows;
      paint();
      const target = destination < sourceIndex ? preview[destination + 1] : preview[destination - 1];
      const saving = !cancel && destination !== sourceIndex && target
        ? move(source.id, target.id, destination > sourceIndex) : Promise.resolve(true);
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
      }
    };
    for (const row of rows) row.element.style.transition = transition;
    source.element.style.transition = "none";
    source.element.style.position = "relative";
    source.element.style.zIndex = "15";
    source.element.style.backgroundColor = "rgb(9, 9, 11)";
    source.element.style.boxShadow = "0 0 0 2px #60a5fa";
    source.element.style.cursor = "grabbing";
    list.setPointerCapture(pointerId);
    window.addEventListener("pointermove", onMove, { passive: false });
    window.addEventListener("pointerup", onUp);
    window.addEventListener("pointercancel", onCancel);
    window.addEventListener("keydown", onKey, true);
    window.addEventListener("blur", onCancel);
    list.addEventListener("lostpointercapture", onCancel);
    setFrozen(options);
    update(); frame = requestAnimationFrame(tick);
  }
  return { options: frozen ?? options, active: frozen !== null, start };
}
