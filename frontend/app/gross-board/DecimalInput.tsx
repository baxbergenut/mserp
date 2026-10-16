import { useState, type InputHTMLAttributes } from "react";
import { compactDecimal, decimalInputValue } from "./board";

export function DecimalInput({ value, displayValue, onValueChange, nonnegative = false, ...props }: Omit<InputHTMLAttributes<HTMLInputElement>, "value" | "onChange"> & {
  value: string;
  displayValue: string;
  onValueChange: (value: string, text: string) => void;
  nonnegative?: boolean;
}) {
  // Database responses normalize numbers to two places. Keep the exact text
  // under the caret while editing, even when an autosave or refresh finishes.
  const [typing, setTyping] = useState<string | null>(null);
  return <input {...props} value={typing ?? displayValue}
    onFocus={event => { setTyping(compactDecimal(value)); props.onFocus?.(event); }}
    onBlur={event => { setTyping(null); props.onBlur?.(event); }}
    onChange={event => {
      const text = event.currentTarget.value;
      const next = decimalInputValue(text, nonnegative);
      if (next === null) return;
      setTyping(text);
      onValueChange(next, text);
    }} />;
}
