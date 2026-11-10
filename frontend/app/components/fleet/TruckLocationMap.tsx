"use client";

import { useEffect, useEffectEvent, useRef, useState } from "react";
import type * as Leaflet from "leaflet";
import "leaflet/dist/leaflet.css";
import type { DriverBoardLocation } from "@/app/lib/types";
import { isOldLocation } from "@/app/driver-board/board";

function locationIcon(L: typeof Leaflet, heading: number | null | undefined, old: boolean) {
  const hasHeading = heading != null && Number.isFinite(heading) && heading >= 0 && heading < 360;
  const marker = document.createElement("div");
  marker.setAttribute("role", "img");
  marker.setAttribute("aria-label", hasHeading ? `Reported heading ${heading} degrees` : "Truck location; heading unavailable");
  marker.dataset.heading = hasHeading ? String(heading) : "unknown";
  marker.style.cssText = `width:30px;height:30px;border:2px solid white;border-radius:50%;background:${old ? "#71717a" : "#8b5cf6"};box-shadow:0 1px 8px #0008;display:grid;place-items:center;transform:rotate(${hasHeading ? heading : 0}deg)`;
  marker.innerHTML = hasHeading
    ? '<svg width="20" height="20" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3 20 21 12 17 4 21Z" fill="white"/></svg>'
    : '<span style="width:9px;height:9px;background:white;border-radius:50%"></span>';
  return L.divIcon({ html: marker, className: "truck-heading-marker", iconSize: [30, 30], iconAnchor: [15, 15] });
}

export function TruckLocationMap({ location }: { location: DriverBoardLocation }) {
  const container = useRef<HTMLDivElement>(null);
  const runtime = useRef<{ L: typeof Leaflet; map: Leaflet.Map; marker: Leaflet.Marker } | null>(null);
  const [failed, setFailed] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(timer);
  }, []);
  const old = isOldLocation(location.reportedAt, now);
  const { latitude, longitude, heading } = location;
  const latest = useEffectEvent(() => ({ latitude, longitude, heading, old }));

  // Keep the map instance for this page visit: telemetry/heading/age changes
  // update the marker instead of recreating the map and replaying the entrance.
  useEffect(() => {
    if (!container.current) return;
    const element = container.current;
    let cancelled = false;
    let map: Leaflet.Map | undefined;
    let resize: ResizeObserver | undefined;
    let visibility: IntersectionObserver | undefined;
    let introFinished = false;
    let removeVisibilityListener: (() => void) | undefined;
    let intro: ReturnType<typeof setTimeout> | undefined;
    const motion = window.matchMedia("(prefers-reduced-motion: reduce)");
    const stopIntro = () => { introFinished = true; clearTimeout(intro); map?.stop(); };
    const reduceMotion = () => {
      if (!motion.matches || !map) return;
      stopIntro();
      const point = runtime.current?.marker.getLatLng();
      if (point) map.setView(point, 10, { animate: false });
    };
    void import("leaflet").then(L => {
      if (cancelled) return;
      const current = latest();
      const point: Leaflet.LatLngTuple = [current.latitude, current.longitude];
      map = L.map(element, { scrollWheelZoom: false, minZoom: 0, fadeAnimation: false }).setView(point, motion.matches ? 10 : 0);
      const tileLayer = L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", {
        // Send only the origin to OSM, never private profile paths/IDs.
        referrerPolicy: "strict-origin",
        maxZoom: 19, attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
      }).addTo(map);
      const tiles = map.getPane("tilePane");
      if (tiles) tiles.style.filter = "invert(1) hue-rotate(180deg) brightness(.75) contrast(.85)";
      const marker = L.marker(point, { icon: locationIcon(L, current.heading, current.old), interactive: false, keyboard: false }).addTo(map);
      runtime.current = { L, map, marker };
      // Wait for painted world tiles and a visible card, not a mount timer.
      // Otherwise the entire entrance can run offscreen or behind loading tiles.
      let visible = false;
      let ready = !tileLayer.isLoading();
      const scheduleIntro = () => {
        clearTimeout(intro);
        if (cancelled || introFinished || motion.matches || !visible || !ready || document.hidden) return;
        intro = setTimeout(() => {
          if (cancelled || introFinished || document.hidden) return;
          introFinished = true;
          map?.flyTo(marker.getLatLng(), 10, { duration: 3.2, animate: true });
        }, 500);
      };
      tileLayer.once("load", () => { ready = true; scheduleIntro(); });
      visibility = new IntersectionObserver(entries => {
        visible = entries.some(entry => entry.isIntersecting && entry.intersectionRatio >= 0.5);
        scheduleIntro();
      }, { threshold: [0, 0.5] });
      visibility.observe(element);
      document.addEventListener("visibilitychange", scheduleIntro);
      removeVisibilityListener = () => document.removeEventListener("visibilitychange", scheduleIntro);
      element.addEventListener("pointerdown", stopIntro);
      element.addEventListener("keydown", stopIntro);
      motion.addEventListener("change", reduceMotion);
      resize = new ResizeObserver(() => map?.invalidateSize({ pan: false }));
      resize.observe(element);
    }).catch(() => { if (!cancelled) setFailed(true); });
    return () => {
      cancelled = true;
      clearTimeout(intro);
      element.removeEventListener("pointerdown", stopIntro);
      element.removeEventListener("keydown", stopIntro);
      motion.removeEventListener("change", reduceMotion);
      resize?.disconnect();
      visibility?.disconnect();
      removeVisibilityListener?.();
      runtime.current = null;
      map?.remove();
    };
  }, []);

  useEffect(() => {
    const active = runtime.current;
    if (!active) return;
    const previous = active.marker.getLatLng();
    active.marker.setLatLng([latitude, longitude]).setIcon(locationIcon(active.L, heading, old));
    if (previous.lat !== latitude || previous.lng !== longitude) {
      active.map.setView([latitude, longitude], active.map.getZoom(), { animate: false });
    }
  }, [latitude, longitude, heading, old]);

  return <div className="relative isolate border-t border-zinc-800">
    <div ref={container} aria-label="Truck location map" className="h-64 w-full bg-zinc-900" />
    {failed && <p role="status" className="absolute inset-x-4 top-4 z-[1000] rounded bg-zinc-950 p-3 text-xs text-zinc-400">Map could not load. Coordinates remain available above.</p>}
  </div>;
}
