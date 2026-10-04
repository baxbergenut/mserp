"use client";

import { useEffect, useRef, useState } from "react";
import { formatPhone, normalizePhone } from "@/app/lib/phone";

export function PhoneCell({ name, phone }: { name: string; phone: string }) {
  const [feedback, setFeedback] = useState("");
  const copyTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  function cancelCopy() {
    if (copyTimer.current !== null) clearTimeout(copyTimer.current);
    copyTimer.current = null;
  }
  useEffect(() => cancelCopy, [phone]);
  const digits = normalizePhone(phone);
  useEffect(() => {
    if (!feedback) return;
    const timer = setTimeout(() => setFeedback(""), 1500);
    return () => clearTimeout(timer);
  }, [feedback]);
  if (!digits) return <span className="px-1">—</span>;
  const number = `+1${digits}`;
  function copy() {
    copyTimer.current = null;
    if (!navigator.clipboard) { setFeedback("Copy failed"); return; }
    navigator.clipboard.writeText(number).then(() => setFeedback("Copied"), () => setFeedback("Copy failed"));
  }
  return <a href={`tel:${number}`} aria-label={`${name} · Phone`} title={`${formatPhone(phone)} · Click to copy · Double-click to call`}
    className="flex h-8 w-full select-none items-center overflow-hidden whitespace-nowrap px-1 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200 focus:ring-1 focus:ring-inset focus:ring-blue-500"
    onMouseDown={event => { if (event.detail >= 2) cancelCopy(); }}
    onClick={event => {
      cancelCopy();
      // Allow the browser's native tel: action only on the second click.
      if (event.detail >= 2) { setFeedback(""); return; }
      event.preventDefault();
      if (event.detail === 0) copy(); // Keyboard activation has no double-click.
      else copyTimer.current = setTimeout(copy, 500);
    }}><span className={feedback === "Copied" ? "text-zinc-500" : ""}>{feedback || formatPhone(phone)}</span><span className="sr-only" role="status">{feedback}</span></a>;
}
