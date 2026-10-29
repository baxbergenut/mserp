"use client";

import { useEffect, useState } from "react";
import { claimBirthday } from "@/app/lib/api";

function throwConfetti() {
  if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
  const layer = document.createElement("div");
  layer.setAttribute("data-birthday-confetti", "");
  layer.setAttribute("aria-hidden", "true");
  Object.assign(layer.style, { position: "fixed", inset: "0", pointerEvents: "none", zIndex: "10000", overflow: "hidden" });
  document.body.append(layer);
  const colors = ["#60a5fa", "#fbbf24", "#f472b6", "#34d399", "#a78bfa"];
  for (let i = 0; i < 90; i++) {
    const piece = document.createElement("span");
    Object.assign(piece.style, { position: "absolute", left: `${Math.random() * 100}%`, top: "-20px", width: "8px", height: "12px", background: colors[i % colors.length], borderRadius: "2px" });
    layer.append(piece);
    piece.animate([
      { transform: "translate(0, -20px) rotate(0deg)", opacity: 1 },
      { transform: `translate(${(Math.random() - 0.5) * 400}px, ${window.innerHeight + 40}px) rotate(${360 + Math.random() * 720}deg)`, opacity: 0 },
    ], { duration: 2400 + Math.random() * 1200, delay: Math.random() * 350, easing: "ease-in", fill: "forwards" });
  }
  return window.setTimeout(() => layer.remove(), 4200);
}

export function BirthdayGreeting({ userId }: { userId: string }) {
  const [name, setName] = useState<string | null>(null);
  const [countdown, setCountdown] = useState<number | null>(null);
  useEffect(() => {
    let running = false, lastCheck = -Infinity, lastDay = "", active = true;
    let lastActivity = performance.now(), target: number | null = null;
    let serverBase: number | null = null, clockBase = performance.now();
    let countdownValue: number | null = null, revealTimer: number | undefined;
    const now = () => serverBase === null ? Date.now() : serverBase + performance.now() - clockBase;
    const dayAt = (instant: number) => new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Tashkent", year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date(instant));
    const present = () => document.visibilityState === "visible" && document.hasFocus() && performance.now() - lastActivity < 60000;
    const showCount = (value: number | null) => {
      if (countdownValue !== value) { countdownValue = value; setCountdown(value); }
    };
    const check = async (midnight = false) => {
      if (document.visibilityState !== "visible" || running) return;
      const day = dayAt(now());
      if (!midnight && day === lastDay && performance.now() - lastCheck < 60000) return;
      running = true;
      lastCheck = performance.now();
      lastDay = day;
      try {
        const greeting = await claimBirthday();
        if (!active) return;
        const instant = Date.parse(greeting.serverNow);
        if (Number.isFinite(instant)) { serverBase = instant; clockBase = performance.now(); lastDay = dayAt(instant); }
        const next = greeting.countdownAt ? Date.parse(greeting.countdownAt) : NaN;
        target = Number.isFinite(next) ? next : null;
        const reveal = () => {
          if (!active) return;
          showCount(null);
          if (greeting.celebrate) setName(greeting.name || "there");
        };
        // Let zero remain visible briefly before revealing the birthday greeting.
        if (midnight) revealTimer = window.setTimeout(reveal, 350);
        else reveal();
      } catch { showCount(null); /* Retry on later activity without interrupting work. */ }
      finally { running = false; }
    };
    const onActivity = () => {
      if (document.visibilityState !== "visible") { showCount(null); return; }
      lastActivity = performance.now();
      if (countdownValue === null) void check();
    };
    const onBlur = () => showCount(null);
    const tick = () => {
      if (!active || target === null) return;
      if (!present()) { showCount(null); return; }
      const remaining = target - now();
      if (remaining > 5000) return;
      if (remaining <= 0) {
        target = null;
        showCount(0);
        void check(true);
      } else showCount(Math.ceil(remaining / 1000));
    };
    // Defer the initial request so React's development effect replay can clean up.
    const initial = window.setTimeout(onActivity, 0);
    const timer = window.setInterval(tick, 100);
    window.addEventListener("focus", onActivity);
    window.addEventListener("blur", onBlur);
    document.addEventListener("visibilitychange", onActivity);
    document.addEventListener("pointerdown", onActivity, { passive: true });
    document.addEventListener("keydown", onActivity);
    document.addEventListener("wheel", onActivity, { passive: true });
    return () => {
      active = false;
      window.clearTimeout(initial);
      window.clearTimeout(revealTimer);
      window.clearInterval(timer);
      window.removeEventListener("focus", onActivity);
      window.removeEventListener("blur", onBlur);
      document.removeEventListener("visibilitychange", onActivity);
      document.removeEventListener("pointerdown", onActivity);
      document.removeEventListener("keydown", onActivity);
      document.removeEventListener("wheel", onActivity);
    };
  }, [userId]);

  useEffect(() => {
    if (name === null) return;
    const timeout = throwConfetti();
    return () => { if (timeout) window.clearTimeout(timeout); document.querySelectorAll('[data-birthday-confetti]').forEach(layer => layer.remove()); };
  }, [name]);

  return <>{countdown !== null && <div role="timer" aria-label="Birthday countdown" aria-live="assertive" className="fixed inset-0 z-[10002] flex items-center justify-center bg-black text-white"><span className="text-[min(35vw,16rem)] font-semibold tabular-nums">{countdown}</span></div>}{name !== null && <div role="status" className="fixed right-4 top-20 z-[10001] max-w-sm rounded-xl border border-accent/40 bg-zinc-900 p-5 shadow-xl">
    <div className="flex items-start gap-4"><div><p className="text-lg font-semibold text-zinc-100">Happy birthday, {name}! 🎉</p><p className="mt-1 text-sm text-zinc-400">Wishing you a wonderful day from all of us at Clustie.</p></div><button type="button" aria-label="Dismiss birthday greeting" onClick={() => setName(null)} className="text-zinc-400 hover:text-zinc-100">✕</button></div>
  </div>}</>;
}
