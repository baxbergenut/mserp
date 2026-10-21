import { Suspense } from "react";
import { WorkOrderDetail } from "../WorkOrderDetail";

export default function WorkOrderDetailPage() {
  return <Suspense fallback={<p className="py-12 text-center text-xs text-zinc-500">Loading work order…</p>}><WorkOrderDetail /></Suspense>;
}
