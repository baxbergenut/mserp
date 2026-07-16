import type { Load } from "../types";

const API_BASE = process.env.NEXT_PUBLIC_LOADS_API_URL ?? "http://localhost:8080";

export async function fetchLoads(): Promise<Load[]> {
  const res = await fetch(`${API_BASE}/loads`, { cache: "no-store" });

  if (!res.ok) {
    throw new Error(`Failed to fetch loads (${res.status} ${res.statusText})`);
  }

  const json = await res.json();

  // Defensive: handle either a raw array or a { data: [...] } wrapper.
  if (Array.isArray(json)) return json as Load[];
  if (Array.isArray(json?.data)) return json.data as Load[];

  return [];
}
