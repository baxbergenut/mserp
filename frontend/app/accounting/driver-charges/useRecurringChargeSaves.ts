"use client";

import { useEffect, useRef, useState, type Dispatch, type SetStateAction } from "react";
import { fetchDriverCharges, saveRecurringCharge } from "@/app/lib/api";
import type { ChargeCell, ChargeData } from "@/app/lib/types";

export function useRecurringChargeSaves(setData: Dispatch<SetStateAction<ChargeData | null>>, notify: (message: string, error?: boolean) => void) {
  const [pending, setPending] = useState<Record<string, ChargeCell>>({});
  const requests = useRef(new Map<string, Promise<void>>());
  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (requests.current.size) { event.preventDefault(); event.returnValue = ""; }
    };
    window.addEventListener("beforeunload", beforeUnload);
    return () => window.removeEventListener("beforeunload", beforeUnload);
  }, []);

  function save(input: ChargeCell, driverName: string, typeName: string) {
    if (requests.current.has(input.driverId)) return;
    setPending(current => ({ ...current, [input.driverId]: input }));
    const request = (async () => {
      let saved = false;
      try {
        await saveRecurringCharge(input);
        saved = true;
        const updated = await fetchDriverCharges(input.driverId);
        // Independent drivers may finish in any order. Merge only this driver,
        // retaining all other completed saves and current type definitions.
        setData(current => current && ({ ...current, schedules: [...current.schedules.filter(s => s.driverId !== input.driverId), ...updated.schedules] }));
        notify(`${driverName} · ${typeName} saved from ${input.weekStart}`);
      } catch (error) {
        if (!saved) {
          // Restore the server's current versions after a stale edit or an
          // uncertain network result, without retrying a financial write.
          try {
            const updated = await fetchDriverCharges(input.driverId);
            setData(current => current && ({ ...current, schedules: [...current.schedules.filter(s => s.driverId !== input.driverId), ...updated.schedules] }));
          } catch { /* Keep the last known data; Reload remains available. */ }
        }
        notify(saved ? `${driverName}: saved, but refresh failed. Reload before editing this driver again.` : `${driverName}: ${error instanceof Error ? error.message : "Unable to save charge"}`, true);
      } finally {
        requests.current.delete(input.driverId);
        setPending(current => { const next = { ...current }; delete next[input.driverId]; return next; });
      }
    })();
    requests.current.set(input.driverId, request);
  }

  // Page-wide mutations/reloads wait for outstanding row saves, so an older
  // whole-page snapshot cannot overwrite their newer results.
  const waitForSaves = () => Promise.allSettled([...requests.current.values()]);
  return { pending, save, waitForSaves };
}
