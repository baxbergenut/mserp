"use client";

import { Landmark } from "lucide-react";
import { PageHeader } from "@/app/components/PageHeader";
import { EscrowTable } from "./EscrowTable";
import { EscrowDefault } from "./EscrowDefault";

export default function EscrowPage() {
  return <div className="space-y-5">
    <PageHeader><div><h1 className="flex items-center gap-2 text-lg font-semibold text-zinc-100"><Landmark className="h-5 w-5 text-zinc-500" />Escrow</h1><p className="mt-1 text-sm text-zinc-500">Driver escrow balances and weekly payroll collections.</p></div></PageHeader>
    <EscrowTable toolbarEnd={<EscrowDefault />} />
  </div>;
}
