"use client";

import { useEffect, useState } from "react";
import type { DriverBoardLocation } from "@/app/lib/types";
import { isOldLocation } from "./board";

const locationTime = (value: string) => new Date(value).toLocaleString("en-US", {
  timeZone: "America/New_York", month: "short", day: "numeric", hour: "numeric", minute: "2-digit",
});

export function formatCoordinates(location: DriverBoardLocation) {
  return `${location.latitude.toFixed(5)}, ${location.longitude.toFixed(5)}`;
}

export function LocationCell({ name, location }: { name: string; location: DriverBoardLocation | null }) {
  const [feedback, setFeedback] = useState("");
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(timer);
  }, []);
  useEffect(() => {
    if (!feedback) return;
    const timer = setTimeout(() => setFeedback(""), 1500);
    return () => clearTimeout(timer);
  }, [feedback]);
  if (!location) return null;
  const coordinates = formatCoordinates(location);
  const old = isOldLocation(location.reportedAt, now);
  function copy() {
    if (!navigator.clipboard) { setFeedback("Copy failed"); return; }
    navigator.clipboard.writeText(coordinates).then(() => setFeedback("Copied"), () => setFeedback("Copy failed"));
  }
  return <button type="button" aria-label={`${name} · Latest location`}
    title={`${coordinates} · Five ELD updated ${locationTime(location.reportedAt)} NY · Click to copy`}
    className={`flex h-8 w-full min-w-0 items-center overflow-hidden whitespace-nowrap px-1.5 text-left hover:bg-zinc-800 focus:ring-1 focus:ring-inset focus:ring-blue-500 ${old ? "text-zinc-400 hover:text-zinc-300" : "text-zinc-300 hover:text-zinc-100"}`}
    onClick={copy}><span className="truncate">{feedback || coordinates}</span><span className="sr-only" role="status">{feedback}</span></button>;
}
