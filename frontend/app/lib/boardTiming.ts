type Board = "status" | "gross";
type Operation = "open" | "save" | "load";
const samples: string[] = [];

// Called after React commits the acknowledged rows. Allow a paint before
// measuring, and omit hidden tabs where animation frames can be suspended.
export function recordBoardPaint(board: Board, operation: Operation, start: number) {
  if (!start || document.hidden) return;
  requestAnimationFrame(() => requestAnimationFrame(() => {
    if (document.hidden) return;
    const ms = Math.round(performance.now() - start);
    if (ms < 0 || ms > 120000) return;
    samples.push(`${board}-${operation}=${ms}`);
    if (samples.length > 12) samples.shift();
  }));
}

// The next normal API request carries the samples. No telemetry-only request.
export function takeBoardTimingHeader(): Record<string,string> {
  return samples.length ? { "X-Clustie-Board-Timing": samples.splice(0).join(",") } : {};
}
