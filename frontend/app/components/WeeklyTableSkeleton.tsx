export function SkeletonBar({ className = "" }: { className?: string }) {
  return <span aria-hidden="true" className={`inline-block rounded bg-zinc-800/80 motion-safe:animate-pulse ${className}`} />;
}

export function WeeklyTableSkeleton({ board = false }: { board?: boolean }) {
  const headers = board
    ? ["Driver", "Truck", "Field", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun", "Original gross", "Driver gross", "Rate balance", "Total miles"]
    : ["Driver", "Truck", "Driver type", "Tariff", "Dispatcher", "Loads", "Total payable"];
  const widths = board ? [170,65,82,145,145,145,145,145,145,145,112,112,112,112] : [24,10,15,17,16,6,12];
  return <div role="status" aria-label={board ? "Loading gross board" : "Loading driver pay"} aria-busy="true" className="max-h-[70vh] overflow-hidden rounded-xl border border-zinc-800">
    <span className="sr-only">{board ? "Loading gross board…" : "Loading driver pay…"}</span>
    <table aria-hidden="true" className={`w-full table-fixed border-separate border-spacing-0 ${board ? "min-w-[1780px]" : "min-w-[960px]"}`}>
      <colgroup>{widths.map((width, index) => <col key={index} style={{ width: board ? width : `${width}%` }} />)}</colgroup>
      <thead><tr className={`${board ? "h-[52px]" : "h-8"} bg-zinc-900 text-left text-[11px] text-zinc-500`}>{headers.map(label => <th key={label} className="border-b border-r border-zinc-800 px-3 font-medium">{label}</th>)}</tr></thead>
      <tbody>
        {board && <tr className="h-8 bg-blue-500/5"><td colSpan={14} className="px-3"><SkeletonBar className="h-3 w-28" /></td></tr>}
        {Array.from({ length: board ? 3 : 12 }, (_, row) => <tr key={row} className={board ? "h-[152px]" : "h-8"}>{headers.map((label, col) => <td key={label} className="border-b border-r border-zinc-800/70 px-3">
          {board && col >= 3 && col <= 9 ? <div className="flex flex-col items-center gap-5 py-3">{[0,1,2,3].map(line => <SkeletonBar key={line} className={`mx-auto block h-3 ${line ? "w-12" : "w-20"}`} />)}</div> : <SkeletonBar className={`h-3 max-w-full ${col === 0 ? row % 2 ? "w-28" : "w-36" : "w-14"}`} />}
        </td>)}</tr>)}
      </tbody>
    </table>
  </div>;
}
