"use client";

import { useCallback, useEffect, useLayoutEffect, useState } from "react";
import { useRouter } from "next/navigation";

export function useBoardNavigation(blocked: boolean, onRequest?: () => void) {
  const router = useRouter();
  const [pendingLink, setPendingLink] = useState<string | null>(null);
  const cancelNavigation = useCallback(() => setPendingLink(null), []);
  const requestNavigation = useCallback((href: string) => {
    onRequest?.();
    setPendingLink(href);
  }, [onRequest]);

  useLayoutEffect(() => {
    if (!blocked) return;
    const beforeUnload = (event: BeforeUnloadEvent) => event.preventDefault();
    const intercept = (event: MouseEvent) => {
      if (event.defaultPrevented || event.button !== 0 || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
      const link = event.target instanceof Element ? event.target.closest<HTMLAnchorElement>("a[href]") : null;
      if (!link || link.hasAttribute("download") || (link.target && link.target !== "_self")) return;
      const current = window.location;
      if (link.protocol !== "http:" && link.protocol !== "https:") return;
      if (link.origin === current.origin && link.pathname === current.pathname && link.search === current.search) return;
      event.preventDefault();
      event.stopPropagation();
      requestNavigation(link.href);
    };
    window.addEventListener("beforeunload", beforeUnload);
    document.addEventListener("click", intercept, true);
    return () => {
      window.removeEventListener("beforeunload", beforeUnload);
      document.removeEventListener("click", intercept, true);
    };
  }, [blocked, requestNavigation]);

  const leave = useCallback(() => {
    if (!pendingLink) return;
    const next = new URL(pendingLink, window.location.href);
    cancelNavigation();
    if (next.href === window.location.href) return;
    if (next.origin === window.location.origin && !next.pathname.startsWith("//")) {
      router.push(next.pathname + next.search + next.hash, { scroll: false });
    } else {
      window.location.assign(next.href);
    }
  }, [cancelNavigation, pendingLink, router]);

  useEffect(() => {
    if (!pendingLink || blocked) return;
    const timer = setTimeout(leave, 0);
    return () => clearTimeout(timer);
  }, [blocked, leave, pendingLink]);

  return { pendingLink, requestNavigation, cancelNavigation, leave };
}
