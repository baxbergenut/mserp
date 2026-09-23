"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { ArrowLeft, Truck as TruckIcon } from "lucide-react";
import { fetchTruck } from "@/app/lib/api";
import type { Truck } from "@/app/lib/types";
import { ErrorBanner } from "@/app/components/management/ManagementUI";
import { RelatedExpenses } from "@/app/components/expenses/RelatedExpenses";

export default function TruckDetailPage() {
  const [truck, setTruck] = useState<Truck | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    const truckID = new URLSearchParams(window.location.search).get("id") ?? "";
    const timeout = window.setTimeout(() => {
      if (!truckID) {
        setError("No truck was selected.");
        return;
      }
      void fetchTruck(truckID)
        .then(setTruck)
        .catch((reason) => setError(reason instanceof Error ? reason.message : "Failed to load truck"));
    }, 0);
    return () => window.clearTimeout(timeout);
  }, []);

  return (
    <div className="space-y-6 animate-fade-in">
      <Link href="/trucks" className="inline-flex items-center gap-1.5 text-[12px] text-zinc-500 transition hover:text-zinc-300">
        <ArrowLeft className="h-3.5 w-3.5" /> Back to trucks
      </Link>
      {error && <ErrorBanner message={error} />}
      {truck && (
        <>
          <div className="rounded-xl border border-zinc-800/60 bg-zinc-900/30 p-5">
            <div className="flex items-center gap-3">
              <TruckIcon className="h-5 w-5 text-blue-400" />
              <div>
                <h1 className="text-lg font-semibold text-zinc-100">Truck {truck.unitNumber}</h1>
                <p className="mt-1 text-[13px] text-zinc-500">{[truck.year, truck.make, truck.model].filter(Boolean).join(" ") || "No vehicle details"}</p>
              </div>
            </div>
            <dl className="mt-5 grid gap-4 text-[12px] sm:grid-cols-4">
              <div><dt className="text-zinc-600">Driver</dt><dd className="mt-1 text-zinc-300">{truck.driverName || "Unassigned"}</dd></div>
              <div><dt className="text-zinc-600">Status</dt><dd className="mt-1 capitalize text-zinc-300">{truck.status.replaceAll("_", " ")}</dd></div>
              <div><dt className="text-zinc-600">VIN</dt><dd className="mt-1 font-mono text-zinc-300">{truck.vin || "—"}</dd></div>
              <div><dt className="text-zinc-600">Ownership</dt><dd className="mt-1 text-zinc-300">{truck.isCompanyOwned ? "Company" : "Owner-operator"}</dd></div>
            </dl>
          </div>
          <RelatedExpenses truckId={truck.id} />
        </>
      )}
    </div>
  );
}
