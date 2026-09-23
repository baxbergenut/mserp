"use client";

import { useId } from "react";
import { CircleHelp, Flag, X } from "lucide-react";
import type { TransactionFlag, TransactionLoadEvidence } from "../lib/types";

function LoadEvidence({ label, load }: { label: string; load: TransactionLoadEvidence | null }) {
  if (!load) return null;
  return (
    <div className="rounded-lg border border-zinc-800 px-3 py-2">
      <p className="text-xs text-zinc-500">{label}</p>
      <p className="mt-1 text-sm text-zinc-200">{load.loadId} · Truck {load.truckUnit || "unspecified"}</p>
      <p className="mt-1 text-xs text-zinc-400">{load.pickupDate || "Missing pickup"} → {load.deliveryDate || "Missing delivery"}</p>
      {load.appointmentFallback && <p className="mt-1 text-xs text-zinc-500">Uses an appointment date where an actual date is missing.</p>}
    </div>
  );
}

export function TransactionFlagIcon({ flag, kind }: { flag?: TransactionFlag; kind: "fuel" | "toll" }) {
  const id = useId();
  if (!flag) return null;
  const review = flag.status === "review";
  const title = review ? "Review transaction" : "Check transaction data";
  const Icon = review ? Flag : CircleHelp;
  return (
    <>
      <button
        type="button"
        popoverTarget={id}
        aria-label={`${title}: ${flag.reason}`}
        title={`${title}: ${flag.reason}`}
        className={`inline-flex rounded-md p-2 transition hover:bg-zinc-800 focus-visible:outline-2 focus-visible:outline-blue-500 ${review ? "text-amber-400" : "text-zinc-500"}`}
      >
        <Icon aria-hidden="true" className="h-4 w-4" />
      </button>
      <div
        id={id}
        popover="auto"
        role="dialog"
        aria-label={title}
        className="fixed inset-0 m-auto max-h-[calc(100dvh-2rem)] w-[calc(100vw-2rem)] max-w-md overflow-y-auto rounded-xl border border-zinc-800 bg-[#111113] p-5 text-left font-sans text-sm font-normal text-zinc-300 shadow-2xl backdrop:bg-black/50"
      >
        <div className="mb-3 flex items-center justify-between gap-3">
          <h2 className="font-semibold text-zinc-100">{title}</h2>
          <button type="button" popoverTarget={id} popoverTargetAction="hide" aria-label="Close" className="rounded-md p-1.5 text-zinc-500 hover:bg-zinc-800 hover:text-zinc-200">
            <X aria-hidden="true" className="h-4 w-4" />
          </button>
        </div>
        <p>{flag.reason}</p>
        <p className="mt-2 text-xs text-zinc-500">Truck {flag.truckUnit || "unknown"} · {flag.transactionDate}</p>
        <div className="mt-4 space-y-2">
          <LoadEvidence label="Previous load" load={flag.previousLoad} />
          <LoadEvidence label="Next load" load={flag.nextLoad} />
          <LoadEvidence label="Load to verify" load={flag.relatedLoad} />
        </div>
        <p className="mt-4 text-xs text-zinc-500">Checks each load from {flag.bufferDays} day before pickup through {flag.bufferDays} day after delivery. This is a review hint, not proof of fraud or a driver departure.</p>
        {kind === "toll" && <p className="mt-2 text-xs text-zinc-500">Uses the stored crossing date, not the posting date. Verify it against the original toll record if the charge looks delayed.</p>}
        <p className="mt-2 text-xs text-zinc-500">Latest load update: {flag.loadsSyncedAt ? new Date(flag.loadsSyncedAt).toLocaleString() : "unavailable"}</p>
      </div>
    </>
  );
}
