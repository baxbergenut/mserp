"use client";

import { useEffect, useState } from "react";
import { formatPhone, normalizePhone } from "@/app/lib/phone";

export function PhoneCell({ name, phone }: { name: string; phone: string }) {
  const [feedback, setFeedback] = useState("");
  const digits = normalizePhone(phone);
  useEffect(() => {
    if (!feedback) return;
    const timer = setTimeout(() => setFeedback(""), 1500);
    return () => clearTimeout(timer);
  }, [feedback]);
  if (!digits) return <span className="px-1">—</span>;
  const number = `+1${digits}`;
  return <a href={`tel:${number}`} aria-label={`${name} · Phone`} title={`${formatPhone(phone)} · Click to copy · Double-click to call`}
    className="flex h-8 w-full select-none items-center overflow-hidden whitespace-nowrap px-1 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200 focus:ring-1 focus:ring-inset focus:ring-blue-500"
    onClick={event => {
      // The second click keeps the native tel: action. Copy immediately on the
      // first click so browsers retain clipboard user-activation permission.
      if (event.detail >= 2) return;
      event.preventDefault();
      if (!navigator.clipboard) { setFeedback("Copy failed"); return; }
      navigator.clipboard.writeText(number).then(() => setFeedback("Copied"), () => setFeedback("Copy failed"));
    }}><span className={feedback === "Copied" ? "text-zinc-500" : ""}>{feedback || formatPhone(phone)}</span><span className="sr-only" role="status">{feedback}</span></a>;
}
