import { useLayoutEffect, useRef, useState, type MouseEvent, type PointerEvent } from "react";
import type { BoardLoad } from "@/app/lib/types";

export function useLoadQueueDrag(
  queue: BoardLoad[],
  revision: string,
  locked: boolean,
  saveOrder: (order: string[]) => Promise<boolean>,
) {
  const sequence = useRef<HTMLElement>(null);
  const cleanup = useRef<(() => void) | null>(null);
  const latest = useRef({ revision, locked });
  const suppressClick = useRef(false);
  const [frozen, setFrozen] = useState<BoardLoad[] | null>(null);
  useLayoutEffect(() => {
    latest.current = { revision, locked };
    if (!frozen) { cleanup.current?.(); cleanup.current = null; }
  }, [revision, locked, frozen]);
  useLayoutEffect(() => () => { cleanup.current?.(); cleanup.current = null; }, []);

  function start(event: PointerEvent<HTMLElement>, id: string) {
    const list = sequence.current;
    const target = event.target;
    if (!list || !(target instanceof Element) || target.closest("[data-load-copy]") ||
        locked || queue.length < 2 || cleanup.current || !event.isPrimary || event.button !== 0) return;
    const pointerTarget = target;
    const region = list;
    const listTop = list.getBoundingClientRect().top;
    const cards = Array.from(list.querySelectorAll<HTMLElement>("[data-queued-load]")).map(element => {
      const box = element.getBoundingClientRect();
      return { element, id: element.dataset.queuedLoad!, top: box.top - listTop, height: box.height };
    });
    const sourceIndex = cards.findIndex(card => card.id === id);
    const source = cards[sourceIndex];
    if (!source) return;
    const others = cards.filter(card => card !== source);
    const gap = cards[1].top - cards[0].top - cards[0].height;
    const min = cards[0].top;
    const last = cards[cards.length - 1];
    const max = last.top + last.height - source.height;
    const pointerId = event.pointerId;
    const startY = event.clientY;
    const offset = startY - listTop - source.top;
    const scroller = list.closest<HTMLElement>("[data-load-scroll]");
    const duration = matchMedia("(prefers-reduced-motion: reduce)").matches ? 0 : 180;
    const transition = `transform ${duration}ms cubic-bezier(.2,.8,.2,1)`;
    const delay = () => new Promise(resolve => setTimeout(resolve, duration));
    let y = startY, destination = sourceIndex, frame = 0, previousFrame = performance.now();
    let moving = false, finished = false, disposed = false, preview = cards;

    function paint(dragTop?: number) {
      let top = min;
      for (const card of preview) {
        const position = card === source && dragTop !== undefined ? dragTop : top;
        card.element.style.transform = `translate3d(0,${position - card.top}px,0)`;
        top += card.height + gap;
      }
    }
    function update() {
      const top = Math.max(min, Math.min(max, y - region.getBoundingClientRect().top - offset));
      let slot = min, distance = Infinity;
      for (let index = 0; index <= others.length; index++) {
        if (Math.abs(top - slot) < distance) { destination = index; distance = Math.abs(top - slot); }
        if (others[index]) slot += others[index].height + gap;
      }
      preview = [...others];
      preview.splice(destination, 0, source);
      paint(top);
    }
    function tick(now: number) {
      if (finished) return;
      const elapsed = Math.min(32, now - previousFrame);
      previousFrame = now;
      if (scroller) {
        const box = scroller.getBoundingClientRect();
        const upper = box.top + 32, lower = box.bottom - 32;
        const speed = y < upper ? -Math.min(1, (upper - y) / 32)
          : y > lower ? Math.min(1, (y - lower) / 32) : 0;
        scroller.scrollTop += speed * elapsed * 0.6;
      }
      update();
      frame = requestAnimationFrame(tick);
    }
    function stopListening() {
      cancelAnimationFrame(frame);
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onPointerCancel);
      window.removeEventListener("keydown", onKey, true);
      window.removeEventListener("blur", onCancel);
      window.removeEventListener("resize", onCancel);
      pointerTarget.removeEventListener("lostpointercapture", onCancel);
      if (pointerTarget.hasPointerCapture(pointerId)) pointerTarget.releasePointerCapture(pointerId);
    }
    async function finish(cancel: boolean) {
      if (finished) return;
      finished = true;
      stopListening();
      if (!moving) { cleanup.current = null; return; }
      // Ignore the click synthesized when a pointer drag ends on a card control.
      setTimeout(() => { suppressClick.current = false; }, 0);
      if (!cancel) update();
      cancel ||= latest.current.locked || latest.current.revision !== revision;
      source.element.style.transition = transition;
      if (cancel) preview = cards;
      paint();
      const saving = !cancel && destination !== sourceIndex
        ? saveOrder(preview.map(card => card.id).reverse())
        : Promise.resolve(true);
      const [saved] = await Promise.all([saving, delay()]);
      if (disposed) return;
      if (!saved) { preview = cards; paint(); await delay(); }
      if (!disposed) setFrozen(null);
    }
    function onMove(pointer: globalThis.PointerEvent) {
      if (pointer.pointerId !== pointerId) return;
      y = pointer.clientY;
      if (!moving) {
        if (Math.abs(y - startY) < 5) return;
        if (latest.current.locked || latest.current.revision !== revision || !pointerTarget.isConnected) {
          onCancel();
          return;
        }
        moving = true;
        suppressClick.current = true;
        setFrozen(queue);
        for (const card of cards) card.element.style.transition = transition;
        source.element.style.transition = "none";
        source.element.dataset.loadDragging = "true";
        pointerTarget.setPointerCapture(pointerId);
        pointerTarget.addEventListener("lostpointercapture", onCancel);
        frame = requestAnimationFrame(tick);
      }
      pointer.preventDefault();
      update();
    }
    function onUp(pointer: globalThis.PointerEvent) {
      if (pointer.pointerId !== pointerId) return;
      y = pointer.clientY;
      void finish(false);
    }
    function onCancel() { void finish(true); }
    function onPointerCancel(pointer: globalThis.PointerEvent) {
      if (pointer.pointerId === pointerId) onCancel();
    }
    function onKey(key: KeyboardEvent) {
      if (key.key !== "Escape" || !moving) return;
      key.preventDefault();
      key.stopPropagation();
      onCancel();
    }

    cleanup.current = () => {
      disposed = true;
      stopListening();
      for (const card of cards) {
        card.element.style.removeProperty("transform");
        card.element.style.removeProperty("transition");
      }
      delete source.element.dataset.loadDragging;
    };
    window.addEventListener("pointermove", onMove, { passive: false });
    window.addEventListener("pointerup", onUp);
    window.addEventListener("pointercancel", onPointerCancel);
    window.addEventListener("keydown", onKey, true);
    window.addEventListener("blur", onCancel);
    window.addEventListener("resize", onCancel);
  }

  function onClickCapture(event: MouseEvent<HTMLElement>) {
    if (!suppressClick.current || event.detail === 0) return;
    event.preventDefault();
    event.stopPropagation();
  }

  return { sequence, queue: frozen ?? queue, start, onClickCapture };
}
