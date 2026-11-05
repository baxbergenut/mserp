"use client";

import { useRef, useState } from "react";
import { transferPayLoadWeek } from "@/app/lib/api";
import type { OutgoingLoadTransfer } from "@/app/lib/types";

type ReturnLoadTarget = Pick<OutgoingLoadTransfer, "driverId" | "sourceDate" | "sourceSlot" | "boardVersion" | "transferVersion">;

export function returnLoadKey(target: ReturnLoadTarget) {
  return `${target.driverId}:${target.sourceDate}:${target.sourceSlot}`;
}

export function useReturnLoad(investor: boolean, onReturned: () => void) {
  const inFlight = useRef(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<{ key: string; message: string } | null>(null);

  async function returnLoad(target: ReturnLoadTarget) {
    if (inFlight.current) return;
    inFlight.current = true;
    setPending(true);
    setError(null);
    try {
      await transferPayLoadWeek(investor, {
        driverId: target.driverId,
        date: target.sourceDate,
        slot: target.sourceSlot,
        boardVersion: target.boardVersion,
        transferVersion: target.transferVersion,
        mode: "restore",
      });
      onReturned();
    } catch (err) {
      setError({ key: returnLoadKey(target), message: err instanceof Error ? err.message : "Unable to return load" });
    } finally {
      inFlight.current = false;
      setPending(false);
    }
  }

  return { pending, error, returnLoad };
}
