"use client";
import type { DetailsHTMLAttributes } from "react";
import { useViewState } from "@/app/lib/viewMemory";

export function RememberedDetails({ memoryKey, ...props }: Omit<DetailsHTMLAttributes<HTMLDetailsElement>, "open" | "onToggle"> & { memoryKey: string }) {
  const [open, setOpen] = useViewState(`details:${memoryKey}`, false);
  return <details {...props} open={open} onToggle={event => setOpen(event.currentTarget.open)} />;
}
