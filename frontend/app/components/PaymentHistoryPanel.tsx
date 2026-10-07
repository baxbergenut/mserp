"use client";

import { useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { History, X } from "lucide-react";
import { IntentLink } from "./IntentLink";
import { usePermissions } from "@/app/lib/access";
import { decimalDisplay, hundredths } from "@/app/gross-board/board";

import type { EscrowRelease } from "@/app/lib/types";

const money = (value: string) => decimalDisplay(hundredths(value), true);

export function PaymentHistoryPanel({ title, paid, remaining, previouslyPaid = "0", payments, driverId, escrow = false, releases = [], held, released, onEditRelease, onClose }: {
  releases?: EscrowRelease[]; held?: string; released?: string; onEditRelease?: (release: EscrowRelease, anchor: { left: number; bottom: number }) => void;
  title: string; paid: string; remaining: string; previouslyPaid?: string;
  payments: { weekStart: string; amount: string }[];
  driverId?: string | null; escrow?: boolean; onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const canReadPay = usePermissions().includes("payroll.read");
  useEffect(() => {
    const element = dialog.current;
    const previous = document.activeElement as HTMLElement | null;
    element?.showModal();
    return () => { element?.close(); previous?.focus(); };
  }, []);
  if (typeof document === "undefined") return null;
  const rows = [...payments].sort((a, b) => b.weekStart.localeCompare(a.weekStart));
  return createPortal(<dialog ref={dialog} aria-label="Payment history" onCancel={event => { event.preventDefault(); onClose(); }}
    onClick={event => { if (event.target === event.currentTarget) { const bounds = event.currentTarget.getBoundingClientRect(); if (event.clientX < bounds.left || event.clientX > bounds.right) onClose(); } }}
    className="fixed inset-0 m-0 ml-auto h-dvh max-h-none w-full max-w-lg border-l border-zinc-800 bg-zinc-950 p-0 text-zinc-300 shadow-2xl backdrop:bg-black/50">
    <div className="flex h-full flex-col">
      <header className="flex items-start gap-3 border-b border-zinc-800 p-5"><History className="mt-0.5 h-5 w-5 text-zinc-500" /><div className="min-w-0 flex-1"><h2 className="font-semibold text-zinc-100">Payment history</h2><p className="mt-1 break-words text-sm text-zinc-400">{title}</p></div><button autoFocus aria-label="Close payment history" onClick={onClose} className="rounded-lg p-1.5 text-zinc-500 hover:bg-zinc-800 hover:text-zinc-200"><X className="h-5 w-5" /></button></header>
      <dl className="grid grid-cols-2 gap-4 border-b border-zinc-800 p-5"><div><dt className="text-xs text-zinc-500">{escrow ? "Collected" : "Paid"}</dt><dd className="mt-1 font-mono text-lg text-emerald-400">{money(paid)}</dd></div><div><dt className="text-xs text-zinc-500">{escrow ? "To collect" : "Remaining"}</dt><dd className="mt-1 font-mono text-lg">{money(remaining)}</dd></div>{escrow && <><div><dt className="text-xs text-zinc-500">Released</dt><dd className="mt-1 font-mono text-lg">{money(released ?? "0")}</dd></div><div><dt className="text-xs text-zinc-500">Held</dt><dd className="mt-1 font-mono text-lg">{money(held ?? "0")}</dd></div></>}</dl>
      <div className="min-h-0 flex-1 overflow-y-auto p-5">
        {releases.length > 0 && <section className="mb-5"><h3 className="mb-2 text-xs text-zinc-500">Releases</h3>{releases.map(release => <div key={release.id} className="flex items-center gap-3 border-b border-zinc-800/60 py-3 text-sm"><div className="flex-1"><span className="text-xs text-zinc-400">Week of {release.weekStart}</span>{release.cancelled && <span className="ml-2 text-xs text-zinc-500">Cancelled</span>}</div><span className={`font-mono ${release.cancelled ? "text-zinc-600 line-through" : "text-emerald-400"}`}>{money(release.amount)}</span>{release.editable && onEditRelease && <button className="text-xs text-blue-400" aria-label={`Edit release for ${release.weekStart}`} onClick={event => onEditRelease(release, event.currentTarget.getBoundingClientRect())}>Edit</button>}</div>)}</section>}
        {!rows.length && hundredths(previouslyPaid) === BigInt(0) ? <p className="text-sm text-zinc-500">No saved payments yet.</p> : <table className="w-full text-left text-sm"><thead><tr className="border-b border-zinc-800 text-xs text-zinc-500"><th className="py-3 font-medium">Week / payment</th><th className="py-3 text-right font-medium">Amount</th></tr></thead><tbody>
          {rows.map(row => <tr key={row.weekStart} className="border-b border-zinc-800/60"><td className="py-3">{escrow && row.weekStart < "2026-09-28" ? "Previously paid" : canReadPay ? <IntentLink className="text-blue-400" href={`/accounting/driver-pay?weekStart=${row.weekStart}${driverId ? `&driverId=${driverId}` : ""}`}>Week of {row.weekStart}</IntentLink> : `Week of ${row.weekStart}`}</td><td className="py-3 text-right font-mono">{money(row.amount)}</td></tr>)}
          {hundredths(previouslyPaid) > BigInt(0) && <tr className="border-b border-zinc-800/60"><td className="py-3">Previously paid</td><td className="py-3 text-right font-mono">{money(previouslyPaid)}</td></tr>}
        </tbody></table>}
      </div>
    </div>
  </dialog>, document.body);
}
