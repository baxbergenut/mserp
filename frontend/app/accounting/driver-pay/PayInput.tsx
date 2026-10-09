"use client";

import { useLayoutEffect, useRef, type InputHTMLAttributes } from "react";
import { registerPayInput } from "./payGrid";

export function PayInput({ onValueChange, ...props }: Omit<InputHTMLAttributes<HTMLInputElement>, "onChange"> & { onValueChange: (value: string) => void }) {
  const ref = useRef<HTMLInputElement>(null);
  useLayoutEffect(() => registerPayInput(ref.current!, onValueChange), [onValueChange]);
  return <input {...props} ref={ref} tabIndex={-1} onChange={event => onValueChange(event.target.value)} />;
}
