"use client";

import { useEffect, useRef, useState } from "react";
import type * as Leaflet from "leaflet";
import "leaflet/dist/leaflet.css";
import { truckHeadingPin, truckHeadingPinSize } from "@/app/components/fleet/truckHeadingPin";
import { statusColor, statusLabel } from "@/app/driver-board/board";
import type { BoardMapLeg, DriverBoardLocation } from "@/app/lib/types";
import { formatRouteMiles, type Coordinates } from "./routeGeometry";

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
  address: string;
  loadNumber: string;
  arrived: boolean;
  eta: string;
}

export interface FleetRouteLine {
  kind: BoardMapLeg["kind"];
  points: Coordinates[];
  loadNumber: string;
  miles?: number;
}

const routeStyle = {
  current: { color: "#38bdf8", border: "#082f49", label: "border-sky-400 bg-sky-950 text-white" },
  deadhead: { color: "#f43f5e", border: "#4c0519", label: "border-red-400 bg-neutral-950 text-[#fca5a5]" },
  "next-load": { color: "#a855f7", border: "#2e1065", label: "border-violet-400 bg-violet-950 text-white" },
};

function stopFlagIcon(L: typeof Leaflet, stops: FleetStopFlag[], showAddress: (flag: HTMLButtonElement, stop: FleetStopFlag) => void) {
  const icon = document.createElement("div");
  icon.className = "relative flex h-7 w-max items-start gap-0.5 border-l-2 border-zinc-100 pl-0.5 drop-shadow-[0_1px_2px_#000]";
  L.DomEvent.disableClickPropagation(icon);
  for (const stop of stops) {
    const pickup = stop.kind === "pickup";
    const flag = document.createElement("button");
    flag.type = "button";
    flag.className = `pointer-events-auto cursor-pointer py-1 pr-3 pl-1 font-mono text-[11px] font-bold leading-none text-neutral-950 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-neutral-950 [clip-path:polygon(0_0,100%_0,calc(100%_-_8px)_50%,100%_100%,0_100%)] ${pickup ? "bg-yellow-300" : "bg-blue-300"}`;
    const abbreviation = pickup ? "PU" : "DEL";
    flag.textContent = stop.arrived ? `At ${abbreviation}${stop.loadNumber ? ` · ${stop.loadNumber}` : ""}` : abbreviation;
    flag.setAttribute("aria-label", pickup ? "Show pickup address" : "Show delivery address");
    flag.addEventListener("click", () => showAddress(flag, stop));
    icon.append(flag);
    if (stop.eta) {
      const eta = document.createElement("span");
      eta.className = "absolute bottom-full left-0 mb-1 w-max whitespace-nowrap rounded border border-sky-400 bg-sky-950 px-1.5 py-0.5 text-[11px] font-semibold leading-none text-white";
      eta.textContent = stop.eta;
      icon.append(eta);
    }
  }
  return L.divIcon({ html: icon, className: "fleet-map-stop", iconSize: [0, 28], iconAnchor: [1, 28] });
}

function truckIcon(L: typeof Leaflet, pin: FleetPin, selected: boolean, hideUnit: boolean) {
  const icon = document.createElement("div");
  icon.className = "relative";
  const marker = truckHeadingPin(pin.location.heading);
  const fill = statusColor(pin.status === "RESERVED" ? "ENROUTE" : pin.status).split(" ").find(value => value.startsWith("bg-"));
  marker.className = `${fill || "bg-zinc-700"} group-focus-visible:outline-2 group-focus-visible:outline-offset-2 group-focus-visible:outline-sky-300 [&>*]:drop-shadow-[0_1px_1px_#000b]`;
  if (selected) marker.classList.add("outline-2", "outline-offset-2", "outline-sky-300");

  const label = document.createElement("span");
  label.className = "absolute bottom-full left-1/2 mb-1 w-max -translate-x-1/2 whitespace-nowrap rounded border border-white/60 bg-neutral-950 px-1.5 py-0.5 font-mono text-[11px] font-bold leading-none text-white shadow-[0_1px_5px_#000b]";
  label.setAttribute("aria-hidden", "true");
  label.textContent = pin.unit || "—";
  icon.append(marker);
  if (!hideUnit) icon.append(label);
  return L.divIcon({ html: icon, className: "fleet-map-pin group", iconSize: [truckHeadingPinSize, truckHeadingPinSize] });
}

export function FleetLocationMap({ pins, selectedDriverId, onSelect, routes, trail, stops, combineTruckLabel }: {
  pins: FleetPin[];
  selectedDriverId: string;
  onSelect: (driverId: string) => void;
  routes: FleetRouteLine[];
  trail: Coordinates[][];
  stops: FleetStopFlag[];
  combineTruckLabel: boolean;
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
      map = L.map(element, { fadeAnimation: false, attributionControl: false }).setView([39.5, -98.35], 4);
      L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", {
        referrerPolicy: "strict-origin",
        maxZoom: 19,
      }).addTo(map);
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
        icon: truckIcon(L, pin, selected, selected && combineTruckLabel),
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
  }, [runtime, pins, selectedDriverId, onSelect, combineTruckLabel]);

  useEffect(() => {
    if (!runtime || (!routes.length && !trail.length)) return;
    const { L, map } = runtime;
    const layer = L.layerGroup().addTo(map);
    if (trail.length) {
      L.polyline(trail, { color: "#451a03", weight: 6, interactive: false }).addTo(layer);
      L.polyline(trail, {
        color: "#fbbf24", weight: 3, dashArray: "5 6", interactive: false, className: "fleet-map-trail",
      }).addTo(layer);
    }
    const labels: Leaflet.Marker[] = [];
    for (const route of routes) {
      const style = routeStyle[route.kind];
      const smoothFactor = route.kind === "current" ? 1 : 2;
      L.polyline(route.points, { color: style.border, weight: 7, interactive: false, smoothFactor }).addTo(layer);
      const line = L.polyline(route.points, {
        color: style.color, weight: 4, interactive: false, smoothFactor,
        className: `fleet-map-route fleet-map-route-${route.kind}`,
      }).addTo(layer);
      const text = [route.kind === "deadhead" ? "Empty miles" : route.loadNumber.trim(), formatRouteMiles(route.miles)].filter(Boolean).join(" · ");
      if (text) {
        const label = document.createElement("span");
        label.className = `absolute top-0 left-0 w-max -translate-x-1/2 -translate-y-1/2 whitespace-nowrap rounded border px-1.5 py-0.5 font-mono text-xs font-semibold leading-none shadow-[0_1px_5px_#000b] ${style.label}`;
        label.textContent = text;
        labels.push(L.marker(line.getCenter(), {
          icon: L.divIcon({ html: label, className: "fleet-map-route-label", iconSize: [0, 0] }),
          interactive: false, keyboard: false, zIndexOffset: -1100,
        }).addTo(layer));
      }
    }
    const selected = pins.find(pin => pin.driverId === selectedDriverId);
    const updateLabels = () => {
      const blocked = stops.map(stop => ({
        point: map.latLngToLayerPoint(stop.point), left: 5,
        right: stop.arrived || stop.eta ? 155 : 40, above: stop.eta ? 52 : 34, below: 5,
      }));
      if (selected) {
        blocked.push({ point: map.latLngToLayerPoint([selected.location.latitude, selected.location.longitude]), left: 26, right: 26, above: 40, below: 15 });
      }
      for (const marker of labels) {
        const element = marker.getElement();
        if (!element) continue;
        const point = map.latLngToLayerPoint(marker.getLatLng());
        const halfWidth = (element.firstElementChild?.getBoundingClientRect().width ?? 0) / 2 + 4;
        const overlaps = blocked.some(box => point.x + halfWidth > box.point.x - box.left && point.x - halfWidth < box.point.x + box.right &&
          point.y + 11 > box.point.y - box.above && point.y - 11 < box.point.y + box.below);
        element.style.visibility = overlaps ? "hidden" : "visible";
        if (!overlaps) blocked.push({ point, left: halfWidth, right: halfWidth, above: 11, below: 11 });
      }
    };
    updateLabels();
    map.on("zoomend", updateLabels);
    return () => {
      map.off("zoomend", updateLabels);
      layer.remove();
    };
  }, [runtime, routes, trail, stops, pins, selectedDriverId]);

  useEffect(() => {
    if (!runtime || !stops.length) return;
    const { L, map } = runtime;
    const layer = L.layerGroup().addTo(map);
    const popup = L.popup({
      autoPan: false,
      maxWidth: 240,
      className: "fleet-map-stop-address",
    });
    const showAddress = (flag: HTMLButtonElement, stop: FleetStopFlag) => {
      const content = document.createElement("div");
      const heading = document.createElement("div");
      heading.className = "mb-1 font-semibold";
      heading.textContent = [stop.kind === "pickup" ? "Pickup address" : "Delivery address", stop.loadNumber].filter(Boolean).join(" · ");
      const address = document.createElement("div");
      address.textContent = stop.address || "Address unavailable";
      content.append(heading, address);
      popup.options.offset = L.point(flag.offsetLeft + flag.offsetWidth / 2 - 1, -32);
      popup.setLatLng(stop.point).setContent(content).openOn(map);
    };
    const stopsByLocation = new Map<string, FleetStopFlag[]>();
    for (const stop of stops) {
      const key = stop.point.join(",");
      const group = stopsByLocation.get(key) ?? [];
      group.push(stop);
      stopsByLocation.set(key, group);
    }
    for (const group of stopsByLocation.values()) {
      L.marker(group[0].point, {
        icon: stopFlagIcon(L, group, showAddress), interactive: false, keyboard: false, zIndexOffset: 2000,
      }).addTo(layer);
    }
    return () => {
      popup.remove();
      layer.remove();
    };
  }, [runtime, stops]);

  return <div className="flex min-h-96 flex-1 flex-col overflow-hidden rounded-xl border border-zinc-800 bg-zinc-900">
    <div className="relative isolate flex-1">
      <div ref={container} aria-label="Fleet locations" className="fleet-tile-map absolute inset-0 bg-card" />
      {failed && <p role="alert" className="absolute inset-x-4 top-4 rounded-lg bg-zinc-950 p-3 text-sm text-zinc-300">The map could not load.</p>}
    </div>
    <div className="border-t border-zinc-800 px-2 py-0.5 text-right text-xs text-zinc-400">
      © <a className="underline hover:text-foreground" href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors
    </div>
  </div>;
}
