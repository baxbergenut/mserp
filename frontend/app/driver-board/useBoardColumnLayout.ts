"use client";

import { useContext, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent } from "react";
import { CurrentUserIDContext } from "@/app/lib/access";
import { boardColumns, defaultColumnOrder, type BoardColumn } from "./boardCells";
import { boardColumnWidths, useBoardColumnWidths, type ColumnWidths } from "./useBoardColumnWidths";

type Layout = { order: BoardColumn[]; widths: ColumnWidths };
const defaultLayout: Layout = { order: defaultColumnOrder, widths: {} };
const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value));
const manualWidth = (value: number) => clamp(Math.round(value), 48, 1200);

function readLayout(key: string): Layout {
  try {
    const saved = JSON.parse(localStorage.getItem(key) ?? "null");
    if (!saved) return defaultLayout;
    const order = new Set<BoardColumn>();
    const widths: ColumnWidths = {};
    if (Array.isArray(saved.order)) {
      for (const column of saved.order) {
        if (typeof column === "string" && Object.hasOwn(boardColumns, column)) order.add(column as BoardColumn);
      }
    }
    for (const column of defaultColumnOrder) {
      order.add(column);
      const width = saved.widths?.[column];
      if (Number.isFinite(width)) widths[column] = manualWidth(width);
    }
    return { order: [...order], widths };
  } catch {
    return defaultLayout;
  }
}

export function useBoardColumnLayout(values: string) {
  const userId = useContext(CurrentUserIDContext);
  const key = `mserp:status-board-columns:v1:${userId}`;
  const [layout, setLayout] = useState(() => readLayout(key));
  const table = useRef<HTMLTableElement>(null);
  const cancel = useRef<(() => void) | null>(null);
  const releaseResizeSpace = useRef<(() => void) | null>(null);
  const { measured, available } = useBoardColumnWidths(table, values);
  const dimensions = useMemo(() => boardColumnWidths(measured, available, layout.widths), [measured, available, layout.widths]);
  // A new layout or new measured widths invalidate the captured header geometry.
  useLayoutEffect(() => () => cancel.current?.(), [layout, dimensions]);
  useLayoutEffect(() => () => releaseResizeSpace.current?.(), []);

  function save(next: Layout) {
    try { localStorage.setItem(key, JSON.stringify(next)); } catch { /* Keep the layout usable in this tab. */ }
    setLayout(next);
  }

  function reset() {
    cancel.current?.();
    releaseResizeSpace.current?.();
    try { localStorage.removeItem(key); } catch { /* Reset the layout in this tab. */ }
    setLayout(defaultLayout);
  }

  function resize(column: BoardColumn, width?: number) {
    const widths = { ...layout.widths };
    if (width === undefined) {
      if (widths[column] === undefined) return;
      delete widths[column];
    } else {
      widths[column] = manualWidth(width);
    }
    save({ ...layout, widths });
  }

  function onHeaderKey(event: ReactKeyboardEvent<HTMLTableCellElement>, column: BoardColumn) {
    if (cancel.current || (event.key !== "ArrowLeft" && event.key !== "ArrowRight")) return;
    event.preventDefault();
    const delta = event.key === "ArrowLeft" ? -1 : 1;
    if (event.shiftKey) {
      resize(column, event.currentTarget.getBoundingClientRect().width + delta * 12);
    } else {
      const order = [...layout.order];
      const from = order.indexOf(column), to = from + delta;
      if (to < 0 || to >= order.length) return;
      order.splice(from, 1);
      order.splice(to, 0, column);
      save({ ...layout, order });
      // React may move the focused header itself while reconciling keyed cells.
      const header = event.currentTarget;
      requestAnimationFrame(() => header.focus({ preventScroll: true }));
    }
  }

  function start(event: ReactPointerEvent<HTMLElement>, column: BoardColumn, resizing = false) {
    const grid = table.current, area = grid?.parentElement, header = grid?.tHead?.rows[0];
    if (!grid || !area || !header || event.button !== 0 || !event.isPrimary || cancel.current) return;
    event.preventDefault();
    event.stopPropagation();
    const pointer = event.pointerId;
    const initialX = event.clientX;
    const initialScroll = area.scrollLeft;
    const scrollLimit = area.scrollWidth - area.clientWidth;
    const resizeScrollWidth = resizing && initialScroll > 0 ? Math.ceil(initialScroll + area.clientWidth) : 0;
    const source = layout.order.indexOf(column);
    const width = header.cells[source].getBoundingClientRect().width;
    const resizeCol = grid.querySelector<HTMLTableColElement>(`colgroup > col:nth-child(${source + 1})`);
    const duration = matchMedia("(prefers-reduced-motion: reduce)").matches ? 0 : 180;
    const transition = `translate ${duration}ms cubic-bezier(.2,.8,.2,1)`;
    let x = initialX, frame = 0, timer = 0, active = false, lastFrame = performance.now();
    let nextWidth = width;
    let committedWidth = dimensions.tableWidth;
    let order = [...layout.order];
    let columns: { column: BoardColumn; left: number; width: number; cells: HTMLTableCellElement[] }[] = [];
    const edge = 36;

    function begin() {
      active = true;
      grid!.setPointerCapture(pointer);
      grid!.dataset.columnGesture = resizing ? "resize" : "move";
      if (resizing) {
        releaseResizeSpace.current?.();
        if (resizeScrollWidth) area!.style.setProperty("--board-resize-scroll-width", `${resizeScrollWidth}px`);
        return;
      }
      const viewport = area!.getBoundingClientRect();
      const rows = Array.from(grid!.tBodies[0].rows).filter(row => {
        if (!row.matches("[data-driver-id], [data-pending-intake-id]")) return false;
        const rect = row.getBoundingClientRect();
        return rect.bottom > viewport.top && rect.top < viewport.bottom;
      });
      let left = 0;
      columns = layout.order.map((id, i) => {
        const cells = [header!.cells[i], ...rows.map(row => row.cells[i])];
        const size = cells[0].getBoundingClientRect().width;
        const result = { column: id, left, width: size, cells };
        left += size;
        return result;
      });
      for (const item of columns) {
        for (const cell of item.cells) {
          cell.style.transition = item.column === column ? "none" : transition;
          if (item.column === column) {
            cell.dataset.draggedColumn = "";
            if (getComputedStyle(cell).backgroundColor === "rgba(0, 0, 0, 0)") cell.dataset.draggedColumn = "opaque";
          }
        }
      }
    }

    function translate(item: (typeof columns)[number], offset: number) {
      const value = offset ? `${offset}px` : "";
      for (const cell of item.cells) {
        if (cell.style.translate !== value) cell.style.translate = value;
      }
    }

    function paint(delta: number, settle = false) {
      if (resizing) {
        nextWidth = manualWidth(width + delta);
        grid!.style.setProperty("--board-preview-width", `${tableWidthFor(nextWidth)}px`);
        resizeCol?.style.setProperty("--board-preview-column-width", `${nextWidth}px`);
        return;
      }
      const moving = columns[source], others = columns.filter(item => item !== moving);
      const left = clamp(moving.left + delta, 0, columns.reduce((end, item) => end + item.width, 0) - moving.width);
      let slot = 0, position = 0;
      while (position < others.length && left > slot + others[position].width / 2) {
        slot += others[position].width;
        position++;
      }
      const arranged = [...others];
      arranged.splice(position, 0, moving);
      if (settle || order[position] !== column) {
        order = arranged.map(item => item.column);
        let nextLeft = 0;
        for (const item of arranged) {
          if (item !== moving || settle) translate(item, nextLeft - item.left);
          nextLeft += item.width;
        }
      }
      if (!settle) translate(moving, left - moving.left);
    }

    const tick = (now: number) => {
      frame = 0;
      const elapsed = Math.min(32, now - lastFrame);
      lastFrame = now;
      if (!resizing) {
        const box = area.getBoundingClientRect();
        const speed = x < box.left + edge ? -clamp((box.left + edge - x) / edge, 0, 1)
          : clamp((x - box.right + edge) / edge, 0, 1);
        const scroll = clamp(area.scrollLeft + speed * elapsed * 0.6, 0, scrollLimit);
        if (speed && scroll !== area.scrollLeft) {
          area.scrollLeft = scroll;
          frame = requestAnimationFrame(tick);
        }
      }
      paint(x - initialX + (resizing ? 0 : area.scrollLeft - initialScroll));
    };

    function tableWidthFor(width: number) {
      return dimensions.tableWidth + width - dimensions.columnWidth(column);
    }

    function releaseScrollSpaceWhenSafe() {
      // Keep the left edge anchored after drop until the user scrolls back into the table.
      const property = "--board-resize-scroll-width";
      if (committedWidth >= resizeScrollWidth) {
        area!.style.removeProperty(property);
        return;
      }
      const release = () => {
        area!.style.removeProperty(property);
        area!.removeEventListener("scroll", onScroll);
        window.removeEventListener("resize", release);
        releaseResizeSpace.current = null;
      };
      const onScroll = () => {
        if (area!.scrollLeft <= Math.max(0, grid!.offsetWidth - area!.clientWidth)) release();
      };
      area!.addEventListener("scroll", onScroll, { passive: true });
      window.addEventListener("resize", release);
      releaseResizeSpace.current = release;
    }

    function stopListening() {
      cancelAnimationFrame(frame);
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onCancel);
      window.removeEventListener("keydown", onKey, true);
      window.removeEventListener("blur", cleanup);
      window.removeEventListener("resize", cleanup);
      grid!.removeEventListener("lostpointercapture", onCancel);
      if (grid!.hasPointerCapture(pointer)) grid!.releasePointerCapture(pointer);
    }

    function cleanup() {
      stopListening();
      window.clearTimeout(timer);
      cancel.current = null;
      if (!active) return;
      delete grid!.dataset.columnGesture;
      if (resizing) {
        grid!.style.removeProperty("--board-preview-width");
        resizeCol?.style.removeProperty("--board-preview-column-width");
        if (resizeScrollWidth) releaseScrollSpaceWhenSafe();
      }
      for (const item of columns) {
        for (const cell of item.cells) {
          cell.style.translate = "";
          cell.style.transition = "";
          delete cell.dataset.draggedColumn;
        }
      }
    }

    function onMove(e: PointerEvent) {
      if (e.pointerId !== pointer) return;
      x = e.clientX;
      if (!active && Math.abs(x - initialX) < 4) return;
      e.preventDefault();
      if (!active) begin();
      if (!frame) {
        lastFrame = performance.now();
        frame = requestAnimationFrame(tick);
      }
    }

    function onUp(e: PointerEvent) {
      if (e.pointerId !== pointer) return;
      if (!active) { cleanup(); return; }
      const delta = e.clientX - initialX + (resizing ? 0 : area!.scrollLeft - initialScroll);
      stopListening();
      if (resizing) {
        paint(delta);
        committedWidth = tableWidthFor(nextWidth);
        cleanup();
        resize(column, nextWidth);
        return;
      }
      for (const cell of columns[source].cells) cell.style.transition = transition;
      paint(delta, true);
      timer = window.setTimeout(() => {
        cleanup();
        if (order.some((id, i) => id !== layout.order[i])) save({ ...layout, order });
      }, duration);
    }

    function onCancel(e: PointerEvent) {
      if (e.pointerId === pointer) cleanup();
    }

    function onKey(e: KeyboardEvent) {
      if (e.key !== "Escape") return;
      e.preventDefault();
      e.stopPropagation();
      cleanup();
    }

    cancel.current = cleanup;
    grid.addEventListener("lostpointercapture", onCancel);
    window.addEventListener("pointermove", onMove, { passive: false });
    window.addEventListener("pointerup", onUp);
    window.addEventListener("pointercancel", onCancel);
    window.addEventListener("keydown", onKey, true);
    window.addEventListener("blur", cleanup);
    window.addEventListener("resize", cleanup);
  }

  return { table, order: layout.order, ...dimensions, start, resize, reset, onHeaderKey };
}
