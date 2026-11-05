"use client";

import { useEffect, useRef, useState } from "react";

export const boardColumns = [
  ["Current load", 90], ["Driver", 180], ["Truck", 64], ["Trailer", 100],
  ["Total gross", 110], ["Driver gross", 110], ["Phone", 124], ["Status", 70],
  ["Destination", 160], ["ETA", 180], ["Next load", 180], ["Notes", 130], ["Home time", 120], ["Driver address", 140],
] as const;
const growing = new Set([0, 2, 9, 10]);
const destinationMaxWidth = 280;

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
      if (!growing.has(i) && i !== 7 && i !== 8) return minimum;
      const cell = table.current?.querySelector(`tr[data-driver-id] > :nth-child(${i + 1})`);
      const label = i === 7 ? cell?.querySelector("button") : cell;
      const style = label ? getComputedStyle(label) : font;
      context.font = `${style.fontWeight} ${style.fontSize} ${style.fontFamily}`;
      // Leave room for the separate truck assignment control and cell padding.
      const padding = i === 2 ? 36 : i === 7 ? 32 : i === 8 || i === 10 ? 48 : 28;
      const measuredValues = cells.map(row => row[i] ?? "").filter(value => value.trim()).map(value => Math.ceil(context.measureText(value).width) + padding).sort((a, b) => a - b);
      // A long custom address should not size every driver's destination cell.
      const contentWidth = i === 8 ? measuredValues[Math.floor((measuredValues.length - 1) * 0.8)] ?? 0 : measuredValues.at(-1) ?? 0;
      const width = Math.max(minimum, contentWidth);
      return i === 8 ? Math.min(destinationMaxWidth, width) : width;
    });
    setWidths(previous => previous.every((width, i) => width === measured[i]) ? previous : measured);
  }, [serialized]);
  const minimumWidth = widths.reduce((sum, width) => sum + width, 0);
  // Only these columns share spare viewport space. Money and phone stay fixed.
  const tableWidth = Math.max(minimumWidth, available);
  const spare = tableWidth - minimumWidth;
  const columnWidth = (i: number) => widths[i] + (growing.has(i) ? spare / growing.size : 0);
  return { table, minimumWidth, tableWidth, columnWidth };
}
