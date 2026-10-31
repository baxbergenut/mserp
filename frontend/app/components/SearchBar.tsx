"use client";

import { Search } from "lucide-react";
import { controlClass } from "./management/ManagementUI";

interface SearchBarProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
}

export function SearchBar({ value, onChange, placeholder }: SearchBarProps) {
  return (
    <div className="relative w-full max-w-xs">
      <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-zinc-600" />
      <input
        type="text"
        aria-label={placeholder ?? "Search loads"}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder ?? "Search loads…"}
        className={`${controlClass} pl-9`}
      />
    </div>
  );
}
