"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { usePermissions } from "../lib/access";
import { fetchFeedbackMessageCount, fetchTaskCount, subscribeTaskEvents } from "../lib/api";

const TaskUpdates = createContext({ count: 0, feedbackCount: 0, revision: 0, refresh: () => {} });
export const useTaskUpdates = () => useContext(TaskUpdates);

export function TaskUpdatesProvider({ children }: { children: React.ReactNode }) {
  const allowed = usePermissions().includes("tasks.read");
  const [count, setCount] = useState(0);
  const [feedbackCount, setFeedbackCount] = useState(0);
  const [revision, setRevision] = useState(0);
  const refresh = useCallback(() => setRevision(value => value + 1), []);
  const refreshCounts = useRef<() => void>(() => {});
  useEffect(() => {
    let active = true;
    // A change arriving during a read needs one subsequent read of committed data.
    // Keep focus, visibility, subscription and timer bursts from stacking requests.
    function reader(fetchCount: () => Promise<{ count: number }>, apply: (n: number) => void, clearOnError: boolean) {
      let running = false, pending = false;
      return async () => {
        if (document.hidden || !active) return;
        pending = true;
        if (running) return;
        running = true;
        try {
          while (active && pending && !document.hidden) {
            pending = false;
            try {
              const value = await fetchCount();
              if (active && !pending && Number.isFinite(value.count)) apply(value.count);
            } catch {
              if (active && !pending && clearOnError) apply(0);
            }
          }
        } finally { running = false; }
      };
    }
    const feedback = reader(fetchFeedbackMessageCount, setFeedbackCount, false);
    const tasks = reader(fetchTaskCount, setCount, true);
    const both = () => { void feedback(); if (allowed) void tasks(); };
    refreshCounts.current = both;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const changed = () => {
      clearTimeout(timer);
      timer = setTimeout(refresh, 150);
    };
    const visible = () => { if (!document.hidden) changed(); };
    const close = allowed ? subscribeTaskEvents(changed, () => { setCount(0); changed(); }) : undefined;
    const poll = window.setInterval(feedback, 30000);
    window.addEventListener("focus", visible);
    document.addEventListener("visibilitychange", visible);
    return () => {
      active = false;
      close?.();
      clearTimeout(timer);
      clearInterval(poll);
      window.removeEventListener("focus", visible);
      document.removeEventListener("visibilitychange", visible);
    };
  }, [allowed, refresh]);
  useEffect(() => { refreshCounts.current(); }, [allowed, revision]);
  const value = useMemo(() => ({ count: (allowed ? count : 0) + feedbackCount, feedbackCount, revision, refresh }), [allowed, count, feedbackCount, revision, refresh]);
  return <TaskUpdates.Provider value={value}>{children}</TaskUpdates.Provider>;
}
