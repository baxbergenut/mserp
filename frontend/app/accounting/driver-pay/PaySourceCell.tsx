"use client";

import { useCallback, useState } from "react";
import { usePermissions } from "@/app/lib/access";
import type { DriverPayLoad } from "@/app/lib/types";
import { decimalDisplay, hundredths } from "@/app/gross-board/board";
import { StatementMenu } from "./StatementMenu";

export function PaySourceCell({ load, field, className, disabled, onAccept }: {
  load: DriverPayLoad; field: "originalRate" | "totalMiles"; className: string; disabled: boolean;
  onAccept?: (load: DriverPayLoad, field: "originalRate" | "totalMiles") => Promise<void>;
}) {
  const permissions = usePermissions();
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const close = useCallback(() => setMenu(null), []);
  const value = load[field];
  const source = field === "originalRate" ? load.systemOriginalRate : load.systemMiles;
  const mismatch = load.loadRecordId !== null && !!value && source != null && (source === "" || hundredths(value) !== hundredths(source));
  const money = field === "originalRate";
  const canAccept = mismatch && onAccept && load.boardVersion && permissions.includes("payroll.write");
  const title = source == null ? undefined : `DataTruck: ${source ? decimalDisplay(hundredths(source), money) : "Not available"}`;
  return <td tabIndex={canAccept ? 0 : undefined} className={`${className} ${mismatch ? "text-red-400" : money ? "text-zinc-300" : "text-zinc-400"}`} title={title}
    aria-label={mismatch ? `${money ? "Original gross" : "Miles"} differs from DataTruck. ${title}` : undefined}
    onContextMenu={event => { if (canAccept) { event.preventDefault(); event.stopPropagation(); setMenu({ x: event.clientX, y: event.clientY }); } }}
    onKeyDown={event => { if (canAccept && (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10"))) { event.preventDefault(); const rect = event.currentTarget.getBoundingClientRect(); setMenu({ x: rect.left, y: rect.bottom }); } }}>
    {value ? decimalDisplay(hundredths(value), money) : "—"}
    {menu && <StatementMenu {...menu} label="Accept system values" detail={<span className="whitespace-nowrap font-mono">{value ? decimalDisplay(hundredths(value), money) : "—"} → {source ? decimalDisplay(hundredths(source), money) : "—"}</span>} disabled={disabled} onClose={close} onSelect={() => { void onAccept?.(load, field); }} />}
  </td>;
}
