"use client";

import { useEffect, useRef, type ReactNode } from "react";
import { X } from "lucide-react";

export function BoardDialog({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const element = dialog.current;
    element?.showModal();
    return () => element?.close();
  }, []);
  return <dialog ref={dialog} aria-label={title} onCancel={onClose}
    onClick={(event) => { if (event.target === event.currentTarget) onClose(); }}
    className="fixed inset-0 m-auto max-h-[85dvh] w-[calc(100vw_-_2rem)] max-w-3xl overflow-hidden rounded-xl border border-zinc-800 bg-zinc-950 p-0 text-zinc-200 shadow-xl backdrop:bg-black/70">
    <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-4">
      <h2 className="text-base font-semibold">{title}</h2>
      <button onClick={onClose} aria-label="Close" className="rounded p-1.5 text-zinc-400 hover:bg-zinc-800"><X className="h-4 w-4" /></button>
    </div>
    <div className="max-h-[calc(85dvh-4.5rem)] overflow-auto p-5 text-sm">{children}</div>
  </dialog>;
}
