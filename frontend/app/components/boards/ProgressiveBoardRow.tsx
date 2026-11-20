"use client";

import { useEffect, useRef, useState, type ReactElement } from "react";
import { flushSync } from "react-dom";

// Mount off-screen editors in small batches, retaining them once mounted. This
// preserves native table selection, find, focus and draft state after opening.
const pending = new Map<HTMLTableRowElement, () => void>();
let timer: ReturnType<typeof setTimeout> | undefined;
let observer: IntersectionObserver | undefined;

function schedule() {
  if (timer !== undefined || !pending.size) return;
  timer = setTimeout(() => {
    timer = undefined;
    for (const activate of [...pending.values()].slice(0, 2)) activate();
    schedule();
  }, 24);
}

function finishSelection(event: Event) {
  const target = event.target instanceof Element ? event.target : null;
  const textEditor = target?.matches("input,textarea");
  // A paste targets one cell. Native editor shortcuts operate on its text.
  // Neither needs the off-screen DOM to exist before the event is handled.
  if (textEditor && (event.type === "copy" || event.type === "cut")) return;
  if (event instanceof KeyboardEvent) {
    if (event.ctrlKey || event.metaKey) {
      const key = event.key.toLowerCase();
      if (key === "v" || (textEditor && ["a", "c", "x", "z", "y"].includes(key))) return;
    }
    if (["Shift", "Control", "Alt", "Meta", "AltGraph"].includes(event.key)) return;
    const navigation = /^(ArrowUp|ArrowDown|ArrowLeft|ArrowRight|PageUp|PageDown|Home|End|Tab|F3)$/.test(event.key);
    if (!navigation && !event.ctrlKey && !event.metaKey) return;
  }
  if (event instanceof PointerEvent && !event.shiftKey) return;
  const table = target?.closest("table");
  if (!table) return;
  const remaining = [...pending].filter(([row]) => row.closest("table") === table);
  // Spreadsheet navigation/copy must see the complete logical grid, including
  // a selection made immediately after arrival, before idle mounting finishes.
  if (remaining.length) flushSync(() => remaining.forEach(([, activate]) => activate()));
}

function watch(row: HTMLTableRowElement, activate: () => void) {
  if (!observer) {
    observer = new IntersectionObserver(entries => {
      for (const entry of entries) if (entry.isIntersecting) pending.get(entry.target as HTMLTableRowElement)?.();
    }, { rootMargin: "120px 0px" });
    for (const event of ["keydown", "pointerdown", "copy", "cut"]) document.addEventListener(event, finishSelection, true);
  }
  pending.set(row, activate);
  observer.observe(row);
  schedule();
  return () => {
    pending.delete(row);
    observer?.unobserve(row);
    if (!pending.size) {
      clearTimeout(timer); timer = undefined;
      observer?.disconnect(); observer = undefined;
      for (const event of ["keydown", "pointerdown", "copy", "cut"]) document.removeEventListener(event, finishSelection, true);
    }
  };
}

export function ProgressiveBoardRow({ immediate, columns, height, label, children }: {
  immediate: boolean; columns: number; height: number; label: string; children: () => ReactElement;
}) {
  const [mounted, setMounted] = useState(immediate);
  const placeholder = useRef<HTMLTableRowElement>(null);
  useEffect(() => {
    if (mounted) return;
    let cancelled = false;
    let stop = () => {};
    const activate = () => { stop(); if (!cancelled) setMounted(true); };
    if (immediate) queueMicrotask(activate);
    else if (placeholder.current) stop = watch(placeholder.current, activate);
    return () => { cancelled = true; stop(); };
  }, [immediate, mounted]);
  if (mounted || immediate) return children();
  return <tr ref={placeholder} data-board-row-pending aria-busy="true" style={{ height }}>
    <th scope="row" className="border-b border-zinc-800 px-3 text-left text-xs font-medium text-zinc-500">{label}</th>
    <td colSpan={columns - 1} className="border-b border-zinc-800" />
  </tr>;
}
