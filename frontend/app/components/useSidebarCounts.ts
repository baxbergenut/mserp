"use client";

import { useEffect, useState } from "react";
import { usePathname } from "next/navigation";
import { usePermissions } from "../lib/access";
import { fetchSidebarCounts } from "../lib/api";
import type { SidebarCounts } from "../lib/types";

const empty: SidebarCounts = { fuel: 0, weighMyTruck: 0, tolls: 0, tollsFrom: "" };

export function useSidebarCounts() {
  const permissions = usePermissions();
  const scope = ["fuel.read", "weighmytruck.read", "tolls.read"].filter(p => permissions.includes(p)).join(",");
  const pathname = usePathname();
  const [saved, setSaved] = useState({ scope: "", counts: empty });
  useEffect(() => {
    if (!scope) return;
    let active = true;
    let request: AbortController | undefined;
    let debounce: ReturnType<typeof setTimeout>;
    const refresh = () => {
      if (document.hidden) return;
      request?.abort();
      request = new AbortController();
      const current = request;
      fetchSidebarCounts(current.signal).then(counts => {
        if (active && !current.signal.aborted) setSaved({ scope, counts });
      }).catch(() => { /* Keep the last known counts during temporary outages. */ });
    };
    const changed = () => { clearTimeout(debounce); debounce = setTimeout(refresh, 150); };
    refresh();
    const timer = setInterval(refresh, 30_000);
    window.addEventListener("focus", changed);
    document.addEventListener("visibilitychange", changed);
    window.addEventListener("mserp:sidebar-counts-changed", changed);
    return () => {
      active = false; request?.abort(); clearInterval(timer); clearTimeout(debounce);
      window.removeEventListener("focus", changed);
      document.removeEventListener("visibilitychange", changed);
      window.removeEventListener("mserp:sidebar-counts-changed", changed);
    };
  }, [scope, pathname]);
  return saved.scope === scope ? saved.counts : empty;
}
