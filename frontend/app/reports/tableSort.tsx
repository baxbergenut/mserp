"use client";

import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { ArrowDown, ArrowUp, ArrowUpDown } from "lucide-react";

type Sort = { key: string; descending: boolean };
type Value = string | number | undefined;
const collator = new Intl.Collator("en-US", { numeric: true, sensitivity: "base" });

export function useTableSort<T>(rows: T[], value: (row: T, key: string) => Value) {
  const [sort, setSort] = useState<Sort>();
  const elements = useRef(new Map<string, HTMLTableRowElement>());
  const positions = useRef(new Map<string, number>());
  const sorted = useMemo(() => !sort ? rows : [...rows].sort((a, b) => {
    const av = value(a, sort.key), bv = value(b, sort.key);
    if (av === undefined || av === "") return bv === undefined || bv === "" ? 0 : 1;
    if (bv === undefined || bv === "") return -1;
    const comparison = typeof av === "number" && typeof bv === "number" ? av - bv : collator.compare(String(av), String(bv));
    return sort.descending ? -comparison : comparison;
  }), [rows, sort, value]);

  // FLIP keeps semantic table layout and variable-height expanded rows. Match
  // the dashboard's 300ms transform movement, including reduced-motion support.
  useLayoutEffect(() => {
    if (!positions.current.size) return;
    const moves = [...elements.current].map(([key, element]) => ({ element, before: positions.current.get(key), after: element.getBoundingClientRect().top }));
    positions.current.clear();
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    for (const { element, before, after } of moves) {
      if (before !== undefined && Math.abs(before - after) > 1) {
        element.animate([{ transform: `translateY(${before - after}px)` }, { transform: "translateY(0)" }],
          { duration: 300, easing: "cubic-bezier(0.4, 0, 0.2, 1)" });
      }
    }
  });

  function changeSort(key: string) {
    positions.current = new Map([...elements.current].map(([id, element]) => [id, element.getBoundingClientRect().top]));
    for (const element of elements.current.values()) for (const animation of element.getAnimations()) animation.cancel();
    setSort(previous => ({ key, descending: previous?.key === key ? !previous.descending : false }));
  }
  function register(key: string, element: HTMLTableRowElement | null) {
    if (element) elements.current.set(key, element); else elements.current.delete(key);
  }
  return { sorted, sort, changeSort, register };
}

export function SortHeader({ column, label, sort, onSort, center = false, className = "" }: {
  column: string; label: string; sort?: Sort; onSort: (key: string) => void; center?: boolean; className?: string;
}) {
  const active = sort?.key === column;
  const Icon = active ? sort.descending ? ArrowDown : ArrowUp : ArrowUpDown;
  return <th scope="col" aria-sort={active ? sort.descending ? "descending" : "ascending" : "none"} className={className}>
    <button type="button" onClick={() => onSort(column)} className={`group flex w-full items-center gap-1.5 whitespace-nowrap py-0.5 font-medium focus-visible:outline-2 focus-visible:outline-blue-500 ${center ? "justify-center" : "text-left"}`}>
      {label}<Icon aria-hidden="true" className={`h-3 w-3 shrink-0 ${active ? "text-blue-400" : "text-zinc-500 opacity-50 group-hover:opacity-100"}`} />
    </button>
  </th>;
}
