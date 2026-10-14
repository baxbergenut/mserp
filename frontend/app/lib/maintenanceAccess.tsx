"use client";

import { useEffect, useState } from "react";
import { fetchMaintenanceAccess } from "./api";
import type { ExpenseCategoryAccess } from "./types";

export function useMaintenanceAccess() {
  const [value, setValue] = useState<ExpenseCategoryAccess | null>(null);
  useEffect(() => { let active = true; fetchMaintenanceAccess().then(v => { if (active) setValue(v); }).catch(() => { /* Hidden when maintenance is unavailable or inaccessible. */ }); return () => { active = false; }; }, []);
  return value;
}
