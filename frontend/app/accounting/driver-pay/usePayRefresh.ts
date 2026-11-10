"use client";

import { useEffect, useLayoutEffect, useRef } from "react";

// One reader owns all invalidations. New hints during a read queue one trailing
// read; editing pauses it without forgetting the hint or replacing a draft.
export function usePayRefresh<T>({ scope, request, blocked, load, receive, failed }: {
  scope: string; request: number; blocked: boolean;
  load: (signal: AbortSignal) => Promise<T>;
  receive: (value: T) => void;
  failed: (reason: unknown) => void;
}) {
  const current = useRef({ scope, request, blocked, load, receive, failed, epoch: 0 });
  useLayoutEffect(() => {
    const previous = current.current;
    // A draft/command can start AND finish while a read is in flight. Checking
    // only the current busy flag would then replace its newer committed receipt.
    const epoch = previous.epoch + Number(scope !== previous.scope || (blocked && !previous.blocked));
    current.current = { scope, request, blocked, load, receive, failed, epoch };
  });
  const wake = useRef<() => void>(() => {});
  useEffect(() => {
    let cancelled = false, inFlight = false;
    let processed = current.current.request;
    let delay = 1000;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let controller: AbortController | undefined;
    const schedule = () => {
      if (cancelled || inFlight || current.current.blocked || current.current.request === processed) return;
      clearTimeout(timer);
      timer = setTimeout(run, 50);
    };
    const run = () => {
      if (cancelled || inFlight || current.current.blocked || current.current.request === processed) return;
      const target = current.current.request;
      const epoch = current.current.epoch;
      controller = new AbortController();
      inFlight = true;
      let retry = false;
      current.current.load(controller.signal).then(value => {
        if (cancelled || current.current.scope !== scope) return;
        if (!current.current.blocked && current.current.epoch === epoch) {
          current.current.receive(value);
          processed = target;
          delay = 1000;
        }
      }).catch(reason => {
        if (cancelled || current.current.scope !== scope) return;
        if (current.current.epoch !== epoch) return;
        current.current.failed(reason);
        retry = true;
      }).finally(() => {
        inFlight = false;
        if (cancelled || current.current.scope !== scope) return;
        if (retry) {
          timer = setTimeout(run, delay);
          delay = Math.min(delay * 2, 30_000);
        } else schedule();
      });
    };
    wake.current = schedule;
    return () => { cancelled = true; clearTimeout(timer); controller?.abort(); wake.current = () => {}; };
  }, [scope]);
  useEffect(() => { wake.current(); }, [scope, request, blocked]);
}
