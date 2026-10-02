"use client";

import { createContext, useContext, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";

const HeaderTarget = createContext<HTMLElement | null | undefined>(undefined);
const RegisterHeaderTarget = createContext<((target: HTMLElement | null) => void) | undefined>(undefined);

export function PageHeaderProvider({ children }: { children: ReactNode }) {
  const [target, setTarget] = useState<HTMLElement | null>(null);
  return <RegisterHeaderTarget.Provider value={setTarget}><HeaderTarget.Provider value={target}>{children}</HeaderTarget.Provider></RegisterHeaderTarget.Provider>;
}

export function PageHeaderSlot() {
  const register = useContext(RegisterHeaderTarget);
  return <div ref={register} data-page-header className="empty:hidden border-t border-zinc-800/40 px-4 py-3 sm:px-6 xl:px-8 [&>div]:min-w-0 [&>div]:flex-wrap [&_h1]:break-words [&_p]:break-words" />;
}

// Keep each page's handlers and live counts in its own component, while rendering
// its heading in the persistent shell. Portals preserve React context and events.
export function PageHeader({ children }: { children: ReactNode }) {
  const target = useContext(HeaderTarget);
  if (target === undefined) return <>{children}</>;
  return target ? createPortal(children, target) : null;
}
