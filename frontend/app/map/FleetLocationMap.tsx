"use client";

import { useEffect, useRef, useState } from "react";
import type * as Leaflet from "leaflet";
import "leaflet/dist/leaflet.css";
import { truckHeadingPin, truckHeadingPinSize } from "@/app/components/fleet/truckHeadingPin";
import { statusColor, statusLabel } from "@/app/driver-board/board";
import type { DriverBoardLocation } from "@/app/lib/types";
import type { Coordinates, remainingRoute } from "./routeGeometry";

export interface FleetPin {
  driverId: string;
  driverName: string;
  truckId: string;
  unit: string;
  status: string;
  location: DriverBoardLocation;
}

export interface FleetStopFlag {
  point: Coordinates;
  kind: "pickup" | "delivery";
  location: string;
}

function stopFlagIcon(L: typeof Leaflet, stops: FleetStopFlag[]) {
  const icon = document.createElement("div");
  icon.className = "pointer-events-auto flex h-7 w-max cursor-default items-start gap-0.5 border-l-2 border-zinc-100 pl-0.5 drop-shadow-[0_1px_2px_#000]";
  icon.setAttribute("role", "img");
  const descriptions: string[] = [];
  for (const stop of stops) {
    const pickup = stop.kind === "pickup";
    const label = pickup ? "Pickup" : "Delivery";
    const flag = document.createElement("span");
    flag.className = `py-1 pr-2 pl-1 font-mono text-[11px] font-bold leading-none text-zinc-950 [clip-path:polygon(0_0,100%_0,85%_50%,100%_100%,0_100%)] ${pickup ? "bg-yellow-300" : "bg-blue-300"}`;
    flag.textContent = pickup ? "PU" : "DEL";
    descriptions.push(stop.location ? `${label} · ${stop.location}` : label);
    icon.append(flag);
  }
  icon.title = descriptions.join("\n");
  icon.setAttribute("aria-label", descriptions.join(", "));
  return L.divIcon({ html: icon, className: "fleet-map-stop", iconSize: [0, 28], iconAnchor: [1, 28] });
}

function truckIcon(L: typeof Leaflet, pin: FleetPin, selected: boolean) {
  const icon = document.createElement("div");
  icon.className = "relative";
  const marker = truckHeadingPin(pin.location.heading);
  const fill = statusColor(pin.status).split(" ").find(value => value.startsWith("bg-"));
  marker.className = `${fill || "bg-zinc-700"} group-focus-visible:outline-2 group-focus-visible:outline-offset-2 group-focus-visible:outline-sky-300 [&>*]:drop-shadow-[0_1px_1px_#000b]`;
  if (selected) marker.classList.add("outline-2", "outline-offset-2", "outline-sky-300");

  const label = document.createElement("span");
  label.className = "absolute bottom-full left-1/2 mb-1 w-max -translate-x-1/2 whitespace-nowrap rounded border border-white/60 bg-zinc-950 px-1.5 py-0.5 font-mono text-[11px] font-bold leading-none text-white shadow-[0_1px_5px_#000b]";
  label.setAttribute("aria-hidden", "true");
  label.textContent = pin.unit || "—";
  icon.append(marker, label);
  return L.divIcon({ html: icon, className: "fleet-map-pin group", iconSize: [truckHeadingPinSize, truckHeadingPinSize] });
}

export function FleetLocationMap({ pins, selectedDriverId, onSelect, route, trail, stops }: {
  pins: FleetPin[];
  selectedDriverId: string;
  onSelect: (driverId: string) => void;
  route: ReturnType<typeof remainingRoute>;
  trail: Coordinates[][];
  stops: FleetStopFlag[];
}) {
  const container = useRef<HTMLDivElement>(null);
  const [runtime, setRuntime] = useState<{ L: typeof Leaflet; map: Leaflet.Map } | null>(null);
  const [failed, setFailed] = useState(false);
  const fittedFleet = useRef(false);

  useEffect(() => {
    const element = container.current;
    if (!element) return;
    let cancelled = false;
    let map: Leaflet.Map | undefined;
    let resize: ResizeObserver | undefined;
    void import("leaflet").then(L => {
      if (cancelled) return;
      map = L.map(element, { fadeAnimation: false }).setView([39.5, -98.35], 4);
      L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", {
        referrerPolicy: "strict-origin",
        maxZoom: 19,
        attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
      }).addTo(map);
      const tiles = map.getPane("tilePane");
      if (tiles) tiles.style.filter = "invert(1) hue-rotate(180deg) brightness(.75) contrast(.85)";
      resize = new ResizeObserver(() => map?.invalidateSize({ pan: false }));
      resize.observe(element);
      fittedFleet.current = false;
      setRuntime({ L, map });
    }).catch(() => { if (!cancelled) setFailed(true); });
    return () => {
      cancelled = true;
      resize?.disconnect();
      map?.remove();
    };
  }, []);

  useEffect(() => {
    if (!runtime) return;
    const { L, map } = runtime;
    const layer = L.layerGroup().addTo(map);
    // A team shares one physical truck. Use the selected driver's status when focused.
    const trucks = new Map<string, FleetPin>();
    for (const pin of pins) {
      const key = pin.truckId || pin.driverId;
      if (!trucks.has(key) || pin.driverId === selectedDriverId) trucks.set(key, pin);
    }
    const bounds = L.latLngBounds([]);
    for (const pin of trucks.values()) {
      const point: Leaflet.LatLngTuple = [pin.location.latitude, pin.location.longitude];
      bounds.extend(point);
      const selected = pin.driverId === selectedDriverId;
      if (selectedDriverId && !selected) continue;
      const marker = L.marker(point, {
        icon: truckIcon(L, pin, selected),
        title: [pin.unit ? `Truck ${pin.unit}` : "Truck", pin.driverName, statusLabel(pin.status)].filter(Boolean).join(" · "),
        zIndexOffset: selected ? 1000 : 0,
        autoPanOnFocus: false,
      }).on("click", () => onSelect(pin.driverId)).addTo(layer);
      const element = marker.getElement();
      if (element) element.setAttribute("aria-label", marker.options.title ?? pin.driverName);
    }
    if (!fittedFleet.current && bounds.isValid()) {
      map.fitBounds(bounds, { paddingTopLeft: [40, 52], paddingBottomRight: [40, 40], maxZoom: 10, animate: false });
      fittedFleet.current = true;
    }
    return () => { layer.remove(); };
  }, [runtime, pins, selectedDriverId, onSelect]);

  useEffect(() => {
    if (!runtime || (!route && !trail.length)) return;
    const { L, map } = runtime;
    const layer = L.layerGroup().addTo(map);
    if (trail.length) {
      L.polyline(trail, { color: "#451a03", weight: 6, interactive: false }).addTo(layer);
      L.polyline(trail, {
        color: "#fbbf24", weight: 3, dashArray: "5 6", interactive: false, className: "fleet-map-trail",
      }).addTo(layer);
    }
    if (route) {
      L.polyline(route.points, { color: "#082f49", weight: 7, interactive: false }).addTo(layer);
      L.polyline(route.points, {
        color: "#38bdf8", weight: 4, interactive: false, dashArray: route.stale ? "7 7" : undefined,
        className: "fleet-map-route",
      }).addTo(layer);
    }
    return () => { layer.remove(); };
  }, [runtime, route, trail]);

  useEffect(() => {
    if (!runtime || !stops.length) return;
    const { L, map } = runtime;
    const layer = L.layerGroup().addTo(map);
    const stopsByLocation = new Map<string, FleetStopFlag[]>();
    for (const stop of stops) {
      const key = stop.point.join(",");
      const group = stopsByLocation.get(key) ?? [];
      group.push(stop);
      stopsByLocation.set(key, group);
    }
    for (const group of stopsByLocation.values()) {
      L.marker(group[0].point, {
        icon: stopFlagIcon(L, group), interactive: false, keyboard: false, zIndexOffset: -1000,
      }).addTo(layer);
    }
    return () => { layer.remove(); };
  }, [runtime, stops]);

  return <div className="relative isolate min-h-96 flex-1 overflow-hidden rounded-xl border border-zinc-800 bg-zinc-900">
    <div ref={container} aria-label="Fleet locations" className="absolute inset-0 bg-zinc-900" />
    {failed && <p role="alert" className="absolute inset-x-4 top-4 rounded-lg bg-zinc-950 p-3 text-sm text-zinc-300">The map could not load.</p>}
  </div>;
}
