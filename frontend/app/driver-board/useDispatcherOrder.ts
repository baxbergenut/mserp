"use client";

import { useContext, useEffect, useMemo, useState } from "react";
import { CurrentUserIDContext } from "@/app/lib/access";
import type { DriverBoardDriver } from "@/app/lib/types";

function readOrder(key: string): string[] {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(key) ?? "[]");
    return Array.isArray(value) && value.every(id => typeof id === "string") ? [...new Set(value)] : [];
  } catch { return []; }
}

export function useDispatcherOrder(drivers: DriverBoardDriver[], autoOrderGroupNames?: Readonly<Record<string, string>>) {
  const userId = useContext(CurrentUserIDContext);
  // AppShell remounts the page when the signed-in account changes.
  const key = `mserp:dispatcher-order:v1:${userId}`;
  const [preferred, setPreferred] = useState(() => readOrder(key));
  useEffect(() => {
    const changed = (event: StorageEvent) => {
      if (event.key === key || event.key === null) setPreferred(readOrder(key));
    };
    window.addEventListener("storage", changed);
    return () => window.removeEventListener("storage", changed);
  }, [key]);
  const ids = useMemo(() => {
    const current = new Set(drivers.map(driver => driver.dispatcherId));
    for (const id of Object.keys(autoOrderGroupNames ?? {})) current.add(id);
    current.add(""); // The empty/pending-hire group is always available.
    const defaults = ["", ...Array.from(current).filter(Boolean)];
    return [...preferred.filter(id => current.has(id)), ...defaults.filter(id => !preferred.includes(id))];
  }, [drivers, autoOrderGroupNames, preferred]);
  const ordered = useMemo(() => {
    const ranks = new Map(ids.map((id, index) => [id, index]));
    return [...drivers].sort((a, b) => ranks.get(a.dispatcherId)! - ranks.get(b.dispatcherId)!);
  }, [drivers, ids]);
  function reorder(id: string, target: string, after: boolean) {
    if (id === target || !ids.includes(id) || !ids.includes(target)) {
      throw new Error("The dispatcher list changed. Try again.");
    }
    const next = ids.filter(value => value !== id);
    next.splice(next.indexOf(target) + Number(after), 0, id);
    // Keep preferences for groups temporarily absent from the active roster.
    const saved = [...next, ...preferred.filter(value => !next.includes(value))];
    let persisted = false;
    try { localStorage.setItem(key, JSON.stringify(saved)); persisted = true; } catch { /* Keep this tab usable when storage is blocked. */ }
    setPreferred(saved);
    return persisted;
  }
  return { drivers: ordered, ids, reorder };
}
