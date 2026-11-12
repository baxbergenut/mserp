"use client";

import { useEffect, useMemo, useState } from "react";
import { ChevronDown, Map as MapIcon } from "lucide-react";
import { currentChargeWeek } from "@/app/accounting/driver-charges/charges";
import { PageHeader } from "@/app/components/PageHeader";
import { isOldLocation, statusColor, statusLabel } from "@/app/driver-board/board";
import { fetchBoardMapHistory, fetchBoardMapRoute, fetchDriverBoard, subscribeBoardEvents } from "@/app/lib/api";
import type { BoardMapHistoryPoint, BoardMapRoute, DriverBoard } from "@/app/lib/types";
import { FleetLocationMap, type FleetStopFlag } from "./FleetLocationMap";
import { drivenTrail, remainingRoute } from "./routeGeometry";

const reportTime = new Intl.DateTimeFormat("en-US", {
  month: "short", day: "numeric", hour: "numeric", minute: "2-digit",
  timeZone: "America/New_York", timeZoneName: "short",
});

export default function FleetMapPage() {
  const [board, setBoard] = useState<DriverBoard | null>(null);
  const [error, setError] = useState("");
  const [selectedId, setSelectedId] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [now, setNow] = useState(() => Date.now());
  const [routeResult, setRouteResult] = useState<{ driverId: string; context: string; route: BoardMapRoute | null } | null>(null);
  const [historyResult, setHistoryResult] = useState<{ driverId: string; truckId: string; points: BoardMapHistoryPoint[] | null } | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    let pending = false;
    let refreshing = false;
    const accept = (value: DriverBoard) => {
      setBoard(value);
      setNow(Date.now());
      setError("");
    };
    const refresh = async () => {
      if (controller.signal.aborted || document.hidden) return;
      if (refreshing) { pending = true; return; }
      refreshing = true;
      try {
        const value = await fetchDriverBoard(currentChargeWeek(), controller.signal);
        if (!controller.signal.aborted) accept(value);
      } catch {
        if (!controller.signal.aborted) setError("Unable to refresh truck locations.");
      } finally {
        refreshing = false;
        if (pending) { pending = false; void refresh(); }
      }
    };
    void refresh();
    const close = subscribeBoardEvents(() => void refresh(), () => {}, connected => {
      if (connected) void refresh();
    }, () => void refresh());
    const timer = setInterval(refresh, 30_000);
    document.addEventListener("visibilitychange", refresh);
    window.addEventListener("focus", refresh);
    return () => {
      controller.abort();
      close();
      clearInterval(timer);
      document.removeEventListener("visibilitychange", refresh);
      window.removeEventListener("focus", refresh);
    };
  }, [attempt]);

  const entries = useMemo(() => new Map(board?.entries.map(entry => [entry.driverId, entry])), [board]);
  const pins = useMemo(() => (board?.drivers ?? []).flatMap(driver => {
    const { location } = driver;
    if (!location || !Number.isFinite(location.latitude) || !Number.isFinite(location.longitude) ||
      Math.abs(location.latitude) > 90 || Math.abs(location.longitude) > 180) return [];
    return [{
      driverId: driver.id, driverName: driver.fullName, unit: driver.truckUnit, truckId: driver.truckId, location,
      status: entries.get(driver.id)?.status ?? "",
    }];
  }), [board, entries]);
  const drivers = useMemo(() => (board?.drivers ?? []).slice().sort((a, b) => a.fullName.localeCompare(b.fullName)), [board]);
  const selected = drivers.find(driver => driver.id === selectedId);
  const selectedPin = pins.find(pin => pin.driverId === selectedId);
  const selectedLoad = selected ? board?.loads[selected.id]?.current : null;
  const stops = useMemo<FleetStopFlag[]>(() => {
    if (!selectedLoad || selectedLoad.sourceMissing) return [];
    return (selectedLoad.stops ?? []).flatMap(({ type, latitude, longitude, streetAddress, location }) => {
      const kind = type.toLowerCase();
      if ((kind !== "pickup" && kind !== "delivery") || latitude == null || longitude == null ||
        !Number.isFinite(latitude) || !Number.isFinite(longitude) ||
        Math.abs(latitude) > 90 || Math.abs(longitude) > 180 || (latitude === 0 && longitude === 0)) return [];
      const address = [streetAddress?.trim(), location?.trim()].filter(Boolean).join(", ");
      return [{ kind, point: [latitude, longitude], address }];
    });
  }, [selectedLoad]);
  const truckId = selectedPin?.truckId;
  const routeContext = selectedPin ? board?.loads[selectedId]?.automation?.context : undefined;
  useEffect(() => {
    if (!routeContext) return;
    const controller = new AbortController();
    const accept = (route: BoardMapRoute | null) => {
      if (!controller.signal.aborted) setRouteResult({ driverId: selectedId, context: routeContext, route });
    };
    void fetchBoardMapRoute(selectedId, controller.signal).then(result => accept(result.route), () => accept(null));
    return () => controller.abort();
  }, [selectedId, routeContext, now]);
  useEffect(() => {
    if (!truckId) return;
    const controller = new AbortController();
    const accept = (points: BoardMapHistoryPoint[] | null) => {
      if (!controller.signal.aborted) setHistoryResult({ driverId: selectedId, truckId, points });
    };
    void fetchBoardMapHistory(selectedId, controller.signal)
      .then(result => accept(result.truckId === truckId ? result.points : null), () => accept(null));
    return () => controller.abort();
  }, [selectedId, truckId, now]);
  const matchingResult = routeResult?.driverId === selectedId && routeResult.context === routeContext ? routeResult : null;
  const mapRoute = matchingResult?.route?.context === routeContext ? matchingResult?.route : null;
  const route = useMemo(() => mapRoute && selectedPin ? remainingRoute(mapRoute, selectedPin.location) : null, [mapRoute, selectedPin]);
  const history = historyResult?.driverId === selectedId && historyResult.truckId === truckId ? historyResult : null;
  const trail = useMemo(() => history?.points && selectedPin ? drivenTrail(history.points, selectedPin.location, now) : [], [history, selectedPin, now]);
  const selectedEntry = selected && entries.get(selected.id);
  const reportedAt = Date.parse(selectedPin?.location.reportedAt ?? "");

  return (
    <section className="flex min-h-[calc(100dvh-7rem)] min-w-0 flex-col gap-3">
      <PageHeader>
        <div className="flex items-center gap-3">
          <MapIcon aria-hidden="true" className="h-5 w-5 text-zinc-500" />
          <h1 className="text-lg font-semibold text-zinc-100">Map</h1>
        </div>
      </PageHeader>
      <div className="grid items-start gap-2 sm:grid-cols-[minmax(10rem,25%)_minmax(0,1fr)] sm:gap-3">
        <div className="relative min-w-0">
          <select aria-label="Choose a driver or truck" value={selected?.id ?? ""}
            onChange={event => setSelectedId(event.target.value)}
            disabled={!drivers.length}
            className="w-full appearance-none rounded-lg border border-zinc-700 bg-zinc-900 py-2 pr-9 pl-3 text-sm text-zinc-100">
            <option value="">{!board && !error ? "Loading truck locations…" : "All Status Board trucks"}</option>
            {drivers.map(driver => <option key={driver.id} value={driver.id}>
              {driver.truckUnit ? `${driver.truckUnit} · ` : ""}{driver.fullName}
            </option>)}
          </select>
          <ChevronDown aria-hidden="true" className="pointer-events-none absolute top-1/2 right-3 size-4 -translate-y-1/2 text-zinc-400" />
        </div>
        <div className="h-22 min-w-0 sm:h-20 lg:h-14">
          {selected && <div aria-label="Selected truck details" role="region"
            className="flex max-h-full flex-wrap items-center gap-x-4 gap-y-0.5 overflow-y-auto rounded-lg border border-zinc-700 bg-zinc-900 px-3 py-1.5 text-sm leading-5">
            <span className="inline-flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5">
              <span className="font-medium text-zinc-100">{selected.fullName}</span>
              {selectedEntry?.status && <span className={`rounded px-1.5 py-0.5 text-xs font-medium ${statusColor(selectedEntry.status)}`}>{statusLabel(selectedEntry.status)}</span>}
            </span>
            <span className="text-zinc-400">{!selectedPin ? "GPS location unavailable" : Number.isFinite(reportedAt) ?
              `${isOldLocation(selectedPin.location.reportedAt, now) ? "Last known location" : "GPS reported"} · ${reportTime.format(reportedAt)}` : "GPS report time unavailable"}</span>
            {selectedPin && <span className="text-zinc-300">
              {route && mapRoute ? <>
                <span className="mr-2 text-sky-300">{route.stale ? "Last TomTom route" : "TomTom route"}</span>
                {mapRoute.stopType === "pickup" ? "Pickup" : mapRoute.stopType === "delivery" ? "Delivery" : "Stop"} · {mapRoute.destination}
                {route.stale && Number.isFinite(Date.parse(mapRoute.reportedAt)) && ` · From GPS at ${reportTime.format(new Date(mapRoute.reportedAt))}`}
              </> : !routeContext ? "No active load stop to route to" : matchingResult ? "TomTom route unavailable" : "Loading TomTom route…"}
            </span>}
            {truckId && <span className={`xl:ml-auto ${trail.length ? "text-amber-300" : "text-zinc-400"}`}>
              {trail.length ? "Driven · Last 24 hours" : !history ? "Loading GPS history…" :
                history.points ? "No GPS trail in the last 24 hours" : "GPS history unavailable"}
            </span>}
          </div>}
        </div>
      </div>
      {error && <div role="alert" className="flex flex-wrap items-center gap-2 text-sm text-amber-200">
        <span>{error}{board ? " Showing last known positions." : ""}</span>
        <button type="button" className="underline" onClick={() => setAttempt(value => value + 1)}>Retry</button>
      </div>}
      <FleetLocationMap pins={pins} selectedDriverId={selected?.id ?? ""} onSelect={setSelectedId}
        route={route} routeLoadNumber={selectedLoad?.number?.trim() ?? ""} trail={trail} stops={stops} />
    </section>
  );
}
