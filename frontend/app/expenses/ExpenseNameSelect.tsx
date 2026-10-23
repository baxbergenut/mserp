"use client";

import { useState } from "react";
import { controlClass } from "../components/management/ManagementUI";

export function ExpenseNameSelect({ value, names, onChange, label = "Expense type", optionLabel = "expense type", className = controlClass }: {
  value: string; names: string[]; onChange: (value: string) => void; label?: string; optionLabel?: string; className?: string;
}) {
  const [customValue, setCustomValue] = useState<string | null>(null);
  const defaultName = names.find(name => name.toLowerCase() === value.trim().toLowerCase());
  const customMode = (customValue !== null && customValue === value) || (!defaultName && !!value);
  return <div className="space-y-2">
    <select aria-label={label} required value={customMode ? "__custom" : defaultName ?? ""} className={className} onChange={event => {
      setCustomValue(event.target.value === "__custom" ? "" : null); onChange(event.target.value === "__custom" ? "" : event.target.value);
    }}>
      <option value="">Select {optionLabel}</option>
      {names.map(name => <option key={name} value={name}>{name}</option>)}
      <option value="__custom">Custom {optionLabel}…</option>
    </select>
    {customMode && <input aria-label={`${label} custom`} required pattern=".*\S.*" maxLength={100} value={value} onChange={event => { setCustomValue(event.target.value); onChange(event.target.value); }} className={className} placeholder={`Enter custom ${optionLabel}`} />}
  </div>;
}
