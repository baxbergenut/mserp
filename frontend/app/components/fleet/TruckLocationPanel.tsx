"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { MapPin } from "lucide-react";
import { fetchDriverTruckLocation, fetchTruckLocation } from "@/app/lib/api";
import type { FleetLocation } from "@/app/lib/types";
import { LocationCell } from "@/app/driver-board/LocationCell";
import { TruckLocationMap } from "./TruckLocationMap";

export function TruckLocationPanel({ truckId, driverId }: { truckId: string; driverId?: never } | { driverId: string; truckId?: never }) {
  const [data, setData] = useState<FleetLocation | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    let cancelled = false;
    let busy = false;
    async function refresh() {
      if (busy || document.visibilityState === "hidden") return;
      busy = true;
      try {
        const value = driverId ? await fetchDriverTruckLocation(driverId) : await fetchTruckLocation(truckId!);
        if (!cancelled) { setData(value); setError(""); }
      } catch (reason) {
        if (!cancelled) setError(reason instanceof Error ? reason.message : "Could not load location.");
      } finally { busy = false; }
    }
    void refresh();
    const timer = setInterval(() => void refresh(), 30_000);
    window.addEventListener("focus", refresh);
    document.addEventListener("visibilitychange", refresh);
    return () => { cancelled = true; clearInterval(timer); window.removeEventListener("focus", refresh); document.removeEventListener("visibilitychange", refresh); };
  }, [truckId, driverId]);
  return <section aria-label="Latest truck location" className="overflow-hidden rounded-xl border border-zinc-800 bg-zinc-900/20">
    <div className="p-4">
      <h2 className="flex items-center gap-2 text-sm font-semibold text-zinc-100"><MapPin className="h-4 w-4 text-zinc-500" />Latest location</h2>
      {driverId && data?.truckId && <Link href={`/trucks/detail?id=${data.truckId}`} className="mt-2 inline-block text-xs text-blue-400 hover:text-blue-300">Truck {data.truckUnit}</Link>}
      {data?.location && <div className="mt-2 text-xs"><LocationCell name={`Truck ${data.truckUnit}`} location={data.location} /></div>}
      {!data && !error && <p className="mt-3 text-xs text-zinc-500">Loading location…</p>}
      {data && !data.location && <p className="mt-3 text-xs text-zinc-500">{data.truckId ? "No location available." : "No assigned truck."}</p>}
      {error && <p role="status" className="mt-3 text-xs text-zinc-400">{data ? "Could not refresh location. Showing the last loaded data." : error}</p>}
    </div>
    {data?.location && <TruckLocationMap location={data.location} />}
  </section>;
}
