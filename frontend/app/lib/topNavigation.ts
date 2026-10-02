"use client";

import { useEffect, useRef } from "react";
import { useSearchParams } from "next/navigation";

// Open the existing form once per explicit quick-create navigation.
export function useQuickCreate(open: () => void | Promise<void>, ready = true) {
  const params = useSearchParams();
  const token = params.get("new");
  const consumed = useRef<string | null>(null);
  useEffect(() => {
    if (!token || !ready || consumed.current === token) return;
    const timer = setTimeout(() => { consumed.current = token; void open(); }, 0);
    return () => clearTimeout(timer);
  }, [token, ready, open]);
}
