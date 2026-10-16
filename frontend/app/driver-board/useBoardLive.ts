"use client";

import { useContext, useEffect, useLayoutEffect, useRef, useState } from "react";
import { CurrentUserIDContext } from "@/app/lib/access";
import { clearBoardPresence, fetchBoardPresence, subscribeBoardEvents, updateBoardPresence } from "@/app/lib/api";
import type { BoardPresence } from "@/app/lib/types";
import { selectedBoardCell } from "./boardCells";

export function useBoardLive(onChanged: () => void) {
  const userId = useContext(CurrentUserIDContext);
  const [clientId] = useState(() => crypto.randomUUID());
  const [peers, setPeers] = useState<BoardPresence[]>([]);
  const [connected, setConnected] = useState(false);
  const callback = useRef(onChanged);
  useLayoutEffect(() => { callback.current = onChanged; }, [onChanged]);
  useEffect(() => {
    let active = true, reading = false, pending = false;
    let selection: { driverId: string; column: string } | null = null;
    let readTimer: ReturnType<typeof setTimeout> | undefined;
    let staleTimer: ReturnType<typeof setTimeout> | undefined;
    let writes = Promise.resolve();
    const read = async () => {
      if (!active || document.hidden || !navigator.onLine) return;
      if (reading) { pending = true; return; }
      reading = true;
      try {
        const rows = await fetchBoardPresence();
        if (active) {
          setPeers(rows.filter(p => p.clientId !== clientId && p.userId !== userId));
          clearTimeout(staleTimer); staleTimer = setTimeout(() => { if (active) setPeers([]); }, 35000);
        }
      } catch { /* The next presence event or expiry check retries without blocking editing. */ }
      finally { reading = false; if (pending) { pending = false; schedule(); } }
    };
    const schedule = () => { clearTimeout(readTimer); readTimer = setTimeout(() => void read(), 60); };
    const announce = () => {
      const cell = selection;
      if (!cell || !active || document.hidden || !navigator.onLine) return;
      // Serialize cursor writes so an older request cannot replace a newer cell.
      writes = writes.catch(() => {}).then(async () => { if (active && !document.hidden && navigator.onLine) await updateBoardPresence(clientId, cell.driverId, cell.column); }).catch(() => {});
    };
    const select = (event: Event) => {
      const cell = selectedBoardCell(event.target);
      if (!cell || !/^[0-9a-f-]{36}$/i.test(cell.driverId) || !cell.column || (selection?.driverId === cell.driverId && selection?.column === cell.column)) return;
      selection = cell; announce();
    };
    const leave = () => { void clearBoardPresence(clientId).catch(() => {}); };
    const visible = () => {
      if (document.hidden) leave();
      else { announce(); schedule(); callback.current(); }
    };
    let close = () => {};
    const connect = () => {
      close();
      if (navigator.onLine) close = subscribeBoardEvents(() => callback.current(), schedule, value => { if (active) setConnected(value); }, () => { if (active) setPeers([]); });
    };
    const online = () => { connect(); announce(); schedule(); callback.current(); };
    const offline = () => { close(); setConnected(false); setPeers([]); };
    connect();
    schedule();
    const heartbeat = setInterval(announce, 10000);
    const expiry = setInterval(() => void read(), 5000);
    document.addEventListener("focusin", select); document.addEventListener("pointerup", select);
    document.addEventListener("visibilitychange", visible); window.addEventListener("pagehide", leave);
    window.addEventListener("online", online); window.addEventListener("offline", offline);
    return () => {
      active = false; close(); clearInterval(heartbeat); clearInterval(expiry); clearTimeout(readTimer); clearTimeout(staleTimer);
      document.removeEventListener("focusin", select); document.removeEventListener("pointerup", select);
      document.removeEventListener("visibilitychange", visible); window.removeEventListener("pagehide", leave);
      window.removeEventListener("online", online); window.removeEventListener("offline", offline);
      leave(); void writes.finally(leave);
    };
  }, [clientId, userId]);
  return { peers, connected };
}
