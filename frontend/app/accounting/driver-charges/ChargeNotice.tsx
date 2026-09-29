"use client";

import { useEffect } from "react";
import { createPortal } from "react-dom";
import { X } from "lucide-react";

export type ChargeNotification = { message: string; error?: boolean };

export default function ChargeNotice({ notice, dismiss }: { notice: ChargeNotification | null; dismiss: () => void }) {
  useEffect(() => {
    if (!notice || notice.error) return;
    const timer = window.setTimeout(dismiss, 4000);
    return () => window.clearTimeout(timer);
  }, [notice, dismiss]);
  if (!notice || typeof document === "undefined") return null;
  // Portaling keeps the toast fixed to the viewport, outside the page's
  // animated transform and table layout.
  return createPortal(<div role={notice.error ? "alert" : "status"} className={`fixed bottom-5 right-5 z-[100] flex max-w-[min(28rem,calc(100vw-2.5rem))] items-start gap-3 rounded-lg border bg-zinc-950 px-4 py-3 text-sm shadow-xl ${notice.error ? "border-red-500/40 text-red-300" : "border-emerald-500/30 text-emerald-300"}`}>
    <span>{notice.message}</span><button type="button" aria-label="Dismiss notification" onClick={dismiss} className="shrink-0 rounded p-0.5 text-zinc-400 hover:text-white"><X size={16} /></button>
  </div>, document.body);
}
