import { useState, type InputHTMLAttributes } from "react";
import { compactDecimal, decimalInputValue } from "./board";

export function DecimalInput({ value, displayValue, onValueChange, nonnegative = false, ...props }: Omit<InputHTMLAttributes<HTMLInputElement>, "value" | "onChange"> & {
  value: string;
  displayValue: string;
  onValueChange: (value: string, event: Event) => void;
  nonnegative?: boolean;
}) {
  // A live value replacement collapses a selection before the first keystroke.
  // Keep selected or edited text until blur. Focus alone still follows live edits.
  const [typing, setTyping] = useState<string | null>(null);
  const [focused, setFocused] = useState(false);
  return <input {...props} value={props.readOnly ? displayValue : typing ?? (focused ? compactDecimal(value) : displayValue)}
    onFocus={event => { setFocused(true); props.onFocus?.(event); }}
    onSelect={event => {
      const input = event.currentTarget;
      if (!props.readOnly && input.selectionStart !== input.selectionEnd) setTyping(input.value);
      props.onSelect?.(event);
    }}
    onBlur={event => { setFocused(false); setTyping(null); props.onBlur?.(event); }}
    onChange={event => {
      if (props.readOnly) return;
      const text = event.currentTarget.value;
      const next = decimalInputValue(text, nonnegative);
      if (next === null) return;
      setTyping(text);
      onValueChange(next, event.nativeEvent);
    }} />;
}
