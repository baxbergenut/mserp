"use client";

import { createContext, useContext, type ReactNode } from "react";
import { usePermissions } from "@/app/lib/access";
import type { GrossBoardEntry } from "@/app/lib/types";
import { returnLoadKey, useReturnLoad } from "@/app/accounting/driver-pay/useReturnLoad";

const ReturnContext = createContext<{ disabled: boolean; returnLoad: (entry: GrossBoardEntry) => void; error: { key: string; message: string } | null } | null>(null);

export function GrossTransferReturn({ disabled, onReturned, children }: { disabled: boolean; onReturned: () => void; children: ReactNode }) {
  const permissions = usePermissions();
  const driver = permissions.includes("driver_pay.edit");
  const investor = !driver && permissions.includes("investor_pay.edit");
  const { pending, error, returnLoad } = useReturnLoad(investor, onReturned);
  const value = driver || investor ? {
    disabled: disabled || pending,
    error,
    returnLoad: (entry: GrossBoardEntry) => returnLoad(transferTarget(entry)),
  } : null;
  return <ReturnContext.Provider value={value}>{children}</ReturnContext.Provider>;
}

function transferTarget(entry: GrossBoardEntry) {
  return {
    driverId: entry.driverId,
    sourceDate: entry.sourceDate || entry.date,
    sourceSlot: entry.sourceSlot ?? entry.slot,
    boardVersion: entry.version,
    transferVersion: entry.transferVersion || 0,
  };
}

export function GrossTransferReturnButton({ entry }: { entry: GrossBoardEntry }) {
  const context = useContext(ReturnContext);
  if (!context || !entry.transferVersion) return null;
  return <><button type="button" disabled={context.disabled} aria-label={`Return ${entry.loadNumber} to original date`} onClick={() => context.returnLoad(entry)} className="mt-2 rounded border border-zinc-700 px-2 py-1 text-[10px] text-blue-300 hover:bg-zinc-800 disabled:opacity-40">Return to original date</button>
    {context.error?.key === returnLoadKey(transferTarget(entry)) && <span role="alert" className="block text-[11px] text-red-300">{context.error.message}</span>}
  </>;
}
