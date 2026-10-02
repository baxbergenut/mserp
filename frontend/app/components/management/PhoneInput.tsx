"use client";

import { useId } from "react";
import { formatPhone, normalizePhone } from "@/app/lib/phone";
import { controlClass } from "./ManagementUI";

export function PhoneInput({ value, onChange, disabled = false }: {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  const hintId = useId();
  const normalized = normalizePhone(value);
  const invalid = normalized === null;
  return <>
    <input
      type="tel"
      inputMode="tel"
      autoComplete="tel-national"
      disabled={disabled}
      value={value}
      pattern="[0-9]{10}"
      title="Enter exactly 10 digits. You can also paste a formatted US number."
      aria-invalid={invalid}
      aria-describedby={hintId}
      onChange={(event) => {
        const next = event.target.value;
        // Valid pasted/formatted values become digits; malformed input remains
        // visible and blocked, rather than silently dropping characters/digits.
        onChange(normalizePhone(next) ?? next);
      }}
      className={controlClass}
      placeholder="5555550123"
    />
    <span id={hintId} className={`block text-[11px] ${invalid ? "text-red-400" : "text-zinc-500"}`}>
      {invalid ? "Enter exactly 10 digits." : formatPhone(value) || "Optional · 10 digits"}
    </span>
  </>;
}
