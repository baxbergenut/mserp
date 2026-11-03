"use client";

import { useEffect, useRef, useState } from "react";

export const boardColumns = [
  ["Current load", 90], ["Driver", 180], ["Truck", 55], ["Trailer", 100],
  ["Total gross", 110], ["Driver gross", 110], ["Phone", 124], ["Status", 140],
  ["Destination", 120], ["ETA", 180], ["Next load", 180], ["Notes", 130], ["Home time", 120], ["Driver address", 140],
] as const;
const growing = new Set([0, 2, 8, 9, 10]);

export function useBoardColumnWidths(values: string[][]) {
  const table = useRef<HTMLTableElement>(null);
  const [available, setAvailable] = useState(0);
  const [widths, setWidths] = useState<number[]>(boardColumns.map(([, width]) => width));
  const serialized = JSON.stringify(values);
  useEffect(() => {
    const viewport = table.current?.parentElement;
    if (!viewport) return;
    const observer = new ResizeObserver(() => setAvailable(viewport.clientWidth));
    observer.observe(viewport);
    return () => observer.disconnect();
  }, []);
  useEffect(() => {
    const context = document.createElement("canvas").getContext("2d");
    if (!context || !table.current) return;
    const font = getComputedStyle(table.current);
    const cells: string[][] = JSON.parse(serialized);
    const measured = boardColumns.map(([, minimum], i) => {
      if (!growing.has(i)) return minimum;
      const cell = table.current?.querySelector(`tr[data-driver-id] > :nth-child(${i + 1})`);
      const style = cell ? getComputedStyle(cell) : font;
      context.font = `${style.fontSize} ${style.fontFamily}`;
      return Math.max(minimum, ...cells.map(row => Math.ceil(context.measureText(row[i] ?? "").width) + (i === 8 || i === 10 ? 48 : 28)));
    });
    setWidths(previous => previous.every((width, i) => width === measured[i]) ? previous : measured);
  }, [serialized]);
  const minimumWidth = widths.reduce((sum, width) => sum + width, 0);
  // Only these columns share spare viewport space. Money and phone stay fixed.
  const tableWidth = Math.max(minimumWidth, available);
  const columnWidth = (i: number) => widths[i] + (growing.has(i) ? (tableWidth - minimumWidth) / growing.size : 0);
  return { table, minimumWidth, tableWidth, columnWidth };
}
