"use client";

import { useLayoutEffect, useMemo, useRef } from "react";
import { createPortal } from "react-dom";
import type { BoardPresence } from "@/app/lib/types";

const colors = ["#2563eb", "#b45309", "#9333ea", "#047857", "#be185d", "#0e7490"];
export function CellPresence({ peers }: { peers: BoardPresence[] }) {
  const overlay = useRef<HTMLDivElement>(null);
  const cursors = useMemo(() => {
    const cells = new Map<string, BoardPresence[]>();
    for (const peer of peers) {
      const key = `${peer.driverId}:${peer.column}`;
      cells.set(key, [...(cells.get(key) ?? []), peer]);
    }
    return Array.from(cells, ([key, people]) => {
      const peer = people[0];
      const hash = Array.from(peer.userId).reduce((sum, ch) => sum + ch.charCodeAt(0), 0);
      return { key, driverId: peer.driverId, column: peer.column, name: Array.from(new Set(people.map(p => p.userName))).join(", "), color: colors[hash % colors.length] };
    });
  }, [peers]);
  useLayoutEffect(() => {
    const root = overlay.current;
    if (!root || !cursors.length) return;
    let frame = 0;
    let dirty = true;
    let table: HTMLTableElement | null = null;
    let area: Element | null = null;
    let main: Element | null = null;
    let targets: { node: HTMLElement; cell: HTMLTableCellElement | undefined }[] = [];
    const resolve = () => {
      dirty = false;
      table = document.querySelector<HTMLTableElement>('[data-status-board-grid]');
      area = table?.closest('[data-scroll-key="driver-board"]') ?? null;
      main = area?.closest('main') ?? null;
      const rows = new Map(Array.from(table?.querySelectorAll<HTMLTableRowElement>('tr[data-driver-id]') ?? [], row => [row.dataset.driverId, row]));
      targets = cursors.map((cursor, index) => {
        const row = rows.get(cursor.driverId);
        const cell = Array.from(row?.cells ?? []).find(cell => cell.dataset.boardColumn === cursor.column);
        const node = root.children[index] as HTMLElement;
        const heading = Array.from(table?.tHead?.rows[0]?.cells ?? []).find(cell => cell.dataset.boardColumn === cursor.column);
        node.setAttribute('aria-label', `${cursor.name} is on ${row?.querySelector('th[scope="row"]')?.textContent ?? "Driver"} · ${heading?.textContent ?? cursor.column}`);
        return { node, cell };
      });
    };
    // ResizeObserver misses pure position/transform changes (sidebar animation,
    // row drag, parent scrolling). Write geometry in the same animation frame,
    // outside React's render queue; cache targets until the board DOM changes.
    const update = () => {
      if (dirty) resolve();
      const bounds = area?.getBoundingClientRect(), viewport = main?.getBoundingClientRect();
      const header = table?.tHead?.getBoundingClientRect();
      const measurements = targets.map(({ node, cell }) => {
        if (!bounds || !cell?.isConnected || table?.dataset.columnGesture) return { node, rect: null };
        const rect = cell.getBoundingClientRect();
        const left = Math.max(rect.left, bounds.left + 1, viewport?.left ?? 0, 0);
        const right = Math.min(rect.right, bounds.right - 1, viewport?.right ?? innerWidth, innerWidth);
        const top = Math.max(rect.top, bounds.top + 1, header?.bottom ?? 0, viewport?.top ?? 0, 0);
        const bottom = Math.min(rect.bottom, bounds.bottom - 1, viewport?.bottom ?? innerHeight, innerHeight);
        return { node, rect: right - left >= 12 && bottom - top >= 12 ? { left, top, width: right - left, height: bottom - top } : null };
      });
      for (const { node, rect } of measurements) {
        node.style.display = rect ? '' : 'none';
        if (rect) {
          node.style.transform = `translate3d(${rect.left}px,${rect.top}px,0)`;
          node.style.width = `${rect.width}px`;
          node.style.height = `${rect.height}px`;
        }
      }
      frame = requestAnimationFrame(update);
    };
    const observer = new MutationObserver(() => { dirty = true; });
    const board = document.querySelector('[data-status-board-scroll]');
    if (board) observer.observe(board, { subtree: true, childList: true });
    update();
    return () => { cancelAnimationFrame(frame); observer.disconnect(); };
  }, [cursors]);
  if (!cursors.length) return null;
  return createPortal(<div ref={overlay} className="pointer-events-none fixed inset-0 z-30" aria-live="polite">
    {cursors.map(cursor => <div key={cursor.key} role="status" data-board-presence={cursor.name}
      className="fixed left-0 top-0 rounded-sm border-2" style={{ display: "none", borderColor: cursor.color }}>
      <span className="absolute -top-4 right-0 max-w-32 truncate rounded-t px-1 text-[10px] leading-[14px] text-white" style={{ backgroundColor: cursor.color }}>{cursor.name}</span>
    </div>)}
  </div>, document.body);
}
