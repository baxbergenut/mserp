"use client";
import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { firstAllowedPage, useAdministrator, usePermissions } from "./lib/access";

export default function Home() {
  const router = useRouter();
  const permissions = usePermissions();
  const administrator = useAdministrator();
  const destination = firstAllowedPage(permissions, administrator);
  useEffect(() => { router.replace(destination); }, [router, destination]);
  return <p role="status">Opening your workspace…</p>;
}
