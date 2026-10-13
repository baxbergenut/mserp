"use client";

import { useEffect, useState } from "react";
import { Check, MapPin, TriangleAlert } from "lucide-react";
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
  const coordinates = location ? formatCoordinates(location) : "";
  const old = location ? isOldLocation(location.reportedAt, now) : false;
  function copy() {
    if (!navigator.clipboard) { setFeedback("Copy failed"); return; }
    navigator.clipboard.writeText(coordinates).then(() => setFeedback("Copied"), () => setFeedback("Copy failed"));
  }
  return <button type="button" aria-label={`${name} · Copy current location`} disabled={!location}
    title={feedback || (location ? `Copy current location · Five ELD updated ${locationTime(location.reportedAt)} NY${old ? " · Location may be outdated" : ""}` : "Location unavailable")}
    className={`flex h-8 w-6 shrink-0 items-center justify-center rounded enabled:hover:bg-zinc-800 focus-visible:outline-2 focus-visible:outline-blue-500 disabled:opacity-30 ${old ? "text-zinc-500 hover:text-zinc-300" : "text-zinc-300 hover:text-zinc-100"}`}
    onClick={copy}>{feedback === "Copied" ? <Check className="h-4 w-4 text-emerald-400" /> : feedback ? <TriangleAlert className="h-4 w-4 text-amber-400" /> : <MapPin className="h-4 w-4" />}<span className="sr-only" role="status">{feedback}</span></button>;
}
