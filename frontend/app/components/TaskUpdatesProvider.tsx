"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import { usePermissions } from "../lib/access";
import { fetchTaskCount, subscribeTaskEvents } from "../lib/api";

const TaskUpdates = createContext({ count: 0, revision: 0, refresh: () => {} });
export const useTaskUpdates = () => useContext(TaskUpdates);

export function TaskUpdatesProvider({ children }: { children: React.ReactNode }) {
  const allowed = usePermissions().includes("tasks.read");
  const [count, setCount] = useState(0);
  const [revision, setRevision] = useState(0);
  const refresh = useCallback(() => setRevision(value => value + 1), []);
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
  const value = useMemo(() => ({ count: allowed ? count : 0, revision, refresh }), [allowed, count, revision, refresh]);
  return <TaskUpdates.Provider value={value}>{children}</TaskUpdates.Provider>;
}
