type Board = "status" | "gross";
type Operation = "open" | "save" | "load";
const samples: string[] = [];

// Called after React commits the acknowledged rows. Allow a paint before
// measuring, and omit hidden tabs where animation frames can be suspended.
export function recordBoardPaint(board: Board, operation: Operation, start: number, sent: number, received: number) {
  if (!start || document.hidden) return;
  const commit = Math.round(performance.now() - start);
  requestAnimationFrame(() => {
    const frame = Math.round(performance.now() - start);
    requestAnimationFrame(() => {
      if (document.hidden) return;
      const ms = Math.round(performance.now() - start);
      if (ms < 0 || ms > 120000) return;
      const before = Math.round(sent - start), response = Math.round(received - start);
      const phases = before >= 0 && before <= response && response <= commit ? `/${before}/${response}/${commit}/${frame}` : "";
      samples.push(`${board}-${operation}=${ms}${phases}`);
      if (samples.length > 12) samples.shift();
    });
  });
}

// The next normal API request carries the samples. No telemetry-only request.
export function takeBoardTimingHeader(): Record<string,string> {
  while (samples.join(",").length > 512) samples.shift();
  return samples.length ? { "X-Clustie-Board-Timing": samples.splice(0).join(",") } : {};
}
