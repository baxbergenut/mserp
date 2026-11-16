"use client";

import { useEffect, useState } from "react";
import { latestPayrollWeek } from "../payrollWeek";
import { currentChargeWeek } from "../driver-charges/charges";

function useCalendarWeek(readWeek: () => string) {
  const [week, setWeek] = useState(readWeek);
  useEffect(() => {
    const update = () => setWeek(readWeek());
    const timer = setInterval(update, 60_000);
    window.addEventListener("focus", update);
    return () => { clearInterval(timer); window.removeEventListener("focus", update); };
  }, [readWeek]);
  return week;
}

export function useLatestPayrollWeek() {
  return useCalendarWeek(latestPayrollWeek);
}

export function useCurrentWeek() {
  return useCalendarWeek(currentChargeWeek);
}
