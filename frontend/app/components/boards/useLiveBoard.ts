"use client";

import { useContext, useEffect, useLayoutEffect, useRef, useState } from "react";
import { CurrentUserIDContext } from "@/app/lib/access";

type Peer = { clientId: string; userId: string };
export type LiveBoardOptions<T, P extends Peer, S extends { driverId: string }> = {
  read: () => Promise<P[]>;
  write: (clientId: string, cell: S) => Promise<void>;
  clear: (clientId: string) => Promise<void>;
  select: (target: EventTarget | null) => S | null;
  subscribe: (changed: (board?: T) => void, presence: (peers?: P[]) => void, connection: (connected: boolean) => void, unauthorized: () => void) => () => void;
};

export function useLiveBoard<T, P extends Peer, S extends { driverId: string }>(options: LiveBoardOptions<T, P, S>, onChanged: (board?: T) => void) {
  const userId = useContext(CurrentUserIDContext);
  const [peers, setPeers] = useState<P[]>([]);
  const [connected, setConnected] = useState(false);
  const callback = useRef(onChanged);
  useLayoutEffect(() => { callback.current = onChanged; }, [onChanged]);
  useEffect(() => {
    // Each scope/week gets a fresh ID, so an old cleanup cannot remove a new cursor.
    const clientId = crypto.randomUUID();
    let active = true, reading = false, pending = false, authorized = true;
    let selection: S | null = null;
    let readTimer: ReturnType<typeof setTimeout> | undefined;
    let staleTimer: ReturnType<typeof setTimeout> | undefined;
    let writes = Promise.resolve();
    const ready = () => active && authorized && !document.hidden && navigator.onLine;
    const accept = (rows: P[]) => {
      if (!ready()) return;
      setPeers(rows.filter(p => p.clientId !== clientId && p.userId !== userId));
      clearTimeout(staleTimer); staleTimer = setTimeout(() => { if (active) setPeers([]); }, 35000);
    };
    const read = async () => {
      if (!ready()) return;
      if (reading) { pending = true; return; }
      reading = true;
      try { accept(await options.read()); }
      catch { /* Retry on a stream event or expiry check. */ }
      finally { reading = false; if (pending) { pending = false; schedule(); } }
    };
    const schedule = () => { clearTimeout(readTimer); readTimer = setTimeout(() => void read(), 0); };
    const announce = () => {
      const cell = selection;
      if (!cell || !ready()) return;
      writes = writes.catch(() => {}).then(async () => { if (ready()) await options.write(clientId, cell); }).catch(() => {});
    };
    const select = (event: Event) => {
      const cell = options.select(event.target);
      if (!cell || !/^[0-9a-f-]{36}$/i.test(cell.driverId) || JSON.stringify(cell) === JSON.stringify(selection)) return;
      selection = cell; announce();
    };
    const leave = () => { void options.clear(clientId).catch(() => {}); };
    const visible = () => {
      if (document.hidden) { leave(); void writes.then(() => { if (document.hidden) leave(); }); }
      else { announce(); schedule(); callback.current(); }
    };
    let close = () => {};
    const connect = () => {
      close();
      if (navigator.onLine && authorized) close = options.subscribe(value => callback.current(value), rows => rows ? accept(rows) : schedule(), value => { if (active) setConnected(value); }, () => { authorized = false; if (active) { setPeers([]); setConnected(false); } });
    };
    const online = () => { connect(); announce(); schedule(); callback.current(); };
    const offline = () => { close(); setConnected(false); setPeers([]); };
    connect(); schedule();
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
  }, [options, userId]);
  return { peers, connected };
}
