"use client";

import { useLayoutEffect, useRef, useState, type InputHTMLAttributes } from "react";
import { registerPayInput } from "./payGrid";
import { centsAmount } from "./pay";
import { hundredths, validDecimal } from "@/app/gross-board/board";

export function PayInput({ onValueChange, normalizeValue, ...props }: Omit<InputHTMLAttributes<HTMLInputElement>, "onChange"> & { onValueChange: (value: string) => void; normalizeValue?: (value: string) => string }) {
  const ref = useRef<HTMLInputElement>(null);
  const [draft, setDraft] = useState<string | null>(null);
  const draftRef = useRef<string | null>(null);
  const update = (value: string) => {
    if (normalizeValue) { draftRef.current = value; setDraft(value); }
    else onValueChange(value);
  };
  useLayoutEffect(() => registerPayInput(ref.current!, update));
  const value = String(props.value ?? "");
  const display = normalizeValue && draft !== null ? draft : normalizeValue && validDecimal(value) ? centsAmount(hundredths(value)) : value;
  return <input {...props} value={display} ref={ref} tabIndex={-1}
    onKeyDown={event => {
      // An uncommitted number is one cell edit, including replacement's first key.
      if (draftRef.current !== null && (event.key === "Escape" || ((event.ctrlKey || event.metaKey) && !event.shiftKey && event.key.toLowerCase() === "z"))) {
        event.preventDefault(); event.stopPropagation(); draftRef.current = null; setDraft(null); event.currentTarget.closest<HTMLElement>("td")?.focus(); return;
      }
      props.onKeyDown?.(event);
    }}
    onBlur={event => {
      const pending = draftRef.current;
      draftRef.current = null; setDraft(null);
      if (pending !== null && normalizeValue) onValueChange(normalizeValue(pending));
      props.onBlur?.(event);
    }} onChange={event => update(event.target.value)} />;
}
