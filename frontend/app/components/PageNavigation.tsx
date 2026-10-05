"use client";

import { IntentLink as Link } from "@/app/components/IntentLink";
import { useCallback, useEffect, useRef, useState } from "react";
import { ArrowLeft } from "lucide-react";
import { BackHrefContext, MarkBackContext, NavigationSearchContext, readMemory, ViewMemoryContext, writeMemory } from "@/app/lib/viewMemory";

type ScrollPosition = Record<string, { top: number; left: number }>;
function scrollKey(element: HTMLElement, main: HTMLElement): string {
  if (element === main) return "main";
  const named = element.dataset.scrollKey ?? element.dataset.payrollScroll ?? element.getAttribute("aria-label") ?? element.id;
  if (named) return `${element.tagName}:${named}`;
  const parts: number[] = [];
  let node: Element | null = element;
  while (node && node !== main) { parts.push(Array.prototype.indexOf.call(node.parentElement?.children, node)); node = node.parentElement; }
  return parts.reverse().join(".");
}

export function PageNavigation({ userId, url, children }: { userId: string; url: string; children: React.ReactNode }) {
  const parsed = new URL(url, "https://mserp.invalid");
  const scope = `mserp-navigation-v1:${userId}:${parsed.pathname}${parsed.searchParams.has("id") ? `:${parsed.searchParams.get("id")}` : ""}`;
  const explicitLoad = parsed.pathname === "/gross-board" && parsed.searchParams.has("date");
  const historyKey = `mserp-navigation-v1:${userId}:history`;
  const [trail] = useState(() => {
    const previous = readMemory<string[]>(historyKey, []).filter(item => item.startsWith("/") && !item.startsWith("//") && !item.startsWith("/login"));
    if (previous.at(-1) === url) return previous;
    const index = readMemory<string | null>(`${historyKey}:back`, null) === url ? previous.lastIndexOf(url) : -1;
    return index >= 0 ? previous.slice(0, index + 1) : [...previous, url].slice(-50);
  });
  const mainRef = useRef<HTMLElement>(null);
  const markBack = useCallback((target: string) => writeMemory(`${historyKey}:back`, target), [historyKey]);
  useEffect(() => {
    writeMemory(historyKey, trail); writeMemory(`${historyKey}:back`, null);
    const click = (event: MouseEvent) => {
      const link = (event.target as Element).closest?.("a[data-navigation-back]");
      if (link && !event.ctrlKey && !event.metaKey && !event.shiftKey && event.button === 0) markBack(link.getAttribute("href") ?? "");
    };
    const pop = () => markBack(window.location.pathname + window.location.search);
    document.addEventListener("click", click, true); window.addEventListener("popstate", pop);
    return () => { document.removeEventListener("click", click, true); window.removeEventListener("popstate", pop); };
  }, [historyKey, trail, markBack]);
  useEffect(() => {
    const main = mainRef.current;
    if (!main) return;
    const key = `${scope}:scroll`;
    const positions = readMemory<ScrollPosition>(key, {});
    let restoring = !explicitLoad && Object.keys(positions).length > 0;
    let frame = 0;
    const elements = () => Object.keys(positions).every(id => id === "main") ? [main] : [main, ...Array.from(main.querySelectorAll<HTMLElement>("*"))];
    const restore = () => {
      if (!restoring) return;
      const remaining = new Set(Object.keys(positions));
      for (const element of elements()) {
        const id = scrollKey(element, main), saved = positions[id];
        if (!saved) continue;
        const left = element.dataset.scrollInitialX === "start" ? 0 : saved.left;
        element.scrollTo({ top: saved.top, left, behavior: "instant" });
        if (Math.abs(element.scrollTop - saved.top) < 2 && Math.abs(element.scrollLeft - left) < 2) remaining.delete(id);
        if (!remaining.size) break;
      }
      if (!remaining.size) restoring = false;
    };
    const queueRestore = () => { if (!restoring) return; cancelAnimationFrame(frame); frame = requestAnimationFrame(restore); };
    const observer = new MutationObserver(queueRestore);
    observer.observe(main, { childList: true, subtree: true });
    const resize = new ResizeObserver(queueRestore);
    if (main.firstElementChild) resize.observe(main.firstElementChild);
    const interrupt = () => { restoring = false; };
    const remember = (event: Event) => {
      if (restoring || !(event.target instanceof HTMLElement)) return;
      const element = event.target;
      if (!main.contains(element) && element !== main) return;
      positions[scrollKey(element, main)] = { top: element.scrollTop, left: element.scrollLeft };
      writeMemory(key, positions);
    };
    main.addEventListener("scroll", remember, true);
    main.addEventListener("wheel", interrupt, { passive: true });
    main.addEventListener("pointerdown", interrupt);
    main.addEventListener("keydown", interrupt);
    queueRestore();
    const timeout = setTimeout(interrupt, 10000);
    return () => {
      clearTimeout(timeout); cancelAnimationFrame(frame); observer.disconnect(); resize.disconnect();
      main.removeEventListener("scroll", remember, true); main.removeEventListener("wheel", interrupt);
      main.removeEventListener("pointerdown", interrupt); main.removeEventListener("keydown", interrupt);
    };
  }, [scope, explicitLoad]);
  const back = trail.at(-2);
  return <ViewMemoryContext.Provider value={scope}><BackHrefContext.Provider value={back}><MarkBackContext.Provider value={markBack}><main ref={mainRef} className="min-w-0 flex-1 overflow-auto">
    <div className="w-full px-4 py-6 sm:px-6 xl:px-8">
      {back && <Link data-navigation-back href={back} prefetch={false} scroll={false} aria-label="Back to previous page" className="relative z-10 float-left mr-3 inline-flex items-center gap-1.5 rounded-md border border-zinc-800 bg-zinc-950/95 px-2 py-1.5 text-xs text-zinc-400 hover:text-blue-300"><ArrowLeft className="h-3.5 w-3.5" />Back</Link>}
      <NavigationSearchContext.Provider value={parsed.searchParams.get("search") ?? undefined}>{children}</NavigationSearchContext.Provider>
    </div>
  </main></MarkBackContext.Provider></BackHrefContext.Provider></ViewMemoryContext.Provider>;
}
