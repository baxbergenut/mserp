"use client";

import { useEffect, useRef, useState, type Dispatch, type SetStateAction } from "react";
import { fetchDriverCharges, saveRecurringCharge } from "@/app/lib/api";
import type { ChargeCell, ChargeData } from "@/app/lib/types";

export function useRecurringChargeSaves(setData: Dispatch<SetStateAction<ChargeData | null>>, notify: (message: string, error?: boolean) => void) {
  const [pending, setPending] = useState<Record<string, ChargeCell>>({});
  const requests = useRef(new Map<string, Promise<string | null>>());
  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (requests.current.size) { event.preventDefault(); event.returnValue = ""; }
    };
    window.addEventListener("beforeunload", beforeUnload);
    return () => window.removeEventListener("beforeunload", beforeUnload);
  }, []);

  function save(input: ChargeCell, driverName: string, typeName: string, quiet = false): Promise<string | null> {
    if (requests.current.has(input.driverId)) return Promise.resolve(`${driverName}: a charge is still saving.`);
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
        if (!quiet) notify(`${driverName} · ${typeName} saved from ${input.weekStart}`);
        return null;
      } catch (error) {
        if (!saved) {
          // Restore the server's current versions after a stale edit or an
          // uncertain network result, without retrying a financial write.
          try {
            const updated = await fetchDriverCharges(input.driverId);
            setData(current => current && ({ ...current, schedules: [...current.schedules.filter(s => s.driverId !== input.driverId), ...updated.schedules] }));
          } catch { /* Keep the last known data; Reload remains available. */ }
        }
        const message = saved ? `${driverName}: saved, but refresh failed. Reload before editing this driver again.` : `${driverName}: ${error instanceof Error ? error.message : "Unable to save charge"}`;
        if (!quiet) notify(message, true);
        return message;
      } finally {
        requests.current.delete(input.driverId);
        setPending(current => { const next = { ...current }; delete next[input.driverId]; return next; });
      }
    })();
    requests.current.set(input.driverId, request);
    return request;
  }

  async function saveAll(targets: { input: ChargeCell; driverName: string }[], typeName: string) {
    const failures: string[] = [];
    // Bound requests while retaining each driver's optimistic version checks.
    for (let offset = 0; offset < targets.length; offset += 4) {
      const results = await Promise.all(targets.slice(offset, offset + 4).map(({ input, driverName }) => save(input, driverName, typeName, true)));
      failures.push(...results.filter((result): result is string => result !== null));
    }
    notify(`${typeName}: ${targets.length - failures.length} of ${targets.length} drivers updated.${failures.length ? ` ${failures.join(" ")}` : ""}`, failures.length > 0);
  }

  // Page-wide mutations/reloads wait for outstanding row saves, so an older
  // whole-page snapshot cannot overwrite their newer results.
  const waitForSaves = () => Promise.allSettled([...requests.current.values()]);
  return { pending, save, saveAll, waitForSaves };
}
