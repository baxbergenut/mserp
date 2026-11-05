"use client";

import { createContext, useContext, useMemo, useState, type ReactNode } from "react";
import { usePermissions } from "@/app/lib/access";
import type { GrossBoardEntry } from "@/app/lib/types";
import { ReturnLoadDialog, type ReturnLoadTarget } from "@/app/accounting/driver-pay/ReturnLoadDialog";

const ReturnContext = createContext<{ disabled: boolean; open: (entry: GrossBoardEntry) => void } | null>(null);

export function GrossTransferReturn({ disabled, onReturned, children }: { disabled: boolean; onReturned: () => void; children: ReactNode }) {
  const permissions = usePermissions();
  const driver = permissions.includes("driver_pay.edit");
  const investor = !driver && permissions.includes("investor_pay.edit");
  const [target, setTarget] = useState<ReturnLoadTarget | null>(null);
  const value = useMemo(() => driver || investor ? { disabled, open: (entry: GrossBoardEntry) => setTarget({
    driverId: entry.driverId, loadNumber: entry.loadNumber, sourceDate: entry.sourceDate || entry.date,
    sourceSlot: entry.sourceSlot ?? entry.slot, boardVersion: entry.version, transferVersion: entry.transferVersion || 0,
  }) } : null, [driver,investor,disabled]);
  return <ReturnContext.Provider value={value}>{children}{target && <ReturnLoadDialog target={target} investor={investor} onClose={() => setTarget(null)} onReturned={onReturned} />}</ReturnContext.Provider>;
}

export function GrossTransferReturnButton({ entry }: { entry: GrossBoardEntry }) {
  const context = useContext(ReturnContext);
  if (!context || !entry.transferVersion) return null;
  return <button type="button" disabled={context.disabled} aria-label={`Return ${entry.loadNumber} to original date`} onClick={() => context.open(entry)} className="mt-2 rounded border border-zinc-700 px-2 py-1 text-[10px] text-blue-300 hover:bg-zinc-800 disabled:opacity-40">Return to original date</button>;
}
