"use client";
import { useEffect, useState } from "react";
import { fetchLoads } from "./api";
import { Load } from "../types";
import { LoadsTable } from "./LoadsTable";

export default function Loads() {
  const [loads, setLoads] = useState<Load[]>([]);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    const loadLoads = async () => {
      setIsLoading(true);
      const data = await fetchLoads();
      setLoads(data);
      setIsLoading(false);
    };

    loadLoads();
  }, []);

  return (
    <LoadsTable
      loads={loads}
      isLoading={isLoading}
      sortKey="PickupTime"
      sortDir="asc"
      onSort={() => {}}
    />
  );
}
