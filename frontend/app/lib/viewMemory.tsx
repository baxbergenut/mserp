"use client";

import { createContext, useCallback, useContext, useState, type Dispatch, type SetStateAction } from "react";

export const ViewMemoryContext = createContext("");
export const BackHrefContext = createContext<string | undefined>(undefined);
export const MarkBackContext = createContext<(url: string) => void>(() => {});
export const useMarkBack = () => useContext(MarkBackContext);
export const useBackHref = () => useContext(BackHrefContext);
const fallback = new Map<string, string>();
export function readMemory<T>(key: string, initial: T): T {
  try {
    const raw = typeof window === "undefined" ? null : fallback.get(key) ?? sessionStorage.getItem(key);
    return raw ? JSON.parse(raw, (_, value) => value?.__viewSet ? new Set(value.__viewSet) : value) : initial;
  } catch { return initial; }
}
export function writeMemory(key: string, value: unknown) {
  const raw = JSON.stringify(value, (_, item) => item instanceof Set ? { __viewSet: [...item] } : item);
  fallback.set(key, raw);
  try { sessionStorage.setItem(key, raw); } catch { /* Memory still works when browser storage is disabled. */ }
}

// View preferences only. Never use this for records, credentials, or form drafts.
export function useViewState<T>(name: string, initial: T | (() => T), initialOverride?: T): [T, Dispatch<SetStateAction<T>>] {
  const scope = useContext(ViewMemoryContext);
  const key = `${scope}:view:${name}`;
  const [value, setValue] = useState<T>(() => initialOverride !== undefined ? initialOverride : readMemory(key, typeof initial === "function" ? (initial as () => T)() : initial));
  const update = useCallback<Dispatch<SetStateAction<T>>>(next => {
    setValue(current => {
      const result = typeof next === "function" ? (next as (value: T) => T)(current) : next;
      writeMemory(key, result);
      return result;
    });
  }, [key]);
  return [value, update];
}
