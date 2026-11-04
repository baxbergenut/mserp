"use client";

import { useContext, useEffect, useMemo, useState } from "react";
import { CurrentUserIDContext } from "@/app/lib/access";
import type { DriverBoardDriver } from "@/app/lib/types";

type Orders = Record<string, string[]>;
function readOrders(key: string): Orders {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(key) ?? "{}");
    if (!value || typeof value !== "object" || Array.isArray(value)) return {};
    return Object.fromEntries(Object.entries(value).filter(([, ids]) =>
      Array.isArray(ids) && ids.every(id => typeof id === "string"))
      .map(([group, ids]) => [group, [...new Set(ids as string[])]]));
  } catch { return {}; }
}

export function useDriverOrder(drivers: DriverBoardDriver[]) {
  const userId = useContext(CurrentUserIDContext);
  // AppShell remounts the page when the signed-in account changes.
  const key = `mserp:driver-order:v1:${userId}`;
  const [preferred, setPreferred] = useState(() => readOrders(key));
  useEffect(() => {
    const changed = (event: StorageEvent) => {
      if (event.key === key || event.key === null) setPreferred(readOrders(key));
    };
    window.addEventListener("storage", changed);
    return () => window.removeEventListener("storage", changed);
  }, [key]);
  const ordered = useMemo(() => {
    const groups = new Map<string, DriverBoardDriver[]>();
    for (const driver of drivers) {
      const group = groups.get(driver.dispatcherId);
      if (group) group.push(driver); else groups.set(driver.dispatcherId, [driver]);
    }
    return Array.from(groups, ([group, rows]) => {
      const ranks = new Map((preferred[group] ?? []).map((id, index) => [id, index]));
      return rows.sort((a, b) => (ranks.get(a.id) ?? Infinity) - (ranks.get(b.id) ?? Infinity));
    }).flat();
  }, [drivers, preferred]);
  function reorder(id: string, target: string, after: boolean, destination: string) {
    if (!drivers.some(driver => driver.id === id) || id === target ||
        (target && !drivers.some(driver => driver.id === target && driver.dispatcherId === destination))) {
      throw new Error("The driver list changed. Try again.");
    }
    // Use the complete group, including rows hidden by filters. A successful
    // assignment may place a driver into a group not yet in the next snapshot.
    const ids = ordered.filter(driver => driver.dispatcherId === destination && driver.id !== id).map(driver => driver.id);
    const position = target ? ids.indexOf(target) + Number(after) : ids.length;
    ids.splice(position, 0, id);
    const saved = { ...preferred, [destination]: [...ids, ...(preferred[destination] ?? []).filter(value => !ids.includes(value))] };
    let persisted = false;
    try { localStorage.setItem(key, JSON.stringify(saved)); persisted = true; } catch { /* Keep the tab usable when storage is blocked. */ }
    setPreferred(saved);
    return persisted;
  }
  return { drivers: ordered, reorder };
}
