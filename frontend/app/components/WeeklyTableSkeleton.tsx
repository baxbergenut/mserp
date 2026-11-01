export function SkeletonBar({ className = "" }: { className?: string }) {
  return <span aria-hidden="true" className={`inline-block rounded bg-zinc-800/80 motion-safe:animate-pulse ${className}`} />;
}

export function DispatcherPaySkeleton() {
  return <div role="status" aria-label="Loading dispatcher pay" aria-busy="true" className="space-y-4">
    <span className="sr-only">Loading dispatcher pay…</span>
    {Array.from({ length: 3 }, (_, row) => <div key={row} aria-hidden="true" className="overflow-hidden rounded-lg border border-zinc-800">
      <div className="flex flex-wrap items-center gap-3 bg-card px-3 py-2">
        <div className="flex min-h-8 min-w-0 flex-1 basis-full items-center gap-3 sm:basis-0">
          <SkeletonBar className="h-4 w-4 shrink-0" /><SkeletonBar className="h-4 w-32 max-w-full" />
          <SkeletonBar className="ml-auto hidden h-3 w-12 sm:inline-block" /><SkeletonBar className="hidden h-3 w-12 sm:inline-block" /><div className="hidden w-28 shrink-0 text-right md:block"><SkeletonBar className="h-3 w-20" /></div><div className="w-24 shrink-0 text-right"><SkeletonBar className="h-4 w-16" /></div>
        </div>
        <div className="flex w-44 shrink-0 justify-end"><SkeletonBar className="h-8 w-20" /></div>
        <div className="w-32 shrink-0 text-right text-xs text-zinc-400"><div>Finalized by</div><SkeletonBar className="h-3 w-20" /></div>
      </div>
    </div>)}
  </div>;
}

export function WeeklyTableSkeleton({ board = false, investor = false }: { board?: boolean; investor?: boolean }) {
  const headers = board
    ? ["Driver", "Truck", "Field", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun", "Total gross", "Total miles", "RPM", "Driver gross", "Rate balance"]
    : [investor ? "Investor" : "Driver", "Truck", investor ? "Driver" : "Driver type", investor ? "Dispatcher" : "Tariff", investor ? "Tariff" : "Dispatcher", "Loads", "Total payable", "Finalized by"];
  const widths = board ? [170,65,82,145,145,145,145,145,145,145,112,112,80,112,112] : [21,8,13,13,14,5,12,14];
  const label = board ? "Loading gross board" : investor ? "Loading investor pay" : "Loading driver pay";
  return <div role="status" aria-label={label} aria-busy="true" className={`max-h-[70vh] overflow-hidden border border-zinc-800 ${board ? "rounded-xl" : "rounded-lg"}`}>
    <span className="sr-only">{label}…</span>
    <table aria-hidden="true" className={`w-full table-fixed border-separate border-spacing-0 ${board ? "min-w-[1880px]" : "min-w-[1120px]"}`}>
      <colgroup>{widths.map((width, index) => <col key={index} style={{ width: board ? width : `${width}%` }} />)}</colgroup>
      <thead><tr className={`${board ? "h-[52px]" : "h-8"} bg-zinc-900 text-left text-[11px] text-zinc-500`}>{headers.map((label, col) => <th key={`${col}:${label}`} className={`border-b border-zinc-800 px-3 font-medium ${board ? "border-r" : col >= 5 ? "text-right" : ""}`}>{label}</th>)}</tr></thead>
      <tbody>
        {board && <tr className="h-8 bg-blue-500/5"><td colSpan={headers.length} className="px-3"><SkeletonBar className="h-3 w-28" /></td></tr>}
        {Array.from({ length: board ? 3 : 12 }, (_, row) => <tr key={row} className={board ? "h-[152px]" : "h-9 bg-zinc-900"}>{headers.map((label, col) => <td key={`${col}:${label}`} className={`border-b border-zinc-800/70 px-3 ${board ? "border-r" : col >= 5 ? "text-right" : ""}`}>
          {board && col >= 3 && col <= 9 ? <div className="flex flex-col items-center gap-5 py-3">{[0,1,2,3].map(line => <SkeletonBar key={line} className={`mx-auto block h-3 ${line ? "w-12" : "w-20"}`} />)}</div> : <SkeletonBar className={`h-3 max-w-full ${col === 0 ? row % 2 ? "w-28" : "w-36" : "w-14"}`} />}
        </td>)}</tr>)}
      </tbody>
    </table>
  </div>;
}
