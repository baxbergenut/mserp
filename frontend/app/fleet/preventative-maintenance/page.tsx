"use client";
import { useEffect } from "react";
import { useRouter } from "next/navigation";
export default function PreventativeMaintenancePage(){const router=useRouter();useEffect(()=>{router.replace("/fleet/service-programs?program=00000000-0000-4000-8000-000000000020");},[router]);return null;}
