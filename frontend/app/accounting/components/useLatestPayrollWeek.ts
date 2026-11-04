"use client";

import { useEffect, useState } from "react";
import { latestPayrollWeek } from "../payrollWeek";

export function useLatestPayrollWeek() {
  const [week, setWeek] = useState(() => latestPayrollWeek());
  useEffect(() => {
    const update = () => setWeek(latestPayrollWeek());
    const timer = setInterval(update, 60_000);
    window.addEventListener("focus", update);
    return () => { clearInterval(timer); window.removeEventListener("focus", update); };
  }, []);
  return week;
}
