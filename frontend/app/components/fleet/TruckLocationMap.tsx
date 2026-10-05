"use client";

import { useEffect, useRef, useState } from "react";
import type { Map as LeafletMap } from "leaflet";
import "leaflet/dist/leaflet.css";
import type { DriverBoardLocation } from "@/app/lib/types";
import { isOldLocation } from "@/app/driver-board/board";

export function TruckLocationMap({ location }: { location: DriverBoardLocation }) {
  const container = useRef<HTMLDivElement>(null);
  const [failed, setFailed] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(timer);
  }, []);
  const old = isOldLocation(location.reportedAt, now);
  const { latitude, longitude, heading } = location;
  useEffect(() => {
    if (!container.current) return;
    const element = container.current;
    let cancelled = false;
    let map: LeafletMap | undefined;
    let resize: ResizeObserver | undefined;
    void import("leaflet").then(L => {
      if (cancelled) return;
      map = L.map(element, { scrollWheelZoom: false, zoomAnimation: false, fadeAnimation: false }).setView([latitude, longitude], 10);
      L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", {
        maxZoom: 19, attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
      }).addTo(map);
      const tiles = map.getPane("tilePane");
      if (tiles) tiles.style.filter = "invert(1) hue-rotate(180deg) brightness(.75) contrast(.85)";
      const hasHeading = heading != null && Number.isFinite(heading) && heading >= 0 && heading < 360;
      const marker = document.createElement("div");
      marker.setAttribute("role", "img");
      marker.setAttribute("aria-label", hasHeading ? `Reported heading ${heading} degrees` : "Truck location; heading unavailable");
      marker.dataset.heading = hasHeading ? String(heading) : "unknown";
      marker.style.cssText = `width:30px;height:30px;border:2px solid white;border-radius:50%;background:${old ? "#71717a" : "#8b5cf6"};box-shadow:0 1px 8px #0008;display:grid;place-items:center;transform:rotate(${hasHeading ? heading : 0}deg)`;
      marker.innerHTML = hasHeading
        ? '<svg width="20" height="20" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3 20 21 12 17 4 21Z" fill="white"/></svg>'
        : '<span style="width:9px;height:9px;background:white;border-radius:50%"></span>';
      L.marker([latitude, longitude], { icon: L.divIcon({ html: marker, className: "truck-heading-marker", iconSize: [30, 30], iconAnchor: [15, 15] }), interactive: false, keyboard: false }).addTo(map);
      resize = new ResizeObserver(() => map?.invalidateSize());
      resize.observe(element);
    }).catch(() => { if (!cancelled) setFailed(true); });
    return () => { cancelled = true; resize?.disconnect(); map?.remove(); };
  }, [latitude, longitude, heading, old]);
  return <div className="relative isolate border-t border-zinc-800">
    <div ref={container} aria-label="Truck location map" className="h-64 w-full bg-zinc-900" />
    {failed && <p role="status" className="absolute inset-x-4 top-4 z-[1000] rounded bg-zinc-950 p-3 text-xs text-zinc-400">Map could not load. Coordinates remain available above.</p>}
  </div>;
}
