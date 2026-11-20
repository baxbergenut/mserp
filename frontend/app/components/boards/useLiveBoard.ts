"use client";

import { useContext, useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from "react";
import { createBoardCursorPublisher } from "./boardCursorPublisher";
import { CurrentUserIDContext } from "@/app/lib/access";

type Peer = { clientId: string; userId: string; expiresAt: string };
export type LiveBoardOptions<T, P extends Peer, S extends { driverId: string }> = {
  read: () => Promise<P[]>;
  write: (clientId: string, cell: S) => Promise<void>;
  clear: (clientId: string) => Promise<void>;
  select: (target: EventTarget | null) => S | null;
  subscribe: (changed: (board?: T) => void, presence: (peers?: P[]) => void, connection: (connected: boolean) => void, unauthorized: () => void) => () => void;
};

// Peer frames update only the cursor overlay. Re-rendering every editor for a
// cursor movement competes with the dispatcher's own input and save receipts.
function createPresenceStore<P>() {
  let peers: P[] = [];
  const listeners = new Set<() => void>();
  return {
    getSnapshot: () => peers,
    subscribe: (listener: () => void) => {
      listeners.add(listener);
      return () => { listeners.delete(listener); };
    },
    publish: (rows: P[]) => {
      if (peers === rows) return;
      peers = rows;
      for (const listener of listeners) listener();
    },
  };
}
export type BoardPresenceStore<P> = ReturnType<typeof createPresenceStore<P>>;
export function useBoardPeers<P>(store: BoardPresenceStore<P>) {
  return useSyncExternalStore(store.subscribe, store.getSnapshot, store.getSnapshot);
}

export function useLiveBoard<T, P extends Peer, S extends { driverId: string }>(options: LiveBoardOptions<T, P, S>, onChanged: (board?: T) => void) {
  const userId = useContext(CurrentUserIDContext);
  const [presence] = useState(createPresenceStore<P>);
  useLayoutEffect(() => { presence.publish([]); }, [presence, options, userId]);
  const [connected, setConnected] = useState(false);
  const callback = useRef(onChanged);
  useLayoutEffect(() => { callback.current = onChanged; }, [onChanged]);
  useEffect(() => {
    // Each scope/week gets a fresh ID, so an old cleanup cannot remove a new cursor.
    const clientId = crypto.randomUUID();
    let active = true, reading = false, pending = false, authorized = true;
    let streaming = false, lastSnapshot = 0;
    let pushVersion = 0;
    let latest: P[] = [];
    let readTimer: ReturnType<typeof setTimeout> | undefined;
    let staleTimer: ReturnType<typeof setTimeout> | undefined;
    const ready = () => active && authorized && !document.hidden && navigator.onLine;
    const cursor = createBoardCursorPublisher<S>(ready, cell => options.write(clientId, cell), () => options.clear(clientId));
    const accept = (rows: P[]) => {
      lastSnapshot = Date.now();
      latest = rows.filter(p => p.clientId !== clientId && p.userId !== userId);
      if (ready()) presence.publish(latest);
      clearTimeout(staleTimer);
      staleTimer = setTimeout(() => { latest = []; if (active) presence.publish([]); }, 35000);
    };
    const read = async () => {
      if (!ready()) return;
      if (reading) { pending = true; return; }
      reading = true;
      const started = pushVersion;
      try { const rows = await options.read(); if (started === pushVersion) accept(rows); }
      catch { /* Retry on a stream event or expiry check. */ }
      finally { reading = false; if (pending) { pending = false; schedule(); } }
    };
    const schedule = () => { clearTimeout(readTimer); readTimer = setTimeout(() => void read(), 0); };
    const select = (event: Event) => {
      const cell = options.select(event.target);
      if (!cell || !/^[0-9a-f-]{36}$/i.test(cell.driverId)) return;
      cursor.select(cell);
    };
    const leave = cursor.leave;
    const visible = () => {
      if (document.hidden) leave();
      else { cursor.resume(); if (streaming && Date.now() - lastSnapshot < 35000) presence.publish(latest); else schedule(); callback.current(); }
    };
    let close = () => {};
    const connect = () => {
      close();
      if (navigator.onLine && authorized) close = options.subscribe(value => callback.current(value), rows => { if (rows) { pushVersion++; accept(rows); } else schedule(); }, value => { streaming = value; if (active) setConnected(value); }, () => { authorized = false; if (active) { presence.publish([]); setConnected(false); } });
    };
    const online = () => { connect(); cursor.resume(); callback.current(); };
    const offline = () => { streaming = false; close(); setConnected(false); presence.publish([]); };
    connect();
    // The stream sends an initial peers frame, pushes moves and confirms leases on heartbeat.
    const expiry = setInterval(() => { if (!streaming || Date.now() - lastSnapshot >= 35000) void read(); }, 15000);
    document.addEventListener("focusin", select); document.addEventListener("pointerup", select);
    document.addEventListener("visibilitychange", visible); window.addEventListener("pagehide", leave);
    window.addEventListener("online", online); window.addEventListener("offline", offline);
    return () => {
      active = false; close(); clearInterval(expiry); clearTimeout(readTimer); clearTimeout(staleTimer);
      document.removeEventListener("focusin", select); document.removeEventListener("pointerup", select);
      document.removeEventListener("visibilitychange", visible); window.removeEventListener("pagehide", leave);
      window.removeEventListener("online", online); window.removeEventListener("offline", offline);
      leave();
    };
  }, [options, userId, presence]);
  return { presence, connected };
}
