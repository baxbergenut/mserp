"use client";

import { useLayoutEffect, useState } from "react";
import { createPortal } from "react-dom";
import type { BoardPresence } from "@/app/lib/types";
import { boardCellKeys } from "./boardCells";

const colors = ["#2563eb", "#b45309", "#9333ea", "#047857", "#be185d", "#0e7490"];
type Cursor = { key: string; name: string; cell: string; color: string; left: number; top: number; width: number; height: number };
export function CellPresence({ peers }: { peers: BoardPresence[] }) {
  const [cursors, setCursors] = useState<Cursor[]>([]);
  useLayoutEffect(() => {
    let frame = 0;
    const update = () => {
      frame = 0;
      const table = document.querySelector<HTMLTableElement>('[data-status-board-grid]');
      const area = table?.closest('[data-scroll-key="driver-board"]');
      if (!table || !area) { setCursors([]); return; }
      const bounds = area.getBoundingClientRect(), header = table.tHead?.getBoundingClientRect();
      const result: Cursor[] = [];
      const cells = new Map<string, BoardPresence[]>();
      for (const p of peers) { const key = p.driverId + ":" + p.column; cells.set(key, [...(cells.get(key) ?? []), p]); }
      for (const [key, people] of cells) {
        const p = people[0];
        const row = Array.from(table.querySelectorAll<HTMLTableRowElement>('tr[data-driver-id]')).find(row => row.dataset.driverId === p.driverId);
        const index = boardCellKeys.indexOf(p.column), cell = row?.cells[index];
        if (!cell || index < 0) continue;
        const rect = cell.getBoundingClientRect();
        const sticky = index > 1 && innerWidth >= 640 ? row!.cells[1].getBoundingClientRect().right : bounds.left + 1;
        const left = Math.max(rect.left, bounds.left + 1, sticky), right = Math.min(rect.right, bounds.right - 1);
        const top = Math.max(rect.top, bounds.top + 1, header?.bottom ?? 0), bottom = Math.min(rect.bottom, bounds.bottom - 1);
        if (right - left < 12 || bottom - top < 12) continue;
        const hash = Array.from(p.userId).reduce((sum, ch) => sum + ch.charCodeAt(0), 0);
        result.push({ key, name: Array.from(new Set(people.map(p => p.userName))).join(", "), cell: `${row!.cells[1].textContent ?? "Driver"} · ${table.tHead?.rows[0].cells[index]?.textContent ?? p.column}`, color: colors[hash % colors.length], left, top, width: right - left, height: bottom - top });
      }
      setCursors(result);
    };
    const schedule = () => { if (!frame) frame = requestAnimationFrame(update); };
    const area = document.querySelector('[data-scroll-key="driver-board"]');
    const observer = new MutationObserver(schedule);
    if (area) observer.observe(area, { subtree: true, childList: true });
    document.addEventListener("scroll", schedule, true); window.addEventListener("resize", schedule);
    schedule();
    return () => { cancelAnimationFrame(frame); observer.disconnect(); document.removeEventListener("scroll", schedule, true); window.removeEventListener("resize", schedule); };
  }, [peers]);
  if (!cursors.length) return null;
  return createPortal(<div className="pointer-events-none fixed inset-0 z-30" aria-live="polite">
    {cursors.map(cursor => <div key={cursor.key} role="status" aria-label={`${cursor.name} is on ${cursor.cell}`} data-board-presence={cursor.name}
      className="fixed rounded-sm border-2" style={{ left: cursor.left, top: cursor.top, width: cursor.width, height: cursor.height, borderColor: cursor.color }}>
      <span className="absolute -top-4 right-0 max-w-32 truncate rounded-t px-1 text-[10px] leading-[14px] text-white" style={{ backgroundColor: cursor.color }}>{cursor.name}</span>
    </div>)}
  </div>, document.body);
}
