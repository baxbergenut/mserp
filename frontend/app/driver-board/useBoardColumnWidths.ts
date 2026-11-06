"use client";

import { useEffect, useMemo, useState, type RefObject } from "react";
import { boardColumns, defaultColumnOrder, type BoardColumn } from "./boardCells";

const growing = new Set<BoardColumn>(["currentLoad", "truck", "eta", "loadPlan"]);
const destinationMaxWidth = 280;
export type ColumnWidths = Partial<Record<BoardColumn, number>>;

export function useBoardColumnWidths(table: RefObject<HTMLTableElement | null>, values: string) {
  const [available, setAvailable] = useState(0);
  const [measured, setMeasured] = useState<ColumnWidths>({});
  useEffect(() => {
    const viewport = table.current?.parentElement;
    if (!viewport) return;
    const observer = new ResizeObserver(() => setAvailable(viewport.clientWidth));
    observer.observe(viewport);
    return () => observer.disconnect();
  }, [table]);
  useEffect(() => {
    const frame = requestAnimationFrame(() => {
      const context = document.createElement("canvas").getContext("2d");
      if (!context || !table.current) return;
      const font = getComputedStyle(table.current);
      const cells: Partial<Record<BoardColumn, string>>[] = JSON.parse(values);
      const widths: ColumnWidths = {};
      for (const column of defaultColumnOrder) {
        if (!growing.has(column) && column !== "status" && column !== "destination") continue;
        const [, minimum] = boardColumns[column];
        const cell = table.current.querySelector(`tr[data-driver-id] > [data-board-column="${column}"]`);
        const label = column === "status" ? cell?.querySelector("button") : cell;
        const style = label ? getComputedStyle(label) : font;
        context.font = `${style.fontWeight} ${style.fontSize} ${style.fontFamily}`;
        // Leave room for the separate truck assignment control and cell padding.
        const padding = column === "truck" ? 36 : column === "status" ? 32 : column === "destination" || column === "loadPlan" ? 48 : 28;
        const measuredValues = cells.map(row => row[column] ?? "").filter(value => value.trim())
          .map(value => Math.ceil(context.measureText(value).width) + padding).sort((a, b) => a - b);
        // A long custom address should not size every driver's destination cell.
        const contentWidth = column === "destination" ? measuredValues[Math.floor((measuredValues.length - 1) * 0.8)] ?? 0 : measuredValues.at(-1) ?? 0;
        const width = Math.max(minimum, contentWidth);
        widths[column] = column === "destination" ? Math.min(destinationMaxWidth, width) : width;
      }
      setMeasured(previous => defaultColumnOrder.every(column => previous[column] === widths[column]) ? previous : widths);
    });
    return () => cancelAnimationFrame(frame);
  }, [table, values]);
  return useMemo(() => ({ measured, available }), [measured, available]);
}

export function boardColumnWidths(measured: ColumnWidths, available: number, overrides: ColumnWidths) {
  const flexible = [...growing].filter(column => overrides[column] === undefined);
  const widths = { ...measured, ...overrides };
  const minimumWidth = defaultColumnOrder.reduce((sum, column) => sum + (widths[column] ?? boardColumns[column][1]), 0);
  const extra = flexible.length ? Math.max(0, available - minimumWidth) / flexible.length : 0;
  return {
    tableWidth: minimumWidth + extra * flexible.length,
    columnWidth: (column: BoardColumn) => (widths[column] ?? boardColumns[column][1]) + (flexible.includes(column) ? extra : 0),
  };
}
