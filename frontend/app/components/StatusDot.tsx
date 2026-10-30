import { statusLabel } from "../lib/utils";
import { statusClass } from "../lib/statusColors";
export function StatusDot({ status }: { status: string }) {
 return <span className="inline-flex items-center gap-2"><span className={`h-1.5 w-1.5 shrink-0 rounded-full ${statusClass(status,"general","dot")}`} aria-hidden /><span className="text-[13px] text-zinc-300">{statusLabel(status)}</span></span>;
}
