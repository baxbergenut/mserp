"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
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
  useEffect(() => {
    let active = true;
    const load = () => { if (!document.hidden) void fetchFeedbackMessageCount().then(value => { if (active) setFeedbackCount(value.count); }).catch(() => {}); };
    load();
    const timer = window.setInterval(load, 30000);
    window.addEventListener("focus", load); document.addEventListener("visibilitychange", load);
    return () => { active = false; clearInterval(timer); window.removeEventListener("focus", load); document.removeEventListener("visibilitychange", load); };
  }, [revision]);
  useEffect(() => {
    if (!allowed) return;
    let active = true;
    fetchTaskCount().then(value => { if (active) setCount(value.count); }).catch(() => { if (active) setCount(0); });
    return () => { active = false; };
  }, [allowed, revision]);
  useEffect(() => {
    if (!allowed) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const changed = () => {
      clearTimeout(timer);
      timer = setTimeout(refresh, 150);
    };
    const visible = () => { if (!document.hidden) changed(); };
    const close = subscribeTaskEvents(changed, () => { setCount(0); refresh(); });
    window.addEventListener("focus", visible);
    document.addEventListener("visibilitychange", visible);
    return () => { close(); clearTimeout(timer); window.removeEventListener("focus", visible); document.removeEventListener("visibilitychange", visible); };
  }, [allowed, refresh]);
  const value = useMemo(() => ({ count: (allowed ? count : 0) + feedbackCount, feedbackCount, revision, refresh }), [allowed, count, feedbackCount, revision, refresh]);
  return <TaskUpdates.Provider value={value}>{children}</TaskUpdates.Provider>;
}
