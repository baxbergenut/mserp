"use client";

import { useCallback, useState, type ReactNode } from "react";
import type { DriverPayLoad } from "@/app/lib/types";
import { decimalDisplay, hundredths } from "@/app/gross-board/board";
import { StatementMenu } from "./StatementMenu";

export function PaySourceCell({ load, field, className, disabled, compareSource, onAccept, children }: {
  load: DriverPayLoad; field: "originalRate" | "totalMiles" | "driverGross"; className: string; disabled: boolean; compareSource: boolean;
  onAccept?: (load: DriverPayLoad, field: "originalRate" | "totalMiles" | "driverGross") => Promise<void>;
  children?: ReactNode;
}) {
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const close = useCallback(() => setMenu(null), []);
  const value = load[field];
  const source = compareSource ? {
    originalRate: load.systemOriginalRate, driverGross: load.systemDriverGross, totalMiles: load.systemMiles,
  }[field] : undefined;
  const mismatch = load.loadRecordId !== null && source != null && (field === "driverGross" ? source === "" || value === "" || hundredths(value) !== hundredths(source) : !!value && (source === "" || hundredths(value) !== hundredths(source)));
  const money = field !== "totalMiles";
  const label = field === "driverGross" ? "Driver gross" : money ? "Total gross" : "Miles";
  const canAccept = !load.payrollSupplement && !load.transferVersion && mismatch && source !== "" && !load.tripReview && onAccept && load.boardVersion && !disabled;
  const title = source == null ? undefined : `DataTruck: ${source ? decimalDisplay(hundredths(source), money) : "Not available"}`;
  return <td tabIndex={canAccept ? 0 : undefined} className={`${className} ${mismatch ? "text-red-400" : money ? "text-zinc-300" : "text-zinc-400"}`} title={title}
    aria-label={mismatch ? `${label} differs from DataTruck. ${title}` : undefined}
    onContextMenu={event => { if (canAccept) { event.preventDefault(); event.stopPropagation(); setMenu({ x: event.clientX, y: event.clientY }); } }}
    onKeyDown={event => { if (canAccept && (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10"))) { event.preventDefault(); const rect = event.currentTarget.getBoundingClientRect(); setMenu({ x: rect.left, y: rect.bottom }); } }}>
    {children ?? (value ? decimalDisplay(hundredths(value), money) : "—")}
    {menu && <StatementMenu {...menu} label={field === "driverGross" ? "Accept DataTruck driver gross" : "Accept system values"} detail={<span className="whitespace-nowrap font-mono">{value ? decimalDisplay(hundredths(value), money) : "—"} → {source ? decimalDisplay(hundredths(source), money) : "—"}</span>} disabled={disabled} onClose={close} onSelect={() => { void onAccept?.(load, field); }} />}
  </td>;
}
